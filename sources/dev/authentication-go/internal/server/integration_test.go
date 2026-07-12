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
	"strconv"
	"testing"
	"time"

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
	privateKeyPath, publicKeyPath := writeTestKeyPair(t)

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
	adminToken, err := jwtMgr.IssueAccessToken(adminUser.ID, res.AppClientID, []string{"admin"}, "admin", domain.MembershipRegular, domain.UserTypeRegular, nil)
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

	patch := ta.do(http.MethodPatch, "/api/users/me", map[string]any{
		"custom_attributes": map[string]any{
			"birthday":  "1990-01-01",
			"gender":    "female",
			"height_cm": 168,
			"weight_kg": 55.5,
		},
	}, ta.bearer(regResp.AccessToken))
	mustStatus(t, patch, http.StatusOK)
	var patched struct {
		CustomAttributes map[string]any `json:"custom_attributes"`
	}
	decode(t, patch, &patched)
	if patched.CustomAttributes["birthday"] != "1990-01-01" || patched.CustomAttributes["height_cm"] != float64(168) {
		t.Fatalf("custom attributes not applied: %+v", patched.CustomAttributes)
	}

	patch = ta.do(http.MethodPatch, "/api/users/me", map[string]any{
		"custom_attributes": map[string]any{
			"weight_kg": 56.2,
			"gender":    nil,
		},
	}, ta.bearer(regResp.AccessToken))
	mustStatus(t, patch, http.StatusOK)
	patched = struct {
		CustomAttributes map[string]any `json:"custom_attributes"`
	}{}
	decode(t, patch, &patched)
	if patched.CustomAttributes["birthday"] != "1990-01-01" || patched.CustomAttributes["weight_kg"] != 56.2 {
		t.Fatalf("custom attributes not merged: %+v", patched.CustomAttributes)
	}
	if _, ok := patched.CustomAttributes["gender"]; ok {
		t.Fatalf("gender should have been removed: %+v", patched.CustomAttributes)
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
		"email": "managed@example.com", "password": "Password1!", "name": "Managed", "role": "user", "membership": "vip1", "user_type": "testing",
		"custom_attributes": map[string]any{
			"birthday":  "1988-08-08",
			"gender":    "male",
			"height_cm": 180,
			"weight_kg": 72.5,
		},
	}, ta.bearer(ta.adminToken))
	mustStatus(t, create, http.StatusOK)
	var u struct {
		ID               string         `json:"id"`
		Role             string         `json:"role"`
		Membership       string         `json:"membership"`
		UserType         string         `json:"user_type"`
		CustomAttributes map[string]any `json:"custom_attributes"`
	}
	decode(t, create, &u)
	if u.Role != "user" || u.Membership != "vip1" || u.UserType != "testing" {
		t.Fatalf("unexpected created user: %+v", u)
	}
	if u.CustomAttributes["birthday"] != "1988-08-08" || u.CustomAttributes["height_cm"] != float64(180) {
		t.Fatalf("custom attributes not created: %+v", u.CustomAttributes)
	}

	get := ta.do(http.MethodGet, "/admin/users/"+u.ID, nil, ta.bearer(ta.adminToken))
	mustStatus(t, get, http.StatusOK)

	filtered := ta.do(http.MethodGet, "/admin/users?user_type=testing", nil, ta.bearer(ta.adminToken))
	mustStatus(t, filtered, http.StatusOK)
	var list struct {
		Users []struct {
			ID       string `json:"id"`
			UserType string `json:"user_type"`
		} `json:"users"`
	}
	decode(t, filtered, &list)
	if len(list.Users) != 1 || list.Users[0].ID != u.ID || list.Users[0].UserType != "testing" {
		t.Fatalf("testing filter did not isolate user: %+v", list.Users)
	}

	upd := ta.do(http.MethodPatch, "/admin/users/"+u.ID, map[string]any{
		"membership": "regular", "is_active": false, "user_type": "regular",
		"custom_attributes": map[string]any{
			"weight_kg": 73.0,
			"gender":    nil,
		},
	}, ta.bearer(ta.adminToken))
	mustStatus(t, upd, http.StatusOK)
	var updated struct {
		Membership       string         `json:"membership"`
		IsActive         bool           `json:"is_active"`
		UserType         string         `json:"user_type"`
		CustomAttributes map[string]any `json:"custom_attributes"`
	}
	decode(t, upd, &updated)
	if updated.Membership != "regular" || updated.IsActive || updated.UserType != "regular" {
		t.Fatalf("update not applied: %+v", updated)
	}
	if updated.CustomAttributes["birthday"] != "1988-08-08" || updated.CustomAttributes["weight_kg"] != float64(73) {
		t.Fatalf("custom attributes not updated: %+v", updated.CustomAttributes)
	}
	if _, ok := updated.CustomAttributes["gender"]; ok {
		t.Fatalf("gender should have been removed: %+v", updated.CustomAttributes)
	}

	del := ta.do(http.MethodDelete, "/admin/users/"+u.ID, nil, ta.bearer(ta.adminToken))
	mustStatus(t, del, http.StatusNoContent)

	getGone := ta.do(http.MethodGet, "/admin/users/"+u.ID, nil, ta.bearer(ta.adminToken))
	mustStatus(t, getGone, http.StatusNotFound)
}

