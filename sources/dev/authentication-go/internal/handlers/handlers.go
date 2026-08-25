// Package handlers implements the HTTP handlers (business logic) for every
// endpoint. Handlers are methods on *Handler and read authenticated context
// (user id, app id, scopes) stashed by the middleware. JSON request/response
// shapes preserve the public API contract so the React dashboard works
// unchanged.
package handlers

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/config"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/repository"
	"github.com/zhaochy1990/auth-service/internal/sms"
)

// Handler bundles the dependencies shared by all HTTP handlers.
type Handler struct {
	Repo repository.Repository
	JWT  *auth.JWTManager
	Cfg  *config.Config
	// SMSStore is the Redis-backed verification-code store; SMSClient wraps
	// Tencent Cloud SMS. Both are always present (constructed with the router)
	// — an unconfigured client makes send fail with sms_not_configured.
	SMSStore  repository.SmsCodeStore
	SMSClient *sms.Client
}

// ErrorResponse is the JSON body returned for every error. It mirrors
// middleware.RespondError: a stable machine-readable `error` code plus a
// human-readable `message`. Referenced by the Swagger `@Failure` annotations.
type ErrorResponse struct {
	Error   string `json:"error" example:"invalid_credentials"`
	Message string `json:"message" example:"Invalid credentials"`
}

// StatusResponse is the JSON body for endpoints that only acknowledge success
// (e.g. logout, revoke). Referenced by Swagger `@Success` annotations.
type StatusResponse struct {
	Status string `json:"status" example:"ok"`
}

// New builds a Handler.
func New(repo repository.Repository, jwt *auth.JWTManager, cfg *config.Config) *Handler {
	return &Handler{
		Repo: repo,
		JWT:  jwt,
		Cfg:  cfg,
	}
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

// requireInviteCode reports whether registration is invite-gated. Read from
// the YAML config / REQUIRE_INVITE_CODE env override at startup; the legacy
// STRIDE_REQUIRE_INVITE_CODE / AUTH_REQUIRE_INVITE_CODE env aliases are folded
// in by config.Load.
func (h *Handler) requireInviteCode() bool {
	return h.Cfg.RequireInviteCode
}

// resolveInviteGate validates a submitted invite code against the registration
// gate and returns the record (nil when the gate is off). Errors are typed
// (missing / invalid / already used). Shared by email registration and SMS
// auto-registration so the two gates cannot drift.
func (h *Handler) resolveInviteGate(ctx context.Context, raw *string) (*domain.InviteCode, error) {
	if !h.requireInviteCode() {
		return nil, nil
	}
	if raw == nil || *raw == "" {
		return nil, apperror.BadRequest("invite_code is required")
	}
	record, err := h.Repo.InviteCodes().GetByCode(ctx, *raw)
	if err != nil {
		return nil, err
	}
	if record == nil || record.IsRevoked {
		return nil, apperror.InviteCodeNotFound()
	}
	if record.Kind == domain.InviteSingleUse && record.UsedAt != nil {
		return nil, apperror.InviteCodeAlreadyUsed()
	}
	return record, nil
}

// registrationGrants derives the registration-time fields from an invite code:
// the granted membership (with expiry), the invite code to stamp on the user,
// and the granted user type. A nil record yields defaults (regular, no
// invite, regular type).
func registrationGrants(record *domain.InviteCode, now time.Time) (domain.MembershipTier, *time.Time, *string, domain.UserType) {
	membership := domain.MembershipRegular
	var membershipExpires *time.Time
	if record != nil && record.GrantsMembership != nil && record.GrantsMembership.IsPaid() {
		membership = *record.GrantsMembership
		if record.GrantsMembershipDays != nil {
			e := now.Add(time.Duration(*record.GrantsMembershipDays) * 24 * time.Hour)
			membershipExpires = &e
		}
	}
	var invitedWith *string
	userType := domain.UserTypeRegular
	if record != nil {
		invitedWith = strPtr(record.Code)
		if record.GrantsUserType != nil {
			userType = domain.UserTypeFromString(string(*record.GrantsUserType))
		}
	}
	return membership, membershipExpires, invitedWith, userType
}

// displayDT formats a time as "YYYY-MM-DD HH:MM:SS" with an optional
// fractional part of 3, 6, or 9 digits.
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

func customAttributesOrEmpty(attributes map[string]any) map[string]any {
	if attributes == nil {
		return map[string]any{}
	}
	return attributes
}

func mergeCustomAttributes(target map[string]any, patch map[string]any) map[string]any {
	if target == nil {
		target = map[string]any{}
	}
	for key, value := range patch {
		if isNilJSONValue(value) {
			delete(target, key)
		} else {
			target[key] = value
		}
	}
	return target
}

func isNilJSONValue(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
