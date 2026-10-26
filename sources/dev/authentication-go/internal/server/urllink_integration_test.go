package server_test

// Integration tests for the two URL Link endpoints: POST /api/internal/
// urllink (X-Internal-Token gated, called by the backends) and POST
// /api/public/mp-link (per-IP rate limited, called by the browser share
// pages). Both share the minting logic: page whitelist, query caps, the
// provider-config credentials, and WeChat error mapping.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/zhaochy1990/auth-service/internal/cos"
	"github.com/zhaochy1990/auth-service/internal/server"
	"github.com/zhaochy1990/auth-service/internal/sms"
)

// newURLLinkTestApp builds the standard harness plus a router wired against
// a fake WeChat API (stable_token + generate_urllink) and the internal
// token, so both link endpoints can be exercised without touching WeChat.
func newURLLinkTestApp(t *testing.T) (*testApp, *gin.Engine, *fakeWxACodeAPI) {
	t.Helper()
	ta := newTestApp(t)
	api := newFakeWxACodeAPI(t)

	ta.cfg.WeChatStableTokenURL = api.srv.URL + "/cgi-bin/stable_token"
	ta.cfg.WeChatURLLinkURL = api.srv.URL + "/wxa/generate_urllink"
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

// urllinkPost posts body to path (an internal or public link endpoint),
// optionally carrying the X-Internal-Token header.
func urllinkPost(engine *gin.Engine, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Internal-Token", token)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

const (
	urllinkInternalPath = "/api/internal/urllink"
	urllinkPublicPath   = "/api/public/mp-link"
)

func TestURLLinkInternalTokenAuth(t *testing.T) {
	_, engine, api := newURLLinkTestApp(t)

	for _, token := range []string{"", "nope"} {
		w := urllinkPost(engine, urllinkInternalPath, `{"page":"pages/index/index"}`, token)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("token %q: status = %d, want 401", token, w.Code)
		}
	}
	if api.linkCalls != 0 {
		t.Fatalf("link calls = %d, want 0 (auth must run before WeChat)", api.linkCalls)
	}

	// The correct token passes the gate (WeChat is reached; an upstream
	// invalid page proves it).
	api.mu.Lock()
	api.linkBodies = []string{`{"errcode":40165,"errmsg":"invalid page"}`}
	api.mu.Unlock()
	w := urllinkPost(engine, urllinkInternalPath, `{"page":"pages/index/index"}`, wxacodeInternalToken)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("valid token: status = %d, want 400 (upstream invalid page)", w.Code)
	}
}

func TestURLLinkPublicPlaneNeedsNoCredential(t *testing.T) {
	_, engine, _ := newURLLinkTestApp(t)

	// No X-Internal-Token, no client auth: the browser share page just
	// posts page+query and gets a link back.
	w := urllinkPost(engine, urllinkPublicPath, `{"page":"pages/index/index","query":"id=abc123"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		URLLink string `json:"url_link"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.URLLink != "https://wxaurl.cn/LINK" {
		t.Fatalf("response = %q (%v), want url_link", w.Body.String(), err)
	}
}

func TestURLLinkRequestValidation(t *testing.T) {
	_, engine, api := newURLLinkTestApp(t)
	cases := []struct {
		name string
		body string
	}{
		{"missing page", `{}`},
		{"page not whitelisted", `{"page":"pages/other/other"}`},
		{"page with traversal", `{"page":"../pages/index/index"}`},
		{"query over 256 chars", `{"page":"pages/index/index","query":"` + strings.Repeat("a", 257) + `"}`},
		{"query bad charset", `{"page":"pages/index/index","query":"id=1&x=你好"}`},
		{"query with space", `{"page":"pages/index/index","query":"id=1 2"}`},
		{"bad env_version", `{"page":"pages/index/index","env_version":"beta"}`},
	}
	for _, tc := range cases {
		w := urllinkPost(engine, urllinkPublicPath, tc.body, "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400 (%s)", tc.name, w.Code, w.Body.String())
		}
	}
	if api.linkCalls != 0 {
		t.Fatalf("link calls = %d, want 0 (validation must run before WeChat)", api.linkCalls)
	}
}

