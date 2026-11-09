package server_test

// Integration tests for the internal code2session endpoint (POST
// /api/internal/wechat/code2session, X-Internal-Token gated): the token
// gate, request validation, the happy path through the fake WeChat
// code2Session server seeded by the standard harness, WeChat error
// mapping, and the openid-only response contract (session_key must never
// ride along).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhaochy1990/auth-service/internal/cos"
	"github.com/zhaochy1990/auth-service/internal/server"
	"github.com/zhaochy1990/auth-service/internal/sms"
)

// newCode2SessionTestApp builds the standard harness (fake WeChat
// code2Session server + seeded wechat provider row) plus a router wired
// with the internal token, so the endpoint can be exercised without
// touching real WeChat.
func newCode2SessionTestApp(t *testing.T) *testApp {
	t.Helper()
	ta := newTestApp(t)
	ta.cfg.InternalAPIToken = wxacodeInternalToken

	cosClient := cos.NewClient(cos.Config{
		SecretID:  ta.cfg.TencentCosSecretID,
		SecretKey: ta.cfg.TencentCosSecretKey,
		Bucket:    ta.cfg.TencentCosBucket,
		Region:    ta.cfg.TencentCosRegion,
		BaseURL:   ta.cfg.TencentCosBaseURL,
	}, mustParseURL(t, ta.fakeCos.URL()))
	ta.engine = server.NewRouter(ta.repo, ta.jwt, ta.cfg, ta.smsStore, sms.NewClient(sms.Config{}, ""), cosClient)
	return ta
}

// code2sessionPost posts body to the internal endpoint, optionally
// carrying the X-Internal-Token header.
func code2sessionPost(ta *testApp, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/internal/wechat/code2session", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Internal-Token", token)
	}
	w := httptest.NewRecorder()
	ta.engine.ServeHTTP(w, req)
	return w
}

func TestCode2SessionInternalTokenAuth(t *testing.T) {
	ta := newCode2SessionTestApp(t)

	for _, token := range []string{"", "nope"} {
		w := code2sessionPost(ta, `{"code":"code-bound"}`, token)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("token %q: status = %d, want 401", token, w.Code)
		}
	}
	if n := len(ta.wechatSrv.appIDs); n != 0 {
		t.Fatalf("wechat calls = %d, want 0 (auth must run before WeChat)", n)
	}
}

func TestCode2SessionRequestValidation(t *testing.T) {
	ta := newCode2SessionTestApp(t)

	cases := []struct {
		name string
		body string
	}{
		{"not JSON", `not-json`},
		{"missing code", `{}`},
		{"empty code", `{"code":""}`},
	}
	for _, tc := range cases {
		w := code2sessionPost(ta, tc.body, wxacodeInternalToken)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400 (%s)", tc.name, w.Code, w.Body.String())
		}
	}
	if n := len(ta.wechatSrv.appIDs); n != 0 {
		t.Fatalf("wechat calls = %d, want 0 (validation must run before WeChat)", n)
	}
}

func TestCode2SessionReturnsOpenIDOnly(t *testing.T) {
	ta := newCode2SessionTestApp(t)

	w := code2sessionPost(ta, `{"code":"code-bound"}`, wxacodeInternalToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["openid"] != "wx_bound" {
		t.Fatalf("openid = %v, want wx_bound", resp["openid"])
	}
	// The openid-only contract: the login session_key is the miniprogram
	// login flow's secret and must never ride along.
	if _, ok := resp["session_key"]; ok {
		t.Fatalf("session_key must not be returned: %v", resp)
	}
	if len(resp) != 1 {
		t.Fatalf("response must carry exactly the openid: %v", resp)
	}

	// The credentials used are the seeded provider row's, verbatim.
	ta.wechatSrv.mu.Lock()
	defer ta.wechatSrv.mu.Unlock()
	if len(ta.wechatSrv.appIDs) != 1 || ta.wechatSrv.appIDs[0] != "test-appid" ||
		ta.wechatSrv.appSecrets[0] != "test-secret" {
		t.Fatalf("wechat credentials = %v/%v, want test-appid/test-secret",
			ta.wechatSrv.appIDs, ta.wechatSrv.appSecrets)
	}
}

func TestCode2SessionWeChatErrorMapping(t *testing.T) {
	ta := newCode2SessionTestApp(t)

	// The fake WeChat server answers 40029 for unknown codes: a stale or
	// already-used wx.login code surfaces as 400 wechat_invalid_code.
	w := code2sessionPost(ta, `{"code":"code-bad"}`, wxacodeInternalToken)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if errResp.Error != "wechat_invalid_code" {
		t.Fatalf("error = %q, want wechat_invalid_code", errResp.Error)
	}
}
