package server_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/config"
	"github.com/zhaochy1990/auth-service/internal/domain"
	mysqlrepo "github.com/zhaochy1990/auth-service/internal/repository/mysql"
	"github.com/zhaochy1990/auth-service/internal/seed"
	"github.com/zhaochy1990/auth-service/internal/server"
)

const defaultTestMySQLDSN = "mysql://auth:auth_password@127.0.0.1:3306/auth_test"

func testMySQLDSN() string {
	if v := os.Getenv("TEST_MYSQL_DSN"); v != "" {
		return v
	}
	return defaultTestMySQLDSN
}

func explicitTestMySQLDSN() bool { return os.Getenv("TEST_MYSQL_DSN") != "" }

func init() { gin.SetMode(gin.TestMode) }

type testApp struct {
	t            *testing.T
	repo         *mysqlrepo.Repository
	engine       *gin.Engine
	cfg          *config.Config
	jwt          *auth.JWTManager
	clientID     string
	clientSecret string
	adminUserID  string
	adminToken   string
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	ctx := context.Background()

	repo, err := mysqlrepo.New(ctx, testMySQLDSN())
	if err != nil {
		if explicitTestMySQLDSN() {
			t.Fatalf("MySQL unavailable (NewRepository): %v", err)
		}
		t.Skipf("MySQL unavailable (NewRepository): %v", err)
	}
	if err := repo.ClearAllTables(ctx); err != nil {
		if explicitTestMySQLDSN() {
			t.Fatalf("MySQL unavailable (ClearAllTables): %v", err)
		}
		t.Skipf("MySQL unavailable (ClearAllTables): %v", err)
	}
	privateKeyPath, publicKeyPath := writeTestKeys(t)

	cfg := &config.Config{
		StorageBackend:            config.StorageBackendMySQL,
		MySQLDSN:                  testMySQLDSN(),
		JWTPrivateKeyPath:         privateKeyPath,
		JWTPublicKeyPath:          publicKeyPath,
		JWTIssuer:                 "auth-service",
		JWTAccessTokenExpirySecs:  3600,
		JWTRefreshTokenExpiryDays: 30,
		CORSAllowedOrigins:        "*",
		EnableTestProviders:       true,
	}
	jwtMgr, err := auth.NewJWTManager(cfg)
	if err != nil {
		t.Fatalf("jwt manager: %v", err)
	}

	pw := "AdminPass1!"
	res, err := seed.Bootstrap(ctx, repo, "test-admin@internal", &pw)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	adminUser, err := repo.Users().FindByEmail(ctx, "test-admin@internal")
	if err != nil || adminUser == nil {
		t.Fatalf("find admin: %v", err)
	}
	adminToken, err := jwtMgr.IssueAccessToken(adminUser.ID, res.AppClientID, []string{"admin"}, "admin", domain.MembershipRegular, nil)
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}

	secret := ""
	if res.AppClientSecret != nil {
		secret = *res.AppClientSecret
	}

	return &testApp{
		t:            t,
		repo:         repo,
		engine:       server.NewRouter(repo, jwtMgr, cfg),
		cfg:          cfg,
		jwt:          jwtMgr,
		clientID:     res.AppClientID,
		clientSecret: secret,
		adminUserID:  adminUser.ID,
		adminToken:   adminToken,
	}
}

func writeTestKeys(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	dir := t.TempDir()
	privatePath := filepath.Join(dir, "private.pem")
	publicPath := filepath.Join(dir, "public.pem")
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	if err := os.WriteFile(privatePath, privatePEM, 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	if err := os.WriteFile(publicPath, publicPEM, 0o600); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	return privatePath, publicPath
}

func (ta *testApp) do(method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	ta.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	ta.engine.ServeHTTP(w, req)
	return w
}

func (ta *testApp) clientHeaders() map[string]string {
	return map[string]string{"X-Client-Id": ta.clientID}
}

func (ta *testApp) bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func decode(t *testing.T, w *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response (%d): %v\nbody: %s", w.Code, err, w.Body.String())
	}
}

func mustStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d\nbody: %s", w.Code, want, w.Body.String())
	}
}

