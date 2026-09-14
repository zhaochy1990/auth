package redis

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/repository"
)

// oauthStatePrefix namespaces third-party OAuth2 link state handles. Keys are
// the opaque handles themselves (128 hex chars), so the value reveals nothing.
const oauthStatePrefix = "oauth:state:"

func oauthStateKey(handle string) string { return oauthStatePrefix + handle }

// StoreState records a link state payload under handle with the given TTL.
func (s *Store) StoreState(ctx context.Context, handle string, st repository.OAuthState, ttl time.Duration) error {
	b, err := json.Marshal(st)
	if err != nil {
		return apperror.Internal()
	}
	return redisErr(s.rdb.Set(ctx, oauthStateKey(handle), b, ttl).Err())
}

// consumeStateScript atomically reads and deletes the handle, so two concurrent
// callbacks can never both consume the same state.
var consumeStateScript = redis.NewScript(`
local v = redis.call('GET', KEYS[1])
if v then
	redis.call('DEL', KEYS[1])
end
return v
`)

// ConsumeState atomically returns the payload and deletes it. A missing or
// expired handle yields (nil, nil); a storage failure is fail-closed (503).
func (s *Store) ConsumeState(ctx context.Context, handle string) (*repository.OAuthState, error) {
	v, err := consumeStateScript.Run(ctx, s.rdb, []string{oauthStateKey(handle)}).Text()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, redisErr(err)
	}
	var st repository.OAuthState
	if err := json.Unmarshal([]byte(v), &st); err != nil {
		return nil, apperror.Internal()
	}
	return &st, nil
}

var _ repository.OAuthStateStore = (*Store)(nil)
