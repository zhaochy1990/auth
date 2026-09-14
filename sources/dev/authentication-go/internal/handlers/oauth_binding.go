package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/zhaochy1990/x/logger"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/brandoauth"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/middleware"
	"github.com/zhaochy1990/auth-service/internal/repository"
)

// oauthLinkStateTTL bounds how long an authorize URL stays usable.
const oauthLinkStateTTL = 10 * time.Minute

// Link-outcome error codes carried on the redirect back to the application.
// access_denied, invalid_request and server_error are the RFC 6749 §4.1.2.1
// authorization-response codes; account_already_linked is our own business code
// because the standard has no equivalent.
const (
	linkErrInvalidRequest = "invalid_request"
	linkErrServerError    = "server_error"
	linkErrAlreadyLinked  = "account_already_linked"
)

type authorizeAccountRequest struct {
	RedirectURI string `json:"redirect_uri"`
}

type authorizeAccountResponse struct {
	ProviderID   string `json:"provider_id"`
	AuthorizeURL string `json:"authorize_url"`
	ExpiresIn    int64  `json:"expires_in"`
}

// oauthRegistry returns the brand registry (which includes the test brand when
// the service runs with test providers enabled).
func (h *Handler) oauthRegistry() *brandoauth.Registry {
	if h.OAuthRegistry == nil {
		return brandoauth.Default()
	}
	return h.OAuthRegistry
}

// oauthClient resolves the brand client for one application from its provider
// config. Shared by the authorize endpoint, the callback and deauthorization so
// the descriptor lookup and config resolution cannot drift apart.
func (h *Handler) oauthClient(ctx context.Context, appID, providerID string) (*brandoauth.Client, error) {
	desc, ok := h.oauthRegistry().Lookup(providerID)
	if !ok {
		return nil, apperror.ProviderNotSupported(providerID)
	}
	cfg, err := h.oauthProviderConfig(ctx, appID, providerID)
	if err != nil {
		return nil, err
	}
	return brandoauth.NewClient(desc, cfg, h.Cfg.OAuthMockEnabled, nil), nil
}

// oauthCallbackURL is this service's public callback for a brand. It is
// configured, never derived from the request Host.
func (h *Handler) oauthCallbackURL(providerID string) string {
	return strings.TrimRight(h.Cfg.OAuthPublicBaseURL, "/") + "/oauth/link/" + providerID + "/callback"
}

