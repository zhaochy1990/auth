package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/middleware"
)

// --- Request / Response types ---

type tokenRequest struct {
	GrantType string `json:"grant_type"`
	// authorization_code flow
	Code         *string `json:"code"`
	RedirectURI  *string `json:"redirect_uri"`
	CodeVerifier *string `json:"code_verifier"`
	// password flow
	Username *string `json:"username"`
	Password *string `json:"password"`
	// refresh_token flow
	RefreshToken *string `json:"refresh_token"`
	// common
	Scope *string `json:"scope"`
}

type oauthTokenResponse struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken *string `json:"refresh_token,omitempty"`
	TokenType    string  `json:"token_type"`
	ExpiresIn    int64   `json:"expires_in"`
	Scope        *string `json:"scope,omitempty"`
}

type revokeRequest struct {
	Token string `json:"token"`
}

type introspectRequest struct {
	Token string `json:"token"`
}

type introspectResponse struct {
	Active bool    `json:"active"`
	Sub    *string `json:"sub,omitempty"`
	Aud    *string `json:"aud,omitempty"`
	Exp    *int64  `json:"exp,omitempty"`
	Scope  *string `json:"scope,omitempty"`
}

// --- Handlers ---

// Token implements the OAuth2 token endpoint (multiple grant types).
func (h *Handler) Token(c *gin.Context) {
	var req tokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	switch req.GrantType {
	case "authorization_code":
		h.handleAuthorizationCode(c, &req)
	case "client_credentials":
		h.handleClientCredentials(c)
	case "refresh_token":
		h.handleRefreshTokenGrant(c, &req)
	case "password":
		h.handlePasswordGrant(c, &req)
	default:
		middleware.RespondError(c, apperror.BadRequest("Unsupported grant_type: "+req.GrantType))
	}
}

func (h *Handler) handleAuthorizationCode(c *gin.Context, req *tokenRequest) {
	ctx := c.Request.Context()
	if req.Code == nil {
		middleware.RespondError(c, apperror.BadRequest("Missing 'code' parameter"))
		return
	}
	if req.RedirectURI == nil {
		middleware.RespondError(c, apperror.BadRequest("Missing 'redirect_uri' parameter"))
		return
	}
	userID, scopes, err := auth.ExchangeAuthCode(ctx, h.Repo, *req.Code, middleware.AppID(c), *req.RedirectURI, req.CodeVerifier)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	user, err := h.Repo.Users().FindByID(ctx, userID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		middleware.RespondError(c, apperror.UserNotFound())
		return
	}
	if !user.IsActive {
		middleware.RespondError(c, apperror.Forbidden())
		return
	}
	membership := h.resolveMembership(ctx, user)
	accessToken, err := h.JWT.IssueAccessToken(userID, middleware.ClientID(c), scopes, user.Role, membership, user.Name)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	refreshToken := auth.GenerateRefreshToken()
	if err := auth.StoreRefreshToken(ctx, h.Repo, userID, middleware.AppID(c), refreshToken, scopes, nil, h.Cfg.JWTRefreshTokenExpiryDays); err != nil {
		middleware.RespondError(c, err)
		return
	}
	scopeStr := strings.Join(scopes, " ")
	c.JSON(http.StatusOK, oauthTokenResponse{
		AccessToken:  accessToken,
		RefreshToken: strPtr(refreshToken),
		TokenType:    "Bearer",
		ExpiresIn:    h.Cfg.JWTAccessTokenExpirySecs,
		Scope:        &scopeStr,
	})
}

func (h *Handler) handleClientCredentials(c *gin.Context) {
	accessToken, err := h.JWT.IssueAppToken(middleware.AppID(c))
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, oauthTokenResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   h.Cfg.JWTAccessTokenExpirySecs,
	})
}

