package brandoauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// --- Registry ---

func TestRegistryLookup(t *testing.T) {
	reg := NewRegistry(corosDescriptor)
	if _, ok := reg.Lookup("coros"); !ok {
		t.Fatal("coros should be registered")
	}
	if _, ok := reg.Lookup("strava"); ok {
		t.Fatal("strava should not be registered")
	}
}

func TestDefaultRegistryHasCoros(t *testing.T) {
	if _, ok := Default().Lookup("coros"); !ok {
		t.Fatal("default registry must contain coros")
	}
}

// --- Config parsing ---

func TestParseConfig(t *testing.T) {
	cfg, err := ParseConfig(`{"client_id":"cid","client_secret":"sec","scopes":["a","b"],"base_url":"https://sandbox.example","authorize_params":{"prompt":"consent"},"mock":true}`)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.ClientID != "cid" || cfg.ClientSecret != "sec" {
		t.Fatalf("credentials = %q/%q", cfg.ClientID, cfg.ClientSecret)
	}
	if cfg.BaseURL != "https://sandbox.example" {
		t.Fatalf("base_url = %q", cfg.BaseURL)
	}
	if len(cfg.Scopes) != 2 || cfg.Scopes[0] != "a" {
		t.Fatalf("scopes = %v", cfg.Scopes)
	}
	if cfg.AuthorizeParams["prompt"] != "consent" {
		t.Fatalf("authorize_params = %v", cfg.AuthorizeParams)
	}
	if !cfg.Mock {
		t.Fatal("mock should be true")
	}
}

func TestParseConfigRejectsMissingCredentials(t *testing.T) {
	if _, err := ParseConfig(`{"client_id":""}`); err == nil {
		t.Fatal("empty client_id should be rejected")
	}
	if _, err := ParseConfig(`not json`); err == nil {
		t.Fatal("malformed config should be rejected")
	}
}

// --- Authorize URL ---

func TestAuthorizeURL(t *testing.T) {
	c := NewClient(corosDescriptor, Config{ClientID: "cid", ClientSecret: "sec"}, false, nil)
	got := c.AuthorizeURL("https://app.example/cb", "state-123")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	if !strings.HasPrefix(got, "https://open.coros.com/oauth/authorize?") {
		t.Fatalf("authorize url base = %q", got)
	}
	q := u.Query()
	if q.Get("client_id") != "cid" {
		t.Fatalf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "https://app.example/cb" {
		t.Fatalf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("state") != "state-123" {
		t.Fatalf("state = %q", q.Get("state"))
	}
	if q.Get("response_type") != "code" {
		t.Fatalf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("scope") != "openid profile" {
		t.Fatalf("scope = %q", q.Get("scope"))
	}
}

func TestAuthorizeURLCarriesAuthorizeParamsAndSandboxBase(t *testing.T) {
	c := NewClient(corosDescriptor, Config{
		ClientID: "cid", ClientSecret: "sec",
		BaseURL:         "https://opentest.coros.com",
		AuthorizeParams: map[string]string{"prompt": "consent"},
	}, false, nil)
	u, _ := url.Parse(c.AuthorizeURL("https://app.example/cb", "s"))
	if !strings.HasPrefix(u.String(), "https://opentest.coros.com/oauth/authorize?") {
		t.Fatalf("sandbox base not used: %q", u.String())
	}
	if u.Query().Get("prompt") != "consent" {
		t.Fatalf("authorize_params not merged: %q", u.String())
	}
}

// --- Exchange ---

func newBrandServer(t *testing.T, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestExchangeCodeFormAuthAndCamelCaseFields(t *testing.T) {
	var gotClientID, gotSecret, gotCode, gotRedirect string
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotClientID = r.PostForm.Get("client_id")
		gotSecret = r.PostForm.Get("client_secret")
		gotCode = r.PostForm.Get("code")
		gotRedirect = r.PostForm.Get("redirect_uri")
		if r.Header.Get("Authorization") != "" {
			t.Errorf("form auth must not send an Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"at","refreshToken":"rt","expiresIn":3600,"openId":"coros-user-1"}`))
	})
	srv := newBrandServer(t, mux)

	c := NewClient(corosDescriptor, Config{ClientID: "cid", ClientSecret: "sec", BaseURL: srv.URL}, false, srv.Client())
	tok, err := c.ExchangeCode(context.Background(), "the-code", "https://app.example/cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if gotClientID != "cid" || gotSecret != "sec" {
		t.Fatalf("form credentials = %q/%q", gotClientID, gotSecret)
	}
	if gotCode != "the-code" || gotRedirect != "https://app.example/cb" {
		t.Fatalf("code/redirect = %q/%q", gotCode, gotRedirect)
	}
	if tok.ProviderAccountID != "coros-user-1" {
		t.Fatalf("provider account id = %q", tok.ProviderAccountID)
	}
	if tok.AccessToken != "at" || tok.RefreshToken != "rt" {
		t.Fatalf("tokens = %q/%q", tok.AccessToken, tok.RefreshToken)
	}
	if tok.ExpiresAt == nil || time.Until(*tok.ExpiresAt) < 59*time.Minute {
		t.Fatalf("expires at = %v", tok.ExpiresAt)
	}
}

func TestExchangeCodeBasicAuth(t *testing.T) {
	desc := corosDescriptor
	desc.ClientAuthStyle = ClientAuthBasic
	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = r.ParseForm()
		if r.PostForm.Get("client_id") != "" {
			t.Errorf("basic auth must not put client_id in the body")
		}
		_, _ = w.Write([]byte(`{"accessToken":"at","refreshToken":"rt","expiresIn":3600,"openId":"u1"}`))
	})
	srv := newBrandServer(t, mux)

	c := NewClient(desc, Config{ClientID: "cid", ClientSecret: "sec", BaseURL: srv.URL}, false, srv.Client())
	if _, err := c.ExchangeCode(context.Background(), "code", "https://app.example/cb"); err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Fatalf("Authorization = %q, want Basic", gotAuth)
	}
}

func TestExchangeCodeEpochMillisExpiry(t *testing.T) {
	desc := corosDescriptor
	desc.Token.ExpiresInField = "expiresAt"
	desc.Token.ExpiresInFormat = ExpiryEpochMillis
	want := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Millisecond)
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(map[string]any{
			"accessToken": "at", "refreshToken": "rt",
			"expiresAt": want.UnixMilli(), "openId": "u1",
		})
		_, _ = w.Write(body)
	})
	srv := newBrandServer(t, mux)

	c := NewClient(desc, Config{ClientID: "cid", ClientSecret: "sec", BaseURL: srv.URL}, false, srv.Client())
	tok, err := c.ExchangeCode(context.Background(), "code", "https://app.example/cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.ExpiresAt == nil || !tok.ExpiresAt.Equal(want) {
		t.Fatalf("expires at = %v, want %v", tok.ExpiresAt, want)
	}
}

