package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/zhaochy1990/auth-service/internal/apperror"
)

const defaultURLLinkURL = "https://api.weixin.qq.com/wxa/generate_urllink"

// urllinkValidityDays: every generated link expires after this many days
// instead of riding generate_urllink's default expiry semantics. The share
// landing page mints a link per visit and redirects immediately, so one day
// is ample, and an explicit expiry avoids the separate quota bucket of
// permanent links. expire_type=1 + expire_interval is the documented
// interval form (1-30 days).
const urllinkValidityDays = 1

// URLLinkParams are the generate_urllink request parameters.
type URLLinkParams struct {
	// Path is the published mini-program page the link opens
	// (generate_urllink only accepts pages that exist in the released
	// mini-program).
	Path string
	// Query is the parameter string the link carries (k=v&k2=v2 form; WeChat
	// allows 1024 chars, the handlers cap it tighter).
	Query string
	// EnvVersion is the mini-program version to open: "release" (default),
	// "trial" or "develop". Only effective when opened outside WeChat
	// browsers.
	EnvVersion string
}

// urllinkRequestBody is the generate_urllink JSON request body.
type urllinkRequestBody struct {
	Path           string `json:"path"`
	Query          string `json:"query,omitempty"`
	ExpireType     int    `json:"expire_type"`
	ExpireInterval int    `json:"expire_interval"`
	EnvVersion     string `json:"env_version,omitempty"`
}

// URLLinkClient calls the WeChat mini-program URL Link API (generate_urllink)
// and owns its stable_token lifecycle. It deliberately mirrors WxACodeClient:
// an in-process singleflight token cache keyed by appid with a refresh margin
// before expiry (auth is single-instance so no Redis), credentials from the
// per-app provider config, endpoint overrides for tests. The two clients hold
// independent caches by design — stable_token is idempotent (repeated calls
// return the same token instead of invalidating it), so at worst the service
// fetches one extra token per expiry window and the caches can never
// invalidate each other.
type URLLinkClient struct {
	httpCli *http.Client
	tokens  *urlLinkTokenCache
	linkURL string
}

// NewURLLinkClient creates the client. stableTokenURL / urllinkURL override
// the WeChat endpoints (used by tests to point at fake servers); pass "" for
// production.
func NewURLLinkClient(stableTokenURL, urllinkURL string) *URLLinkClient {
	if urllinkURL == "" {
		urllinkURL = defaultURLLinkURL
	}
	return &URLLinkClient{
		httpCli: &http.Client{Timeout: 15 * time.Second},
		tokens:  newURLLinkTokenCache(stableTokenURL),
		linkURL: urllinkURL,
	}
}

// Generate returns a URL Link that opens the mini-program page for params.
// errcode 40001 (token invalidated mid-life) force-refreshes the token and
// retries the link call exactly once.
func (c *URLLinkClient) Generate(ctx context.Context, appID, secret string, p URLLinkParams) (string, error) {
	link, err := c.generate(ctx, appID, secret, p, false)
	if err != nil {
		if ae, ok := apperror.As(err); ok && ae.Type == "wechat_invalid_token" {
			link, err = c.generate(ctx, appID, secret, p, true)
		}
	}
	return link, err
}