func (h *Handler) handleRefreshTokenGrant(c *gin.Context, req *tokenRequest) {
	ctx := c.Request.Context()
	if req.RefreshToken == nil {
		middleware.RespondError(c, apperror.BadRequest("Missing 'refresh_token' parameter"))
		return
	}
	userID, newRefreshToken, scopes, err := auth.RotateRefreshToken(ctx, h.Repo, *req.RefreshToken, middleware.AppID(c), h.Cfg.JWTRefreshTokenExpiryDays)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	user, err := h.Repo.Users().FindByID(ctx, userID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		middleware.RespondError(c, apperror.UserNotFound())
		return
	}
	if !user.IsActive {
		middleware.RespondError(c, apperror.Forbidden())
		return
	}
	membership := h.resolveMembership(ctx, user)
	accessToken, err := h.JWT.IssueAccessToken(userID, middleware.ClientID(c), scopes, user.Role, membership, user.Name)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	scopeStr := strings.Join(scopes, " ")
	c.JSON(http.StatusOK, oauthTokenResponse{
		AccessToken:  accessToken,
		RefreshToken: strPtr(newRefreshToken),
		TokenType:    "Bearer",
		ExpiresIn:    h.Cfg.JWTAccessTokenExpirySecs,
		Scope:        &scopeStr,
	})
}

func (h *Handler) handlePasswordGrant(c *gin.Context, req *tokenRequest) {
	ctx := c.Request.Context()
	if req.Username == nil {
		middleware.RespondError(c, apperror.BadRequest("Missing 'username' parameter"))
		return
	}
	if req.Password == nil {
		middleware.RespondError(c, apperror.BadRequest("Missing 'password' parameter"))
		return
	}
	user, err := h.Repo.Users().FindByEmail(ctx, *req.Username)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		middleware.RespondError(c, apperror.InvalidCredentials())
		return
	}
	account, err := h.Repo.Accounts().FindByUserAndProvider(ctx, user.ID, "password")
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if account == nil || account.Credential == nil {
		middleware.RespondError(c, apperror.InvalidCredentials())
		return
	}
	ok, err := auth.VerifyPassword(*req.Password, *account.Credential)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if !ok {
		middleware.RespondError(c, apperror.InvalidCredentials())
		return
	}

	app, err := h.Repo.Applications().FindByID(ctx, middleware.AppID(c))
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if app == nil {
		middleware.RespondError(c, apperror.ApplicationNotFound())
		return
	}
	allowedScopes := auth.DecodeStringArray(app.AllowedScopes)

	var scopes []string
	if req.Scope != nil {
		for _, s := range strings.Split(*req.Scope, " ") {
			if contains(allowedScopes, s) {
				scopes = append(scopes, s)
			}
		}
	} else {
		scopes = allowedScopes
	}

	if !user.IsActive {
		middleware.RespondError(c, apperror.Forbidden())
		return
	}
	membership := h.resolveMembership(ctx, user)
	accessToken, err := h.JWT.IssueAccessToken(user.ID, middleware.ClientID(c), scopes, user.Role, membership, user.Name)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	refreshToken := auth.GenerateRefreshToken()
	if err := auth.StoreRefreshToken(ctx, h.Repo, user.ID, middleware.AppID(c), refreshToken, scopes, nil, h.Cfg.JWTRefreshTokenExpiryDays); err != nil {
		middleware.RespondError(c, err)
		return
	}
	scopeStr := strings.Join(scopes, " ")
	c.JSON(http.StatusOK, oauthTokenResponse{
		AccessToken:  accessToken,
		RefreshToken: strPtr(refreshToken),
		TokenType:    "Bearer",
		ExpiresIn:    h.Cfg.JWTAccessTokenExpirySecs,
		Scope:        &scopeStr,
	})
}

// Revoke revokes a refresh token. Per RFC 7009, always returns 200.
func (h *Handler) Revoke(c *gin.Context) {
	var req revokeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	_ = auth.RevokeRefreshToken(c.Request.Context(), h.Repo, req.Token)
	c.JSON(http.StatusOK, gin.H{})
}

// Introspect reports whether an access token is active (RFC 7662 subset).
func (h *Handler) Introspect(c *gin.Context) {
	var req introspectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	claims, err := h.JWT.VerifyAccessToken(req.Token)
	if err != nil {
		c.JSON(http.StatusOK, introspectResponse{Active: false})
		return
	}
	scope := strings.Join(claims.Scopes, " ")
	exp := claims.Exp
	c.JSON(http.StatusOK, introspectResponse{
		Active: true,
		Sub:    strPtr(claims.Sub),
		Aud:    strPtr(claims.Aud),
		Exp:    &exp,
		Scope:  &scope,
	})
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
