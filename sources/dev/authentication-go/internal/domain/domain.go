// Package domain holds the storage-agnostic entity models and value types for
// the auth service. These structs are the lingua franca between the handlers
// (business logic) and the repository adapters (storage). They carry no
// storage- or transport-specific concerns so the backing store can be swapped
// without touching business logic.
package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// MembershipTier is the entitlement level of a user, orthogonal to Role (which
// governs authorization: admin vs user). Regular is the free default; paid
// tiers (Vip1, and future Vip2/Vip3) may carry an expiry via
// User.MembershipExpiresAt. Stored/transmitted as the snake_case string.
type MembershipTier string

const (
	MembershipRegular MembershipTier = "regular"
	MembershipVip1    MembershipTier = "vip1"
)

// MembershipFromString parses the snake_case representation, falling back to
// Regular for unknown values so old/foreign rows never fail to deserialize.
func MembershipFromString(s string) MembershipTier {
	switch s {
	case string(MembershipVip1):
		return MembershipVip1
	default:
		return MembershipRegular
	}
}

// IsPaid reports whether this is a paid tier (anything other than Regular).
func (t MembershipTier) IsPaid() bool { return t != MembershipRegular && t != "" }

// UserType classifies account usage, independent of Role and Membership.
// Regular is the default for all historical users; Testing marks accounts whose
// data should be treated as non-production/test data by downstream systems.
type UserType string

const (
	UserTypeRegular UserType = "regular"
	UserTypeTesting UserType = "testing"
)

// UserTypeFromString parses the snake_case representation, falling back to
// Regular so old/foreign rows never fail to deserialize.
func UserTypeFromString(s string) UserType {
	switch s {
	case string(UserTypeTesting):
		return UserTypeTesting
	default:
		return UserTypeRegular
	}
}

// Valid reports whether this value is a supported user type.
func (t UserType) Valid() bool { return t == UserTypeRegular || t == UserTypeTesting }

// InviteCodeKind is the reuse policy of an invite code.
//
// SingleUse codes are consumed by the first successful registration and then
// rejected. LongTerm codes can be used by any number of registrations and are
// never marked used; they are disabled only via revoke.
type InviteCodeKind string

const (
	InviteSingleUse InviteCodeKind = "single_use"
	InviteLongTerm  InviteCodeKind = "long_term"
)

// InviteKindFromString parses the stored value, defaulting to SingleUse so rows
// that predate the field deserialize as single-use.
func InviteKindFromString(s string) InviteCodeKind {
	switch s {
	case string(InviteLongTerm):
		return InviteLongTerm
	default:
		return InviteSingleUse
	}
}

// LoginRecord is a single login event (timestamp + IP).
type LoginRecord struct {
	At time.Time
	IP string
}

// PhoneNumber is a mainland-China mobile number, stored as bare 11 digits
// (no +86 prefix). It is a value type: valid values always match
// `^1[3-9]\d{9}$`.
type PhoneNumber string

var phoneNumberRe = regexp.MustCompile(`^1[3-9]\d{9}$`)

// ParsePhoneNumber validates and normalizes a mainland-China mobile number.
// Surrounding whitespace is trimmed; anything that does not match
// `^1[3-9]\d{9}$` (11 digits starting 1[3-9]) is rejected.
func ParsePhoneNumber(s string) (PhoneNumber, error) {
	s = strings.TrimSpace(s)
	if !phoneNumberRe.MatchString(s) {
		return "", fmt.Errorf("invalid phone number %q", s)
	}
	return PhoneNumber(s), nil
}

// Valid reports whether the value is a well-formed mainland-China mobile
// number. Zero-value and hand-built values may be invalid.
func (p PhoneNumber) Valid() bool { return phoneNumberRe.MatchString(string(p)) }

// String returns the bare 11-digit form.
func (p PhoneNumber) String() string { return string(p) }

// Masked returns a masked display form (e.g. 138****5678) for UI fallback.
func (p PhoneNumber) Masked() string {
	s := string(p)
	if !p.Valid() {
		return s
	}
	return s[:3] + "****" + s[7:]
}

