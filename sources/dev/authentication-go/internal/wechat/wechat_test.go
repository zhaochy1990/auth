package wechat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhaochy1990/auth-service/internal/apperror"
)

// newTestServer returns a fake code2Session server whose response depends on
// the js_code query parameter.
func newTestServer(responses map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("js_code")
		body, ok := responses[code]
		if !ok {
			body = `{"errcode":40029,"errmsg":"invalid code"}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func TestCode2SessionSuccess(t *testing.T) {
	srv := newTestServer(map[string]string{
		"good": `{"openid":"wx_abc","session_key":"sk_1","unionid":"wx_union"}`,
	})
	defer srv.Close()

	c := NewClient("app1", "secret1", srv.URL)
	res, err := c.Code2Session(context.Background(), "good")
	if err != nil {
		t.Fatalf("Code2Session: %v", err)
	}
	if res.OpenID != "wx_abc" || res.SessionKey != "sk_1" {
		t.Fatalf("unexpected session: %+v", res)
	}
	if res.UnionID == nil || *res.UnionID != "wx_union" {
		t.Fatalf("expected unionid, got %+v", res)
	}
}

func TestCode2SessionErrorCode(t *testing.T) {
	srv := newTestServer(map[string]string{
		"bad": `{"errcode":40029,"errmsg":"invalid code"}`,
	})
	defer srv.Close()

	c := NewClient("app1", "secret1", srv.URL)
	_, err := c.Code2Session(context.Background(), "bad")
	if err == nil {
		t.Fatal("expected error")
	}
	ae, ok := err.(*apperror.Error)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if ae.Status != http.StatusBadRequest || ae.Type != "wechat_invalid_code" {
		t.Fatalf("unexpected error: %+v", ae)
	}
}

func TestCode2SessionNotConfigured(t *testing.T) {
	c := NewClient("", "", "")
	_, err := c.Code2Session(context.Background(), "code")
	if err == nil {
		t.Fatal("expected not-configured error")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCode2SessionNetworkFailure(t *testing.T) {
	// Point at a closed port → network error → 400 wechat_api_error per spec
	// ("WeChat API failure → 400 with a clear error code").
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := NewClient("app1", "secret1", url)
	_, err := c.Code2Session(context.Background(), "code")
	if err == nil {
		t.Fatal("expected network error")
	}
	ae, ok := err.(*apperror.Error)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if ae.Status != http.StatusBadRequest || ae.Type != "wechat_api_error" {
		t.Fatalf("unexpected error: %+v", ae)
	}
}