func TestExchangeCodeRFC3339Expiry(t *testing.T) {
	desc := corosDescriptor
	desc.Token.ExpiresInField = "expireTime"
	desc.Token.ExpiresInFormat = ExpiryRFC3339
	want := time.Now().Add(90 * time.Minute).UTC().Truncate(time.Second)
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(map[string]any{
			"accessToken": "at", "refreshToken": "rt",
			"expireTime": want.Format(time.RFC3339), "openId": "u1",
		})
		_, _ = w.Write(body)
	})
	srv := newBrandServer(t, mux)

	c := NewClient(desc, Config{ClientID: "cid", ClientSecret: "sec", BaseURL: srv.URL}, false, srv.Client())
	tok, err := c.ExchangeCode(context.Background(), "code", "https://app.example/cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.ExpiresAt == nil || !tok.ExpiresAt.Equal(want) {
		t.Fatalf("expires at = %v, want %v", tok.ExpiresAt, want)
	}
}

func TestExchangeCodeFetchesUserInfoWhenTokenHasNoUserID(t *testing.T) {
	desc := corosDescriptor
	desc.Token.UserIDField = ""
	desc.UserInfoIDPath = "data.user.id"
	var gotUserInfoAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accessToken":"at","refreshToken":"rt","expiresIn":3600}`))
	})
	mux.HandleFunc("/oauth/userinfo", func(w http.ResponseWriter, r *http.Request) {
		gotUserInfoAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":{"user":{"id":"nested-user"}}}`))
	})
	srv := newBrandServer(t, mux)

	c := NewClient(desc, Config{ClientID: "cid", ClientSecret: "sec", BaseURL: srv.URL}, false, srv.Client())
	tok, err := c.ExchangeCode(context.Background(), "code", "https://app.example/cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.ProviderAccountID != "nested-user" {
		t.Fatalf("provider account id = %q", tok.ProviderAccountID)
	}
	if gotUserInfoAuth != "Bearer at" {
		t.Fatalf("userinfo Authorization = %q", gotUserInfoAuth)
	}
	if len(tok.RawIdentityResponse) == 0 {
		t.Fatal("raw identity response must be preserved")
	}
}