// AuthorizeAccount starts a third-party OAuth2 account link: it mints a
// single-use state handle bound to the authenticated user, stores it, and
// returns the brand's authorization URL to redirect the browser to.
//
// @Summary		Start linking a third-party watch account
// @Description	Returns the provider's OAuth2 authorization URL. The caller redirects the user there; the provider returns to the callback endpoint.
// @Tags			users
// @Accept			json
// @Produce		json
// @Param			provider_id	path		string					true	"Provider id (e.g. coros)"
// @Param			body		body		authorizeAccountRequest	true	"Registered callback URI"
// @Success		200			{object}	authorizeAccountResponse
// @Failure		400			{object}	ErrorResponse
// @Failure		401			{object}	ErrorResponse
// @Failure		503			{object}	ErrorResponse
// @Security		BearerAuth
// @Router			/api/users/me/accounts/{provider_id}/authorize [post]
func (h *Handler) AuthorizeAccount(c *gin.Context) {
	ctx := c.Request.Context()
	providerID := c.Param("provider_id")
	if _, ok := h.oauthRegistry().Lookup(providerID); !ok {
		middleware.RespondError(c, apperror.ProviderNotSupported(providerID))
		return
	}
	var req authorizeAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	if req.RedirectURI == "" {
		middleware.RespondError(c, apperror.BadRequest("redirect_uri is required"))
		return
	}
	app, err := h.Repo.Applications().FindByClientID(ctx, middleware.ClientID(c))
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if app == nil {
		middleware.RespondError(c, apperror.ApplicationNotFound())
		return
	}
	// Resolve the brand config first so an unconfigured provider reports
	// provider_not_configured rather than a misleading redirect error.
	client, err := h.oauthClient(ctx, app.ID, providerID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if !app.RedirectURIAllowed(req.RedirectURI) {
		middleware.RespondError(c, apperror.InvalidRedirectURI())
		return
	}
	if h.OAuthStateStore == nil {
		middleware.RespondError(c, apperror.ServiceUnavailable())
		return
	}

	handle := auth.RandomHex(64)
	state := repository.OAuthState{
		UserID:      middleware.UserID(c),
		AppID:       app.ID,
		ProviderID:  providerID,
		RedirectURI: req.RedirectURI,
		CreatedAt:   time.Now().UTC(),
	}
	if err := h.OAuthStateStore.StoreState(ctx, handle, state, oauthLinkStateTTL); err != nil {
		middleware.RespondError(c, err)
		return
	}

	c.JSON(http.StatusOK, authorizeAccountResponse{
		ProviderID:   providerID,
		AuthorizeURL: client.AuthorizeURL(h.oauthCallbackURL(providerID), handle),
		ExpiresIn:    int64(oauthLinkStateTTL.Seconds()),
	})
}

// OAuthCallback completes a third-party OAuth2 account link. It is public (the
// browser carries no Bearer token); the security comes from the single-use
// state handle minted for the authenticated user. It never renders HTML: once
// the redirect target is trusted, every outcome is a 302 carrying the OAuth2
// authorization-response parameters (RFC 6749 §4.1.2.1) — success carries no
// `error`, failures carry `error` / `error_description`. Only when the redirect
// target itself cannot be trusted (unknown/expired state, unregistered URI)
// does it respond with a plain 4xx, mirroring §4.1.2.1's "MUST NOT redirect to
// the invalid redirection URI".
//
// @Summary		Third-party OAuth2 link callback
// @Tags			oauth
// @Param			provider_id	path		string	true	"Provider id"
// @Param			code		query		string	false	"Authorization code"
// @Param			state		query		string	true	"Opaque state handle"
// @Success		302			"Redirect to the registered callback URI (success or error query)"
// @Failure		400			{object}	ErrorResponse
// @Failure		503			{object}	ErrorResponse
// @Router			/oauth/link/{provider_id}/callback [get]
func (h *Handler) OAuthCallback(c *gin.Context) {
	ctx := c.Request.Context()
	providerID := c.Param("provider_id")
	if _, ok := h.oauthRegistry().Lookup(providerID); !ok {
		middleware.RespondError(c, apperror.ProviderNotSupported(providerID))
		return
	}
	if h.OAuthStateStore == nil {
		middleware.RespondError(c, apperror.ServiceUnavailable())
		return
	}
	handle := c.Query("state")
	if handle == "" {
		middleware.RespondError(c, apperror.InvalidOAuthState())
		return
	}
	state, err := h.OAuthStateStore.ConsumeState(ctx, handle)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if state == nil || state.ProviderID != providerID {
		middleware.RespondError(c, apperror.InvalidOAuthState())
		return
	}
	app, err := h.Repo.Applications().FindByID(ctx, state.AppID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	// The redirect target must still be one the application registered; if not,
	// we cannot safely send the browser anywhere.
	if app == nil || !app.RedirectURIAllowed(state.RedirectURI) {
		middleware.RespondError(c, apperror.InvalidRedirectURI())
		return
	}

	// From here the target is trusted: report every business outcome by
	// redirecting, not by returning a JSON error to the browser.
	if providerErr := c.Query("error"); providerErr != "" {
		h.redirectLinkResult(c, state.RedirectURI, providerID, providerErr, c.Query("error_description"))
		return
	}
	code := c.Query("code")
	if code == "" {
		h.redirectLinkResult(c, state.RedirectURI, providerID, linkErrInvalidRequest, "Missing 'code' parameter")
		return
	}

	client, err := h.oauthClient(ctx, app.ID, providerID)
	if err != nil {
		h.redirectLinkResult(c, state.RedirectURI, providerID, linkErrServerError, "Account linking is not configured")
		return
	}
	// redirect_uri MUST be identical to the one used in the authorization
	// request (RFC 6749 §4.1.3) — our callback URL, not the page the user
	// started from.
	token, err := client.ExchangeCode(ctx, code, h.oauthCallbackURL(providerID))
	if err != nil {
		logger.S().Warnw("oauth code exchange failed", "provider_id", providerID, "error", err)
		h.redirectLinkResult(c, state.RedirectURI, providerID, linkErrServerError, "The provider could not complete the link")
		return
	}
	token.BindingAppID = app.ID

	// The brand identity must not already belong to a different STRIDE user.
	// Re-linking the same identity to the same user is idempotent.
	existing, err := h.Repo.Accounts().FindByUserAndProvider(ctx, state.UserID, providerID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if existing != nil {
		if existing.ProviderAccountID != nil && *existing.ProviderAccountID == token.ProviderAccountID {
			h.redirectLinkResult(c, state.RedirectURI, providerID, "", "")
			return
		}
		h.redirectLinkResult(c, state.RedirectURI, providerID, linkErrAlreadyLinked,
			"This watch account is already linked to another STRIDE account")
		return
	}
	linked, err := h.Repo.Accounts().FindByProviderAccount(ctx, providerID, token.ProviderAccountID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if linked != nil {
		h.redirectLinkResult(c, state.RedirectURI, providerID, linkErrAlreadyLinked,
			"This watch account is already linked to another STRIDE account")
		return
	}

	now := time.Now().UTC()
	account := &domain.Account{
		ID:                uuid.NewString(),
		UserID:            state.UserID,
		ProviderID:        providerID,
		ProviderAccountID: strPtr(token.ProviderAccountID),
		ProviderMetadata:  string(token.Metadata()),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if token.RefreshToken != "" {
		account.Credential = strPtr(token.RefreshToken)
	}
	if err := h.Repo.Accounts().Insert(ctx, account); err != nil {
		middleware.RespondError(c, err)
		return
	}
	h.redirectLinkResult(c, state.RedirectURI, providerID, "", "")
}

// redirectLinkResult sends the browser back to the application's registered URI
// with the link outcome as OAuth2 authorization-response parameters. An empty
// errCode marks success; otherwise error / error_description are set.
func (h *Handler) redirectLinkResult(c *gin.Context, redirectURI, providerID, errCode, errDescription string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		middleware.RespondError(c, apperror.InvalidRedirectURI())
		return
	}
	q := u.Query()
	q.Set("provider_id", providerID)
	if errCode != "" {
		q.Set("error", errCode)
		if errDescription != "" {
			q.Set("error_description", errDescription)
		}
	}
	u.RawQuery = q.Encode()
	c.Redirect(http.StatusFound, u.String())
}

// revokeOAuthAccount best-effort asks the brand to revoke a linked account's
// grant. It is shared by self-unlink, admin unlink and account deletion and
// resolves the credentials from the application the link was created through
// (recorded in the metadata), not the application making the request. Failures
// are logged, never returned: the local unlink must still succeed.
func (h *Handler) revokeOAuthAccount(ctx context.Context, account *domain.Account) {
	if account == nil {
		return
	}
	desc, ok := h.oauthRegistry().Lookup(account.ProviderID)
	if !ok {
		return
	}
	appID := bindingAppID(account.ProviderMetadata)
	if appID == "" {
		return
	}
	cfg, err := h.oauthProviderConfig(ctx, appID, account.ProviderID)
	if err != nil {
		return
	}
	refreshToken := ""
	if account.Credential != nil {
		refreshToken = *account.Credential
	}
	client := brandoauth.NewClient(desc, cfg, h.Cfg.OAuthMockEnabled, nil)
	if err := client.Deauthorize(ctx, refreshToken); err != nil {
		logger.S().Warnw("oauth deauthorize failed",
			"provider_id", account.ProviderID, "user_id", account.UserID, "error", err)
	}
}

// bindingAppID reads the application id a link was created through from the
// account metadata.
func bindingAppID(metadata string) string {
	var m struct {
		AppID string `json:"app_id"`
	}
	if err := json.Unmarshal([]byte(metadata), &m); err != nil {
		return ""
	}
	return m.AppID
}

// oauthProviderConfig loads the brand configuration for one application. It is
// shared by the authorize endpoint, the callback and the deauthorize helper.
func (h *Handler) oauthProviderConfig(ctx context.Context, appID, providerID string) (brandoauth.Config, error) {
	provider, err := h.Repo.AppProviders().FindByAppAndProvider(ctx, appID, providerID)
	if err != nil {
		return brandoauth.Config{}, err
	}
	if provider == nil || !provider.IsActive {
		return brandoauth.Config{}, apperror.ProviderNotConfigured()
	}
	return brandoauth.ParseConfig(provider.Config)
}
