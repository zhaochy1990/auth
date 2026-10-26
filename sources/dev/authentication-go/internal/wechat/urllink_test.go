package wechat

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zhaochy1990/auth-service/internal/apperror"
)

// newURLLinkClient builds a URLLinkClient against the fake WeChat server.
func newURLLinkClient(f *fakeWxServer) *URLLinkClient {
	return NewURLLinkClient(f.srv.URL+"/cgi-bin/stable_token", f.srv.URL+"/wxa/generate_urllink")
}

func TestURLLinkHappyPath(t *testing.T) {
	f := newFakeWxServer(t)
	c := newURLLinkClient(f)

	link, err := c.Generate(context.Background(), "appid1", "secret1", URLLinkParams{
		Path:  "pages/race-center/detail",
		Query: "id=abc123",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if link != "https://wxaurl.cn/LINK" {
		t.Fatalf("link = %q", link)
	}

	tokens := f.tokenCalls()
	if len(tokens) != 1 {
		t.Fatalf("token calls = %d, want 1", len(tokens))
	}
	tk := tokens[0]
	if tk.GrantType != "client_credential" || tk.AppID != "appid1" || tk.Secret != "secret1" || tk.ForceRefresh {
		t.Fatalf("token request = %+v", tk)
	}

	links := f.linkCalls()
	if len(links) != 1 {
		t.Fatalf("link calls = %d, want 1", len(links))
	}
	lc := links[0]
	if lc.AccessToken != "TOKEN" {
		t.Fatalf("link access_token = %q, want TOKEN", lc.AccessToken)
	}
	// Links are minted with the documented 1-day interval expiry (never the
	// ambiguous permanent default), and env_version stays unset for release.
	if lc.Body.Path != "pages/race-center/detail" || lc.Body.Query != "id=abc123" {
		t.Fatalf("link body = %+v", lc.Body)
	}
	if lc.Body.ExpireType != 1 || lc.Body.ExpireInterval != urllinkValidityDays || lc.Body.EnvVersion != "" {
		t.Fatalf("link expiry = %+v, want expire_type=1 interval=%d", lc.Body, urllinkValidityDays)
	}
}

func TestURLLinkEnvVersionPassedThrough(t *testing.T) {
	f := newFakeWxServer(t)
	c := newURLLinkClient(f)
	if _, err := c.Generate(context.Background(), "appid1", "secret1", URLLinkParams{Path: "p", EnvVersion: "trial"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	links := f.linkCalls()
	if len(links) != 1 || links[0].Body.EnvVersion != "trial" {
		t.Fatalf("link body = %+v, want env_version=trial", links)
	}
}

func TestURLLinkTokenCachedAcrossCalls(t *testing.T) {
	f := newFakeWxServer(t)
	c := newURLLinkClient(f)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := c.Generate(ctx, "appid1", "secret1", URLLinkParams{Path: "p"}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	tokenCalls, _ := f.counts()
	if tokenCalls != 1 {
		t.Fatalf("token calls = %d, want 1 (cache must serve repeats)", tokenCalls)
	}
	if links := f.linkCalls(); len(links) != 3 {
		t.Fatalf("link calls = %d, want 3", len(links))
	}

	// A second appid gets its own token: the cache is keyed by appid.
	if _, err := c.Generate(ctx, "appid2", "secret2", URLLinkParams{Path: "p"}); err != nil {
		t.Fatalf("second appid: %v", err)
	}
	if tokenCalls, _ = f.counts(); tokenCalls != 2 {
		t.Fatalf("token calls after second appid = %d, want 2", tokenCalls)
	}
}

func TestURLLinkTokenSingleflight(t *testing.T) {
	f := newFakeWxServer(t)
	c := newURLLinkClient(f)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Generate(context.Background(), "appid1", "secret1", URLLinkParams{Path: "p"})
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

func TestURLLinkToken40001RetriesWithForceRefresh(t *testing.T) {
	f := newFakeWxServer(t)
	f.mu.Lock()
	f.tokenBodies = []string{`{"errcode":40001,"errmsg":"invalid credential"}`, `{"access_token":"TOKEN2","expires_in":7200}`}
	f.mu.Unlock()

	c := newURLLinkClient(f)
	if _, err := c.Generate(context.Background(), "appid1", "secret1", URLLinkParams{Path: "p"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	tokens := f.tokenCalls()
	if len(tokens) != 2 || !tokens[1].ForceRefresh {
		t.Fatalf("token calls = %+v, want a force_refresh retry", tokens)
	}
}

func TestURLLinkCall40001ForceRefreshesTokenAndRetries(t *testing.T) {
	f := newFakeWxServer(t)
	f.mu.Lock()
	// First link call: token TOKEN was invalidated server-side. Retry after
	// a forced mint (TOKEN2) succeeds.
	f.linkBodies = []string{`{"errcode":40001,"errmsg":"invalid token"}`, `{"errcode":0,"url_link":"https://wxaurl.cn/LINK2"}`}
	f.tokenBodies = []string{`{"access_token":"TOKEN1","expires_in":7200}`, `{"access_token":"TOKEN2","expires_in":7200}`}
	f.mu.Unlock()

	c := newURLLinkClient(f)
	link, err := c.Generate(context.Background(), "appid1", "secret1", URLLinkParams{Path: "p"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if link != "https://wxaurl.cn/LINK2" {
		t.Fatalf("link = %q", link)
	}
	tokens := f.tokenCalls()
	if len(tokens) != 2 || !tokens[1].ForceRefresh {
		t.Fatalf("token calls = %+v, want one forced refresh", tokens)
	}
	links := f.linkCalls()
	if len(links) != 2 || links[0].AccessToken != "TOKEN1" || links[1].AccessToken != "TOKEN2" {
		t.Fatalf("link calls = %+v, want retry with the refreshed token", links)
	}
}

func TestURLLinkErrorMapping(t *testing.T) {
	cases := []struct {
		body    string
		status  int
		errType string
	}{
		{`{"errcode":40165,"errmsg":"invalid path"}`, 400, "wechat_invalid_page"},
		{`{"errcode":40212,"errmsg":"invalid query"}`, 400, "wechat_invalid_query"},
		{`{"errcode":44990,"errmsg":"qps limit"}`, 429, "wechat_rate_limited"},
		{`{"errcode":44993,"errmsg":"daily quota"}`, 429, "wechat_rate_limited"},
		{`{"errcode":76543,"errmsg":"other"}`, 502, "wechat_api_error"},
	}
	for _, tc := range cases {
		f := newFakeWxServer(t)
		f.mu.Lock()
		f.linkBodies = []string{tc.body}
		f.mu.Unlock()
		c := newURLLinkClient(f)
		_, err := c.Generate(context.Background(), "appid1", "secret1", URLLinkParams{Path: "p"})
		ae, ok := err.(*apperror.Error)
		if !ok {
			t.Fatalf("body %s: expected *apperror.Error, got %v", tc.body, err)
		}
		if ae.Status != tc.status || ae.Type != tc.errType {
			t.Fatalf("body %s: got (%d, %s), want (%d, %s)", tc.body, ae.Status, ae.Type, tc.status, tc.errType)
		}
	}
}

func TestURLLinkMalformedResponses(t *testing.T) {
	// JSON success body without url_link.
	f := newFakeWxServer(t)
	f.mu.Lock()
	f.linkBodies = []string{`{"errcode":0,"errmsg":"ok"}`}
	f.mu.Unlock()
	c := newURLLinkClient(f)
	if _, err := c.Generate(context.Background(), "appid1", "secret1", URLLinkParams{Path: "p"}); err == nil {
		t.Fatal("expected error for link response without url_link")
	}

	// Token endpoint answering success without access_token.
	f2 := newFakeWxServer(t)
	f2.mu.Lock()
	f2.tokenBodies = []string{`{"expires_in":7200}`}
	f2.mu.Unlock()
	c2 := newURLLinkClient(f2)
	if _, err := c2.Generate(context.Background(), "appid1", "secret1", URLLinkParams{Path: "p"}); err == nil {
		t.Fatal("expected error for token response without access_token")
	}
}

func TestURLLinkNotConfigured(t *testing.T) {
	f := newFakeWxServer(t)
	c := newURLLinkClient(f)
	_, err := c.Generate(context.Background(), "", "secret", URLLinkParams{Path: "p"})
	ae, ok := err.(*apperror.Error)
	if !ok || ae.Type != "wechat_not_configured" {
		t.Fatalf("got %v, want wechat_not_configured", err)
	}
	if len(f.linkCalls()) != 0 {
		t.Fatal("no WeChat call may happen without credentials")
	}
}

// The fake server must also reject unknown paths so a miswired client URL
// surfaces as an error instead of a silent success.
func TestURLLinkWrongEndpointFails(t *testing.T) {
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)
	c := NewURLLinkClient(srv.URL+"/cgi-bin/stable_token", srv.URL+"/wxa/generate_urllink")
	if _, err := c.Generate(context.Background(), "appid1", "secret1", URLLinkParams{Path: "p"}); err == nil || !strings.Contains(err.Error(), "WeChat API error") {
		t.Fatalf("got %v, want a WeChat API error for the 404 endpoint", err)
	}
}
