// Package handlers implements the HTTP handlers (business logic) for every
// endpoint, ported from the Rust `handlers` module. Handlers are methods on
// *Handler and read authenticated context (user id, app id, scopes) stashed by
// the middleware. JSON request/response shapes match the Rust types so the
// existing React dashboard works unchanged.
package handlers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/config"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/repository"
)

// Handler bundles the dependencies shared by all HTTP handlers.
type Handler struct {
	Repo repository.Repository
	JWT  *auth.JWTManager
	Cfg  *config.Config
}

// New builds a Handler.
func New(repo repository.Repository, jwt *auth.JWTManager, cfg *config.Config) *Handler {
	return &Handler{Repo: repo, JWT: jwt, Cfg: cfg}
}

// resolveMembership returns the user's effective tier, lazily downgrading an
// expired paid tier to Regular and persisting that change (best-effort).
func (h *Handler) resolveMembership(ctx context.Context, user *domain.User) domain.MembershipTier {
	now := time.Now().UTC()
	if user.IsMembershipExpired(now) {
		user.Membership = domain.MembershipRegular
		user.MembershipExpiresAt = nil
		user.UpdatedAt = now
		_ = h.Repo.Users().Update(ctx, user) // best-effort; failure must not block token issuance
	}
	return user.Membership
}

// requireInviteCode reports whether registration is invite-gated (env flag,
// read per-request to match the Rust behavior).
func requireInviteCode() bool {
	return strings.EqualFold(os.Getenv("STRIDE_REQUIRE_INVITE_CODE"), "true")
}

func appVersion() string {
	if v := os.Getenv("APP_VERSION"); v != "" {
		return v
	}
	return "dev"
}

// displayDT formats a time the way Rust's chrono NaiveDateTime Display does:
// "YYYY-MM-DD HH:MM:SS" with an optional fractional part of 3, 6, or 9 digits.
func displayDT(t time.Time) string {
	t = t.UTC()
	base := t.Format("2006-01-02 15:04:05")
	ns := t.Nanosecond()
	switch {
	case ns == 0:
		return base
	case ns%1_000_000 == 0:
		return fmt.Sprintf("%s.%03d", base, ns/1_000_000)
	case ns%1_000 == 0:
		return fmt.Sprintf("%s.%06d", base, ns/1_000)
	default:
		return fmt.Sprintf("%s.%09d", base, ns)
	}
}

func displayDTPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := displayDT(*t)
	return &s
}

func strPtr(s string) *string { return &s }