// User is an end user of the system.
type User struct {
	ID    string
	Email *string
	// Phone is the user's mainland-China mobile number (bare 11 digits, no
	// +86 prefix). Nil for users without a phone (email/WeChat accounts). At
	// most one account holds a given phone number.
	Phone         *string
	Name          *string
	AvatarURL     *string
	EmailVerified bool
	Role          string // "user" | "admin"
	// UserType classifies whether this is a normal account or a testing account.
	UserType UserType
	IsActive bool
	// Note is an admin-only free-form note, never surfaced via user-facing APIs.
	Note *string
	// CustomAttributes holds app-specific user profile attributes such as
	// birthday, gender, height_cm, and weight_kg.
	CustomAttributes map[string]any
	CreatedAt        time.Time
	UpdatedAt        time.Time
	// LastLoginAt is the most recent successful login timestamp.
	LastLoginAt *time.Time
	// RecentLogins holds the last 3 login records (most recent first).
	RecentLogins []LoginRecord
	// InviteCode is the code this user registered with, if invite-gated.
	// Backend-only (not surfaced via user-facing APIs).
	InviteCode *string
	// Membership is the stored entitlement tier, independent of Role.
	Membership MembershipTier
	// MembershipExpiresAt is when a paid membership lapses. Nil means no expiry
	// (permanent grant, or a Regular user).
	MembershipExpiresAt *time.Time
	// WeChatBound reports whether the account has at least one bound WeChat
	// identity (see WeChatLink). Populated by the repository layer.
	WeChatBound bool
}

// WeChatLink is a single bound WeChat identity: one mini-program appid and the
// openid of the WeChat user within it. A user may hold links for several
// mini-programs; the same WeChat person has a different openid per mini-program
// and is correlated across mini-programs by UnionID (when present).
type WeChatLink struct {
	UserID      string
	WeChatAppID string
	OpenID      string
	UnionID     *string
	CreatedAt   time.Time
}

// IsMembershipExpired reports whether a paid membership has lapsed as of now.
// Regular users and paid memberships without an expiry are never expired.
func (u *User) IsMembershipExpired(now time.Time) bool {
	return u.Membership.IsPaid() &&
		u.MembershipExpiresAt != nil &&
		!u.MembershipExpiresAt.After(now)
}

// EffectiveMembership returns the tier as of now: the stored tier, or Regular
// if the paid membership has expired.
func (u *User) EffectiveMembership(now time.Time) MembershipTier {
	if u.IsMembershipExpired(now) {
		return MembershipRegular
	}
	return u.Membership
}

// Application is an OAuth2 client application.
type Application struct {
	ID               string
	Name             string
	ClientID         string
	ClientSecretHash string
	RedirectURIs     string // JSON-encoded array
	AllowedScopes    string // JSON-encoded array
	IsActive         bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
	// WeChatAppID is the WeChat mini-program app id configured for this
	// application. Empty (with an empty WeChatAppSecret) disables the WeChat
	// login endpoints for this application.
	WeChatAppID string
	// WeChatAppSecret is the WeChat mini-program app secret configured for this
	// application. It must be sent in plaintext to WeChat's code2Session API, so
	// it is stored as configured (encrypt at rest if the deployment requires it)
	// rather than hashed.
	WeChatAppSecret string
}

// AppProvider is an auth-provider configuration attached to an Application.
type AppProvider struct {
	ID         string
	AppID      string
	ProviderID string
	Config     string // JSON-encoded provider config
	IsActive   bool
	CreatedAt  time.Time
}

// Account links a user to a provider identity (and, for password, a credential).
type Account struct {
	ID                string
	UserID            string
	ProviderID        string
	ProviderAccountID *string
	Credential        *string
	ProviderMetadata  string // JSON-encoded
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// AuthorizationCode is a short-lived OAuth2 authorization code (with PKCE).
type AuthorizationCode struct {
	Code                string
	AppID               string
	UserID              string
	RedirectURI         string
	Scopes              string // JSON-encoded array
	CodeChallenge       *string
	CodeChallengeMethod *string
	ExpiresAt           time.Time
	Used                bool
	CreatedAt           time.Time
}

// RefreshToken is a hashed, rotating refresh token.
type RefreshToken struct {
	ID        string
	UserID    string
	AppID     string
	TokenHash string
	Scopes    string // JSON-encoded array
	DeviceID  *string
	ExpiresAt time.Time
	Revoked   bool
	CreatedAt time.Time
}

// InviteCode gates registration and may grant a membership tier.
type InviteCode struct {
	ID        string
	Code      string
	CreatedBy string
	CreatedAt time.Time
	UsedAt    *time.Time
	UsedBy    *string
	IsRevoked bool
	Kind      InviteCodeKind
	// GrantsMembership, when a paid tier, grants that tier on registration.
	GrantsMembership *MembershipTier
	// GrantsMembershipDays bounds the granted membership's validity (in days),
	// counted from registration. Nil with a set GrantsMembership = permanent.
	GrantsMembershipDays *int64
	// GrantsUserType, when set, classifies users registered with this code.
	GrantsUserType *UserType
}

// Team is a user-owned group.
type Team struct {
	ID          string
	Name        string
	Description *string
	OwnerUserID string
	IsOpen      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TeamMembership links a user to a team with a role ("owner" | "member").
type TeamMembership struct {
	TeamID   string
	UserID   string
	Role     string
	JoinedAt time.Time
}
