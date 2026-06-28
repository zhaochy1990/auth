// Package auth holds the security core: JWT (RS256) issuance/verification,
// password and client-secret hashing, password policy, PKCE, and the OAuth2
// authorization-code / refresh-token helpers. It mirrors the Rust `auth`
// module (jwt.rs, password.rs, oauth2.rs).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/config"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/repository"
)

// ─── JWT ─────────────────────────────────────────────────────────────────────

// AccessClaims is the access-token payload. Field names and shape match the
// Rust `Claims` (aud is a single string, membership is a snake_case string).
type AccessClaims struct {
	Sub        string   `json:"sub"`
	Aud        string   `json:"aud"`
	Iss        string   `json:"iss"`
	Exp        int64    `json:"exp"`
	Iat        int64    `json:"iat"`
	Scopes     []string `json:"scopes"`
	Role       string   `json:"role"`
	Membership string   `json:"membership"`
	Name       *string  `json:"name,omitempty"`
}

func (c AccessClaims) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.Exp, 0)), nil
}
func (c AccessClaims) GetIssuedAt() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.Iat, 0)), nil
}
func (c AccessClaims) GetNotBefore() (*jwt.NumericDate, error) { return nil, nil }
func (c AccessClaims) GetIssuer() (string, error)              { return c.Iss, nil }
func (c AccessClaims) GetSubject() (string, error)             { return c.Sub, nil }
func (c AccessClaims) GetAudience() (jwt.ClaimStrings, error)  { return jwt.ClaimStrings{c.Aud}, nil }

// Tier returns the effective membership tier from the claim, treating an
// absent/unknown value as Regular.
func (c AccessClaims) Tier() domain.MembershipTier {
	return domain.MembershipFromString(c.Membership)
}

// AppClaims is the client-credentials token payload.
type AppClaims struct {
	Sub       string `json:"sub"`
	Iss       string `json:"iss"`
	Exp       int64  `json:"exp"`
	Iat       int64  `json:"iat"`
	GrantType string `json:"grant_type"`
}

func (c AppClaims) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.Exp, 0)), nil
}
func (c AppClaims) GetIssuedAt() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.Iat, 0)), nil
}
func (c AppClaims) GetNotBefore() (*jwt.NumericDate, error) { return nil, nil }
func (c AppClaims) GetIssuer() (string, error)              { return c.Iss, nil }
func (c AppClaims) GetSubject() (string, error)             { return c.Sub, nil }
func (c AppClaims) GetAudience() (jwt.ClaimStrings, error)  { return nil, nil }

// JWTManager issues and verifies RS256 tokens.
type JWTManager struct {
	priv             *rsa.PrivateKey
	pub              *rsa.PublicKey
	issuer           string
	accessExpirySecs int64
}

// NewJWTManager loads the RSA keypair from disk.
func NewJWTManager(cfg *config.Config) (*JWTManager, error) {
	privBytes, err := os.ReadFile(cfg.JWTPrivateKeyPath)
	if err != nil {
		return nil, err
	}
	pubBytes, err := os.ReadFile(cfg.JWTPublicKeyPath)
	if err != nil {
		return nil, err
	}
	priv, err := jwt.ParseRSAPrivateKeyFromPEM(privBytes)
	if err != nil {
		return nil, err
	}
	pub, err := jwt.ParseRSAPublicKeyFromPEM(pubBytes)
	if err != nil {
		return nil, err
	}
	return &JWTManager{priv: priv, pub: pub, issuer: cfg.JWTIssuer, accessExpirySecs: cfg.JWTAccessTokenExpirySecs}, nil
}

