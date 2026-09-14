package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zhaochy1990/auth-service/internal/domain"
)

// fakeBrandServer simulates a watch brand's OAuth2 endpoints, recording the
// per-application credentials it received (so tests prove the config, not a
// hardcoded value, was used).
type fakeBrandServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	auth []string // client_id seen at the token/deauthorize endpoints
	secs []string
	code []string
	deau []string
	// failDeauthorize makes the deauthorize endpoint return 502.
	failDeauthorize bool
}

func (f *fakeBrandServer) URL() string { return f.srv.URL }
func (f *fakeBrandServer) Close()      { f.srv.Close() }

func (f *fakeBrandServer) lastCredentials() (clientID, secret string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.auth) == 0 {
		return "", ""
	}
	return f.auth[len(f.auth)-1], f.secs[len(f.secs)-1]
}

func (f *fakeBrandServer) deauthorizedTokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.deau))
	copy(out, f.deau)
	return out
}

func newFakeBrandServer(t *testing.T) *fakeBrandServer {
	t.Helper()
	f := &fakeBrandServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		code := r.PostForm.Get("code")
		f.mu.Lock()
		f.auth = append(f.auth, r.PostForm.Get("client_id"))
		f.secs = append(f.secs, r.PostForm.Get("client_secret"))
		f.code = append(f.code, code)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"at-` + code + `","refreshToken":"rt-` + code + `","expiresIn":3600}`))
	})
	mux.HandleFunc("/oauth/userinfo", func(w http.ResponseWriter, r *http.Request) {
		// The access token carries the code so the identity is deterministic and
		// the test exercises the userinfo (no user id in the token response) path.
		id := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer at-")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openId":"coros-` + id + `"}`))
	})
	mux.HandleFunc("/oauth/deauthorize", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.deau = append(f.deau, r.PostForm.Get("token"))
		fail := f.failDeauthorize
		f.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"server_error"}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// configureCorosProvider registers a coros provider config on the bootstrap
