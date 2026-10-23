package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zhaochy1990/auth-service/internal/apperror"
)

// stableTokenRequest records one stable_token call.
type stableTokenRequest struct {
	AppID        string `json:"appid"`
	Secret       string `json:"secret"`
	GrantType    string `json:"grant_type"`
	ForceRefresh bool   `json:"force_refresh"`
}

// codeRequest records one getwxacodeunlimit call.
type codeRequest struct {
	AccessToken string
	Body        wxacodeRequestBody
}

// fakeWxServer fakes stable_token + getwxacodeunlimit. Response bodies are
// popped FIFO from tokenBodies / codeBodies (defaults: a success token
// access_token=TOKEN, a 1px PNG). A code body is served as image/png when it
// starts with "image:", otherwise as application/json.
type fakeWxServer struct {
	srv *httptest.Server

	mu          sync.Mutex
	tokenBodies []string
	tokenReqs   []stableTokenRequest
	codeBodies  []string
	codeReqs    []codeRequest
}

func newFakeWxServer(t *testing.T) *fakeWxServer {
	t.Helper()
	f := &fakeWxServer{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/stable_token":
			var body stableTokenRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			resp := `{"access_token":"TOKEN","expires_in":7200}`
			if n := len(f.tokenBodies); n > 0 {
				resp = f.tokenBodies[0]
				f.tokenBodies = f.tokenBodies[1:]
			}
			f.tokenReqs = append(f.tokenReqs, body)
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(resp))
		case "/wxa/getwxacodeunlimit":
			var body wxacodeRequestBody
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			resp := "image:pngdata"
			if n := len(f.codeBodies); n > 0 {
				resp = f.codeBodies[0]
				f.codeBodies = f.codeBodies[1:]
			}
			f.codeReqs = append(f.codeReqs, codeRequest{
				AccessToken: r.URL.Query().Get("access_token"),
				Body:        body,
			})
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

// counts returns the stable_token / getwxacodeunlimit call totals.
func (f *fakeWxServer) counts() (tokenCalls, codeCalls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tokenReqs), len(f.codeReqs)
}

// tokenCalls snapshots the recorded stable_token requests.
func (f *fakeWxServer) tokenCalls() []stableTokenRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]stableTokenRequest(nil), f.tokenReqs...)
}

