// Package repository defines the storage abstraction for the auth service.
//
// This is the swappable adapter boundary: handlers depend only on these
// interfaces, never on a concrete store. The MySQL runtime adapter lives in the
// mysql subpackage.
package repository

import (
	"context"
	"time"

	"github.com/zhaochy1990/auth-service/internal/domain"
)

// UserListSortBy selects the server-side ordering used for admin user lists.
type UserListSortBy string

const (
	UserListSortByName        UserListSortBy = "name"
	UserListSortByLastLoginAt UserListSortBy = "last_login_at"
)

// SortOrder is the direction for server-side list ordering.
type SortOrder string

const (
	SortOrderAsc  SortOrder = "asc"
	SortOrderDesc SortOrder = "desc"
)

// UserListSort is the complete sort request for admin user lists.
type UserListSort struct {
	By    UserListSortBy
	Order SortOrder
}

// DefaultUserListSort keeps the admin user list ordered by display name.
func DefaultUserListSort() UserListSort {
	return UserListSort{By: UserListSortByName, Order: SortOrderAsc}
}

// ParseUserListSort normalizes query values, keeping unknown values backward-compatible.
func ParseUserListSort(sortBy, sortOrder string) UserListSort {
	sort := DefaultUserListSort()
	if sortBy == string(UserListSortByLastLoginAt) {
		sort.By = UserListSortByLastLoginAt
	}
	if sortOrder == string(SortOrderDesc) {
		sort.Order = SortOrderDesc
	}
	return sort
}

// UserRepository persists users.
type UserRepository interface {
	FindByID(ctx context.Context, id string) (*domain.User, error)
	FindByEmail(ctx context.Context, email string) (*domain.User, error)
	// FindByWeChatOpenID returns the user holding the WeChat identity
	// (wechatAppID, openid), or nil when unbound.
	FindByWeChatOpenID(ctx context.Context, wechatAppID, openid string) (*domain.User, error)
	// FindByWeChatUnionID returns the user holding a WeChat link with the given
	// unionid, or nil when none. Used to prevent one WeChat person (across
	// mini-programs) being bound to two accounts.
	FindByWeChatUnionID(ctx context.Context, unionid string) (*domain.User, error)
	// FindByPhone returns the user holding the given mainland-China phone
	// number (bare 11 digits), or nil when no account has it.
	FindByPhone(ctx context.Context, phone string) (*domain.User, error)
	// FindWeChatLink returns the user's WeChat link for one mini-program, or nil.
	FindWeChatLink(ctx context.Context, userID, wechatAppID string) (*domain.WeChatLink, error)
	// LinkWeChat binds a WeChat identity to a user. A duplicate (the same
	// identity already bound, or the user already linked for this mini-program)
	// surfaces as a conflict error.
	LinkWeChat(ctx context.Context, userID, wechatAppID, openid string, unionid *string) error
	// DeleteWeChatLinksByUser removes all of a user's WeChat links.
	DeleteWeChatLinksByUser(ctx context.Context, userID string) error
	Insert(ctx context.Context, u *domain.User) error
	Update(ctx context.Context, u *domain.User) error
	DeleteByID(ctx context.Context, id string) error
	CountAll(ctx context.Context) (uint64, error)
	CountSince(ctx context.Context, since time.Time) (uint64, error)
	// ListPaginated returns a page of users. search is a case-insensitive
	// substring match on email/name; idSearch is a case-insensitive substring
	// match on the user id (UUID). Empty filters are ignored.
	ListPaginated(ctx context.Context, search, idSearch string, userType *domain.UserType, sort UserListSort, offset, limit uint64) ([]domain.User, uint64, error)
	// RecordLogin appends a login record (timestamp + IP), keeping at most the
	// 3 most recent entries, and updates LastLoginAt.
	RecordLogin(ctx context.Context, userID, ip string) error
}

// ApplicationRepository persists OAuth2 applications.
type ApplicationRepository interface {
	FindByID(ctx context.Context, id string) (*domain.Application, error)
	FindByClientID(ctx context.Context, clientID string) (*domain.Application, error)
	FindByName(ctx context.Context, name string) (*domain.Application, error)
	FindAll(ctx context.Context) ([]domain.Application, error)
	Insert(ctx context.Context, a *domain.Application) error
	Update(ctx context.Context, a *domain.Application) error
	CountAll(ctx context.Context) (uint64, error)
	CountActive(ctx context.Context) (uint64, error)
}