// application and points its redirect allowlist at the test callback URL.
func (ta *testApp) configureCorosProvider(t *testing.T, baseURL string, extra map[string]any) {
	t.Helper()
	ctx := context.Background()
	app, err := ta.repo.Applications().FindByClientID(ctx, ta.clientID)
	if err != nil || app == nil {
		t.Fatalf("find app: %v", err)
	}
	app.RedirectURIs = `["https://app.example/oauth/done"]`
	app.UpdatedAt = time.Now().UTC()
	if err := ta.repo.Applications().Update(ctx, app); err != nil {
		t.Fatalf("update app: %v", err)
	}
	cfg := map[string]any{"client_id": "coros-app-id", "client_secret": "coros-app-secret"}
	if baseURL != "" {
		cfg["base_url"] = baseURL
	}
	for k, v := range extra {
		cfg[k] = v
	}
	b, _ := json.Marshal(cfg)
	if existing, _ := ta.repo.AppProviders().FindByAppAndProvider(ctx, app.ID, "coros"); existing != nil {
		if err := ta.repo.AppProviders().DeleteByID(ctx, existing.ID); err != nil {
			t.Fatalf("delete existing coros provider: %v", err)
		}
	}
	if err := ta.repo.AppProviders().Insert(ctx, &domain.AppProvider{
		ID: uuid.NewString(), AppID: app.ID, ProviderID: "coros",
		Config: string(b), IsActive: true, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("insert coros provider: %v", err)
	}
}

// startCorosAuthorize calls the authorize endpoint with a bearer token and
// returns the authorize URL and the opaque state handle embedded in it.
func (ta *testApp) startCorosAuthorize(t *testing.T, token, redirectURI string) (string, string) {
	t.Helper()
	w := ta.do(http.MethodPost, "/api/users/me/accounts/coros/authorize", map[string]any{
		"redirect_uri": redirectURI,
	}, ta.bearer(token))
	mustStatus(t, w, http.StatusOK)
	var resp struct {
		ProviderID   string `json:"provider_id"`
		AuthorizeURL string `json:"authorize_url"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	decode(t, w, &resp)
	if resp.ProviderID != "coros" || resp.ExpiresIn <= 0 {
		t.Fatalf("authorize response = %+v", resp)
	}
	u, err := url.Parse(resp.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatalf("authorize url has no state: %q", resp.AuthorizeURL)
	}
	return resp.AuthorizeURL, state
}

func (ta *testApp) callback(providerID, code, state string) *httptest.ResponseRecorder {
	q := url.Values{}
	if code != "" {
		q.Set("code", code)
	}
	q.Set("state", state)
	return ta.do(http.MethodGet, "/oauth/link/"+providerID+"/callback?"+q.Encode(), nil, nil)
}

// --- Authorize endpoint ---

func TestAuthorizeRequiresBearer(t *testing.T) {
	ta := newTestApp(t)
	w := ta.do(http.MethodPost, "/api/users/me/accounts/coros/authorize", map[string]any{
		"redirect_uri": "https://app.example/oauth/done",
	}, nil)
	mustStatus(t, w, http.StatusUnauthorized)
}

func TestAuthorizeUnknownProvider(t *testing.T) {
	ta := newTestApp(t)
	token := ta.registerUser(t, "unknown-provider@example.com")
	w := ta.do(http.MethodPost, "/api/users/me/accounts/strava/authorize", map[string]any{
		"redirect_uri": "https://app.example/oauth/done",
	}, ta.bearer(token))
	mustStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "provider_not_supported")
}

func TestAuthorizeUnconfiguredProvider(t *testing.T) {
	ta := newTestApp(t)
	token := ta.registerUser(t, "unconfigured@example.com")
	w := ta.do(http.MethodPost, "/api/users/me/accounts/coros/authorize", map[string]any{
		"redirect_uri": "https://app.example/oauth/done",
	}, ta.bearer(token))
	mustStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "provider_not_configured")
}

func TestAuthorizeRejectsUnregisteredRedirectURI(t *testing.T) {
	ta := newTestApp(t)
	fake := newFakeBrandServer(t)
	ta.configureCorosProvider(t, fake.URL(), nil)
	token := ta.registerUser(t, "bad-redirect@example.com")

	w := ta.do(http.MethodPost, "/api/users/me/accounts/coros/authorize", map[string]any{
		"redirect_uri": "https://evil.example/steal",
	}, ta.bearer(token))
	mustStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_redirect_uri")
}

func TestAuthorizeUsesPerApplicationCredentials(t *testing.T) {
	ta := newTestApp(t)
	fake := newFakeBrandServer(t)
	ta.configureCorosProvider(t, fake.URL(), nil)
	token := ta.registerUser(t, "creds@example.com")

	authorizeURL, _ := ta.startCorosAuthorize(t, token, "https://app.example/oauth/done")
	u, _ := url.Parse(authorizeURL)
	if u.Query().Get("client_id") != "coros-app-id" {
		t.Fatalf("authorize client_id = %q, want per-app config", u.Query().Get("client_id"))
	}
	if !strings.Contains(authorizeURL, "/oauth/authorize") {
		t.Fatalf("authorize url = %q", authorizeURL)
	}
}

// --- Callback endpoint ---

func TestCallbackSuccessStoresTokensAndRedirects(t *testing.T) {
	ta := newTestApp(t)
	fake := newFakeBrandServer(t)
	ta.configureCorosProvider(t, fake.URL(), nil)
	token := ta.registerUser(t, "callback-ok@example.com")

	_, state := ta.startCorosAuthorize(t, token, "https://app.example/oauth/done")
	w := ta.callback("coros", "code-1", state)
	mustStatus(t, w, http.StatusFound)
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if loc.Query().Get("error") != "" {
		t.Fatalf("redirect = %q, want no error on success", w.Header().Get("Location"))
	}
	if loc.Query().Get("provider_id") != "coros" {
		t.Fatalf("redirect = %q, want provider_id=coros", w.Header().Get("Location"))
	}

	// The exchange used the application config's credentials.
	cid, secret := fake.lastCredentials()
	if cid != "coros-app-id" || secret != "coros-app-secret" {
		t.Fatalf("exchange credentials = %q/%q", cid, secret)
	}

	// The account row holds the refresh token as credential and the access
	// token + raw responses in metadata.
	ctx := context.Background()
	userID := ta.userIDFromToken(t, token)
	account, err := ta.repo.Accounts().FindByUserAndProvider(ctx, userID, "coros")
	if err != nil || account == nil {
		t.Fatalf("find account: %v (%v)", err, account)
	}
	if account.ProviderAccountID == nil || *account.ProviderAccountID != "coros-code-1" {
		t.Fatalf("provider_account_id = %v", account.ProviderAccountID)
	}
	if account.Credential == nil || *account.Credential != "rt-code-1" {
		t.Fatalf("credential = %v, want refresh token", account.Credential)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(account.ProviderMetadata), &meta); err != nil {
		t.Fatalf("metadata json: %v", err)
	}
	if meta["access_token"] != "at-code-1" {
		t.Fatalf("metadata access_token = %v", meta["access_token"])
	}
	if _, ok := meta["raw_token_response"]; !ok {
		t.Fatal("metadata must preserve the raw token response")
	}
	if _, ok := meta["raw_identity_response"]; !ok {
		t.Fatal("metadata must preserve the raw identity response")
	}
	if _, ok := meta["expires_at"]; !ok {
		t.Fatal("metadata must record the token expiry")
	}
	if meta["app_id"] == nil || meta["app_id"] == "" {
		t.Fatalf("metadata must record the binding app id, got %v", meta["app_id"])
	}
}

func TestCallbackStateIsSingleUse(t *testing.T) {
	ta := newTestApp(t)
	fake := newFakeBrandServer(t)
	ta.configureCorosProvider(t, fake.URL(), nil)
	token := ta.registerUser(t, "single-use@example.com")

	_, state := ta.startCorosAuthorize(t, token, "https://app.example/oauth/done")
	if w := ta.callback("coros", "code-1", state); w.Code != http.StatusFound {
		t.Fatalf("first callback status = %d", w.Code)
	}
	second := ta.callback("coros", "code-2", state)
	mustStatus(t, second, http.StatusBadRequest)
	assertErrorCode(t, second, "invalid_oauth_state")
}

func TestCallbackUnknownState(t *testing.T) {
	ta := newTestApp(t)
	fake := newFakeBrandServer(t)
	ta.configureCorosProvider(t, fake.URL(), nil)

	w := ta.callback("coros", "code-1", "a-state-that-was-never-minted")
	mustStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_oauth_state")
}

func TestCallbackRejectsCrossProviderState(t *testing.T) {
	ta := newTestApp(t)
	fake := newFakeBrandServer(t)
	ta.configureCorosProvider(t, fake.URL(), nil)
	token := ta.registerUser(t, "cross-provider@example.com")

	_, state := ta.startCorosAuthorize(t, token, "https://app.example/oauth/done")
	// The same handle replayed against a different provider path is rejected.
	// A handle minted for one provider is rejected at another registered
	// provider's callback (the test brand is registered because the harness
	// enables test providers).
	w := ta.callback("testbrand", "code-1", state)
	mustStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_oauth_state")
}

func TestCallbackConflictWhenIdentityBoundToAnotherUser(t *testing.T) {
	ta := newTestApp(t)
	fake := newFakeBrandServer(t)
	ta.configureCorosProvider(t, fake.URL(), nil)

	user1 := ta.registerUser(t, "owner-1@example.com")
	_, state1 := ta.startCorosAuthorize(t, user1, "https://app.example/oauth/done")
	if w := ta.callback("coros", "shared", state1); w.Code != http.StatusFound {
		t.Fatalf("first bind status = %d\nbody: %s", w.Code, w.Body.String())
	}

	user2 := ta.registerUser(t, "owner-2@example.com")
	_, state2 := ta.startCorosAuthorize(t, user2, "https://app.example/oauth/done")
	w := ta.callback("coros", "shared", state2)
	mustStatus(t, w, http.StatusFound)
	assertLinkError(t, w, "account_already_linked")
}

func TestCallbackProviderErrorRedirectsWithFailure(t *testing.T) {
	ta := newTestApp(t)
	fake := newFakeBrandServer(t)
	ta.configureCorosProvider(t, fake.URL(), nil)
	token := ta.registerUser(t, "denied@example.com")

	_, state := ta.startCorosAuthorize(t, token, "https://app.example/oauth/done")
	w := ta.do(http.MethodGet, "/oauth/link/coros/callback?error=access_denied&state="+url.QueryEscape(state), nil, nil)
	mustStatus(t, w, http.StatusFound)
	loc, _ := url.Parse(w.Header().Get("Location"))
	if loc.Query().Get("error") != "access_denied" {
		t.Fatalf("redirect = %q, want error=access_denied", w.Header().Get("Location"))
	}
}

func TestCallbackMissingCodeRedirectsWithInvalidRequest(t *testing.T) {
	ta := newTestApp(t)
	fake := newFakeBrandServer(t)
	ta.configureCorosProvider(t, fake.URL(), nil)
	token := ta.registerUser(t, "no-code@example.com")

	_, state := ta.startCorosAuthorize(t, token, "https://app.example/oauth/done")
	w := ta.callback("coros", "", state)
	mustStatus(t, w, http.StatusFound)
	assertLinkError(t, w, "invalid_request")
}

func TestCallbackExchangeFailureRedirectsWithServerError(t *testing.T) {
	ta := newTestApp(t)
	// A brand server whose token endpoint rejects the code.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	t.Cleanup(srv.Close)
	ta.configureCorosProvider(t, srv.URL, nil)
	token := ta.registerUser(t, "exchange-fail@example.com")

	_, state := ta.startCorosAuthorize(t, token, "https://app.example/oauth/done")
	w := ta.callback("coros", "bad-code", state)
	mustStatus(t, w, http.StatusFound)
	assertLinkError(t, w, "server_error")
}

// --- Unlink / deauthorize ---

func TestUnlinkCallsProviderDeauthorize(t *testing.T) {
	ta := newTestApp(t)
	fake := newFakeBrandServer(t)
	ta.configureCorosProvider(t, fake.URL(), nil)
	token := ta.registerUser(t, "unlink@example.com")

	_, state := ta.startCorosAuthorize(t, token, "https://app.example/oauth/done")
	if w := ta.callback("coros", "code-9", state); w.Code != http.StatusFound {
		t.Fatalf("bind status = %d", w.Code)
	}
	w := ta.do(http.MethodDelete, "/api/users/me/accounts/coros", nil, ta.bearer(token))
	mustStatus(t, w, http.StatusOK)

	tokens := fake.deauthorizedTokens()
	if len(tokens) != 1 || tokens[0] != "rt-code-9" {
		t.Fatalf("deauthorized tokens = %v, want [rt-code-9]", tokens)
	}
}

// TestAdminUnlinkDeauthorizesViaBindingApp covers the case the credentials live
// on the app that created the link, while the caller is a different app (admin
// console): deauthorization must resolve the binding app from the account
// metadata, not from the calling token.
func TestAdminUnlinkDeauthorizesViaBindingApp(t *testing.T) {
	ta := newTestApp(t)
	fake := newFakeBrandServer(t)
	ta.configureCorosProvider(t, fake.URL(), nil)
	token := ta.registerUser(t, "admin-unlink@example.com")
	_, state := ta.startCorosAuthorize(t, token, "https://app.example/oauth/done")
	if w := ta.callback("coros", "code-7", state); w.Code != http.StatusFound {
		t.Fatalf("bind status = %d", w.Code)
	}

	// A second application the admin request is issued through. It has no coros
	// provider config, so only the binding app's metadata can drive revocation.
	ctx := context.Background()
	now := time.Now().UTC()
	other := &domain.Application{
		ID: uuid.NewString(), Name: "other-app", ClientID: "other-client",
		ClientSecretHash: "x", RedirectURIs: "[]", AllowedScopes: "[]",
		IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := ta.repo.Applications().Insert(ctx, other); err != nil {
		t.Fatalf("insert second app: %v", err)
	}
	adminToken, err := ta.jwt.IssueAccessToken(ta.adminUserID, "other-client", []string{"admin"}, "admin",
		domain.MembershipRegular, domain.UserTypeRegular, nil)
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}

	userID := ta.userIDFromToken(t, token)
	w := ta.do(http.MethodDelete, "/admin/users/"+userID+"/accounts/coros", nil, ta.bearer(adminToken))
	mustStatus(t, w, http.StatusOK)
	tokens := fake.deauthorizedTokens()
	if len(tokens) != 1 || tokens[0] != "rt-code-7" {
		t.Fatalf("deauthorized tokens = %v, want [rt-code-7]", tokens)
	}
}

func TestUnlinkSucceedsWhenDeauthorizeFails(t *testing.T) {
	ta := newTestApp(t)
	fake := newFakeBrandServer(t)
	ta.configureCorosProvider(t, fake.URL(), nil)
	fake.failDeauthorize = true
	token := ta.registerUser(t, "unlink-fail@example.com")

	_, state := ta.startCorosAuthorize(t, token, "https://app.example/oauth/done")
	if w := ta.callback("coros", "code-10", state); w.Code != http.StatusFound {
		t.Fatalf("bind status = %d", w.Code)
	}
	w := ta.do(http.MethodDelete, "/api/users/me/accounts/coros", nil, ta.bearer(token))
	mustStatus(t, w, http.StatusOK)

	ctx := context.Background()
	userID := ta.userIDFromToken(t, token)
	account, err := ta.repo.Accounts().FindByUserAndProvider(ctx, userID, "coros")
	if err != nil {
		t.Fatalf("find account: %v", err)
	}
	if account != nil {
		t.Fatal("account must be deleted even when deauthorize fails")
	}
}

// --- Mock end-to-end ---

func TestMockFlowEndToEnd(t *testing.T) {
	ta := newTestApp(t)
	ta.cfg.OAuthMockEnabled = true
	ta.configureCorosProvider(t, "", map[string]any{"mock": true})
	token := ta.registerUser(t, "mock@example.com")

	_, state := ta.startCorosAuthorize(t, token, "https://app.example/oauth/done")
	w := ta.callback("coros", "mock-code", state)
	mustStatus(t, w, http.StatusFound)

	list := ta.do(http.MethodGet, "/api/users/me/accounts", nil, ta.bearer(token))
	mustStatus(t, list, http.StatusOK)
	if !strings.Contains(list.Body.String(), "coros") {
		t.Fatalf("accounts list = %s, want coros", list.Body.String())
	}
}

// --- Link guard and route coexistence ---

func TestLinkEndpointDirectsOAuthProvidersToAuthorize(t *testing.T) {
	ta := newTestApp(t)
	fake := newFakeBrandServer(t)
	ta.configureCorosProvider(t, fake.URL(), nil)
	token := ta.registerUser(t, "link-guard@example.com")

	link := ta.do(http.MethodPost, "/api/users/me/accounts/coros/link", map[string]any{
		"credential": map[string]any{"code": "x"},
	}, ta.bearer(token))
	mustStatus(t, link, http.StatusBadRequest)
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	decode(t, link, &body)
	if body.Error == "provider_not_supported" {
		t.Fatalf("oauth provider should not read provider_not_supported: %+v", body)
	}
	if !strings.Contains(strings.ToLower(body.Message), "authorize") {
		t.Fatalf("message = %q, want a pointer to the authorize endpoint", body.Message)
	}

	// Both routes remain reachable side by side.
	if _, state := ta.startCorosAuthorize(t, token, "https://app.example/oauth/done"); state == "" {
		t.Fatal("authorize route must stay reachable alongside link")
	}
}

func TestListAccountsDoesNotLeakTokens(t *testing.T) {
	ta := newTestApp(t)
	fake := newFakeBrandServer(t)
	ta.configureCorosProvider(t, fake.URL(), nil)
	token := ta.registerUser(t, "no-leak@example.com")

	_, state := ta.startCorosAuthorize(t, token, "https://app.example/oauth/done")
	if w := ta.callback("coros", "code-secret", state); w.Code != http.StatusFound {
		t.Fatalf("bind status = %d", w.Code)
	}
	list := ta.do(http.MethodGet, "/api/users/me/accounts", nil, ta.bearer(token))
	mustStatus(t, list, http.StatusOK)
	body := strings.ToLower(list.Body.String())
	for _, forbidden := range []string{"access_token", "refresh_token", "at-secret", "rt-secret", "credential", "metadata"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("accounts list leaks %q: %s", forbidden, list.Body.String())
		}
	}
}

// --- helpers ---

func assertErrorCode(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	decode(t, w, &body)
	if body.Error != want {
		t.Fatalf("error = %q, want %q (body: %s)", body.Error, want, w.Body.String())
	}
}

func assertLinkError(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body: %s)", w.Code, w.Body.String())
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if got := loc.Query().Get("error"); got != want {
		t.Fatalf("link error = %q, want %q (location: %s)", got, want, loc)
	}
}

func (ta *testApp) userIDFromToken(t *testing.T, accessToken string) string {
	t.Helper()
	claims, err := ta.jwt.VerifyAccessToken(accessToken)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	return claims.Sub
}
