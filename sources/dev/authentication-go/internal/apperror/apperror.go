// Package apperror defines the application's typed error model and its mapping
// to HTTP responses. It mirrors the Rust `AppError` enum: each variant carries
// an HTTP status, a stable machine-readable `error` type string, and a
// human-readable message. The JSON body shape is always {"error","message"}.
package apperror

import (
	"errors"
	"net/http"
)

// Error is a typed application error with an HTTP status and stable error code.
type Error struct {
	Status  int
	Type    string // stable machine-readable code, e.g. "invalid_credentials"
	Message string
}

func (e *Error) Error() string { return e.Message }

// New builds an Error.
func New(status int, errType, message string) *Error {
	return &Error{Status: status, Type: errType, Message: message}
}

// As extracts an *Error from any error, falling back to a generic 500.
// The boolean reports whether the error was already an *Error.
func As(err error) (*Error, bool) {
	var ae *Error
	if errors.As(err, &ae) {
		return ae, true
	}
	return Internal(), false
}

// --- Variants (1:1 with the Rust AppError enum) ---

func InvalidCredentials() *Error {
	return New(http.StatusUnauthorized, "invalid_credentials", "Invalid credentials")
}
func UserNotFound() *Error {
	return New(http.StatusNotFound, "user_not_found", "User not found")
}
func UserAlreadyExists() *Error {
	return New(http.StatusConflict, "user_already_exists", "User already exists")
}
func ApplicationNotFound() *Error {
	return New(http.StatusNotFound, "application_not_found", "Application not found")
}
func ApplicationNotActive() *Error {
	return New(http.StatusForbidden, "application_not_active", "Application not active")
}
func ProviderNotSupported(id string) *Error {
	return New(http.StatusBadRequest, "provider_not_supported", "Provider not supported: "+id)
}
func ProviderNotConfigured() *Error {
	return New(http.StatusBadRequest, "provider_not_configured", "Provider not configured for this application")
}
func InvalidAuthorizationCode() *Error {
	return New(http.StatusBadRequest, "invalid_authorization_code", "Invalid authorization code")
}
func AuthorizationCodeExpired() *Error {
	return New(http.StatusBadRequest, "authorization_code_expired", "Authorization code expired")
}
func InvalidRedirectURI() *Error {
	return New(http.StatusBadRequest, "invalid_redirect_uri", "Invalid redirect URI")
}
func InvalidCodeVerifier() *Error {
	return New(http.StatusBadRequest, "invalid_code_verifier", "Invalid PKCE code verifier")
}
func InvalidToken() *Error {
	return New(http.StatusUnauthorized, "invalid_token", "Invalid or expired token")
}
func TokenRevoked() *Error {
	return New(http.StatusUnauthorized, "token_revoked", "Token revoked")
}
func RefreshTokenExpired() *Error {
	return New(http.StatusUnauthorized, "refresh_token_expired", "Refresh token expired")
}
func InvalidScope() *Error {
	return New(http.StatusBadRequest, "invalid_scope", "Invalid scope")
}
func MissingClientID() *Error {
	return New(http.StatusBadRequest, "missing_client_id", "Missing X-Client-Id header")
}
func Unauthorized() *Error {
	return New(http.StatusUnauthorized, "unauthorized", "Unauthorized")
}
func Forbidden() *Error {
	return New(http.StatusForbidden, "forbidden", "Forbidden")
}
func UserDisabled() *Error {
	return New(http.StatusForbidden, "user_disabled", "User account is disabled")
}
func AccountAlreadyLinked() *Error {
	return New(http.StatusConflict, "account_already_linked", "Account already linked")
}
func CannotUnlinkLastAccount() *Error {
	return New(http.StatusBadRequest, "cannot_unlink_last_account", "Cannot unlink last account")
}
func InviteCodeNotFound() *Error {
	return New(http.StatusUnauthorized, "invalid_invite_code", "Invite code not found or invalid")
}
func InviteCodeAlreadyUsed() *Error {
	return New(http.StatusConflict, "invite_code_already_used", "Invite code has already been used")
}
func TeamNotFound() *Error {
	return New(http.StatusNotFound, "team_not_found", "Team not found")
}
func TeamNotOpen() *Error {
	return New(http.StatusForbidden, "team_not_open", "Team is not open for joining")
}
func OwnerCannotLeaveAsLastMember() *Error {
	return New(http.StatusBadRequest, "owner_cannot_leave_as_last_member", "Owner cannot leave team while still the only member")
}
func TeamOwnerRequired() *Error {
	return New(http.StatusForbidden, "team_owner_required", "Only the team owner can perform this action")
}
func TeamTransferTargetNotMember() *Error {
	return New(http.StatusBadRequest, "team_transfer_target_not_member", "New owner must already be a team member")
}
func UserOwnsTeams(n int) *Error {
	return New(http.StatusConflict, "user_owns_teams", "User still owns "+itoa(n)+" team(s)")
}
func BadRequest(msg string) *Error {
	return New(http.StatusBadRequest, "bad_request", msg)
}

// Internal returns a generic 500 with a non-leaky message.
func Internal() *Error {
	return New(http.StatusInternalServerError, "internal_error", "Internal server error")
}

// Database wraps a storage-layer failure as a generic 500 (the underlying
// detail is logged by the caller, never returned to the client).
func Database(_ string) *Error { return Internal() }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
