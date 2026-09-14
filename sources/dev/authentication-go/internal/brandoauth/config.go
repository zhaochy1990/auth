package brandoauth

import (
	"encoding/json"

	"github.com/zhaochy1990/auth-service/internal/apperror"
)

// Config is the per-application brand configuration, stored as the JSON config
// of an auth_app_providers row and edited from the admin dashboard. It holds
// only operator-supplied values — the structural brand differences live in the
// Descriptor.
type Config struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	// Scopes override Descriptor.DefaultScopes when non-empty.
	Scopes []string `json:"scopes,omitempty"`
	// BaseURL overrides Descriptor.DefaultBaseURL (sandbox or a test server).
	BaseURL string `json:"base_url,omitempty"`
	// AuthorizeParams are brand-specific extra query parameters added to the
	// authorize URL (e.g. COROS's "bind state").
	AuthorizeParams map[string]string `json:"authorize_params,omitempty"`
	// Mock requests synthetic responses. It only takes effect together with the
	// service-wide master switch, so a misconfigured application in production
	// still talks to the real brand.
	Mock bool `json:"mock,omitempty"`
}

// ParseConfig decodes and validates an application provider config. A missing
// client id or secret is rejected so callers can return a clear
// "not configured" error rather than attempting a doomed HTTP call.
func ParseConfig(raw string) (Config, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return Config{}, apperror.ProviderNotConfigured()
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return Config{}, apperror.ProviderNotConfigured()
	}
	return cfg, nil
}
