// Package providers implements pluggable external auth providers. Current
// providers: wechat and test (test is gated).
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

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
	case "wechat":
		return newWeChat(config)
	case "test":
		if allowTest {
			return &testProvider{}, nil
		}
		return nil, apperror.ProviderNotSupported(providerID)
	default:
		return nil, apperror.ProviderNotSupported(providerID)
	}
}

// providerError is the 502 used when an external provider call fails.
func providerError() *apperror.Error {
	return apperror.New(http.StatusBadGateway, "provider_error", "External provider error")
}

// --- WeChat ---

type weChatProvider struct {
	appID  string
	secret string
	client *http.Client
}

type weChatConfig struct {
	AppID  string `json:"appid"`
	Secret string `json:"secret"`
}

type weChatCredential struct {
	Code string `json:"code"`
}

type jsCode2SessionResponse struct {
	OpenID     *string `json:"openid"`
	SessionKey *string `json:"session_key"`
	UnionID    *string `json:"unionid"`
	ErrCode    *int64  `json:"errcode"`
	ErrMsg     *string `json:"errmsg"`
}

func newWeChat(config json.RawMessage) (Provider, error) {
	var cfg weChatConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return nil, apperror.BadRequest(fmt.Sprintf("Invalid WeChat config: %v", err))
	}
	return &weChatProvider{
		appID:  cfg.AppID,
		secret: cfg.Secret,
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (p *weChatProvider) ID() string { return "wechat" }

func (p *weChatProvider) Authenticate(ctx context.Context, credential json.RawMessage) (*UserInfo, error) {
	var cred weChatCredential
	if err := json.Unmarshal(credential, &cred); err != nil {
		return nil, apperror.BadRequest(`Invalid WeChat credential: expected {"code": "..."}`)
	}

	q := url.Values{}
	q.Set("appid", p.appID)
	q.Set("secret", p.secret)
	q.Set("js_code", cred.Code)
	q.Set("grant_type", "authorization_code")
	endpoint := "https://api.weixin.qq.com/sns/jscode2session?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, apperror.Internal()
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, providerError()
	}
	defer resp.Body.Close()

	var body jsCode2SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, providerError()
	}

	if body.ErrCode != nil && *body.ErrCode != 0 {
		msg := ""
		if body.ErrMsg != nil {
			msg = *body.ErrMsg
		}
		return nil, apperror.BadRequest(fmt.Sprintf("WeChat API error %d: %s", *body.ErrCode, msg))
	}
	if body.OpenID == nil {
		return nil, apperror.BadRequest("WeChat API did not return openid")
	}

	// Do NOT persist session_key — it is a sensitive server-side secret.
	meta, _ := json.Marshal(map[string]any{
		"openid":  *body.OpenID,
		"unionid": body.UnionID,
	})
	return &UserInfo{
		ProviderAccountID: *body.OpenID,
		Metadata:          meta,
	}, nil
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
