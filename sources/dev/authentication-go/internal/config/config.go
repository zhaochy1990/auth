// Package config loads service configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration.
type Config struct {
	StorageBackend            string
	MySQLDSN                  string
	MySQLTLSCAPEM             string
	MySQLTLSCAPath            string
	JWTPrivateKeyPath         string
	JWTPublicKeyPath          string
	JWTIssuer                 string
	JWTAccessTokenExpirySecs  int64
	JWTRefreshTokenExpiryDays int64
	ServerHost                string
	ServerPort                int
	CORSAllowedOrigins        string
	// EnableTestProviders gates the "test" auth provider. Off in production.
	EnableTestProviders bool
	// SwaggerEnabled gates the /swagger UI + spec. Off in production. The UI is
	// only served when the binary is also built with `-tags swagger`.
	SwaggerEnabled bool
	// WeChatCode2SessionURL overrides the WeChat code2Session endpoint (tests).
	WeChatCode2SessionURL string
	// RedisAddr / RedisPassword / RedisDB configure the Redis connection used
	// for short-lived SMS verification codes. An unreachable Redis makes the
	// SMS endpoints fail closed (503) rather than falling back to another
	// store.
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	// SMSTestMode fixes the verification code at 123456 and skips the Tencent
	// Cloud SMS HTTP call, keeping demos and automated tests deterministic
	// without real SMS.
	SMSTestMode bool
	// Tencent Cloud SMS global configuration. Missing values do not prevent
	// startup; /api/auth/sms/send returns sms_not_configured in that case.
	TencentSMSSecretID   string
	TencentSMSSecretKey  string
	TencentSMSSDKAppID   string
	TencentSMSSignName   string
	TencentSMSTemplateID string
	TencentSMSRegion     string
	// SMSSendRateLimit / SMSVerifyRateLimit are the per-IP hourly request caps
	// for the SMS send / verify endpoints.
	SMSSendRateLimit   int
	SMSVerifyRateLimit int
}

const (
	StorageBackendMySQL = "mysql"
)

// FromEnv builds a Config from environment variables. Storage defaults to
// MySQL when MYSQL_DSN is present, otherwise Azure Tables for rollback
// compatibility during the migration window.
func FromEnv() (*Config, error) {
	backend := StorageBackendMySQL
	mysqlDSN := os.Getenv("MYSQL_DSN")
	mysqlTLSCAPEM := os.Getenv("MYSQL_TLS_CA_PEM")
	mysqlTLSCAPath := os.Getenv("MYSQL_TLS_CA_PATH")
	switch backend {
	case StorageBackendMySQL:
		if mysqlDSN == "" {
			return nil, fmt.Errorf("MYSQL_DSN is required when STORAGE_BACKEND=mysql")
		}
	default:
		return nil, fmt.Errorf("unsupported STORAGE_BACKEND %q", backend)
	}
	return &Config{
		StorageBackend:            backend,
		MySQLDSN:                  mysqlDSN,
		MySQLTLSCAPEM:             mysqlTLSCAPEM,
		MySQLTLSCAPath:            mysqlTLSCAPath,
		JWTPrivateKeyPath:         EnvOr("JWT_PRIVATE_KEY_PATH", "keys/private.pem"),
		JWTPublicKeyPath:          EnvOr("JWT_PUBLIC_KEY_PATH", "keys/public.pem"),
		JWTIssuer:                 EnvOr("JWT_ISSUER", "auth-service"),
		JWTAccessTokenExpirySecs:  envInt64("JWT_ACCESS_TOKEN_EXPIRY_SECS", 3600),
		JWTRefreshTokenExpiryDays: envInt64("JWT_REFRESH_TOKEN_EXPIRY_DAYS", 30),
		ServerHost:                EnvOr("SERVER_HOST", "127.0.0.1"),
		ServerPort:                int(envInt64("SERVER_PORT", 3000)),
		CORSAllowedOrigins:        EnvOr("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:3000"),
		EnableTestProviders:       envBool("AUTH_ENABLE_TEST_PROVIDERS", false),
		SwaggerEnabled:            envBool("SWAGGER_ENABLED", false),
		WeChatCode2SessionURL:     os.Getenv("WECHAT_CODE2SESSION_URL"),
		RedisAddr:                 EnvOr("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:             os.Getenv("REDIS_PASSWORD"),
		RedisDB:                   int(envInt64("REDIS_DB", 0)),
		SMSTestMode:               envBool("AUTH_SMS_TEST_MODE", false),
		TencentSMSSecretID:        os.Getenv("TENCENT_SMS_SECRET_ID"),
		TencentSMSSecretKey:       os.Getenv("TENCENT_SMS_SECRET_KEY"),
		TencentSMSSDKAppID:        os.Getenv("TENCENT_SMS_SDK_APP_ID"),
		TencentSMSSignName:        os.Getenv("TENCENT_SMS_SIGN_NAME"),
		TencentSMSTemplateID:      os.Getenv("TENCENT_SMS_TEMPLATE_ID"),
		TencentSMSRegion:          EnvOr("TENCENT_SMS_REGION", "ap-guangzhou"),
		SMSSendRateLimit:          int(envInt64("SMS_SEND_RATE_LIMIT", 10)),
		SMSVerifyRateLimit:        int(envInt64("SMS_VERIFY_RATE_LIMIT", 60)),
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