// AccountRepository persists user-provider account links.
type AccountRepository interface {
	FindByUserAndProvider(ctx context.Context, userID, providerID string) (*domain.Account, error)
	FindByProviderAccount(ctx context.Context, providerID, providerAccountID string) (*domain.Account, error)
	FindAllByUser(ctx context.Context, userID string) ([]domain.Account, error)
	CountByUser(ctx context.Context, userID string) (uint64, error)
	Insert(ctx context.Context, a *domain.Account) error
	Update(ctx context.Context, a *domain.Account) error
	DeleteByID(ctx context.Context, id string) error
	DeleteAllByUser(ctx context.Context, userID string) error
}

// AppProviderRepository persists per-app provider configs.
type AppProviderRepository interface {
	FindByAppAndProvider(ctx context.Context, appID, providerID string) (*domain.AppProvider, error)
	FindAllByApp(ctx context.Context, appID string) ([]domain.AppProvider, error)
	Insert(ctx context.Context, ap *domain.AppProvider) error
	DeleteByID(ctx context.Context, id string) error
}

// AuthCodeRepository persists OAuth2 authorization codes.
type AuthCodeRepository interface {
	FindByCode(ctx context.Context, code string) (*domain.AuthorizationCode, error)
	Insert(ctx context.Context, c *domain.AuthorizationCode) error
	MarkUsed(ctx context.Context, code string) error
	DeleteAllByUser(ctx context.Context, userID string) error
}

// RefreshTokenRepository persists refresh tokens.
type RefreshTokenRepository interface {
	FindByTokenHash(ctx context.Context, hash string) (*domain.RefreshToken, error)
	Insert(ctx context.Context, t *domain.RefreshToken) error
	Revoke(ctx context.Context, id string) error
	DeleteAllByUser(ctx context.Context, userID string) error
}

// InviteCodeRepository persists invite codes.
type InviteCodeRepository interface {
	Create(ctx context.Context, createdBy string, kind domain.InviteCodeKind, grants *domain.MembershipTier, grantDays *int64, grantsUserType *domain.UserType) (*domain.InviteCode, error)
	GetByCode(ctx context.Context, code string) (*domain.InviteCode, error)
	// MarkUsed atomically marks the code used (compare-and-swap on the store's
	// optimistic concurrency token). Returns an "already used" error on a race.
	MarkUsed(ctx context.Context, code, userID string) error
	List(ctx context.Context, usedOnly *bool) ([]domain.InviteCode, error)
	Revoke(ctx context.Context, code string) error
}

// TeamRepository persists teams.
type TeamRepository interface {
	FindByID(ctx context.Context, id string) (*domain.Team, error)
	FindAllOpen(ctx context.Context) ([]domain.Team, error)
	FindAllOwnedByUser(ctx context.Context, userID string) ([]domain.Team, error)
	Insert(ctx context.Context, t *domain.Team) error
	Update(ctx context.Context, t *domain.Team) error
	DeleteByID(ctx context.Context, id string) error
}

// TeamMembershipRepository persists team memberships.
type TeamMembershipRepository interface {
	FindAllByTeam(ctx context.Context, teamID string) ([]domain.TeamMembership, error)
	FindAllByUser(ctx context.Context, userID string) ([]domain.TeamMembership, error)
	Find(ctx context.Context, teamID, userID string) (*domain.TeamMembership, error)
	Insert(ctx context.Context, m *domain.TeamMembership) error
	CountByTeam(ctx context.Context, teamID string) (uint64, error)
	Delete(ctx context.Context, teamID, userID string) error
	DeleteAllByTeam(ctx context.Context, teamID string) error
	DeleteAllByUser(ctx context.Context, userID string) error
}

// SmsVerifyResult is the outcome of a SmsCodeStore.VerifyCode call.
type SmsVerifyResult int

const (
	// SmsVerifyOK means the submitted code matched and was consumed.
	SmsVerifyOK SmsVerifyResult = iota
	// SmsVerifyInvalid means the submitted code was wrong and attempts remain.
	SmsVerifyInvalid
	// SmsVerifyExpired means no active code exists for the phone (never sent,
	// expired, or already consumed).
	SmsVerifyExpired
	// SmsVerifyAttemptsExceeded means the attempt cap was hit; the code was
	// invalidated.
	SmsVerifyAttemptsExceeded
)

// SmsScene identifies the purpose a verification code was minted for. A code
// lives under a per-scene key, so a code texted for one action can never drive
// another (ADR 0010). The send cooldown and daily cap are deliberately NOT
// scoped by scene — they stay keyed by phone alone.
type SmsScene string