func TestListUsersWithAppTokenAndPagination(t *testing.T) {
	ta := newTestApp(t)
	if ta.clientSecret == "" {
		t.Skip("client secret not available")
	}

	for i := 1; i <= 3; i++ {
		create := ta.do(http.MethodPost, "/admin/users", map[string]any{
			"email": "listed" + strconv.Itoa(i) + "@example.com", "password": "Password1!", "role": "user",
		}, ta.bearer(ta.adminToken))
		mustStatus(t, create, http.StatusOK)
	}

	tok := ta.do(http.MethodPost, "/oauth/token", map[string]any{"grant_type": "client_credentials"}, map[string]string{
		"Authorization": basicAuth(ta.clientID, ta.clientSecret),
	})
	mustStatus(t, tok, http.StatusOK)
	var tr struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	decode(t, tok, &tr)
	if tr.AccessToken == "" || tr.TokenType != "Bearer" {
		t.Fatalf("bad app token response: %+v", tr)
	}

	first := ta.do(http.MethodGet, "/admin/users?page=1&per_page=2", nil, ta.bearer(tr.AccessToken))
	mustStatus(t, first, http.StatusOK)
	var firstPage struct {
		Users []struct {
			ID string `json:"id"`
		} `json:"users"`
		Total   uint64 `json:"total"`
		Page    uint64 `json:"page"`
		PerPage uint64 `json:"per_page"`
	}
	decode(t, first, &firstPage)
	if len(firstPage.Users) != 2 || firstPage.Total != 4 || firstPage.Page != 1 || firstPage.PerPage != 2 {
		t.Fatalf("unexpected first page: %+v", firstPage)
	}

	second := ta.do(http.MethodGet, "/admin/users?page=2&per_page=2", nil, ta.bearer(tr.AccessToken))
	mustStatus(t, second, http.StatusOK)
	var secondPage struct {
		Users []struct {
			ID string `json:"id"`
		} `json:"users"`
		Total   uint64 `json:"total"`
		Page    uint64 `json:"page"`
		PerPage uint64 `json:"per_page"`
	}
	decode(t, second, &secondPage)
	if len(secondPage.Users) != 2 || secondPage.Total != 4 || secondPage.Page != 2 || secondPage.PerPage != 2 {
		t.Fatalf("unexpected second page: %+v", secondPage)
	}
	seen := map[string]bool{}
	for _, u := range firstPage.Users {
		seen[u.ID] = true
	}
	for _, u := range secondPage.Users {
		if seen[u.ID] {
			t.Fatalf("pagination returned duplicate user id %q", u.ID)
		}
	}
}

