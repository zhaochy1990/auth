// Package snapshot defines a storage-neutral export format for auth data.
package snapshot

import "github.com/zhaochy1990/auth-service/internal/domain"

// Data contains the domain rows needed to move between repository adapters.
type Data struct {
	Applications    []domain.Application
	Users           []domain.User
	Accounts        []domain.Account
	AppProviders    []domain.AppProvider
	AuthCodes       []domain.AuthorizationCode
	RefreshTokens   []domain.RefreshToken
	InviteCodes     []domain.InviteCode
	Teams           []domain.Team
	TeamMemberships []domain.TeamMembership
}

// Counts returns row counts by logical collection name.
func (d Data) Counts() map[string]int {
	return map[string]int{
		"applications":     len(d.Applications),
		"users":            len(d.Users),
		"accounts":         len(d.Accounts),
		"app_providers":    len(d.AppProviders),
		"auth_codes":       len(d.AuthCodes),
		"refresh_tokens":   len(d.RefreshTokens),
		"invite_codes":     len(d.InviteCodes),
		"teams":            len(d.Teams),
		"team_memberships": len(d.TeamMemberships),
	}
}