// codeCalls snapshots the recorded getwxacodeunlimit requests.
func (f *fakeWxServer) codeCalls() []codeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]codeRequest(nil), f.codeReqs...)
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func TestGetUnlimitedQRCodeHappyPath(t *testing.T) {
	f := newFakeWxServer(t)
	want := testPNG(t)
	f.mu.Lock()
	f.codeBodies = []string{"image:" + string(want)}
	f.mu.Unlock()

	c := NewWxACodeClient(f.srv.URL+"/cgi-bin/stable_token", f.srv.URL+"/wxa/getwxacodeunlimit")
	got, err := c.GetUnlimitedQRCode(context.Background(), "appid1", "secret1", WxACodeParams{
		Page:  "pages/coach/index",
		Scene: "c=abc123",
	})
	if err != nil {
		t.Fatalf("GetUnlimitedQRCode: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("png mismatch: got %d bytes, want %d", len(got), len(want))
	}

	tokens := f.tokenCalls()
	if len(tokens) != 1 {
		t.Fatalf("token calls = %d, want 1", len(tokens))
	}
	tk := tokens[0]
	if tk.GrantType != "client_credential" || tk.AppID != "appid1" || tk.Secret != "secret1" {
		t.Fatalf("token request = %+v", tk)
	}
	if tk.ForceRefresh {
		t.Fatal("first token request must not force_refresh")
	}

	codes := f.codeCalls()
	if len(codes) != 1 {
		t.Fatalf("code calls = %d, want 1", len(codes))
	}
	if codes[0].AccessToken != "TOKEN" {
		t.Fatalf("code access_token = %q, want TOKEN", codes[0].AccessToken)
	}
	// release (empty env_version) checks the page against the published
	// mini-program; width unset stays unset (WeChat default 430).
	if !codes[0].Body.CheckPath || codes[0].Body.EnvVersion != "" || codes[0].Body.Width != 0 {
		t.Fatalf("code body = %+v", codes[0].Body)
	}
	if codes[0].Body.Page != "pages/coach/index" || codes[0].Body.Scene != "c=abc123" {
		t.Fatalf("code page/scene = %+v", codes[0].Body)
	}
}

func TestCodeCheckPathFollowsEnvVersion(t *testing.T) {
	f := newFakeWxServer(t)
	c := NewWxACodeClient(f.srv.URL+"/cgi-bin/stable_token", f.srv.URL+"/wxa/getwxacodeunlimit")
	// Trial-version codes may target pages that are not published yet, so
	// page existence must not be checked.
	if _, err := c.GetUnlimitedQRCode(context.Background(), "appid1", "secret1", WxACodeParams{Page: "p", Scene: "s", EnvVersion: "trial"}); err != nil {
		t.Fatalf("trial call: %v", err)
	}
	codes := f.codeCalls()
	if len(codes) != 1 || codes[0].Body.CheckPath || codes[0].Body.EnvVersion != "trial" {
		t.Fatalf("trial code request = %+v, want check_path=false env_version=trial", codes)
	}
}

func TestStableTokenCachedAcrossCalls(t *testing.T) {
	f := newFakeWxServer(t)
	c := NewWxACodeClient(f.srv.URL+"/cgi-bin/stable_token", f.srv.URL+"/wxa/getwxacodeunlimit")
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := c.GetUnlimitedQRCode(ctx, "appid1", "secret1", WxACodeParams{Page: "p", Scene: "s"}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	tokenCalls, codeCalls := f.counts()
	if tokenCalls != 1 {
		t.Fatalf("token calls = %d, want 1 (cache must serve repeats)", tokenCalls)
	}
	if codeCalls != 3 {
		t.Fatalf("code calls = %d, want 3", codeCalls)
	}

	// A second appid gets its own token: the cache is keyed by appid.
	if _, err := c.GetUnlimitedQRCode(ctx, "appid2", "secret2", WxACodeParams{Page: "p", Scene: "s"}); err != nil {
		t.Fatalf("second appid: %v", err)
	}
	if tokenCalls, _ = f.counts(); tokenCalls != 2 {
		t.Fatalf("token calls after second appid = %d, want 2", tokenCalls)
	}
}

func TestStableTokenSingleflight(t *testing.T) {
	f := newFakeWxServer(t)
	c := NewWxACodeClient(f.srv.URL+"/cgi-bin/stable_token", f.srv.URL+"/wxa/getwxacodeunlimit")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.GetUnlimitedQRCode(context.Background(), "appid1", "secret1", WxACodeParams{Page: "p", Scene: "s"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent call: %v", err)
		}
	}
	if tokenCalls, _ := f.counts(); tokenCalls != 1 {
		t.Fatalf("token calls = %d, want 1 (singleflight must collapse the burst)", tokenCalls)
	}
}

func TestStableToken40001RetriesWithForceRefresh(t *testing.T) {
	f := newFakeWxServer(t)
	f.mu.Lock()
	f.tokenBodies = []string{`{"errcode":40001,"errmsg":"invalid credential"}`, `{"access_token":"TOKEN2","expires_in":7200}`}
	f.mu.Unlock()

	c := NewWxACodeClient(f.srv.URL+"/cgi-bin/stable_token", f.srv.URL+"/wxa/getwxacodeunlimit")
	png, err := c.GetUnlimitedQRCode(context.Background(), "appid1", "secret1", WxACodeParams{Page: "p", Scene: "s"})
	if err != nil {
		t.Fatalf("GetUnlimitedQRCode: %v", err)
	}
	if string(png) != "pngdata" {
		t.Fatalf("png = %q", png)
	}
	tokens := f.tokenCalls()
	if len(tokens) != 2 || !tokens[1].ForceRefresh {
		t.Fatalf("token calls = %+v, want a force_refresh retry", tokens)
	}
}

func TestCodeCall40001ForceRefreshesTokenAndRetries(t *testing.T) {
	f := newFakeWxServer(t)
	f.mu.Lock()
	// First code call: token TOKEN was invalidated server-side. Retry after
	// a forced mint (TOKEN2) succeeds.
	f.codeBodies = []string{`{"errcode":40001,"errmsg":"invalid token"}`, "image:pngdata"}
	f.tokenBodies = []string{`{"access_token":"TOKEN1","expires_in":7200}`, `{"access_token":"TOKEN2","expires_in":7200}`}
	f.mu.Unlock()

	c := NewWxACodeClient(f.srv.URL+"/cgi-bin/stable_token", f.srv.URL+"/wxa/getwxacodeunlimit")
	png, err := c.GetUnlimitedQRCode(context.Background(), "appid1", "secret1", WxACodeParams{Page: "p", Scene: "s"})
	if err != nil {
		t.Fatalf("GetUnlimitedQRCode: %v", err)
	}
	if string(png) != "pngdata" {
		t.Fatalf("png = %q", png)
	}
	tokens := f.tokenCalls()
	if len(tokens) != 2 || !tokens[1].ForceRefresh {
		t.Fatalf("token calls = %+v, want one forced refresh", tokens)
	}
	codes := f.codeCalls()
	if len(codes) != 2 || codes[0].AccessToken != "TOKEN1" || codes[1].AccessToken != "TOKEN2" {
		t.Fatalf("code calls = %+v, want retry with the refreshed token", codes)
	}
}

func TestCodeErrorMapping(t *testing.T) {
	cases := []struct {
		body    string
		status  int
		errType string
	}{
		{`{"errcode":41030,"errmsg":"invalid page"}`, 400, "wechat_invalid_page"},
		{`{"errcode":40129,"errmsg":"invalid scene"}`, 400, "wechat_invalid_scene"},
		{`{"errcode":45009,"errmsg":"reach limit"}`, 429, "wechat_rate_limited"},
		{`{"errcode":76543,"errmsg":"other"}`, 502, "wechat_api_error"},
	}
	for _, tc := range cases {
		f := newFakeWxServer(t)
		f.mu.Lock()
		f.codeBodies = []string{tc.body}
		f.mu.Unlock()
		c := NewWxACodeClient(f.srv.URL+"/cgi-bin/stable_token", f.srv.URL+"/wxa/getwxacodeunlimit")
		_, err := c.GetUnlimitedQRCode(context.Background(), "appid1", "secret1", WxACodeParams{Page: "p", Scene: "s"})
		ae, ok := err.(*apperror.Error)
		if !ok {
			t.Fatalf("body %s: expected *apperror.Error, got %v", tc.body, err)
		}
		if ae.Status != tc.status || ae.Type != tc.errType {
			t.Fatalf("body %s: got (%d, %s), want (%d, %s)", tc.body, ae.Status, ae.Type, tc.status, tc.errType)
		}
	}
}

func TestGetUnlimitedQRCodeMalformedResponses(t *testing.T) {
	// JSON success body without access_token.
	f := newFakeWxServer(t)
	f.mu.Lock()
	f.tokenBodies = []string{`{"expires_in":7200}`}
	f.mu.Unlock()
	c := NewWxACodeClient(f.srv.URL+"/cgi-bin/stable_token", f.srv.URL+"/wxa/getwxacodeunlimit")
	if _, err := c.stableToken(context.Background(), "appid1", "secret1", false); err == nil {
		t.Fatal("expected error for token response without access_token")
	}

	// Image endpoint answering JSON success (errcode 0) is still a failure.
	f2 := newFakeWxServer(t)
	f2.mu.Lock()
	f2.codeBodies = []string{`{"errcode":0,"errmsg":"ok"}`}
	f2.mu.Unlock()
	c2 := NewWxACodeClient(f2.srv.URL+"/cgi-bin/stable_token", f2.srv.URL+"/wxa/getwxacodeunlimit")
	if _, err := c2.GetUnlimitedQRCode(context.Background(), "appid1", "secret1", WxACodeParams{Page: "p", Scene: "s"}); err == nil {
		t.Fatal("expected error for non-image code response")
	}
}

func TestStableTokenNotConfigured(t *testing.T) {
	f := newFakeWxServer(t)
	c := NewWxACodeClient(f.srv.URL+"/cgi-bin/stable_token", f.srv.URL+"/wxa/getwxacodeunlimit")
	_, err := c.stableToken(context.Background(), "", "secret", false)
	ae, ok := err.(*apperror.Error)
	if !ok || ae.Type != "wechat_not_configured" {
		t.Fatalf("got %v, want wechat_not_configured", err)
	}
}
