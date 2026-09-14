# Third-party watch account linking is a generic, provider-parameterized OAuth2 layer

STRIDE links watch brands (COROS first, then Strava / Suunto / Polar) with the
standard OAuth2 authorization-code flow, so the user never hands us their watch
password and we hold a revocable token instead of a password equivalent. The
engine is deliberately **parameterized by provider**, not written per brand: a
brand is a `Descriptor` (endpoint paths, token field names, client-auth style,
where the stable user id lives) in one registry, and the callback route is
`/oauth/link/{provider_id}/callback`. Adding the second brand should be a
descriptor plus one provider config row; if it needs a change to the callback
flow, this abstraction is wrong (stride-devops#284).

**Why not extend the existing credential-validation provider interface.** Its
shape is "take a credential, verify it, return an identity" — a single
request/response. OAuth2 linking is "send the user to a browser, get redirected
back, exchange a code". They share no abstraction; forcing OAuth2 into the
credential interface would deform both. WeChat also stays on its own
`token_exchange` path for the same reason. Brand OAuth2 therefore gets its own
package (`internal/brandoauth`), a pure HTTP client with no framework and no
storage dependency, mirroring the existing `internal/wechat` client.

**Why the brand's structural differences are code, not configuration.** Token
field names, the client-authentication style (form body vs HTTP Basic), the
expiry format and the user-id path are compile-time facts about a brand — the
kind of thing that is unit-testable and should never be typed into an
operations console. Only operator inputs (`client_id`, `client_secret`,
`scopes`, `base_url`, `authorize_params`, `mock`) live in the application's
provider config, edited from the admin dashboard. The service adds exactly two
config items: the externally reachable base URL used to build the callback URL
(never derived from the request `Host`, which a reverse proxy can be tricked
into spoofing) and a service-wide mock master switch. Mocking requires **both**
that switch and the application's own `mock` flag, so one bad production config
cannot silently turn real bindings into fakes.

**Why the state lives in Redis.** The state is an opaque 128-hex handle whose
payload (STRIDE user id, app id, provider id, redirect URI, created-at) is
stored server-side with a 10-minute TTL and consumed by an atomic get-and-delete
— single use, no replay. Storing the *user id* server-side, written from the
authenticated Bearer session and never accepted from the callback, is the pivot
of the CSRF argument: an attacker-minted state can only bind the attacker's own
STRIDE user, and a state leaked to a victim still resolves to the attacker's
account. The callback cross-checks the state's provider id against the URL path,
so a state minted for one brand cannot drive another's callback. The store fails
closed (503) when Redis is down rather than degrading to "skip the check".
Sessions are not double-submitted: the state already is a standard anti-CSRF
token.

**Why tokens are stored in plaintext.** The refresh token goes in the existing
`credential` column and the access token plus expiry, scope and the brand's raw
responses go in `provider_metadata`.`access_token` is kept as a cache; the raw
responses are preserved verbatim so a future data-sync integration does not have
to re-fetch. This matches the existing WeChat-secret precedent. Encrypt-at-rest
is a separate follow-up: the repository has no encryption helper and the
production key management is not ready, so encrypting now would only add a new
failure mode ("the key is lost, every user's grant is void"). `provider_account_id`
(the brand's stable user id) is required: without it `UNIQUE (provider_id,
provider_account_id)` silently stops constraining and one watch account could be
bound to two STRIDE users. A brand that fails to return a stable user id surfaces
as an error rather than a silent NULL.

**Why no mini-program path, and why Garmin is separate.** The WeChat mini-program
`web-view` cannot open COROS's authorization page at all — every URL in the
web-view must be on a business-domain allowlist that requires hosting a
verification file at the target domain root, which we cannot do for COROS. That
is a WeChat-container limitation independent of OAuth2; the mini-program route
is evaluated later (system browser / STRIDE H5 / COROS mini-program support).
Garmin's OAuth1→OAuth2 two-stage SSO has a different topology and does not reuse
this parameterized callback; it will be its own work with no storage change.
Token refresh is out of scope in v1 (no caller yet) though the refresh token is
stored complete. `running`'s watch-data sync still talks to COROS's unofficial
Training Hub API; moving it to the official open platform is a separate change.

Status: accepted (stride-devops#284).
