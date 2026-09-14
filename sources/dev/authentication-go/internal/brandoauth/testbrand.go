package brandoauth

// TestBrand is a second registered brand used only when the service runs with
// test providers enabled (auth_enable_test_providers). It lets integration
// tests exercise provider-parameterized guards — notably rejecting a state
// minted for one provider when it arrives at another provider's callback —
// without registering a real second brand. It is never part of the production
// registry (see server.NewRouter).
func TestBrand() Descriptor {
	return Descriptor{
		ID:              "testbrand",
		DisplayName:     "Test Brand",
		DefaultBaseURL:  "https://testbrand.invalid",
		AuthorizePath:   "/oauth/authorize",
		TokenPath:       "/oauth/token",
		UserInfoPath:    "/oauth/userinfo",
		DeauthorizePath: "/oauth/deauthorize",
		DefaultScopes:   []string{"openid"},
		ClientAuthStyle: ClientAuthForm,
		Token: TokenResponseMapping{
			AccessTokenField:  "accessToken",
			RefreshTokenField: "refreshToken",
			ExpiresInField:    "expiresIn",
			ExpiresInFormat:   ExpirySeconds,
			UserIDField:       "openId",
		},
		UserInfoIDPath: "openId",
	}
}
