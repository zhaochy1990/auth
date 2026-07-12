// Package config loads service configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration.
type Config struct {
	StorageBackend               string
	AzureStorageConnectionString string
	MySQLDSN                     string
	JWTPrivateKeyPath            string
	JWTPublicKeyPath             string
	JWTIssuer                    string
	JWTAccessTokenExpirySecs     int64
	JWTRefreshTokenExpiryDays    int64
	ServerHost                   string
	ServerPort                   int
	CORSAllowedOrigins           string
	// EnableTestProviders gates the "test" auth provider (the Go equivalent of
	// the Rust `test-providers` cargo feature). Off in production.
	EnableTestProviders bool
}

const (
	StorageBackendAzureTable = "azure_table"
	StorageBackendMySQL      = "mysql"
)

// FromEnv builds a Config from environment variables. Storage defaults to
// MySQL when MYSQL_DSN is present, otherwise Azure Tables for rollback
// compatibility during the migration window.
func FromEnv() (*Config, error) {
	backend := EnvOr("STORAGE_BACKEND", "")
	if backend == "" {
		if os.Getenv("MYSQL_DSN") != "" {
			backend = StorageBackendMySQL
		} else {
			backend = StorageBackendAzureTable
		}
	}
	conn := os.Getenv("AZURE_STORAGE_CONNECTION_STRING")
	mysqlDSN := os.Getenv("MYSQL_DSN")
	switch backend {
	case StorageBackendAzureTable:
		if conn == "" {
			return nil, fmt.Errorf("AZURE_STORAGE_CONNECTION_STRING is required when STORAGE_BACKEND=azure_table")
		}
	case StorageBackendMySQL:
		if mysqlDSN == "" {
			return nil, fmt.Errorf("MYSQL_DSN is required when STORAGE_BACKEND=mysql")
		}
	default:
		return nil, fmt.Errorf("unsupported STORAGE_BACKEND %q", backend)
	}
	return &Config{
		StorageBackend:               backend,
		AzureStorageConnectionString: conn,
		MySQLDSN:                     mysqlDSN,
		JWTPrivateKeyPath:            EnvOr("JWT_PRIVATE_KEY_PATH", "keys/private.pem"),
		JWTPublicKeyPath:             EnvOr("JWT_PUBLIC_KEY_PATH", "keys/public.pem"),
		JWTIssuer:                    EnvOr("JWT_ISSUER", "auth-service"),
		JWTAccessTokenExpirySecs:     envInt64("JWT_ACCESS_TOKEN_EXPIRY_SECS", 3600),
		JWTRefreshTokenExpiryDays:    envInt64("JWT_REFRESH_TOKEN_EXPIRY_DAYS", 30),
		ServerHost:                   EnvOr("SERVER_HOST", "127.0.0.1"),
		ServerPort:                   int(envInt64("SERVER_PORT", 3000)),
		CORSAllowedOrigins:           EnvOr("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:3000"),
		EnableTestProviders:          envBool("AUTH_ENABLE_TEST_PROVIDERS", false),
	}, nil
}

// Addr returns the host:port the server should bind to.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.ServerHost, c.ServerPort)
}

func EnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		switch v {
		case "1", "true", "TRUE", "True", "yes", "on":
			return true
		case "0", "false", "FALSE", "False", "no", "off":
			return false
		}
	}
	return def
}