// IssueAccessToken mints a user access token.
func (m *JWTManager) IssueAccessToken(userID, clientID string, scopes []string, role string, membership domain.MembershipTier, name *string) (string, error) {
	if scopes == nil {
		scopes = []string{}
	}
	now := time.Now().Unix()
	claims := AccessClaims{
		Sub: userID, Aud: clientID, Iss: m.issuer,
		Exp: now + m.accessExpirySecs, Iat: now,
		Scopes: scopes, Role: role, Membership: string(membership), Name: name,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := tok.SignedString(m.priv)
	if err != nil {
		return "", apperror.Internal()
	}
	return s, nil
}

// IssueAppToken mints a client-credentials token.
func (m *JWTManager) IssueAppToken(appID string) (string, error) {
	now := time.Now().Unix()
	claims := AppClaims{Sub: appID, Iss: m.issuer, Exp: now + m.accessExpirySecs, Iat: now, GrantType: "client_credentials"}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := tok.SignedString(m.priv)
	if err != nil {
		return "", apperror.Internal()
	}
	return s, nil
}

// AccessTokenExpirySecs exposes the configured access-token TTL.
func (m *JWTManager) AccessTokenExpirySecs() int64 { return m.accessExpirySecs }

// VerifyAccessToken validates and parses a user access token. It enforces the
// issuer and the required claims (sub, aud, exp, iat) — matching the Rust
// verifier's set_required_spec_claims. The audience value itself is not
// validated (no expected audience is configured).
func (m *JWTManager) VerifyAccessToken(token string) (*AccessClaims, error) {
	claims := &AccessClaims{}
	_, err := jwt.ParseWithClaims(token, claims, m.keyfunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, apperror.InvalidToken()
	}
	// Reject a validly-signed token missing any required claim (sub/aud/iat);
	// exp is already enforced by WithExpirationRequired.
	if claims.Sub == "" || claims.Aud == "" || claims.Iat == 0 {
		return nil, apperror.InvalidToken()
	}
	return claims, nil
}

// VerifyAppToken validates and parses a client-credentials token.
func (m *JWTManager) VerifyAppToken(token string) (*AppClaims, error) {
	claims := &AppClaims{}
	_, err := jwt.ParseWithClaims(token, claims, m.keyfunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(m.issuer),
	)
	if err != nil {
		return nil, apperror.InvalidToken()
	}
	return claims, nil
}

func (m *JWTManager) keyfunc(_ *jwt.Token) (interface{}, error) { return m.pub, nil }

// ─── Password & client secrets ───────────────────────────────────────────────

// HashPassword hashes a password with Argon2id (PHC string output).
func HashPassword(password string) (string, error) {
	h, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		return "", apperror.Internal()
	}
	return h, nil
}

// VerifyPassword reports whether password matches the stored Argon2id hash.
// A malformed hash yields an internal error; a mismatch yields (false, nil).
func VerifyPassword(password, hash string) (bool, error) {
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false, apperror.Internal()
	}
	return match, nil
}

// HashClientSecret hashes a high-entropy client secret with SHA-256. Argon2's
// brute-force resistance is unnecessary here and its cost would bottleneck
// every OAuth2 request.
func HashClientSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyClientSecret verifies a client secret. Supports SHA-256 (new) and
// Argon2 (legacy) hashes.
func VerifyClientSecret(secret, hash string) (bool, error) {
	if hexHash, ok := strings.CutPrefix(hash, "sha256:"); ok {
		sum := sha256.Sum256([]byte(secret))
		computed := hex.EncodeToString(sum[:])
		if len(computed) != len(hexHash) {
			return false, nil
		}
		return subtle.ConstantTimeCompare([]byte(computed), []byte(hexHash)) == 1, nil
	}
	return VerifyPassword(secret, hash)
}

// ValidatePassword enforces password complexity. Mirrors the Rust rules.
func ValidatePassword(password string) error {
	if len(password) < 8 {
		return apperror.BadRequest("Password must be at least 8 characters")
	}
	if len(password) > 128 {
		return apperror.BadRequest("Password must not exceed 128 characters")
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		}
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
		if !(unicode.IsLetter(r) || unicode.IsNumber(r)) {
			hasSpecial = true
		}
	}
	if !hasUpper {
		return apperror.BadRequest("Password must contain at least one uppercase letter")
	}
	if !hasLower {
		return apperror.BadRequest("Password must contain at least one lowercase letter")
	}
	if !hasDigit {
		return apperror.BadRequest("Password must contain at least one digit")
	}
	if !hasSpecial {
		return apperror.BadRequest("Password must contain at least one special character")
	}
	return nil
}

// ─── OAuth2 helpers (codes, tokens, PKCE) ────────────────────────────────────

// RandomHex returns nBytes of crypto-random data, hex-encoded. Single source
// for the service's random tokens, client secrets, and ids.
func RandomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GenerateAuthCode returns a cryptographically random authorization code.
func GenerateAuthCode() string { return RandomHex(64) }

// GenerateRefreshToken returns a cryptographically random refresh token.
func GenerateRefreshToken() string { return RandomHex(32) }

// GenerateClientID returns an OAuth2 client_id of the form "app_<24 chars>".
func GenerateClientID() string {
	return "app_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
}

// HashToken hashes a token with SHA-256 for storage.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// VerifyPKCE verifies a PKCE code_verifier against a code_challenge.
func VerifyPKCE(verifier, challenge, method string) bool {
	switch method {
	case "S256":
		sum := sha256.Sum256([]byte(verifier))
		return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
	case "plain":
		return verifier == challenge
	default:
		return false
	}
}

func encodeScopes(scopes []string) string {
	if scopes == nil {
		scopes = []string{}
	}
	b, _ := json.Marshal(scopes)
	return string(b)
}

