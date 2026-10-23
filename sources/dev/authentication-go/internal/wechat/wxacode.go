// WxACodeClient generates WeChat mini-program codes (getwxacodeunlimit,
// interface B: the only code endpoint without a total quota and never
// expiring). It owns the stable_token lifecycle: stable_token is isolated
// from the legacy token API (repeated calls return the same token instead of
// invalidating it), so the client caches it in-process (singleflight, a
// refresh margin before expiry; auth is single-instance so no Redis) keyed
// by appid — the credentials come from the per-app provider config, not from
// static configuration.
package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/zhaochy1990/auth-service/internal/apperror"
)

const (
	defaultStableTokenURL = "https://api.weixin.qq.com/cgi-bin/stable_token"
	defaultWxACodeURL     = "https://api.weixin.qq.com/wxa/getwxacodeunlimit"
	// tokenRefreshMargin is how long before expiry a cached token is
	// refetched, so a request never rides a token that expires mid-flight.
	tokenRefreshMargin = 5 * time.Minute
)

// WxACodeParams are the getwxacodeunlimit request parameters.
type WxACodeParams struct {
	// Page is the page the code opens (must exist in the published
	// mini-program for WeChat to accept it).
	Page string
	// Scene is the parameter string the code carries (max 32 visible chars),
	// readable by the mini-program via wx.getEnterOptions.
	Scene string
	// EnvVersion is the mini-program version to open: "release" (default),
	// "trial" or "develop". Empty means release.
	EnvVersion string
	// Width is the code width in px (280-1280). Zero means the WeChat
	// default (430).
	Width int
}

// checkPath reports the getwxacodeunlimit check_path value: page existence is
// only checked against the published mini-program, so trial/develop codes to
// not-yet-published pages must skip the check or WeChat rejects them (41030).
func (p WxACodeParams) checkPath() bool {
	return p.EnvVersion == "" || p.EnvVersion == "release"
}

// body is the getwxacodeunlimit JSON request body.
// wxacodeRequestBody is the getwxacodeunlimit JSON request body.
type wxacodeRequestBody struct {
	Page       string `json:"page"`
	Scene      string `json:"scene"`
	CheckPath  bool   `json:"check_path"`
	EnvVersion string `json:"env_version,omitempty"`
	Width      int    `json:"width,omitempty"`
}

// body renders the params as the getwxacodeunlimit JSON request body.
func (p WxACodeParams) body() wxacodeRequestBody {
	return wxacodeRequestBody{
		Page:       p.Page,
		Scene:      p.Scene,
		CheckPath:  p.checkPath(),
		EnvVersion: p.EnvVersion,
		Width:      p.Width,
	}
}

// tokenEntry is a cached stable_token with its expiry.
type tokenEntry struct {
	token     string
	expiresAt time.Time
}

// WxACodeClient calls the WeChat mini-program code API and manages the
// stable_token credentials it needs.
type WxACodeClient struct {
	httpCli  *http.Client
	tokenURL string
	codeURL  string

	g   singleflight.Group
	mu  sync.Mutex
	tok map[string]tokenEntry // keyed by appid
}

// NewWxACodeClient creates the client. stableTokenURL / wxacodeURL override
// the WeChat endpoints (used by tests to point at fake servers); pass "" for
// production.
func NewWxACodeClient(stableTokenURL, wxacodeURL string) *WxACodeClient {
	if stableTokenURL == "" {
		stableTokenURL = defaultStableTokenURL
	}
	if wxacodeURL == "" {
		wxacodeURL = defaultWxACodeURL
	}
	return &WxACodeClient{
		httpCli:  &http.Client{Timeout: 15 * time.Second},
		tokenURL: stableTokenURL,
		codeURL:  wxacodeURL,
		tok:      map[string]tokenEntry{},
	}
}

