// Package providers implements pluggable external auth providers. The only
// built-in provider is "test", which is gated (rejected unless allowTest is
// enabled). WeChat is NOT offered here — it is served exclusively by the OAuth2
// token_exchange grant via the canonical internal/wechat client.
package providers

import (
	"context"
	"encoding/json"

	"github.com/zhaochy1990/auth-service/internal/apperror"
)

// UserInfo is the normalized identity returned by a provider.
type UserInfo struct {
	ProviderAccountID string
	Email             *string
	Name              *string
	AvatarURL         *string
	Metadata          json.RawMessage
}

// Provider authenticates a credential and returns the resolved identity.
type Provider interface {
	ID() string
	Authenticate(ctx context.Context, credential json.RawMessage) (*UserInfo, error)
}

// Create builds a provider by id. allowTest enables the "test" provider, which
// is otherwise rejected.
func Create(providerID string, config json.RawMessage, allowTest bool) (Provider, error) {
	switch providerID {
	case "test":
		if allowTest {
			return &testProvider{}, nil
		}
		return nil, apperror.ProviderNotSupported(providerID)
	default:
		return nil, apperror.ProviderNotSupported(providerID)
	}
}

// --- Test provider (gated) ---

type testProvider struct{}

type testCredential struct {
	AccountID string  `json:"account_id"`
	Email     *string `json:"email"`
	Name      *string `json:"name"`
}

func (p *testProvider) ID() string { return "test" }

func (p *testProvider) Authenticate(_ context.Context, credential json.RawMessage) (*UserInfo, error) {
	var cred testCredential
	if err := json.Unmarshal(credential, &cred); err != nil || cred.AccountID == "" {
		return nil, apperror.BadRequest("Invalid test credential")
	}
	meta, _ := json.Marshal(map[string]any{"provider": "test"})
	return &UserInfo{
		ProviderAccountID: cred.AccountID,
		Email:             cred.Email,
		Name:              cred.Name,
		Metadata:          meta,
	}, nil
}