func TestExchangeCodeMissingUserInfoIDErrors(t *testing.T) {
	desc := corosDescriptor
	desc.Token.UserIDField = ""
	desc.UserInfoIDPath = "data.user.id"
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accessToken":"at","refreshToken":"rt"}`))
	})
	mux.HandleFunc("/oauth/userinfo", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{}}`))
	})
	srv := newBrandServer(t, mux)

	c := NewClient(desc, Config{ClientID: "cid", ClientSecret: "sec", BaseURL: srv.URL}, false, srv.Client())
	if _, err := c.ExchangeCode(context.Background(), "code", "https://app.example/cb"); err == nil {
		t.Fatal("missing stable user id must be an error")
	}
}

func TestExchangeCodeProviderErrorStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"bad code"}`))
	})
	srv := newBrandServer(t, mux)

	c := NewClient(corosDescriptor, Config{ClientID: "cid", ClientSecret: "sec", BaseURL: srv.URL}, false, srv.Client())
	_, err := c.ExchangeCode(context.Background(), "code", "https://app.example/cb")
	if err == nil {
		t.Fatal("provider error must surface")
	}
	if !strings.Contains(err.Error(), "bad code") {
		t.Fatalf("error = %v, want provider detail", err)
	}
}

// --- Deauthorize ---

func TestDeauthorizePostsRefreshToken(t *testing.T) {
	var gotBody url.Values
	desc := corosDescriptor
	desc.ClientAuthStyle = ClientAuthForm
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/deauthorize", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotBody = r.PostForm
		_, _ = w.Write([]byte(`{}`))
	})
	srv := newBrandServer(t, mux)

	c := NewClient(desc, Config{ClientID: "cid", ClientSecret: "sec", BaseURL: srv.URL}, false, srv.Client())
	if err := c.Deauthorize(context.Background(), "rt"); err != nil {
		t.Fatalf("Deauthorize: %v", err)
	}
	if gotBody.Get("token") != "rt" || gotBody.Get("client_id") != "cid" {
		t.Fatalf("deauthorize body = %v", gotBody)
	}
}

func TestDeauthorizeWithoutEndpointIsNoop(t *testing.T) {
	desc := corosDescriptor
	desc.DeauthorizePath = ""
	called := false
	srv := newBrandServer(t, http.NewServeMux())
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return srv.Client().Transport.RoundTrip(r)
	})}
	c := NewClient(desc, Config{ClientID: "cid", ClientSecret: "sec", BaseURL: srv.URL}, false, httpClient)
	if err := c.Deauthorize(context.Background(), "rt"); err != nil {
		t.Fatalf("Deauthorize: %v", err)
	}
	if called {
		t.Fatal("no HTTP call expected when the descriptor has no deauthorize path")
	}
}

// --- Mock ---

func TestMockModeZeroNetwork(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("mock mode must not perform HTTP requests (%s)", r.URL)
		return nil, nil
	})}
	c := NewClient(corosDescriptor, Config{ClientID: "cid", ClientSecret: "sec", Mock: true}, true, httpClient)

	if got := c.AuthorizeURL("https://app.example/cb", "s"); !strings.Contains(got, "state=s") {
		t.Fatalf("mock authorize url = %q", got)
	}
	tok, err := c.ExchangeCode(context.Background(), "code-1", "https://app.example/cb")
	if err != nil {
		t.Fatalf("mock ExchangeCode: %v", err)
	}
	if tok.ProviderAccountID == "" || tok.AccessToken == "" {
		t.Fatalf("mock token incomplete: %+v", tok)
	}
	if err := c.Deauthorize(context.Background(), "rt"); err != nil {
		t.Fatalf("mock Deauthorize: %v", err)
	}
}

func TestMockRequiresMasterSwitch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accessToken":"real","refreshToken":"rt","expiresIn":60,"openId":"real-user"}`))
	})
	srv := newBrandServer(t, mux)
	// Provider config says mock, but the master switch is off → real HTTP.
	c := NewClient(corosDescriptor, Config{ClientID: "cid", ClientSecret: "sec", Mock: true, BaseURL: srv.URL}, false, srv.Client())
	tok, err := c.ExchangeCode(context.Background(), "code", "https://app.example/cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "real" {
		t.Fatalf("expected real HTTP response, got %+v", tok)
	}
}

// --- Metadata ---

func TestTokenMetadataPreservesRawResponses(t *testing.T) {
	tok := &Token{
		AccessToken:         "at",
		Scopes:              []string{"openid"},
		ExpiresAt:           ptrTime(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)),
		RawTokenResponse:    json.RawMessage(`{"accessToken":"at"}`),
		RawIdentityResponse: json.RawMessage(`{"openId":"u1"}`),
	}
	raw := tok.Metadata()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("metadata json: %v", err)
	}
	if m["access_token"] != "at" {
		t.Fatalf("access_token = %v", m["access_token"])
	}
	if m["scope"] != "openid" {
		t.Fatalf("scope = %v", m["scope"])
	}
	if _, ok := m["refresh_token"]; ok {
		t.Fatal("refresh token must not be duplicated into metadata")
	}
	if _, ok := m["raw_token_response"]; !ok {
		t.Fatal("raw token response must be preserved")
	}
	if _, ok := m["raw_identity_response"]; !ok {
		t.Fatal("raw identity response must be preserved")
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
