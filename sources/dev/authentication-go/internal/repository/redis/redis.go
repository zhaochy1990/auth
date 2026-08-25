// Package redis implements the repository.SmsCodeStore interface against Redis.
//
// Verification codes are stored under three keys per phone:
//
//	sms:code:{phone}     JSON {"hash","attempts"} — SHA-256 of the code plus
//	                     the failed-attempt counter; single-use, 5-minute TTL
//	sms:cooldown:{phone} presence marker — 60-second TTL between sends
//	sms:daily:{phone}    integer send counter — 24h TTL, 10 sends/day cap
//
// The store fails closed: any Redis failure (connectivity, script errors)
// surfaces as a 503 service_unavailable apperror so the SMS endpoints never
// fall back to a second store. Consume and attempt accounting are atomic Lua
// scripts, so concurrent verifies cannot both consume the same code.
package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/repository"
)

const (
	// CooldownTTL is the minimum gap between sends to the same phone (60 s).
	CooldownTTL = 60 * time.Second
	// DailyWindow is the per-phone daily counter window (24 h).
	DailyWindow = 24 * time.Hour
	// DailyMax is the maximum codes per phone per day.
	DailyMax = 10
)

func key(prefix, phone string) string { return "sms:" + prefix + ":" + phone }

// Store is the Redis-backed SmsCodeStore.
type Store struct {
	rdb redis.UniversalClient
}

// New builds a Store backed by the Redis instance at addr. The connection is
// lazy: constructing the store never fails, and every operation reports a
// fail-closed 503 while Redis is unreachable.
func New(addr, password string, db int) *Store {
	return &Store{rdb: redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})}
}

// Ping verifies Redis connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return redisErr(s.rdb.Ping(ctx).Err())
}

// FlushDB removes all keys in the selected database. Intended for the test
// harness (which mirrors the MySQL ClearAllTables convention); never called by
// the service at runtime.
func (s *Store) FlushDB(ctx context.Context) error {
	return redisErr(s.rdb.FlushDB(ctx).Err())
}

func (s *Store) ReserveCooldown(ctx context.Context, phone string) (bool, error) {
	ok, err := s.rdb.SetNX(ctx, key("cooldown", phone), "1", CooldownTTL).Result()
	if err != nil {
		return false, redisErr(err)
	}
	return ok, nil
}

var reserveDailyScript = redis.NewScript(`
local count = redis.call('GET', KEYS[1])
if count and tonumber(count) >= tonumber(ARGV[1]) then
	return -1
end
count = redis.call('INCR', KEYS[1])
if count == 1 then
	redis.call('EXPIRE', KEYS[1], ARGV[2])
end
return count
`)

func (s *Store) ReserveDailyCount(ctx context.Context, phone string) error {
	n, err := reserveDailyScript.Run(ctx, s.rdb, []string{key("daily", phone)}, DailyMax, int64(DailyWindow.Seconds())).Int64()
	if err != nil {
		return redisErr(err)
	}
	if n < 0 {
		return apperror.SmsDailyLimit()
	}
	return nil
}

type codeRecord struct {
	Hash     string `json:"hash"`
	Attempts int    `json:"attempts"`
}

func (s *Store) StoreCode(ctx context.Context, phone, code string, ttl time.Duration) error {
	rec := codeRecord{Hash: hashCode(code)}
	b, err := json.Marshal(rec)
	if err != nil {
		return apperror.Internal()
	}
	return redisErr(s.rdb.Set(ctx, key("code", phone), b, ttl).Err())
}

var verifyScript = redis.NewScript(`
local v = redis.call('GET', KEYS[1])
if not v then
	return -1 -- no active code (never sent / expired / consumed)
end
local t = cjson.decode(v)
if t.hash == ARGV[1] then
	redis.call('DEL', KEYS[1])
	return 0 -- ok, consumed
end
t.attempts = t.attempts + 1
if t.attempts >= tonumber(ARGV[2]) then
	redis.call('DEL', KEYS[1])
	return 2 -- attempts exceeded, code invalidated
end
redis.call('SET', KEYS[1], cjson.encode(t), 'KEEPTTL')
return 1 -- invalid, attempts remain
`)

func (s *Store) VerifyCode(ctx context.Context, phone, code string, maxAttempts int) (repository.SmsVerifyResult, error) {
	n, err := verifyScript.Run(ctx, s.rdb, []string{key("code", phone)}, hashCode(code), maxAttempts).Int64()
	if err != nil {
		return 0, redisErr(err)
	}
	switch n {
	case 0:
		return repository.SmsVerifyOK, nil
	case 1:
		return repository.SmsVerifyInvalid, nil
	case 2:
		return repository.SmsVerifyAttemptsExceeded, nil
	default:
		return repository.SmsVerifyExpired, nil
	}
}

var releaseSendScript = redis.NewScript(`
redis.call('DEL', KEYS[1])
if redis.call('EXISTS', KEYS[2]) == 1 then
	return redis.call('DECR', KEYS[2])
end
return 0
`)

func (s *Store) ReleaseSend(ctx context.Context, phone string) error {
	return redisErr(releaseSendScript.Run(ctx, s.rdb, []string{key("cooldown", phone), key("daily", phone)}).Err())
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// redisErr maps a Redis failure to a fail-closed 503. Already-typed apperrors
// (e.g. daily limit) pass through.
func redisErr(err error) error {
	if err == nil {
		return nil
	}
	var ae *apperror.Error
	if errors.As(err, &ae) {
		return ae
	}
	return apperror.ServiceUnavailable()
}

var _ repository.SmsCodeStore = (*Store)(nil)
