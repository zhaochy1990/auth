package cos

import (
	"context"
	"strings"
	"testing"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/x/logger"
)

func init() {
	// Config.Configured logs warnings via the global logger; without this the
	// empty-config tests panic on a nil logger.
	logger.MustGetLogger(&logger.LoggerConfig{
		Format: "console", ServiceName: "auth-service-cos-test", Level: "error", Development: true,
	})
}

// An unconfigured Config must never report as configured, and an unconfigured
// client's Upload must fail with cos_not_configured (the handler guard relies
// on this first check).
func TestConfiguredAndUploadNotConfigured(t *testing.T) {
	cfg := Config{}
	if cfg.Configured() {
		t.Fatal("empty Config must not report as configured")
	}
	c := NewClient(cfg, nil)
	if c.Configured() {
		t.Fatal("empty client must not report as configured")
	}
	err := c.Upload(context.Background(), "avatars/u.jpg", strings.NewReader("x"), "image/jpeg")
	ae, ok := apperror.As(err)
	if !ok || ae.Type != "cos_not_configured" {
		t.Fatalf("Upload on unconfigured client = %v, want cos_not_configured", err)
	}
}

// Config with every credential set reports configured; a partial config does
// not (each missing field must fail the check).
func TestConfiguredRequiresAllFields(t *testing.T) {
	base := Config{SecretID: "a", SecretKey: "b", Bucket: "c", BaseURL: "d"}

	full := base
	full.Region = "ap-shanghai"
	if !full.Configured() {
		t.Fatal("full Config must report as configured")
	}

	missing := []string{"SecretID", "SecretKey", "Bucket", "Region", "BaseURL"}
	for _, field := range missing {
		c := base
		switch field {
		case "SecretID":
			c.SecretID = ""
		case "SecretKey":
			c.SecretKey = ""
		case "Bucket":
			c.Bucket = ""
		case "Region":
			c.Region = ""
		case "BaseURL":
			c.BaseURL = ""
		}
		if c.Configured() {
			t.Fatalf("Config missing %s must not report as configured", field)
		}
	}
}

// PublicURL joins the base URL and key exactly once.
func TestPublicURL(t *testing.T) {
	c := NewClient(Config{BaseURL: "https://b.cos.ap-shanghai.myqcloud.com"}, nil)
	if got := c.PublicURL("avatars/u.jpg"); got != "https://b.cos.ap-shanghai.myqcloud.com/avatars/u.jpg" {
		t.Fatalf("PublicURL = %q", got)
	}
}
