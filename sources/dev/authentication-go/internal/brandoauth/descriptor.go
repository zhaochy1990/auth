// Package brandoauth is the pluggable third-party OAuth2 account-linking layer.
//
// A "brand" is one OAuth2 provider (COROS first, then Strava / Suunto / Polar).
// Every brand is described by a Descriptor: the compile-time facts that differ
// between brands (endpoint paths, token field names, client authentication
// style, where the stable user id lives). The Registry is the single dispatch
// point — an unregistered provider id is never served.
//
// This package is a pure HTTP client: no HTTP framework, no storage. Per-brand
// credentials (client_id / client_secret / scopes / base_url / mock switch)
// come from the calling application's provider config; see Config.
package brandoauth

import "strings"

// ClientAuthStyle is how a brand expects the client credentials at the token
// endpoint.
type ClientAuthStyle string

const (
	// ClientAuthForm puts client_id / client_secret in the form body.
	ClientAuthForm ClientAuthStyle = "form"
	// ClientAuthBasic sends them via HTTP Basic.
	ClientAuthBasic ClientAuthStyle = "basic"
)

// ExpiryFormat describes how a token response encodes the access-token expiry.
type ExpiryFormat string

const (
	// ExpirySeconds is a relative lifetime in seconds from now.
	ExpirySeconds ExpiryFormat = "seconds"
	// ExpiryEpochMillis is an absolute epoch timestamp in milliseconds.
	ExpiryEpochMillis ExpiryFormat = "epoch_millis"
	// ExpiryRFC3339 is an absolute RFC3339 timestamp string.
	ExpiryRFC3339 ExpiryFormat = "rfc3339"
)

// TokenResponseMapping declares where a brand puts its token fields and the
// stable user id. Brands differ (snake_case vs camelCase, id in the token
// response vs a separate userinfo call), so these are descriptor facts, not
// configuration.
type TokenResponseMapping struct {
	// AccessTokenField / RefreshTokenField / ExpiresInField are the response
	// keys. An empty ExpiresInField means the token has no declared expiry.
	AccessTokenField  string
	RefreshTokenField string
	ExpiresInField    string
	ExpiresInFormat   ExpiryFormat
	// UserIDField is the token-response key holding the stable user id. When
	// empty, the user id is fetched from the userinfo endpoint instead.
	UserIDField string
}

// Descriptor is one brand's compile-time description.
type Descriptor struct {
	// ID is both the registry key and the value stored in
	// auth_accounts.provider_id.
	ID          string
	DisplayName string
	// DefaultBaseURL is the production open-platform base; a per-application
	// base_url overrides it (e.g. to point at the sandbox or a test server).
	DefaultBaseURL string
	// AuthorizePath / TokenPath / UserInfoPath / DeauthorizePath are appended
	// to the resolved base URL. An empty DeauthorizePath means the brand has no
	// revocation endpoint and deauthorization is a no-op.
	AuthorizePath   string
	TokenPath       string
	UserInfoPath    string
	DeauthorizePath string
	// DefaultScopes apply when the application config does not specify scopes.
	DefaultScopes []string
	// ScopeSeparator joins scopes in the authorize URL; defaults to a space.
	ScopeSeparator string
	// ClientAuthStyle is how the token endpoint authenticates the client.
	ClientAuthStyle ClientAuthStyle
	// Token maps the token response fields.
	Token TokenResponseMapping
	// UserInfoIDPath is the dot path to the stable user id inside the userinfo
	// response, used when Token.UserIDField is empty.
	UserInfoIDPath string
}

// Registry maps provider ids to descriptors.
type Registry struct {
	byID map[string]Descriptor
}

// NewRegistry builds a registry from descriptors.
func NewRegistry(descs ...Descriptor) *Registry {
	r := &Registry{byID: make(map[string]Descriptor, len(descs))}
	for _, d := range descs {
		r.byID[d.ID] = d
	}
	return r
}

// Lookup returns the descriptor for a provider id, if registered.
func (r *Registry) Lookup(id string) (Descriptor, bool) {
	d, ok := r.byID[id]
	return d, ok
}

func (d Descriptor) scopeSeparator() string {
	if d.ScopeSeparator == "" {
		return " "
	}
	return d.ScopeSeparator
}

// baseURL resolves the effective open-platform base.
func (d Descriptor) baseURL(override string) string {
	if override != "" {
		return strings.TrimRight(override, "/")
	}
	return strings.TrimRight(d.DefaultBaseURL, "/")
}

// defaultRegistry is the single brand dispatch point used by the handlers.
// Adding a brand means adding a descriptor here — the callback engine is
// parameterized and does not change.
var defaultRegistry = NewRegistry(corosDescriptor)

// Default returns the process-wide brand registry.
func Default() *Registry { return defaultRegistry }