func TestURLLinkInternalHappyPath(t *testing.T) {
	_, engine, api := newURLLinkTestApp(t)

	w := urllinkPost(engine, urllinkInternalPath, `{"page":"pages/race-center/detail","query":"id=race42&tab=result"}`, wxacodeInternalToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		URLLink string `json:"url_link"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.URLLink != "https://wxaurl.cn/LINK" {
		t.Fatalf("response = %q (%v)", w.Body.String(), err)
	}
	// One stable_token call served the mint (credentials from the wechat
	// provider config row, asserted in unit tests).
	if tokenCalls, _ := api.counts(); tokenCalls != 1 {
		t.Fatalf("token calls = %d, want 1", tokenCalls)
	}
	if api.linkCalls != 1 {
		t.Fatalf("link calls = %d, want 1", api.linkCalls)
	}
}

func TestURLLinkPublicRateLimit(t *testing.T) {
	_, engine, api := newURLLinkTestApp(t)

	// 10 requests per minute per IP: the first 10 mint, the 11th is 429
	// (every request shares one httptest remote IP, i.e. one bucket).
	for i := 0; i < 10; i++ {
		if w := urllinkPost(engine, urllinkPublicPath, `{"page":"pages/index/index"}`, ""); w.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i+1, w.Code)
		}
	}
	w := urllinkPost(engine, urllinkPublicPath, `{"page":"pages/index/index"}`, "")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("request 11: status = %d, want 429 (%s)", w.Code, w.Body.String())
	}
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil || errResp.Error != "rate_limited" {
		t.Fatalf("error = %q (%v), want rate_limited", errResp.Error, err)
	}

	// The limiter is per-IP, not global: the internal plane has no limiter
	// and still mints.
	if w := urllinkPost(engine, urllinkInternalPath, `{"page":"pages/index/index"}`, wxacodeInternalToken); w.Code != http.StatusOK {
		t.Fatalf("internal after public 429: status = %d, want 200", w.Code)
	}
	if api.linkCalls != 11 {
		t.Fatalf("link calls = %d, want 11", api.linkCalls)
	}
}

func TestURLLinkWeChatErrorMapping(t *testing.T) {
	cases := []struct {
		upstream string
		status   int
		errType  string
	}{
		{`{"errcode":40165,"errmsg":"invalid path"}`, http.StatusBadRequest, "wechat_invalid_page"},
		{`{"errcode":40212,"errmsg":"invalid query"}`, http.StatusBadRequest, "wechat_invalid_query"},
		{`{"errcode":44990,"errmsg":"qps limit"}`, http.StatusTooManyRequests, "wechat_rate_limited"},
		{`{"errcode":44993,"errmsg":"daily quota"}`, http.StatusTooManyRequests, "wechat_rate_limited"},
	}
	for _, tc := range cases {
		_, engine, api := newURLLinkTestApp(t)
		api.mu.Lock()
		api.linkBodies = []string{tc.upstream}
		api.mu.Unlock()
		w := urllinkPost(engine, urllinkPublicPath, `{"page":"pages/index/index"}`, "")
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

func TestURLLinkWeChatNotConfigured(t *testing.T) {
	ta, engine, _ := newURLLinkTestApp(t)

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

	for _, path := range []string{urllinkInternalPath, urllinkPublicPath} {
		token := ""
		if path == urllinkInternalPath {
			token = wxacodeInternalToken
		}
		w := urllinkPost(engine, path, `{"page":"pages/index/index"}`, token)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", path, w.Code)
		}
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil || errResp.Error != "wechat_not_configured" {
			t.Fatalf("%s: error = %q (%v), want wechat_not_configured", path, errResp.Error, err)
		}
	}
}
