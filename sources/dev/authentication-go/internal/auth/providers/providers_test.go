package providers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/zhaochy1990/auth-service/internal/apperror"
)

// errType extracts the stable machine-readable error type from an *apperror.Error.
func errType(err error) string {
	var ae *apperror.Error
	if errors.As(err, &ae) {
		return ae.Type
	}
	return ""
}

func TestCreateRejectsWeChatAlways(t *testing.T) {
	// WeChat is no longer available through the generic provider login path;
	// it is served only by the OAuth2 token_exchange grant. Rejection is
	// unconditional — allowTest must not revive it.
	for _, allowTest := range []bool{false, true} {
		p, err := Create("wechat", json.RawMessage(`{"appid":"x","secret":"y"}`), allowTest)
		if p != nil {
			t.Fatalf("Create(wechat, allowTest=%v) returned provider %T, want nil", allowTest, p)
		}
		if got := errType(err); got != "provider_not_supported" {
			t.Fatalf("Create(wechat, allowTest=%v) error type = %q, want provider_not_supported (err=%v)", allowTest, got, err)
		}
	}
}

func TestCreateUnknownProviderRejected(t *testing.T) {
	_, err := Create("unknown", json.RawMessage(`{}`), true)
	if got := errType(err); got != "provider_not_supported" {
		t.Fatalf("Create(unknown) error type = %q, want provider_not_supported (err=%v)", got, err)
	}
}

func TestCreateTestProviderGated(t *testing.T) {
	// Disabled: reject like any unsupported provider.
	p, err := Create("test", json.RawMessage(`{}`), false)
	if p != nil || errType(err) != "provider_not_supported" {
		t.Fatalf("Create(test, allowTest=false) = (%v, %v), want (nil, provider_not_supported)", p, err)
	}

	// Enabled: it works and authenticates the credential.
	p, err = Create("test", json.RawMessage(`{}`), true)
	if err != nil {
		t.Fatalf("Create(test, allowTest=true) error = %v", err)
	}
	if p.ID() != "test" {
		t.Fatalf("Create(test) provider ID = %q, want test", p.ID())
	}
	info, err := p.Authenticate(context.Background(), json.RawMessage(`{"account_id":"acct-1","email":"tp@example.com","name":"TP"}`))
	if err != nil {
		t.Fatalf("test provider Authenticate error = %v", err)
	}
	if info.ProviderAccountID != "acct-1" {
		t.Fatalf("test provider account id = %q, want acct-1", info.ProviderAccountID)
	}
}