const (
	// SmsSceneLogin is the SMS login-or-register flow (/api/auth/sms/verify).
	SmsSceneLogin SmsScene = "login"
	// SmsSceneBindPhone proves phone ownership for binding a phone to an
	// account: POST /api/users/me/phone and the wechat_phone_bind grant.
	SmsSceneBindPhone SmsScene = "bind_phone"
	// SmsSceneResetPassword is the 找回密码 set-or-reset flow (endpoint not
	// built yet; the scene is reserved so its codes are isolated from now on).
	SmsSceneResetPassword SmsScene = "reset_password"
)

// SmsSceneFromRequest parses a client-supplied scene value. The empty string
// defaults to SmsSceneLogin so clients predating scenes keep working; a
// non-empty unknown value is rejected (ok=false).
func SmsSceneFromRequest(raw string) (SmsScene, bool) {
	if raw == "" {
		return SmsSceneLogin, true
	}
	switch SmsScene(raw) {
	case SmsSceneLogin, SmsSceneBindPhone, SmsSceneResetPassword:
		return SmsScene(raw), true
	default:
		return "", false
	}
}

// SmsCodeStore is the backing store for the short-lived, single-use SMS
// verification codes (Redis). Every method fails — rather than falling back —
// when Redis is unreachable, so the SMS endpoints fail closed on a Redis
// outage.
type SmsCodeStore interface {
	// ReserveCooldown atomically claims a send slot for phone inside the
	// current 60-second window. It returns false (no error) once the window's
	// send cap is exhausted; the running window is left untouched.
	ReserveCooldown(ctx context.Context, phone string) (bool, error)
	// ReserveDailyCount atomically increments the per-phone daily send counter
	// (24h window), returning an error when the configured daily cap is
	// reached. On an over-limit call the counter is left unchanged.
	ReserveDailyCount(ctx context.Context, phone string) error
	// StoreCode records a fresh verification code for phone under the scene's
	// key (stored as a SHA-256 hash, single-use, with the given TTL) and resets
	// the failed attempt counter.
	StoreCode(ctx context.Context, scene SmsScene, phone, code string, ttl time.Duration) error
	// VerifyCode checks a submitted code against the one stored for the scene.
	// On success the code is consumed atomically (SmsVerifyOK). A mismatch
	// increments the failed-attempt counter; maxAttempts failed attempts
	// invalidate the code (SmsVerifyAttemptsExceeded). A missing record is
	// SmsVerifyExpired.
	VerifyCode(ctx context.Context, scene SmsScene, phone, code string, maxAttempts int) (SmsVerifyResult, error)
	// ReleaseSend undoes a reserved window slot and daily increment when a
	// send failed before the code was stored (best-effort).
	ReleaseSend(ctx context.Context, phone string) error
	// Ping verifies Redis connectivity (used by the test harness to skip when
	// Redis is unavailable, mirroring the MySQL convention).
	Ping(ctx context.Context) error
}

// OAuthState is the payload behind an opaque third-party OAuth2 link state
// handle. The handle itself carries no information; this payload is stored
// server-side and consumed once.
type OAuthState struct {
	UserID      string    `json:"user_id"`
	AppID       string    `json:"app_id"`
	ProviderID  string    `json:"provider_id"`
	RedirectURI string    `json:"redirect_uri"`
	CreatedAt   time.Time `json:"created_at"`
}

// OAuthStateStore is the backing store for third-party OAuth2 link state
// handles (Redis). It is fail-closed: an unreachable store surfaces as a 503,
// so a callback can never proceed with a state check that did not happen.
type OAuthStateStore interface {
	// StoreState records the payload under handle with the given TTL.
	StoreState(ctx context.Context, handle string, st OAuthState, ttl time.Duration) error
	// ConsumeState atomically returns the payload and deletes it (single use).
	// A missing or expired handle returns (nil, nil).
	ConsumeState(ctx context.Context, handle string) (*OAuthState, error)
}

// Repository is the composite store handed to handlers.
type Repository interface {
	Users() UserRepository
	Applications() ApplicationRepository
	Accounts() AccountRepository
	AppProviders() AppProviderRepository
	AuthCodes() AuthCodeRepository
	RefreshTokens() RefreshTokenRepository
	InviteCodes() InviteCodeRepository
	Teams() TeamRepository
	TeamMemberships() TeamMembershipRepository

	// DeleteUser removes a user and every dependent row in one transaction.
	// When the target is an active administrator it locks the active-admin rows
	// first and refuses if none other would remain, so two concurrent admin
	// deletions can never leave the system with zero administrators (and no way
	// back into the console).
	DeleteUser(ctx context.Context, userID string) error
}
