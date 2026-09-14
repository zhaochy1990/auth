package brandoauth

// corosDescriptor describes COROS, the first brand on this layer.
//
// Several endpoint paths, the client authentication style and the expiry field
// format are not yet confirmed against COROS's official open-platform docs
// (see the ticket's "需要向高驰确认的项"). They are collected here so that
// confirming them is a one-place edit — no engine change. Until then, local and
// CI runs exercise the whole flow through mock mode (Config.Mock + the service
// master switch) or a fake server pointed at via base_url.
var corosDescriptor = Descriptor{
	ID:             "coros",
	DisplayName:    "高驰 COROS",
	DefaultBaseURL: "https://open.coros.com",
	// Paths pending confirmation from COROS.
	AuthorizePath:   "/oauth/authorize",
	TokenPath:       "/oauth/token",
	UserInfoPath:    "/oauth/userinfo",
	DeauthorizePath: "/oauth/deauthorize",
	DefaultScopes:   []string{"openid", "profile"},
	// Client authentication style pending confirmation (form body vs Basic).
	ClientAuthStyle: ClientAuthForm,
	Token: TokenResponseMapping{
		AccessTokenField:  "accessToken",
		RefreshTokenField: "refreshToken",
		ExpiresInField:    "expiresIn",
		ExpiresInFormat:   ExpirySeconds,
		// The stable COROS user id is the hard dependency for the uniqueness
		// constraint; it is read from the token response when present, else
		// from the userinfo endpoint.
		UserIDField: "openId",
	},
	UserInfoIDPath: "openId",
}
