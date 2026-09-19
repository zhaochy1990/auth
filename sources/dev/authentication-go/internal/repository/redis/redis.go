// Package redis implements the repository.SmsCodeStore interface against Redis
// and the repository.OAuthStateStore interface for third-party OAuth2 link
// state.
//
// Verification codes are stored under three keys per phone:
//
//	sms:code:{phone}     JSON {"hash","attempts"} — SHA-256 of the code plus
//	                     the failed-attempt counter; single-use, 5-minute TTL
//	sms:cooldown:{phone} integer send counter — 60-second fixed window; at
//	                     most SendWindowMax sends per window (default 5)
//	sms:daily:{phone}    integer send counter — 24h TTL; at most DailyMax
//	                     sends per day (default 20)
//
// The window and daily caps come from config (sms_send_window_max /
// sms_daily_max); New falls back to the package defaults for non-positive
// values. The store fails closed: any Redis failure (connectivity, script
// errors) surfaces as a 503 service_unavailable apperror so the SMS endpoints
// never fall back to a second store. Consume and attempt accounting are atomic
// Lua scripts, so concurrent verifies cannot both consume the same code.
//
// Third-party OAuth2 link state (oauth:state:{handle}) is single-use, TTL-bound
// and consumed atomically, and also fails closed; see oauth_state.go.
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
	// SendWindow is the per-phone send window (a 60 s fixed window keyed on
	// the first send inside it).
	SendWindow = 60 * time.Second
	// DefaultSendWindowMax is the per-phone send cap within one window when
	// config does not provide one (sms_send_window_max).
	DefaultSendWindowMax = 5
	// DailyWindow is the per-phone daily counter window (24 h).
	DailyWindow = 24 * time.Hour
	// DefaultDailyMax is the per-phone daily send cap when config does not
	// provide one (sms_daily_max).
	DefaultDailyMax = 20
)

func key(prefix, phone string) string { return "sms:" + prefix + ":" + phone }

// Store is the Redis-backed SmsCodeStore.
type Store struct {
	rdb           redis.UniversalClient
	sendWindowMax int
	dailyMax      int
}

// New builds a Store backed by the Redis instance at addr. sendWindowMax caps
// sends per phone within one SendWindow and dailyMax per phone per day;
// non-positive values fall back to the package defaults. The connection is
// lazy: constructing the store never fails, and every operation reports a
// fail-closed 503 while Redis is unreachable.
func New(addr, password string, db, sendWindowMax, dailyMax int) *Store {
	if sendWindowMax <= 0 {
		sendWindowMax = DefaultSendWindowMax
	}
	if dailyMax <= 0 {
		dailyMax = DefaultDailyMax
	}
	return &Store{
		rdb:           redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db}),
		sendWindowMax: sendWindowMax,
		dailyMax:      dailyMax,
	}
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

var reserveSendScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
	redis.call('EXPIRE', KEYS[1], ARGV[1])
end
if count > tonumber(ARGV[2]) then
	return -1
end
return count
`)

// ReserveCooldown claims a slot in the phone's send window — a 60-second
// fixed window keyed on the first send inside it. It returns false once
// sendWindowMax sends have already been reserved within the current window.
func (s *Store) ReserveCooldown(ctx context.Context, phone string) (bool, error) {
	n, err := reserveSendScript.Run(ctx, s.rdb, []string{key("cooldown", phone)}, int64(SendWindow.Seconds()), s.sendWindowMax).Int64()
	if err != nil {
		return false, redisErr(err)
	}
	return n >= 0, nil
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
	n, err := reserveDailyScript.Run(ctx, s.rdb, []string{key("daily", phone)}, s.dailyMax, int64(DailyWindow.Seconds())).Int64()
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
if redis.call('EXISTS', KEYS[1]) == 1 then
	local n = redis.call('DECR', KEYS[1])
	if n <= 0 then
		redis.call('DEL', KEYS[1])
	end
end
if redis.call('EXISTS', KEYS[2]) == 1 then
	return redis.call('DECR', KEYS[2])
end
return 0
`)

// ReleaseSend gives back a reserved window slot and daily increment after a
// failed send, so an upstream error does not consume the phone's quota.
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