// stableToken returns a valid access_token for appid, served from the cache
// while it keeps the refresh margin. With forceRefresh the cache is bypassed
// and WeChat is asked to mint a fresh token (stable_token's force_refresh).
func (c *WxACodeClient) stableToken(ctx context.Context, appID, secret string, forceRefresh bool) (string, error) {
	if appID == "" || secret == "" {
		return "", apperror.WeChatNotConfigured()
	}
	if !forceRefresh {
		c.mu.Lock()
		e, ok := c.tok[appID]
		c.mu.Unlock()
		if ok && time.Until(e.expiresAt) > tokenRefreshMargin {
			return e.token, nil
		}
	}
	// Coll concurrent misses on the same appid into one WeChat call so a
	// burst of codes never mints a stampede of tokens.
	v, err, _ := c.g.Do(appID, func() (any, error) {
		return c.fetchStableToken(ctx, appID, secret, forceRefresh)
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// fetchStableToken POSTs the stable_token request (grant_type
// client_credential, credentials in the body — unlike the legacy GET token
// API, so the two cannot invalidate each other). errcode 40001 retries once
// with force_refresh: the cached token may have been invalidated server-side
// (e.g. secret rotation) and only a forced mint recovers.
func (c *WxACodeClient) fetchStableToken(ctx context.Context, appID, secret string, forceRefresh bool) (string, error) {
	send := func(force bool) (string, time.Time, error) {
		body, _ := json.Marshal(map[string]any{
			"grant_type":    "client_credential",
			"appid":         appID,
			"secret":        secret,
			"force_refresh": force,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, bytes.NewReader(body))
		if err != nil {
			return "", time.Time{}, wxacodeAPIError("could not build token request")
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpCli.Do(req)
		if err != nil {
			return "", time.Time{}, wxacodeAPIError("token request failed")
		}
		defer resp.Body.Close()
		var raw struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int64  `json:"expires_in"`
			ErrCode     int    `json:"errcode"`
			ErrMsg      string `json:"errmsg"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			return "", time.Time{}, wxacodeAPIError("malformed token response")
		}
		if raw.ErrCode != 0 {
			return "", time.Time{}, mapWxACodeError(raw.ErrCode, raw.ErrMsg)
		}
		if raw.AccessToken == "" {
			return "", time.Time{}, wxacodeAPIError("no access_token in response")
		}
		expires := time.Now().Add(time.Duration(raw.ExpiresIn) * time.Second)
		if raw.ExpiresIn <= 0 {
			expires = time.Now().Add(7200 * time.Second)
		}
		return raw.AccessToken, expires, nil
	}

	token, expires, err := send(forceRefresh)
	// 40001 only recovers via a forced mint, and only one retry: a second
	// 40001 means the credentials themselves are rejected.
	if err != nil && !forceRefresh {
		if ae, ok := apperror.As(err); ok && ae.Type == "wechat_invalid_token" {
			token, expires, err = send(true)
		}
	}
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.tok[appID] = tokenEntry{token: token, expiresAt: expires}
	c.mu.Unlock()
	return token, nil
}

// GetUnlimitedQRCode renders the unlimited mini-program code for params and
// returns the PNG bytes. The endpoint answers with mixed content types: an
// image/* body is the code itself, anything else is a JSON errcode. errcode
// 40001 (token invalidated mid-life) force-refreshes the token and retries
// the code call exactly once.
func (c *WxACodeClient) GetUnlimitedQRCode(ctx context.Context, appID, secret string, p WxACodeParams) ([]byte, error) {
	png, err := c.getWXACode(ctx, appID, secret, p, false)
	if err != nil {
		if ae, ok := apperror.As(err); ok && ae.Type == "wechat_invalid_token" {
			png, err = c.getWXACode(ctx, appID, secret, p, true)
		}
	}
	return png, err
}

// getWXACode performs one getwxacodeunlimit call with the (possibly
// force-refreshed) token.
func (c *WxACodeClient) getWXACode(ctx context.Context, appID, secret string, p WxACodeParams, refresh bool) ([]byte, error) {
	token, err := c.stableToken(ctx, appID, secret, refresh)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(p.body())
	reqURL := c.codeURL + "?access_token=" + url.QueryEscape(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, wxacodeAPIError("could not build code request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, wxacodeAPIError("code request failed")
	}
	defer resp.Body.Close()

	if strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		png, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, wxacodeAPIError("could not read code image")
		}
		return png, nil
	}
	var raw struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, wxacodeAPIError("malformed code response")
	}
	if raw.ErrCode != 0 {
		return nil, mapWxACodeError(raw.ErrCode, raw.ErrMsg)
	}
	return nil, wxacodeAPIError("expected image response")
}

// wxacodeAPIError returns a 502: the internal caller's request was fine, the
// WeChat upstream (or our call to it) failed. The 4xx codes below are the
// only errors attributable to the caller.
func wxacodeAPIError(detail string) error {
	return apperror.New(502, "wechat_api_error", "WeChat API error: "+detail)
}

// mapWxACodeError translates a stable_token / getwxacodeunlimit errcode to an
// apperror. 40001 keeps a machine-readable type so the retry logic can
// recognize it; 41030/40129/45009 are mapped per the internal endpoint spec.
func mapWxACodeError(code int, msg string) error {
	switch code {
	case 40001:
		return apperror.New(502, "wechat_invalid_token", "WeChat access_token is invalid")
	case 41030:
		return apperror.New(400, "wechat_invalid_page", "WeChat rejected the page: it does not exist in the published mini-program")
	case 40129:
		return apperror.New(400, "wechat_invalid_scene", "WeChat rejected the scene: over 32 chars or unsupported characters")
	case 45009:
		return apperror.New(429, "wechat_rate_limited", "WeChat API rate limit exceeded")
	default:
		return apperror.New(502, "wechat_api_error", fmt.Sprintf("WeChat API error %d: %s", code, msg))
	}
}
