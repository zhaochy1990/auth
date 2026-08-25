package mysql

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
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
