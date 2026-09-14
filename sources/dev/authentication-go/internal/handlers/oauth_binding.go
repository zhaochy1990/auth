package handlers

import (
	"context"
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

const (
	linkStatusSuccess = "success"
	linkStatusError   = "error"
)

type authorizeAccountRequest struct {
	RedirectURI string `json:"redirect_uri"`
}

type authorizeAccountResponse struct {
	ProviderID   string `json:"provider_id"`
	AuthorizeURL string `json:"authorize_url"`
	ExpiresIn    int64  `json:"expires_in"`
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
	desc, ok := brandoauth.Default().Lookup(providerID)
	if !ok {
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
	cfg, err := h.oauthProviderConfig(ctx, app.ID, providerID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if !redirectURIAllowed(app, req.RedirectURI) {
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

	callbackURL := strings.TrimRight(h.Cfg.OAuthPublicBaseURL, "/") + "/oauth/link/" + providerID + "/callback"
	client := brandoauth.NewClient(desc, cfg, h.Cfg.OAuthMockEnabled, nil)
	c.JSON(http.StatusOK, authorizeAccountResponse{
		ProviderID:   providerID,
		AuthorizeURL: client.AuthorizeURL(callbackURL, handle),
		ExpiresIn:    int64(oauthLinkStateTTL.Seconds()),
	})
}

// OAuthCallback completes a third-party OAuth2 account link. It is public (the
// browser carries no Bearer token); the security comes from the single-use
// state handle minted for the authenticated user. It never renders HTML — it
// only redirects back to the registered callback URI with a success or failure
// marker.
//
// @Summary		Third-party OAuth2 link callback
// @Tags			oauth
// @Param			provider_id	path		string	true	"Provider id"
// @Param			code		query		string	false	"Authorization code"
// @Param			state		query		string	true	"Opaque state handle"
// @Success		302			"Redirect to the registered callback URI"
// @Failure		400			{object}	ErrorResponse
// @Failure		502			{object}	ErrorResponse
// @Failure		503			{object}	ErrorResponse
// @Router			/oauth/link/{provider_id}/callback [get]
func (h *Handler) OAuthCallback(c *gin.Context) {
	ctx := c.Request.Context()
	providerID := c.Param("provider_id")
	desc, ok := brandoauth.Default().Lookup(providerID)
	if !ok {
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
	if app == nil || !redirectURIAllowed(app, state.RedirectURI) {
		middleware.RespondError(c, apperror.InvalidRedirectURI())
		return
	}

	// The user denied (or the brand otherwise reported an error) — send them
	// back to where they started with a failure marker.
	if providerErr := c.Query("error"); providerErr != "" {
		c.Redirect(http.StatusFound, linkResultURL(state.RedirectURI, linkStatusError, providerID, providerErr))
		return
	}
	code := c.Query("code")
	if code == "" {
		middleware.RespondError(c, apperror.BadRequest("Missing 'code' parameter"))
		return
	}

	cfg, err := h.oauthProviderConfig(ctx, app.ID, providerID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	client := brandoauth.NewClient(desc, cfg, h.Cfg.OAuthMockEnabled, nil)
	token, err := client.ExchangeCode(ctx, code, state.RedirectURI)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}

	// The brand identity must not already belong to a different STRIDE user.
	// Re-linking the same identity to the same user is idempotent.
	existing, err := h.Repo.Accounts().FindByUserAndProvider(ctx, state.UserID, providerID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if existing != nil {
		if existing.ProviderAccountID != nil && *existing.ProviderAccountID == token.ProviderAccountID {
			c.Redirect(http.StatusFound, linkResultURL(state.RedirectURI, linkStatusSuccess, providerID, ""))
			return
		}
		middleware.RespondError(c, apperror.AccountAlreadyLinked())
		return
	}
	linked, err := h.Repo.Accounts().FindByProviderAccount(ctx, providerID, token.ProviderAccountID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if linked != nil {
		middleware.RespondError(c, apperror.AccountAlreadyLinked())
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
	c.Redirect(http.StatusFound, linkResultURL(state.RedirectURI, linkStatusSuccess, providerID, ""))
}

// revokeOAuthAccount best-effort asks the brand to revoke a linked account's
// grant. It is shared by self-unlink, admin unlink and account deletion.
// Failures are logged, never returned: the local unlink must still succeed.
func (h *Handler) revokeOAuthAccount(ctx context.Context, account *domain.Account, appClientID string) {
	if account == nil || appClientID == "" {
		return
	}
	desc, ok := brandoauth.Default().Lookup(account.ProviderID)
	if !ok {
		return
	}
	app, err := h.Repo.Applications().FindByClientID(ctx, appClientID)
	if err != nil || app == nil {
		return
	}
	cfg, err := h.oauthProviderConfig(ctx, app.ID, account.ProviderID)
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

// redirectURIAllowed reports whether uri is one of the application's registered
// callback URIs (exact match). This is the open-redirect guard for the link
// flow; the existing OAuth2 token endpoint is intentionally left unchanged.
func redirectURIAllowed(app *domain.Application, uri string) bool {
	for _, registered := range auth.DecodeStringArray(app.RedirectURIs) {
		if registered == uri {
			return true
		}
	}
	return false
}

// linkResultURL appends the link outcome marker to the registered callback URI,
// preserving any query it already carries.
func linkResultURL(redirectURI, status, providerID, detail string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return redirectURI
	}
	q := u.Query()
	q.Set("link_status", status)
	q.Set("provider_id", providerID)
	if status == linkStatusError && detail != "" {
		q.Set("link_error", detail)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
