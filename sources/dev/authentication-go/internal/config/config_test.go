package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFromYAML(t *testing.T) {
	// Neutralize overrides from the ambient environment so the test is hermetic.
	t.Setenv("MYSQL_DSN", "")
	t.Setenv("SERVER_PORT", "")
	t.Setenv("CONFIG_PATH", "")

	path := writeConfig(t, `
mysql_dsn: "mysql://user:pass@127.0.0.1:3306/db"
server_port: 8080
jwt_access_token_expiry_secs: 7200
swagger_enabled: true
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MySQLDSN != "mysql://user:pass@127.0.0.1:3306/db" {
		t.Errorf("MySQLDSN = %q", cfg.MySQLDSN)
	}
	if cfg.ServerPort != 8080 {
		t.Errorf("ServerPort = %d", cfg.ServerPort)
	}
	if cfg.JWTAccessTokenExpirySecs != 7200 {
		t.Errorf("JWTAccessTokenExpirySecs = %d", cfg.JWTAccessTokenExpirySecs)
	}
	if !cfg.SwaggerEnabled {
		t.Error("SwaggerEnabled = false, want true")
	}
	// Keys absent from the file stay zero-valued: the YAML file is the source
	// of truth, there are no code-side defaults anymore.
	if cfg.ServerHost != "" {
		t.Errorf("ServerHost = %q, want empty (key not in file)", cfg.ServerHost)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	path := writeConfig(t, `
mysql_dsn: "mysql://yaml:yaml@127.0.0.1:3306/yaml"
server_port: 3000
jwt_issuer: ""
`)
	t.Setenv("MYSQL_DSN", "mysql://env:env@127.0.0.1:3306/env")
	t.Setenv("SERVER_PORT", "9999")
	t.Setenv("JWT_ISSUER", "env-issuer")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MySQLDSN != "mysql://env:env@127.0.0.1:3306/env" {
		t.Errorf("MySQLDSN = %q, want env override", cfg.MySQLDSN)
	}
	if cfg.ServerPort != 9999 {
		t.Errorf("ServerPort = %d, want env override", cfg.ServerPort)
	}
	if cfg.JWTIssuer != "env-issuer" {
		t.Errorf("JWTIssuer = %q, want env override", cfg.JWTIssuer)
	}
}

func TestLoadConfigPathEnv(t *testing.T) {
	path := writeConfig(t, "mysql_dsn: mysql://u:p@h:3306/db\n")
	t.Setenv("CONFIG_PATH", path)
	t.Setenv("MYSQL_DSN", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MySQLDSN != "mysql://u:p@h:3306/db" {
		t.Errorf("MySQLDSN = %q", cfg.MySQLDSN)
	}
}

func TestLoadRequireInviteCodeEnv(t *testing.T) {
	path := writeConfig(t, "mysql_dsn: mysql://u:p@h:3306/db\nrequire_invite_code: false\n")
	t.Setenv("REQUIRE_INVITE_CODE", "true")
	t.Setenv("STRIDE_REQUIRE_INVITE_CODE", "")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.RequireInviteCode {
		t.Error("RequireInviteCode = false, want env override")
	}
}

func TestLoadLegacyInviteAliases(t *testing.T) {
	path := writeConfig(t, "mysql_dsn: mysql://u:p@h:3306/db\n")
	for _, name := range []string{"STRIDE_REQUIRE_INVITE_CODE", "AUTH_REQUIRE_INVITE_CODE"} {
		t.Setenv("REQUIRE_INVITE_CODE", "")
		t.Setenv(name, "true")

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load (%s): %v", name, err)
		}
		if !cfg.RequireInviteCode {
			t.Errorf("%s=true: RequireInviteCode = false, want true", name)
		}
	}
}

func TestLoadRequiresMySQLDSN(t *testing.T) {
	path := writeConfig(t, "server_port: 3000\n")
	t.Setenv("MYSQL_DSN", "")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load succeeded without mysql_dsn, want validation error")
	}
	if !strings.Contains(err.Error(), "MySQLDSN") {
		t.Errorf("error = %v, want MySQLDSN validation failure", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	// A missing file must surface as an error, not a panic.
	_, err := Load(filepath.Join(t.TempDir(), "nope.yml"))
	if err == nil {
		t.Fatal("Load succeeded without a config file, want error")
	}
}
