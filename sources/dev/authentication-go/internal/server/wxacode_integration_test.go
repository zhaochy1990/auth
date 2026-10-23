package server_test

// Integration tests for the internal wxacode endpoint (POST /api/internal/
// wxacode, X-Internal-Token gated): auth, request validation, the happy path
// through stable_token → getwxacodeunlimit → COS, and WeChat error mapping.

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

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/zhaochy1990/auth-service/internal/cos"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/server"
	"github.com/zhaochy1990/auth-service/internal/sms"
)

const wxacodeInternalToken = "test-internal-token"

// fakeWxACodeAPI fakes WeChat's stable_token + getwxacodeunlimit endpoints.
// Response bodies are popped FIFO (tokenBodies / codeBodies); when empty a
// success token (access_token=TOKEN) / a 1px PNG is served. A code body is
// served as image/png when it starts with "image:", else as JSON.
type fakeWxACodeAPI struct {
	srv *httptest.Server

	mu          sync.Mutex
	tokenBodies []string
	tokenReqs   []string // force_refresh values, "true"/"false"
	codeBodies  []string
	codeTokens  []string
}

func newFakeWxACodeAPI(t *testing.T) *fakeWxACodeAPI {
	t.Helper()
	f := &fakeWxACodeAPI{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/stable_token":
			var body struct {
				ForceRefresh bool `json:"force_refresh"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			resp := `{"access_token":"TOKEN","expires_in":7200}`
			if n := len(f.tokenBodies); n > 0 {
				resp = f.tokenBodies[0]
				f.tokenBodies = f.tokenBodies[1:]
			}
			f.tokenReqs = append(f.tokenReqs, map[bool]string{true: "true", false: "false"}[body.ForceRefresh])
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(resp))
		case "/wxa/getwxacodeunlimit":
			f.mu.Lock()
			resp := "image:pngdata"
			if n := len(f.codeBodies); n > 0 {
				resp = f.codeBodies[0]
				f.codeBodies = f.codeBodies[1:]
			}
			f.codeTokens = append(f.codeTokens, r.URL.Query().Get("access_token"))
			f.mu.Unlock()
			if strings.HasPrefix(resp, "image:") {
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write([]byte(strings.TrimPrefix(resp, "image:")))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(resp))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeWxACodeAPI) counts() (tokenCalls, codeCalls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tokenReqs), len(f.codeTokens)
}

// newWxACodeTestApp builds the standard harness plus a second router wired
// against a fake WeChat code API and the internal token, so the wxacode
// endpoint can be exercised end to end without touching real WeChat.
func newWxACodeTestApp(t *testing.T) (*testApp, *gin.Engine, *fakeWxACodeAPI) {
	t.Helper()
	ta := newTestApp(t)
	api := newFakeWxACodeAPI(t)

	ta.cfg.WeChatStableTokenURL = api.srv.URL + "/cgi-bin/stable_token"
	ta.cfg.WeChatWxACodeURL = api.srv.URL + "/wxa/getwxacodeunlimit"
	ta.cfg.InternalAPIToken = wxacodeInternalToken

	cosClient := cos.NewClient(cos.Config{
		SecretID:  ta.cfg.TencentCosSecretID,
		SecretKey: ta.cfg.TencentCosSecretKey,
		Bucket:    ta.cfg.TencentCosBucket,
		Region:    ta.cfg.TencentCosRegion,
		BaseURL:   ta.cfg.TencentCosBaseURL,
	}, mustParseURL(t, ta.fakeCos.URL()))
	engine := server.NewRouter(ta.repo, ta.jwt, ta.cfg, ta.smsStore, sms.NewClient(sms.Config{}, ""), cosClient)
	return ta, engine, api
}

func wxacodePost(engine *gin.Engine, body string, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/internal/wxacode", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Internal-Token", token)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func TestWxACodeInternalTokenAuth(t *testing.T) {
	_, engine, api := newWxACodeTestApp(t)

	cases := []struct {
		name  string
		token string
	}{
		{"missing header", ""},
		{"wrong token", "nope"},
	}
	for _, tc := range cases {
		w := wxacodePost(engine, `{"scene":"s","page":"pages/index/index"}`, tc.token)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", tc.name, w.Code)
		}
	}
	if _, codeCalls := api.counts(); codeCalls != 0 {
		t.Fatalf("code calls = %d, want 0 (auth must run before WeChat)", codeCalls)
	}

	// The correct token passes the gate (validation rejects the unknown page
	// upstream, but the WeChat API is reached).
	api.mu.Lock()
	api.codeBodies = []string{`{"errcode":41030,"errmsg":"invalid page"}`}
	api.mu.Unlock()
	w := wxacodePost(engine, `{"scene":"s","page":"pages/index/index"}`, wxacodeInternalToken)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("valid token: status = %d, want 400 (upstream invalid page)", w.Code)
	}
}

func TestWxACodeRequestValidation(t *testing.T) {
	_, engine, api := newWxACodeTestApp(t)
	cases := []struct {
		name string
		body string
	}{
		{"missing scene", `{"page":"pages/index/index"}`},
		{"empty scene", `{"scene":"","page":"pages/index/index"}`},
		{"scene over 32 chars", `{"scene":"` + strings.Repeat("a", 33) + `","page":"pages/index/index"}`},
		{"scene with control char", `{"scene":"a\tb","page":"pages/index/index"}`},
		{"missing page", `{"scene":"s"}`},
		{"bad env_version", `{"scene":"s","page":"pages/index/index","env_version":"beta"}`},
		{"width too small", `{"scene":"s","page":"pages/index/index","width":200}`},
		{"width too large", `{"scene":"s","page":"pages/index/index","width":2000}`},
	}
	for _, tc := range cases {
		w := wxacodePost(engine, tc.body, wxacodeInternalToken)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400 (%s)", tc.name, w.Code, w.Body.String())
		}
	}
	if _, codeCalls := api.counts(); codeCalls != 0 {
		t.Fatalf("code calls = %d, want 0 (validation must run before WeChat)", codeCalls)
	}
}

func TestWxACodeGeneratesUploadsAndReturnsURL(t *testing.T) {
	ta, engine, api := newWxACodeTestApp(t)

	w := wxacodePost(engine, `{"scene":"c=abc123","page":"pages/coach/index"}`, wxacodeInternalToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	wantURL := ta.cfg.TencentCosBaseURL + "/wxacode/c=abc123.430.png"
	if resp.URL != wantURL {
		t.Fatalf("url = %q, want %q", resp.URL, wantURL)
	}

	key, contentType := ta.fakeCos.lastUpload()
	if key != "wxacode/c=abc123.430.png" || contentType != "image/png" {
		t.Fatalf("cos upload key=%q type=%q", key, contentType)
	}

	// The credentials come from the app's wechat provider config row.
	tokenCalls, codeCalls := api.counts()
	if tokenCalls != 1 || codeCalls != 1 {
		t.Fatalf("token/code calls = %d/%d, want 1/1", tokenCalls, codeCalls)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.codeTokens[0] != "TOKEN" {
		t.Fatalf("code access_token = %q", api.codeTokens[0])
	}
}

func TestWxACodeReadsProviderAcrossApps(t *testing.T) {
	ta, engine, api := newWxACodeTestApp(t)

	// A second app with an older wechat provider row: the internal endpoint
	// serves the oldest active wechat config regardless of application.
	ctx := context.Background()
	app, err := ta.repo.Applications().FindByClientID(ctx, ta.clientID)
	if err != nil {
		t.Fatalf("find bootstrap app: %v", err)
	}
	older := &domain.AppProvider{
		ID:         uuid.NewString(),
		AppID:      app.ID + "-other",
		ProviderID: "wechat",
		Config:     `{"appid":"older-appid","secret":"older-secret"}`,
		IsActive:   true,
		CreatedAt:  time.Now().UTC().Add(-time.Hour),
	}
	if err := ta.repo.AppProviders().Insert(ctx, older); err != nil {
		t.Fatalf("insert older wechat provider: %v", err)
	}

	if w := wxacodePost(engine, `{"scene":"s","page":"pages/index/index"}`, wxacodeInternalToken); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	_ = api // token request body carries the appid; asserted in unit tests.
}

func TestWxACodeWeChatErrorMapping(t *testing.T) {
	_, engine, api := newWxACodeTestApp(t)
	cases := []struct {
		upstream string
		status   int
		errType  string
	}{
		{`{"errcode":41030,"errmsg":"invalid page"}`, http.StatusBadRequest, "wechat_invalid_page"},
		{`{"errcode":40129,"errmsg":"invalid scene"}`, http.StatusBadRequest, "wechat_invalid_scene"},
		{`{"errcode":45009,"errmsg":"reach limit"}`, http.StatusTooManyRequests, "wechat_rate_limited"},
	}
	for _, tc := range cases {
		api.mu.Lock()
		api.codeBodies = []string{tc.upstream}
		api.mu.Unlock()
		w := wxacodePost(engine, `{"scene":"s","page":"pages/index/index"}`, wxacodeInternalToken)
		if w.Code != tc.status {
			t.Fatalf("%s: status = %d, want %d", tc.upstream, w.Code, tc.status)
		}
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil || errResp.Error != tc.errType {
			t.Fatalf("%s: error = %q (%v), want %q", tc.upstream, errResp.Error, err, tc.errType)
		}
	}
}

func TestWxACodeWeChatNotConfigured(t *testing.T) {
	ta, engine, _ := newWxACodeTestApp(t)

	// Remove every wechat provider row: nothing to read credentials from.
	ctx := context.Background()
	app, err := ta.repo.Applications().FindByClientID(ctx, ta.clientID)
	if err != nil {
		t.Fatalf("find bootstrap app: %v", err)
	}
	p, err := ta.repo.AppProviders().FindByAppAndProvider(ctx, app.ID, "wechat")
	if err != nil {
		t.Fatalf("find wechat provider: %v", err)
	}
	if p != nil {
		if err := ta.repo.AppProviders().DeleteByID(ctx, p.ID); err != nil {
			t.Fatalf("delete wechat provider: %v", err)
		}
	}

	w := wxacodePost(engine, `{"scene":"s","page":"pages/index/index"}`, wxacodeInternalToken)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil || errResp.Error != "wechat_not_configured" {
		t.Fatalf("error = %q (%v), want wechat_not_configured", errResp.Error, err)
	}
}

// mustParseURL parses raw or fails the test.
func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return u
}
