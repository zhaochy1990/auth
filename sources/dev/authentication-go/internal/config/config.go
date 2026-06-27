// Package config loads service configuration from the environment, mirroring
// the Rust `Config::from_env`. Only AZURE_STORAGE_CONNECTION_STRING is required;
// everything else has a sensible default.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration.
type Config struct {
	AzureStorageConnectionString string
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

// FromEnv builds a Config from environment variables. Returns an error only
// when the one required variable is missing.
func FromEnv() (*Config, error) {
	conn := os.Getenv("AZURE_STORAGE_CONNECTION_STRING")
	if conn == "" {
		return nil, fmt.Errorf("AZURE_STORAGE_CONNECTION_STRING is required")
	}
	return &Config{
		AzureStorageConnectionString: conn,
		JWTPrivateKeyPath:            envOr("JWT_PRIVATE_KEY_PATH", "keys/private.pem"),
		JWTPublicKeyPath:             envOr("JWT_PUBLIC_KEY_PATH", "keys/public.pem"),
		JWTIssuer:                    envOr("JWT_ISSUER", "auth-service"),
		JWTAccessTokenExpirySecs:     envInt64("JWT_ACCESS_TOKEN_EXPIRY_SECS", 3600),
		JWTRefreshTokenExpiryDays:    envInt64("JWT_REFRESH_TOKEN_EXPIRY_DAYS", 30),
		ServerHost:                   envOr("SERVER_HOST", "127.0.0.1"),
		ServerPort:                   int(envInt64("SERVER_PORT", 3000)),
		CORSAllowedOrigins:           envOr("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:3000"),
		EnableTestProviders:          envBool("AUTH_ENABLE_TEST_PROVIDERS", false),
	}, nil
}

// Addr returns the host:port the server should bind to.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.ServerHost, c.ServerPort)
}

func envOr(key, def string) string {
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
