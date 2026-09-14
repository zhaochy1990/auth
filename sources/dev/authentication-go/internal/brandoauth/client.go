package brandoauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zhaochy1990/auth-service/internal/apperror"
)

// Token is the normalized result of exchanging an authorization code: the
// brand's stable account id plus the tokens and metadata to persist.
type Token struct {
	ProviderAccountID string
	AccessToken       string
	RefreshToken      string
	Scopes            []string
	ExpiresAt         *time.Time
	// RawTokenResponse / RawIdentityResponse are the brand's responses, kept
	// verbatim so a future data-sync integration does not have to re-fetch.
	RawTokenResponse    json.RawMessage
	RawIdentityResponse json.RawMessage
}

// Metadata renders the normalized provider_metadata JSON for auth_accounts:
// fixed key names (access_token, expires_at, scope) plus the untouched raw
// responses. The refresh token is deliberately absent — it is stored in the
// credential column as the single long-lived secret.
func (t *Token) Metadata() json.RawMessage {
	m := map[string]any{"access_token": t.AccessToken}
	if t.ExpiresAt != nil {
		m["expires_at"] = t.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if len(t.Scopes) > 0 {
		m["scope"] = strings.Join(t.Scopes, " ")
	}
	if len(t.RawTokenResponse) > 0 {
		m["raw_token_response"] = t.RawTokenResponse
	}
	if len(t.RawIdentityResponse) > 0 {
		m["raw_identity_response"] = t.RawIdentityResponse
	}
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(`{"access_token":""}`)
	}
	return b
}

// Client is a brand HTTP client. It holds no framework or storage concerns.
type Client struct {
	desc       Descriptor
	cfg        Config
	mock       bool
	httpClient *http.Client
}

// NewClient builds a client for a brand. mockEnabled is the service-wide master
// switch: mocking happens only when it is true AND the application config also
// opts in. A nil httpClient gets a 10s-timeout default.
func NewClient(desc Descriptor, cfg Config, mockEnabled bool, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		desc:       desc,
		cfg:        cfg,
		mock:       cfg.Mock && mockEnabled,
		httpClient: httpClient,
	}
}

// scopes returns the configured scopes, falling back to the descriptor default.
func (c *Client) scopes() []string {
	if len(c.cfg.Scopes) > 0 {
		return c.cfg.Scopes
	}
	return c.desc.DefaultScopes
}

// AuthorizeURL builds the brand's authorization URL the user is redirected to.
func (c *Client) AuthorizeURL(redirectURI, state string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", c.cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	if scopes := c.scopes(); len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, c.desc.scopeSeparator()))
	}
	for k, v := range c.cfg.AuthorizeParams {
		q.Set(k, v)
	}
	return c.desc.baseURL(c.cfg.BaseURL) + c.desc.AuthorizePath + "?" + q.Encode()
}

