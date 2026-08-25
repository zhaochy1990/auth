// Package config loads service configuration from a YAML file, overridable by
// environment variables, using the viper wrapper in
// github.com/zhaochy1990/x/viper (no hand-rolled env loader).
package config

import (
	"fmt"
	"os"
	"strings"

	xviper "github.com/zhaochy1990/x/viper"
)

// Config holds all runtime configuration. Every field carries a mapstructure
// tag whose value is both the YAML key and, uppercased, the environment
// variable that overrides it (viper's env-key replacer maps "-" and "." to
// "_", so e.g. `mysql_dsn` <- MYSQL_DSN).
//
// Env overrides only apply to keys that exist in the YAML file — viper checks
// the environment per config key, it does not discover env-only vars. Keep the
// shipped config.yml (baked into the image) as the complete key list, or a
// deployment's custom file must spell out every key it wants to override.
type Config struct {
	// MySQL is the only storage backend; MySQLDSN is required. Accepts URL
	// style (mysql://user:pass@host:3306/db) and Go driver style DSNs.
	MySQLDSN       string `mapstructure:"mysql_dsn" validate:"required"`
	MySQLTLSCAPEM  string `mapstructure:"mysql_tls_ca_pem"`
	MySQLTLSCAPath string `mapstructure:"mysql_tls_ca_path"`

	JWTPrivateKeyPath         string `mapstructure:"jwt_private_key_path"`
	JWTPublicKeyPath          string `mapstructure:"jwt_public_key_path"`
	JWTIssuer                 string `mapstructure:"jwt_issuer"`
	JWTAccessTokenExpirySecs  int64  `mapstructure:"jwt_access_token_expiry_secs"`
	JWTRefreshTokenExpiryDays int64  `mapstructure:"jwt_refresh_token_expiry_days"`

	ServerHost         string `mapstructure:"server_host"`
	ServerPort         int    `mapstructure:"server_port"`
	CORSAllowedOrigins string `mapstructure:"cors_allowed_origins"`

	// EnableTestProviders gates the "test" auth provider. Off in production.
	EnableTestProviders bool `mapstructure:"auth_enable_test_providers"`
	// SwaggerEnabled gates the /swagger UI + spec. Off in production. The UI is
	// only served when the binary is also built with `-tags swagger`.
	SwaggerEnabled bool `mapstructure:"swagger_enabled"`
	// WeChatCode2SessionURL overrides the WeChat code2Session endpoint (tests).
	WeChatCode2SessionURL string `mapstructure:"wechat_code2session_url"`

	// RedisAddr / RedisPassword / RedisDB configure the Redis connection used
	// for short-lived SMS verification codes. An unreachable Redis makes the
	// SMS endpoints fail closed (503) rather than falling back to another
	// store.
	RedisAddr     string `mapstructure:"redis_addr"`
	RedisPassword string `mapstructure:"redis_password"`
	RedisDB       int    `mapstructure:"redis_db"`
	// SMSTestMode fixes the verification code at 123456 and skips the Tencent
	// Cloud SMS HTTP call, keeping demos and automated tests deterministic
	// without real SMS.
	SMSTestMode bool `mapstructure:"auth_sms_test_mode"`

	// Tencent Cloud SMS global configuration. Missing values do not prevent
	// startup; /api/auth/sms/send returns sms_not_configured in that case.
	TencentSMSSecretID   string `mapstructure:"tencent_sms_secret_id"`
	TencentSMSSecretKey  string `mapstructure:"tencent_sms_secret_key"`
	TencentSMSSDKAppID   string `mapstructure:"tencent_sms_sdk_app_id"`
	TencentSMSSignName   string `mapstructure:"tencent_sms_sign_name"`
	TencentSMSTemplateID string `mapstructure:"tencent_sms_template_id"`
	TencentSMSRegion     string `mapstructure:"tencent_sms_region"`
	// SMSSendRateLimit / SMSVerifyRateLimit are the per-IP hourly request caps
	// for the SMS send / verify endpoints.
	SMSSendRateLimit   int `mapstructure:"sms_send_rate_limit"`
	SMSVerifyRateLimit int `mapstructure:"sms_verify_rate_limit"`

	// RequireInviteCode gates registration on a valid invite code. Env
	// override is REQUIRE_INVITE_CODE; the legacy STRIDE_REQUIRE_INVITE_CODE /
	// AUTH_REQUIRE_INVITE_CODE vars are honored as aliases so existing
	// deployments keep working (see applyLegacyEnvAliases).
	RequireInviteCode bool `mapstructure:"require_invite_code"`

	// LogLevel / LogFormat configure the zap logger
	// (debug|info|warning|error / json|console).
	LogLevel  string `mapstructure:"log_level"`
	LogFormat string `mapstructure:"log_format"`
	// AppVersion is reported by /health and the admin stats endpoint.
	AppVersion string `mapstructure:"app_version"`
}

// Load reads the YAML config file and applies environment overrides via
// x/viper.MustLoadConfig. An empty configPath falls back to the CONFIG_PATH
// environment variable and finally to /etc/viper.yml. The x loader panics on
// fatal config errors (missing file, unmarshal, validation); Load recovers the
// panic into a returned error so the CLI prints a single "error: ..." line.
func Load(configPath string) (cfg *Config, err error) {
	defer func() {
		if r := recover(); r != nil {
			cfg = nil
			err = fmt.Errorf("load config: %v", r)
		}
	}()

	cfg = &Config{}
	xviper.MustLoadConfig("", configPath, cfg)
	applyLegacyEnvAliases(cfg)
	return cfg, nil
}

// applyLegacyEnvAliases folds pre-YAML environment flags into the config so
// deployments that set the old variable names keep working during the
// migration. The new env override for RequireInviteCode is REQUIRE_INVITE_CODE;
// STRIDE_REQUIRE_INVITE_CODE / AUTH_REQUIRE_INVITE_CODE are the historical
// names.
func applyLegacyEnvAliases(cfg *Config) {
	if !cfg.RequireInviteCode {
		cfg.RequireInviteCode = strings.EqualFold(os.Getenv("STRIDE_REQUIRE_INVITE_CODE"), "true") ||
			strings.EqualFold(os.Getenv("AUTH_REQUIRE_INVITE_CODE"), "true")
	}
}

// Addr returns the host:port the server should bind to.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.ServerHost, c.ServerPort)
}
