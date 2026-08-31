package mysql

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/zhaochy1990/auth-service/internal/domain"
)

const defaultTestMySQLAdminDSN = "mysql://root:root_password@127.0.0.1:3306/"

func testMySQLAdminDSN() string {
	if v := os.Getenv("TEST_MYSQL_ADMIN_DSN"); v != "" {
		return v
	}
	return defaultTestMySQLAdminDSN
}

func newTestRepository(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	ctx := context.Background()
	adminDSN := testMySQLAdminDSN()
	normalizedAdminDSN, err := normalizeDSN(adminDSN, Options{})
	if err != nil {
		t.Fatalf("invalid TEST_MYSQL_ADMIN_DSN: %v", err)
	}
	adminDB, err := sql.Open("mysql", normalizedAdminDSN)
	if err != nil {
		t.Fatalf("open admin MySQL connection: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if err := adminDB.PingContext(ctx); err != nil {
		t.Skipf("MySQL admin connection unavailable: %v", err)
	}

	dbName := fmt.Sprintf("auth_repo_test_%d_%d", os.Getpid(), time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+dbName+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatalf("create isolated test database: %v", err)
	}
	t.Cleanup(func() { _, _ = adminDB.ExecContext(context.Background(), "DROP DATABASE "+dbName) })

	repo, err := New(ctx, databaseDSN(adminDSN, dbName))
	if err != nil {
		t.Fatalf("open isolated MySQL repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo, ctx
}

func databaseDSN(adminDSN, dbName string) string {
	if strings.Contains(adminDSN, "://") {
		u, err := url.Parse(adminDSN)
		if err == nil {
			u.Path = "/" + dbName
			return u.String()
		}
	}
	cfg, err := mysqldriver.ParseDSN(adminDSN)
	if err != nil {
		return adminDSN
	}
	cfg.DBName = dbName
	return cfg.FormatDSN()
}

func TestNormalizeDSNWithTLSCAPathRegistersNamedTLSConfig(t *testing.T) {
	assertNormalizeDSNWithTLSCA(t, Options{TLSCAPath: writeTestCA(t)})
}

func TestNormalizeDSNWithTLSCAPEMRegistersNamedTLSConfig(t *testing.T) {
	assertNormalizeDSNWithTLSCA(t, Options{TLSCAPEM: testCA(t)})
}

func assertNormalizeDSNWithTLSCA(t *testing.T, opts Options) {
	t.Helper()
	for _, raw := range []string{
		"mysql://auth:auth_password@example.tencentcdb.com:3306/auth",
		"auth:auth_password@tcp(example.tencentcdb.com:3306)/auth",
	} {
		t.Run(raw, func(t *testing.T) {
			dsn, err := normalizeDSN(raw, opts)
			if err != nil {
				t.Fatalf("normalize DSN: %v", err)
			}
			cfg, err := mysqldriver.ParseDSN(dsn)
			if err != nil {
				t.Fatalf("parse normalized DSN: %v", err)
			}
			if cfg.TLSConfig != tencentTLSConfigName {
				t.Fatalf("TLSConfig = %q, want %q", cfg.TLSConfig, tencentTLSConfigName)
			}
			if cfg.ParseTime != true || cfg.Loc != time.UTC || !strings.Contains(dsn, "charset=utf8mb4") {
				t.Fatalf("normalization defaults not preserved: %+v", cfg)
			}
		})
	}
}

func writeTestCA(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/ca.pem"
	if err := os.WriteFile(path, []byte(testCA(t)), 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}
	return path
}

func testCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"Codex"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	cert, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}))
}