// DecodeStringArray unmarshals a JSON string array, returning a non-nil empty
// slice on error or null (so it always serializes back as []). Shared by scope
// decoding and the handlers that read stored redirect_uris/allowed_scopes.
func DecodeStringArray(s string) []string {
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil || out == nil {
		return []string{}
	}
	return out
}

// StoreAuthCode persists an authorization code (10-minute TTL).
func StoreAuthCode(ctx context.Context, repo repository.Repository, code, appID, userID, redirectURI string, scopes []string, challenge, method *string) error {
	now := time.Now().UTC()
	ac := &domain.AuthorizationCode{
		Code:                code,
		AppID:               appID,
		UserID:              userID,
		RedirectURI:         redirectURI,
		Scopes:              encodeScopes(scopes),
		CodeChallenge:       challenge,
		CodeChallengeMethod: method,
		ExpiresAt:           now.Add(10 * time.Minute),
		Used:                false,
		CreatedAt:           now,
	}
	return repo.AuthCodes().Insert(ctx, ac)
}

// ExchangeAuthCode validates an authorization code, enforces PKCE, marks it
// used, and returns the user id and granted scopes.
func ExchangeAuthCode(ctx context.Context, repo repository.Repository, code, appID, redirectURI string, verifier *string) (string, []string, error) {
	ac, err := repo.AuthCodes().FindByCode(ctx, code)
	if err != nil {
		return "", nil, err
	}
	if ac == nil || ac.Used {
		return "", nil, apperror.InvalidAuthorizationCode()
	}
	if ac.AppID != appID {
		return "", nil, apperror.InvalidAuthorizationCode()
	}
	if ac.RedirectURI != redirectURI {
		return "", nil, apperror.InvalidRedirectURI()
	}
	if ac.ExpiresAt.Before(time.Now().UTC()) {
		return "", nil, apperror.AuthorizationCodeExpired()
	}
	if ac.CodeChallenge != nil {
		method := "plain"
		if ac.CodeChallengeMethod != nil {
			method = *ac.CodeChallengeMethod
		}
		if verifier == nil {
			return "", nil, apperror.InvalidCodeVerifier()
		}
		if !VerifyPKCE(*verifier, *ac.CodeChallenge, method) {
			return "", nil, apperror.InvalidCodeVerifier()
		}
	}
	if err := repo.AuthCodes().MarkUsed(ctx, code); err != nil {
		return "", nil, err
	}
	return ac.UserID, DecodeStringArray(ac.Scopes), nil
}

// StoreRefreshToken persists a hashed refresh token.
func StoreRefreshToken(ctx context.Context, repo repository.Repository, userID, appID, token string, scopes []string, deviceID *string, expiryDays int64) error {
	now := time.Now().UTC()
	rt := &domain.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    userID,
		AppID:     appID,
		TokenHash: HashToken(token),
		Scopes:    encodeScopes(scopes),
		DeviceID:  deviceID,
		ExpiresAt: now.AddDate(0, 0, int(expiryDays)),
		Revoked:   false,
		CreatedAt: now,
	}
	return repo.RefreshTokens().Insert(ctx, rt)
}

// RotateRefreshToken validates a refresh token and issues a replacement,
// revoking the old one. Returns the user id, new token, and scopes.
func RotateRefreshToken(ctx context.Context, repo repository.Repository, token, appID string, expiryDays int64) (string, string, []string, error) {
	stored, err := repo.RefreshTokens().FindByTokenHash(ctx, HashToken(token))
	if err != nil {
		return "", "", nil, err
	}
	if stored == nil {
		return "", "", nil, apperror.InvalidToken()
	}
	if stored.Revoked {
		return "", "", nil, apperror.TokenRevoked()
	}
	if stored.AppID != appID {
		return "", "", nil, apperror.InvalidToken()
	}
	if stored.ExpiresAt.Before(time.Now().UTC()) {
		return "", "", nil, apperror.RefreshTokenExpired()
	}
	if err := repo.RefreshTokens().Revoke(ctx, stored.ID); err != nil {
		return "", "", nil, err
	}
	newToken := GenerateRefreshToken()
	scopes := DecodeStringArray(stored.Scopes)
	if err := StoreRefreshToken(ctx, repo, stored.UserID, appID, newToken, scopes, stored.DeviceID, expiryDays); err != nil {
		return "", "", nil, err
	}
	return stored.UserID, newToken, scopes, nil
}

// RevokeRefreshToken revokes a refresh token by its raw value.
func RevokeRefreshToken(ctx context.Context, repo repository.Repository, token string) error {
	stored, err := repo.RefreshTokens().FindByTokenHash(ctx, HashToken(token))
	if err != nil {
		return err
	}
	if stored == nil {
		return apperror.InvalidToken()
	}
	return repo.RefreshTokens().Revoke(ctx, stored.ID)
}