// generate performs one generate_urllink call with the (possibly
// force-refreshed) token.
func (c *URLLinkClient) generate(ctx context.Context, appID, secret string, p URLLinkParams, refresh bool) (string, error) {
	token, err := c.tokens.stableToken(ctx, appID, secret, refresh)
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(urllinkRequestBody{
		Path:           p.Path,
		Query:          p.Query,
		ExpireType:     1,
		ExpireInterval: urllinkValidityDays,
		EnvVersion:     p.EnvVersion,
	})
	reqURL := c.linkURL + "?access_token=" + url.QueryEscape(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return "", urllinkAPIError("could not build link request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return "", urllinkAPIError("link request failed")
	}
	defer resp.Body.Close()

	// generate_urllink always answers JSON, success and error alike.
	var raw struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		URLLink string `json:"url_link"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return "", urllinkAPIError("malformed link response")
	}
	if raw.ErrCode != 0 {
		return "", mapURLLinkError(raw.ErrCode, raw.ErrMsg)
	}
	if raw.URLLink == "" {
		return "", urllinkAPIError("no url_link in response")
	}
	return raw.URLLink, nil
}

// urlLinkTokenCache is the client's stable_token cache — the same lifecycle
// as the wxacode client's (singleflight per appid, refresh margin before
// expiry), kept separate so the two clients stay independently deployable.
type urlLinkTokenCache struct {
	httpCli  *http.Client
	tokenURL string

	g   singleflight.Group
	mu  sync.Mutex
	tok map[string]tokenEntry // keyed by appid
}

func newURLLinkTokenCache(tokenURL string) *urlLinkTokenCache {
	if tokenURL == "" {
		tokenURL = defaultStableTokenURL
	}
	return &urlLinkTokenCache{
		httpCli:  &http.Client{Timeout: 15 * time.Second},
		tokenURL: tokenURL,
		tok:      map[string]tokenEntry{},
	}
}

// stableToken returns a valid access_token for appid, served from the cache
// while it keeps the refresh margin. With forceRefresh the cache is bypassed
// and WeChat is asked to mint a fresh token (stable_token's force_refresh).
func (c *urlLinkTokenCache) stableToken(ctx context.Context, appID, secret string, forceRefresh bool) (string, error) {
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
	// burst of links never mints a stampede of tokens.
	v, err, _ := c.g.Do(appID, func() (any, error) {
		return c.fetchStableToken(ctx, appID, secret, forceRefresh)
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// fetchStableToken POSTs the stable_token request (grant_type
// client_credential, credentials in the body). errcode 40001 retries once
// with force_refresh: the cached token may have been invalidated server-side
// (e.g. secret rotation) and only a forced mint recovers.
func (c *urlLinkTokenCache) fetchStableToken(ctx context.Context, appID, secret string, forceRefresh bool) (string, error) {
	// Re-check the cache inside the flight: a caller that missed the cache
	// just before another flight finished would otherwise mint a second
	// token — singleflight only collapses Do calls that overlap in time.
	if !forceRefresh {
		c.mu.Lock()
		e, ok := c.tok[appID]
		c.mu.Unlock()
		if ok && time.Until(e.expiresAt) > tokenRefreshMargin {
			return e.token, nil
		}
	}
	send := func(force bool) (string, time.Time, error) {
		body, _ := json.Marshal(map[string]any{
			"grant_type":    "client_credential",
			"appid":         appID,
			"secret":        secret,
			"force_refresh": force,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, bytes.NewReader(body))
		if err != nil {
			return "", time.Time{}, urllinkAPIError("could not build token request")
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpCli.Do(req)
		if err != nil {
			return "", time.Time{}, urllinkAPIError("token request failed")
		}
		defer resp.Body.Close()
		var raw struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int64  `json:"expires_in"`
			ErrCode     int    `json:"errcode"`
			ErrMsg      string `json:"errmsg"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			return "", time.Time{}, urllinkAPIError("malformed token response")
		}
		if raw.ErrCode != 0 {
			return "", time.Time{}, mapURLLinkError(raw.ErrCode, raw.ErrMsg)
		}
		if raw.AccessToken == "" {
			return "", time.Time{}, urllinkAPIError("no access_token in response")
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

// urllinkAPIError returns a 502: the caller's request was fine, the WeChat
// upstream (or our call to it) failed. The 4xx codes in mapURLLinkError are
// the only errors attributable to the caller.
func urllinkAPIError(detail string) error {
	return apperror.New(502, "wechat_api_error", "WeChat API error: "+detail)
}

// mapURLLinkError translates a stable_token / generate_urllink errcode to an
// apperror. 40001 keeps a machine-readable type so the retry logic can
// recognize it; 40165/40212 are the caller-attributable request errors;
// 44990/44993 are WeChat's QPS and daily quota limits.
func mapURLLinkError(code int, msg string) error {
	switch code {
	case 40001:
		return apperror.New(502, "wechat_invalid_token", "WeChat access_token is invalid")
	case 40165:
		return apperror.New(400, "wechat_invalid_page", "WeChat rejected the page: it does not exist in the published mini-program")
	case 40212:
		return apperror.New(400, "wechat_invalid_query", "WeChat rejected the query: it must be k=v pairs joined by &")
	case 44990:
		return apperror.New(429, "wechat_rate_limited", "WeChat API rate limit exceeded")
	case 44993:
		return apperror.New(429, "wechat_rate_limited", "WeChat daily URL Link quota exceeded")
	default:
		return apperror.New(502, "wechat_api_error", fmt.Sprintf("WeChat API error %d: %s", code, msg))
	}
}