// TestMigrateWeChatProviderConfig verifies the schema migration that moves
// legacy app-level WeChat credentials (wechat_app_id / wechat_app_secret) into
// the application's WeChat provider config, preserving any existing provider
// config, then drops the legacy columns.
func TestMigrateWeChatProviderConfig(t *testing.T) {
	repo, ctx := newTestRepository(t)

	// Recreate the pre-#187 schema: the two legacy app-level WeChat columns.
	for _, stmt := range []string{
		"ALTER TABLE auth_applications ADD COLUMN wechat_app_id VARCHAR(128) NULL AFTER allowed_scopes",
		"ALTER TABLE auth_applications ADD COLUMN wechat_app_secret TEXT NULL AFTER wechat_app_id",
	} {
		if _, err := repo.db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("add legacy wechat column: %v", err)
		}
	}

	now := time.Now().UTC()
	insertLegacyApp := func(id, name, clientID, appID, secret string) {
		t.Helper()
		if _, err := repo.db.ExecContext(ctx, `INSERT INTO auth_applications
			(id, name, client_id, client_secret_hash, redirect_uris, allowed_scopes, is_active, wechat_app_id, wechat_app_secret, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, name, clientID, "hash", "[]", "[]", true, appID, secret, now, now); err != nil {
			t.Fatalf("insert legacy app %s: %v", name, err)
		}
	}

	// App with no existing provider config → backfilled from the columns.
	appID1 := uuid.NewString()
	insertLegacyApp(appID1, "Legacy WeChat App", "legacy-client-1", "legacy-appid", "legacy-secret")

	// App with an existing wechat provider config → preserved as source of
	// truth (appid kept), and the missing secret backfilled from the column.
	appID2 := uuid.NewString()
	insertLegacyApp(appID2, "Provider WeChat App", "legacy-client-2", "legacy-appid-2", "legacy-secret-2")
	if err := repo.AppProviders().Insert(ctx, &domain.AppProvider{
		ID: uuid.NewString(), AppID: appID2, ProviderID: "wechat",
		Config: `{"appid":"already-set-appid"}`, IsActive: true, CreatedAt: now,
	}); err != nil {
		t.Fatalf("insert existing wechat provider: %v", err)
	}

	if err := repo.migrateWeChatProviderConfig(ctx); err != nil {
		t.Fatalf("migrate wechat provider config: %v", err)
	}

	// App 1: provider row created with appid/secret from the legacy columns.
	p1, err := repo.AppProviders().FindByAppAndProvider(ctx, appID1, "wechat")
	if err != nil {
		t.Fatalf("find migrated provider 1: %v", err)
	}
	if p1 == nil {
		t.Fatal("expected a wechat provider row for app 1")
	}
	var cfg1 map[string]any
	if err := json.Unmarshal([]byte(p1.Config), &cfg1); err != nil {
		t.Fatalf("unmarshal config 1: %v", err)
	}
	if cfg1["appid"] != "legacy-appid" || cfg1["secret"] != "legacy-secret" {
		t.Fatalf("app 1 config = %v, want appid=legacy-appid secret=legacy-secret", cfg1)
	}

	// App 2: existing appid preserved, secret backfilled from the column.
	p2, err := repo.AppProviders().FindByAppAndProvider(ctx, appID2, "wechat")
	if err != nil {
		t.Fatalf("find migrated provider 2: %v", err)
	}
	if p2 == nil {
		t.Fatal("expected a wechat provider row for app 2")
	}
	var cfg2 map[string]any
	if err := json.Unmarshal([]byte(p2.Config), &cfg2); err != nil {
		t.Fatalf("unmarshal config 2: %v", err)
	}
	if cfg2["appid"] != "already-set-appid" {
		t.Fatalf("app 2 appid overwritten (want preserved): %v", cfg2)
	}
	if cfg2["secret"] != "legacy-secret-2" {
		t.Fatalf("app 2 secret not backfilled (want legacy-secret-2): %v", cfg2)
	}

	// The legacy columns must be dropped after the migration.
	for _, col := range []string{"wechat_app_id", "wechat_app_secret"} {
		exists, err := repo.columnExists(ctx, "auth_applications", col)
		if err != nil {
			t.Fatalf("check column %s: %v", col, err)
		}
		if exists {
			t.Fatalf("column %s should have been dropped by the migration", col)
		}
	}
}

// TestNewWithLegacyWeChatColumnsDoesNotPanic is a regression test for the
// nil-pointer crash that occurred when NewWithOptions called EnsureSchema
// (which runs the WeChat provider-config backfill) before initializing the
// app-providers sub-repo. It constructs a pre-migration schema with populated
// legacy wechat_app_id / wechat_app_secret columns, then drives New() — the
// exact startup path that crashed in production — and verifies the backfill
// completes successfully.
func TestNewWithLegacyWeChatColumnsDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	adminDSN := testMySQLAdminDSN()
	normalizedAdminDSN, err := normalizeDSN(adminDSN, Options{})
	if err != nil {
		t.Fatalf("invalid TEST_MYSQL_ADMIN_DSN: %v", err)
	}
	adminDB, err := sql.Open("mysql", normalizedAdminDSN)
	if err != nil {
		t.Fatalf("open admin MySQL connection: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if err := adminDB.PingContext(ctx); err != nil {
		t.Skipf("MySQL admin connection unavailable: %v", err)
	}

	dbName := fmt.Sprintf("auth_repo_test_%d_%d", os.Getpid(), time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+dbName+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatalf("create isolated test database: %v", err)
	}
	t.Cleanup(func() { _, _ = adminDB.ExecContext(context.Background(), "DROP DATABASE "+dbName) })

	userDSN := databaseDSN(adminDSN, dbName)
	normalizedUserDSN, err := normalizeDSN(userDSN, Options{})
	if err != nil {
		t.Fatalf("normalize user DSN: %v", err)
	}
	userDB, err := sql.Open("mysql", normalizedUserDSN)
	if err != nil {
		t.Fatalf("open user MySQL connection: %v", err)
	}
	t.Cleanup(func() { _ = userDB.Close() })

	// Build the pre-migration schema by running the base CREATE TABLEs and
	// then adding the legacy wechat_app_id / wechat_app_secret columns.
	for _, stmt := range schemaStatements {
		if _, err := userDB.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("create base table: %v", err)
		}
	}
	for _, stmt := range []string{
		"ALTER TABLE auth_applications ADD COLUMN wechat_app_id VARCHAR(128) NULL AFTER allowed_scopes",
		"ALTER TABLE auth_applications ADD COLUMN wechat_app_secret TEXT NULL AFTER wechat_app_id",
	} {
		if _, err := userDB.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("add legacy wechat column: %v", err)
		}
	}

	now := time.Now().UTC()
	appID := uuid.NewString()
	if _, err := userDB.ExecContext(ctx, `INSERT INTO auth_applications
		(id, name, client_id, client_secret_hash, redirect_uris, allowed_scopes, is_active, wechat_app_id, wechat_app_secret, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		appID, "Legacy App", "legacy-client", "hash", "[]", "[]", true,
		"prod-appid", "prod-secret", now, now); err != nil {
		t.Fatalf("insert legacy app: %v", err)
	}
	_ = userDB.Close()

	// Drive New() — the full constructor path that runs EnsureSchema.
	// Before the fix this panicked on the nil appProvRepo.
	repo, err := New(ctx, userDSN)
	if err != nil {
		t.Fatalf("New() returned error (expected success): %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	// The backfill must have created a wechat provider row.
	p, err := repo.AppProviders().FindByAppAndProvider(ctx, appID, "wechat")
	if err != nil {
		t.Fatalf("find wechat provider: %v", err)
	}
	if p == nil {
		t.Fatal("expected a wechat provider row after New(), got nil")
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(p.Config), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if cfg["appid"] != "prod-appid" {
		t.Fatalf("appid = %v, want prod-appid", cfg["appid"])
	}
	if cfg["secret"] != "prod-secret" {
		t.Fatalf("secret = %v, want prod-secret", cfg["secret"])
	}

	// The legacy columns must have been dropped.
	for _, col := range []string{"wechat_app_id", "wechat_app_secret"} {
		exists, err := repo.columnExists(ctx, "auth_applications", col)
		if err != nil {
			t.Fatalf("check column %s: %v", col, err)
		}
		if exists {
			t.Fatalf("column %s should have been dropped by New()", col)
		}
	}
}