func basicAuth(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// --- Tests ---

func TestHealth(t *testing.T) {
	ta := newTestApp(t)
	w := ta.do(http.MethodGet, "/health", nil, nil)
	mustStatus(t, w, http.StatusOK)
	var body map[string]any
	decode(t, w, &body)
	if body["status"] != "ok" {
		t.Fatalf("status field = %v", body["status"])
	}
}

func TestRegisterLoginRefreshLogout(t *testing.T) {
	ta := newTestApp(t)

	reg := ta.do(http.MethodPost, "/api/auth/register", map[string]any{
		"email": "u1@example.com", "password": "Password1!", "name": "U1",
	}, ta.clientHeaders())
	mustStatus(t, reg, http.StatusCreated)
	var regResp struct {
		UserID       string `json:"user_id"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	decode(t, reg, &regResp)
	if regResp.AccessToken == "" || regResp.RefreshToken == "" || regResp.UserID == "" {
		t.Fatalf("register missing tokens: %+v", regResp)
	}
	if regResp.TokenType != "Bearer" || regResp.ExpiresIn != 3600 {
		t.Fatalf("unexpected token meta: %+v", regResp)
	}

	dup := ta.do(http.MethodPost, "/api/auth/register", map[string]any{
		"email": "u1@example.com", "password": "Password1!",
	}, ta.clientHeaders())
	mustStatus(t, dup, http.StatusConflict)

	login := ta.do(http.MethodPost, "/api/auth/login", map[string]any{
		"email": "u1@example.com", "password": "Password1!",
	}, ta.clientHeaders())
	mustStatus(t, login, http.StatusOK)
	var loginResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	decode(t, login, &loginResp)

	bad := ta.do(http.MethodPost, "/api/auth/login", map[string]any{
		"email": "u1@example.com", "password": "WrongPass1!",
	}, ta.clientHeaders())
	mustStatus(t, bad, http.StatusUnauthorized)

	refresh := ta.do(http.MethodPost, "/api/auth/refresh", map[string]any{
		"refresh_token": loginResp.RefreshToken,
	}, ta.clientHeaders())
	mustStatus(t, refresh, http.StatusOK)
	var refreshResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	decode(t, refresh, &refreshResp)
	if refreshResp.RefreshToken == loginResp.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}

	reuse := ta.do(http.MethodPost, "/api/auth/refresh", map[string]any{
		"refresh_token": loginResp.RefreshToken,
	}, ta.clientHeaders())
	mustStatus(t, reuse, http.StatusUnauthorized)

	logout := ta.do(http.MethodPost, "/api/auth/logout", map[string]any{
		"refresh_token": refreshResp.RefreshToken,
	}, ta.bearer(refreshResp.AccessToken))
	mustStatus(t, logout, http.StatusOK)
}

func TestMissingClientID(t *testing.T) {
	ta := newTestApp(t)
	w := ta.do(http.MethodPost, "/api/auth/login", map[string]any{
		"email": "x@example.com", "password": "Password1!",
	}, nil)
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "missing_client_id" {
		t.Fatalf("error = %v", body["error"])
	}
}

func TestUserProfile(t *testing.T) {
	ta := newTestApp(t)
	reg := ta.do(http.MethodPost, "/api/auth/register", map[string]any{
		"email": "prof@example.com", "password": "Password1!", "name": "Prof",
	}, ta.clientHeaders())
	mustStatus(t, reg, http.StatusCreated)
	var regResp struct {
		AccessToken string `json:"access_token"`
	}
	decode(t, reg, &regResp)

	me := ta.do(http.MethodGet, "/api/users/me", nil, ta.bearer(regResp.AccessToken))
	mustStatus(t, me, http.StatusOK)
	var prof struct {
		Email      string `json:"email"`
		Name       string `json:"name"`
		Membership string `json:"membership"`
	}
	decode(t, me, &prof)
	if prof.Email != "prof@example.com" || prof.Membership != "regular" {
		t.Fatalf("unexpected profile: %+v", prof)
	}

	noauth := ta.do(http.MethodGet, "/api/users/me", nil, nil)
	mustStatus(t, noauth, http.StatusUnauthorized)
}

func TestAdminApplications(t *testing.T) {
	ta := newTestApp(t)

	create := ta.do(http.MethodPost, "/admin/applications", map[string]any{
		"name": "My App", "redirect_uris": []string{"https://app.example.com/cb"}, "allowed_scopes": []string{"openid", "profile"},
	}, ta.bearer(ta.adminToken))
	mustStatus(t, create, http.StatusOK)
	var app struct {
		ID           string   `json:"id"`
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	decode(t, create, &app)
	if app.ClientSecret == "" || app.ClientID == "" || len(app.RedirectURIs) != 1 {
		t.Fatalf("unexpected app: %+v", app)
	}

	list := ta.do(http.MethodGet, "/admin/applications", nil, ta.bearer(ta.adminToken))
	mustStatus(t, list, http.StatusOK)
	var apps []map[string]any
	decode(t, list, &apps)
	if len(apps) < 2 {
		t.Fatalf("expected >=2 apps, got %d", len(apps))
	}

	noauth := ta.do(http.MethodGet, "/admin/applications", nil, nil)
	if noauth.Code != http.StatusUnauthorized && noauth.Code != http.StatusForbidden {
		t.Fatalf("expected 401/403, got %d", noauth.Code)
	}
}

func TestAdminUsersCRUD(t *testing.T) {
	ta := newTestApp(t)

	create := ta.do(http.MethodPost, "/admin/users", map[string]any{
		"email": "managed@example.com", "password": "Password1!", "name": "Managed", "role": "user", "membership": "vip1",
	}, ta.bearer(ta.adminToken))
	mustStatus(t, create, http.StatusOK)
	var u struct {
		ID         string `json:"id"`
		Role       string `json:"role"`
		Membership string `json:"membership"`
	}
	decode(t, create, &u)
	if u.Role != "user" || u.Membership != "vip1" {
		t.Fatalf("unexpected created user: %+v", u)
	}

	get := ta.do(http.MethodGet, "/admin/users/"+u.ID, nil, ta.bearer(ta.adminToken))
	mustStatus(t, get, http.StatusOK)

	upd := ta.do(http.MethodPatch, "/admin/users/"+u.ID, map[string]any{
		"membership": "regular", "is_active": false,
	}, ta.bearer(ta.adminToken))
	mustStatus(t, upd, http.StatusOK)
	var updated struct {
		Membership string `json:"membership"`
		IsActive   bool   `json:"is_active"`
	}
	decode(t, upd, &updated)
	if updated.Membership != "regular" || updated.IsActive {
		t.Fatalf("update not applied: %+v", updated)
	}

	del := ta.do(http.MethodDelete, "/admin/users/"+u.ID, nil, ta.bearer(ta.adminToken))
	mustStatus(t, del, http.StatusNoContent)

	getGone := ta.do(http.MethodGet, "/admin/users/"+u.ID, nil, ta.bearer(ta.adminToken))
	mustStatus(t, getGone, http.StatusNotFound)
}

func TestInviteCodeGatingAndMembershipGrant(t *testing.T) {
	ta := newTestApp(t)

	mk := ta.do(http.MethodPost, "/admin/invite-codes?grants_membership=vip1&grants_membership_days=30", nil, ta.bearer(ta.adminToken))
	mustStatus(t, mk, http.StatusOK)
	var code struct {
		Code             string `json:"code"`
		Kind             string `json:"kind"`
		GrantsMembership string `json:"grants_membership"`
	}
	decode(t, mk, &code)
	if code.Code == "" || code.Kind != "single_use" || code.GrantsMembership != "vip1" {
		t.Fatalf("unexpected invite code: %+v", code)
	}

	t.Setenv("STRIDE_REQUIRE_INVITE_CODE", "true")

	noCode := ta.do(http.MethodPost, "/api/auth/register", map[string]any{
		"email": "gated@example.com", "password": "Password1!",
	}, ta.clientHeaders())
	mustStatus(t, noCode, http.StatusBadRequest)

	ok := ta.do(http.MethodPost, "/api/auth/register", map[string]any{
		"email": "gated@example.com", "password": "Password1!", "invite_code": code.Code,
	}, ta.clientHeaders())
	mustStatus(t, ok, http.StatusCreated)
	var reg struct {
		AccessToken string `json:"access_token"`
	}
	decode(t, ok, &reg)

	me := ta.do(http.MethodGet, "/api/users/me", nil, ta.bearer(reg.AccessToken))
	mustStatus(t, me, http.StatusOK)
	var prof struct {
		Membership          string  `json:"membership"`
		MembershipExpiresAt *string `json:"membership_expires_at"`
	}
	decode(t, me, &prof)
	if prof.Membership != "vip1" || prof.MembershipExpiresAt == nil {
		t.Fatalf("membership grant not applied: %+v", prof)
	}

	reuse := ta.do(http.MethodPost, "/api/auth/register", map[string]any{
		"email": "other@example.com", "password": "Password1!", "invite_code": code.Code,
	}, ta.clientHeaders())
	mustStatus(t, reuse, http.StatusConflict)
}

func TestTeamsFlow(t *testing.T) {
	ta := newTestApp(t)

	tokA := ta.registerUser(t, "owner@example.com")
	tokB := ta.registerUser(t, "member@example.com")

	create := ta.do(http.MethodPost, "/api/teams", map[string]any{"name": "Squad", "description": "desc"}, ta.bearer(tokA))
	mustStatus(t, create, http.StatusOK)
	var team struct {
		ID          string `json:"id"`
		MemberCount uint64 `json:"member_count"`
	}
	decode(t, create, &team)
	if team.MemberCount != 1 {
		t.Fatalf("expected 1 member, got %d", team.MemberCount)
	}

	join := ta.do(http.MethodPost, "/api/teams/"+team.ID+"/join", nil, ta.bearer(tokB))
	mustStatus(t, join, http.StatusOK)

	get := ta.do(http.MethodGet, "/api/teams/"+team.ID, nil, ta.bearer(tokB))
	mustStatus(t, get, http.StatusOK)
	var got struct {
		MemberCount uint64 `json:"member_count"`
	}
	decode(t, get, &got)
	if got.MemberCount != 2 {
		t.Fatalf("expected 2 members, got %d", got.MemberCount)
	}

	leave := ta.do(http.MethodPost, "/api/teams/"+team.ID+"/leave", nil, ta.bearer(tokB))
	mustStatus(t, leave, http.StatusOK)

	del := ta.do(http.MethodDelete, "/api/teams/"+team.ID, nil, ta.bearer(tokA))
	mustStatus(t, del, http.StatusOK)
}

func TestOAuth2PasswordGrantAndIntrospect(t *testing.T) {
	ta := newTestApp(t)
	if ta.clientSecret == "" {
		t.Skip("client secret not available")
	}

	create := ta.do(http.MethodPost, "/admin/users", map[string]any{
		"email": "oauthuser@example.com", "password": "Password1!", "role": "user",
	}, ta.bearer(ta.adminToken))
	mustStatus(t, create, http.StatusOK)

	basic := basicAuth(ta.clientID, ta.clientSecret)
	tok := ta.do(http.MethodPost, "/oauth/token", map[string]any{
		"grant_type": "password", "username": "oauthuser@example.com", "password": "Password1!",
	}, map[string]string{"Authorization": basic})
	mustStatus(t, tok, http.StatusOK)
	var tr struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	decode(t, tok, &tr)
	if tr.AccessToken == "" || tr.TokenType != "Bearer" {
		t.Fatalf("bad token resp: %+v", tr)
	}

	intr := ta.do(http.MethodPost, "/oauth/introspect", map[string]any{"token": tr.AccessToken}, map[string]string{"Authorization": basic})
	mustStatus(t, intr, http.StatusOK)
	var ir struct {
		Active bool `json:"active"`
	}
	decode(t, intr, &ir)
	if !ir.Active {
		t.Fatal("expected active token")
	}

	intr2 := ta.do(http.MethodPost, "/oauth/introspect", map[string]any{"token": "garbage"}, map[string]string{"Authorization": basic})
	mustStatus(t, intr2, http.StatusOK)
	var ir2 struct {
		Active bool `json:"active"`
	}
	decode(t, intr2, &ir2)
	if ir2.Active {
		t.Fatal("expected inactive token")
	}

	badBasic := basicAuth(ta.clientID, "wrongsecret")
	badTok := ta.do(http.MethodPost, "/oauth/token", map[string]any{
		"grant_type": "password", "username": "oauthuser@example.com", "password": "Password1!",
	}, map[string]string{"Authorization": badBasic})
	mustStatus(t, badTok, http.StatusUnauthorized)
}

func TestProviderLoginTestProvider(t *testing.T) {
	ta := newTestApp(t)
	ctx := context.Background()

	app, err := ta.repo.Applications().FindByClientID(ctx, ta.clientID)
	if err != nil || app == nil {
		t.Fatalf("find app: %v", err)
	}
	add := ta.do(http.MethodPost, "/admin/applications/"+app.ID+"/providers", map[string]any{
		"provider_id": "test", "config": map[string]any{},
	}, ta.bearer(ta.adminToken))
	mustStatus(t, add, http.StatusOK)

	login := ta.do(http.MethodPost, "/api/auth/provider/test/login", map[string]any{
		"credential": map[string]any{"account_id": "acct-1", "email": "tp@example.com", "name": "TP"},
	}, ta.clientHeaders())
	mustStatus(t, login, http.StatusOK)
	var tr struct {
		AccessToken string `json:"access_token"`
	}
	decode(t, login, &tr)
	if tr.AccessToken == "" {
		t.Fatal("expected access token from provider login")
	}

	login2 := ta.do(http.MethodPost, "/api/auth/provider/test/login", map[string]any{
		"credential": map[string]any{"account_id": "acct-1"},
	}, ta.clientHeaders())
	mustStatus(t, login2, http.StatusOK)
}

func (ta *testApp) registerUser(t *testing.T, email string) string {
	t.Helper()
	w := ta.do(http.MethodPost, "/api/auth/register", map[string]any{
		"email": email, "password": "Password1!",
	}, ta.clientHeaders())
	mustStatus(t, w, http.StatusCreated)
	var r struct {
		AccessToken string `json:"access_token"`
	}
	decode(t, w, &r)
	return r.AccessToken
}
