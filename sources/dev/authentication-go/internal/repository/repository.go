// Package repository defines the storage abstraction for the auth service.
//
// This is the swappable adapter boundary: handlers depend only on these
// interfaces, never on a concrete store. The MySQL runtime adapter lives in the
// mysql subpackage; the Azure Table adapter is retained as a legacy migration
// source.
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
	Insert(ctx context.Context, u *domain.User) error
	Update(ctx context.Context, u *domain.User) error
	DeleteByID(ctx context.Context, id string) error
	CountAll(ctx context.Context) (uint64, error)
	CountSince(ctx context.Context, since time.Time) (uint64, error)
	ListPaginated(ctx context.Context, search string, userType *domain.UserType, sort UserListSort, offset, limit uint64) ([]domain.User, uint64, error)
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
}