func writeTestKeyPair(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	dir := t.TempDir()
	privateKeyPath := filepath.Join(dir, "private.pem")
	publicKeyPath := filepath.Join(dir, "public.pem")
	privateBlock := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	publicBlock := &pem.Block{Type: "PUBLIC KEY", Bytes: pub}
	if err := os.WriteFile(privateKeyPath, pem.EncodeToMemory(privateBlock), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	if err := os.WriteFile(publicKeyPath, pem.EncodeToMemory(publicBlock), 0o600); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	return privateKeyPath, publicKeyPath
}

func TestAdminUsersListSortsByNamePinyinBeforePagination(t *testing.T) {
	ta := newTestApp(t)

	for _, item := range []struct {
		email string
		name  string
	}{
		{email: "zhang-sort@example.com", name: "张三"},
		{email: "alice-sort@example.com", name: "Alice"},
		{email: "li-sort@example.com", name: "李四"},
		{email: "an-sort@example.com", name: "安安"},
	} {
		create := ta.do(http.MethodPost, "/admin/users", map[string]any{
			"email": item.email, "password": "Password1!", "name": item.name,
		}, ta.bearer(ta.adminToken))
		mustStatus(t, create, http.StatusOK)
	}

	page1 := ta.do(http.MethodGet, "/admin/users?search=-sort%40example.com&page=1&per_page=2", nil, ta.bearer(ta.adminToken))
	mustStatus(t, page1, http.StatusOK)
	var first struct {
		Total uint64 `json:"total"`
		Users []struct {
			Name string `json:"name"`
		} `json:"users"`
	}
	decode(t, page1, &first)
	if first.Total != 4 || len(first.Users) != 2 || first.Users[0].Name != "Alice" || first.Users[1].Name != "安安" {
		t.Fatalf("unexpected first page: %+v", first)
	}

	page2 := ta.do(http.MethodGet, "/admin/users?search=-sort%40example.com&page=2&per_page=2", nil, ta.bearer(ta.adminToken))
	mustStatus(t, page2, http.StatusOK)
	var second struct {
		Users []struct {
			Name string `json:"name"`
		} `json:"users"`
	}
	decode(t, page2, &second)
	if len(second.Users) != 2 || second.Users[0].Name != "李四" || second.Users[1].Name != "张三" {
		t.Fatalf("unexpected second page: %+v", second)
	}
}

func TestAdminUsersListSortsByLastLogin(t *testing.T) {
	ta := newTestApp(t)

	ta.registerUser(t, "older-login@example.com")
	time.Sleep(5 * time.Millisecond)
	ta.registerUser(t, "newer-login@example.com")

	list := ta.do(http.MethodGet, "/admin/users?search=-login%40example.com&sort_by=last_login_at&sort_order=desc", nil, ta.bearer(ta.adminToken))
	mustStatus(t, list, http.StatusOK)
	var body struct {
		Total uint64 `json:"total"`
		Users []struct {
			Email string `json:"email"`
		} `json:"users"`
	}
	decode(t, list, &body)
	if body.Total != 2 || len(body.Users) != 2 || body.Users[0].Email != "newer-login@example.com" || body.Users[1].Email != "older-login@example.com" {
		t.Fatalf("unexpected last-login order: %+v", body)
	}
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

func TestInviteCodeGrantsTestingUserType(t *testing.T) {
	ta := newTestApp(t)

	mk := ta.do(http.MethodPost, "/admin/invite-codes?kind=long_term&grants_user_type=testing", nil, ta.bearer(ta.adminToken))
	mustStatus(t, mk, http.StatusOK)
	var code struct {
		Code           string  `json:"code"`
		Kind           string  `json:"kind"`
		GrantsUserType *string `json:"grants_user_type"`
	}
	decode(t, mk, &code)
	if code.Code == "" || code.Kind != "long_term" || code.GrantsUserType == nil || *code.GrantsUserType != "testing" {
		t.Fatalf("unexpected invite code: %+v", code)
	}

	t.Setenv("STRIDE_REQUIRE_INVITE_CODE", "true")

	ok := ta.do(http.MethodPost, "/api/auth/register", map[string]any{
		"email": "testing-invite@example.com", "password": "Password1!", "invite_code": code.Code,
	}, ta.clientHeaders())
	mustStatus(t, ok, http.StatusCreated)
	var reg struct {
		UserID      string `json:"user_id"`
		AccessToken string `json:"access_token"`
	}
	decode(t, ok, &reg)

	claims, err := ta.jwt.VerifyAccessToken(reg.AccessToken)
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	if claims.Type() != domain.UserTypeTesting {
		t.Fatalf("token user_type = %q, want testing", claims.UserType)
	}

	get := ta.do(http.MethodGet, "/admin/users/"+reg.UserID, nil, ta.bearer(ta.adminToken))
	mustStatus(t, get, http.StatusOK)
	var user struct {
		UserType string `json:"user_type"`
	}
	decode(t, get, &user)
	if user.UserType != "testing" {
		t.Fatalf("registered user type = %q, want testing", user.UserType)
	}
}

func TestInviteCodeUserTypeGrantValidation(t *testing.T) {
	ta := newTestApp(t)

	regular := ta.do(http.MethodPost, "/admin/invite-codes?grants_user_type=regular", nil, ta.bearer(ta.adminToken))
	mustStatus(t, regular, http.StatusOK)
	var code struct {
		GrantsUserType *string `json:"grants_user_type"`
	}
	decode(t, regular, &code)
	if code.GrantsUserType != nil {
		t.Fatalf("regular user type should not be stored as an invite grant: %+v", code)
	}

	invalid := ta.do(http.MethodPost, "/admin/invite-codes?grants_user_type=employee", nil, ta.bearer(ta.adminToken))
	mustStatus(t, invalid, http.StatusBadRequest)
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