// ExchangeCode swaps an authorization code for tokens and resolves the brand's
// stable account id.
func (c *Client) ExchangeCode(ctx context.Context, code, redirectURI string) (*Token, error) {
	if c.mock {
		return c.mockToken(code), nil
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	if c.desc.ClientAuthStyle != ClientAuthBasic {
		form.Set("client_id", c.cfg.ClientID)
		form.Set("client_secret", c.cfg.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.desc.baseURL(c.cfg.BaseURL)+c.desc.TokenPath, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, oauthProviderError("could not build token request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if c.desc.ClientAuthStyle == ClientAuthBasic {
		req.SetBasicAuth(c.cfg.ClientID, c.cfg.ClientSecret)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, oauthProviderError("token request failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, oauthProviderError("could not read token response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, oauthProviderError(providerErrorDetail(body, resp.StatusCode))
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, oauthProviderError("malformed token response")
	}

	tok := &Token{
		AccessToken:      stringField(raw, c.desc.Token.AccessTokenField),
		RefreshToken:     stringField(raw, c.desc.Token.RefreshTokenField),
		Scopes:           c.scopes(),
		RawTokenResponse: json.RawMessage(body),
	}
	if tok.AccessToken == "" {
		return nil, oauthProviderError("token response is missing an access token")
	}
	if exp := expiryFrom(raw, c.desc.Token); exp != nil {
		tok.ExpiresAt = exp
	}

	if id := stringField(raw, c.desc.Token.UserIDField); id != "" {
		tok.ProviderAccountID = id
		return tok, nil
	}

	userInfoBody, err := c.fetchUserInfo(ctx, tok.AccessToken)
	if err != nil {
		return nil, err
	}
	tok.RawIdentityResponse = userInfoBody
	var identity map[string]any
	if err := json.Unmarshal(userInfoBody, &identity); err != nil {
		return nil, oauthProviderError("malformed userinfo response")
	}
	id, ok := dotPath(identity, c.desc.UserInfoIDPath)
	if !ok || id == "" {
		return nil, oauthProviderError("userinfo is missing the stable user id")
	}
	tok.ProviderAccountID = id
	return tok, nil
}

// Deauthorize best-effort notifies the brand that the grant is revoked. A brand
// without a deauthorize endpoint is a no-op. An empty refresh token also skips.
func (c *Client) Deauthorize(ctx context.Context, refreshToken string) error {
	if c.desc.DeauthorizePath == "" || refreshToken == "" {
		return nil
	}
	if c.mock {
		return nil
	}

	form := url.Values{}
	form.Set("token", refreshToken)
	if c.desc.ClientAuthStyle != ClientAuthBasic {
		form.Set("client_id", c.cfg.ClientID)
		form.Set("client_secret", c.cfg.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.desc.baseURL(c.cfg.BaseURL)+c.desc.DeauthorizePath, strings.NewReader(form.Encode()))
	if err != nil {
		return oauthProviderError("could not build deauthorize request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if c.desc.ClientAuthStyle == ClientAuthBasic {
		req.SetBasicAuth(c.cfg.ClientID, c.cfg.ClientSecret)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return oauthProviderError("deauthorize request failed")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return oauthProviderError(providerErrorDetail(body, resp.StatusCode))
	}
	return nil
}

func (c *Client) fetchUserInfo(ctx context.Context, accessToken string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.desc.baseURL(c.cfg.BaseURL)+c.desc.UserInfoPath, nil)
	if err != nil {
		return nil, oauthProviderError("could not build userinfo request")
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, oauthProviderError("userinfo request failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, oauthProviderError("could not read userinfo response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, oauthProviderError(providerErrorDetail(body, resp.StatusCode))
	}
	return json.RawMessage(body), nil
}

// mockToken synthesizes a deterministic identity for the given code with no
// network call.
func (c *Client) mockToken(code string) *Token {
	expires := time.Now().UTC().Add(time.Hour)
	return &Token{
		ProviderAccountID:   "mock-" + code,
		AccessToken:         "mock-access-" + code,
		RefreshToken:        "mock-refresh-" + code,
		Scopes:              c.scopes(),
		ExpiresAt:           &expires,
		RawTokenResponse:    json.RawMessage(`{"mock":true}`),
		RawIdentityResponse: json.RawMessage(`{"mock":true}`),
	}
}

// providerErrorDetail extracts a human-readable detail from a brand error body.
func providerErrorDetail(body []byte, status int) string {
	var e struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		Message          string `json:"message"`
	}
	if err := json.Unmarshal(body, &e); err == nil {
		switch {
		case e.ErrorDescription != "":
			return e.ErrorDescription
		case e.Message != "":
			return e.Message
		case e.Error != "":
			return e.Error
		}
	}
	return "HTTP " + strconv.Itoa(status)
}

func stringField(m map[string]any, key string) string {
	if key == "" {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// expiryFrom converts the mapped expiry field into an absolute time. A missing
// or unparseable value yields nil (no declared expiry) rather than an error:
// the access token still works, only the cached expiry is unknown.
func expiryFrom(raw map[string]any, mapping TokenResponseMapping) *time.Time {
	if mapping.ExpiresInField == "" {
		return nil
	}
	v, ok := raw[mapping.ExpiresInField]
	if !ok {
		return nil
	}
	switch mapping.ExpiresInFormat {
	case ExpiryEpochMillis:
		if n, ok := numeric(v); ok {
			t := time.UnixMilli(int64(n)).UTC()
			return &t
		}
	case ExpiryRFC3339:
		if s, ok := v.(string); ok {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				t = t.UTC()
				return &t
			}
		}
	default: // ExpirySeconds
		if n, ok := numeric(v); ok {
			t := time.Now().UTC().Add(time.Duration(n) * time.Second)
			return &t
		}
	}
	return nil
}

func numeric(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	}
	return 0, false
}

// dotPath walks a dot-separated path (e.g. "data.user.id") in a decoded JSON
// object and returns the string at the leaf.
func dotPath(m map[string]any, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	var cur any = m
	for _, part := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = obj[part]
		if !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	return s, ok
}

// oauthProviderError is the stable 502 surfaced to callers when the brand is
// unreachable or returns an error.
func oauthProviderError(detail string) error {
	return apperror.New(http.StatusBadGateway, "oauth_provider_error", "OAuth provider error: "+detail)
}
