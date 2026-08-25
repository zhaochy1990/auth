package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/middleware"
	"github.com/zhaochy1990/auth-service/internal/wechat"
)

// wechatSubjectTokenType is the subject_token_type value identifying a WeChat
// mini-program js_code.
const wechatSubjectTokenType = "wechat_mini_program"

// handleTokenExchange implements the RFC 8693 token_exchange grant for WeChat
// mini-program login. subject_token is the wx.login() code; the WeChat
// mini-program credentials are read from the calling application's row (per-app
// config, never env). With email+password present the exchanged identity is
// bound to that account instead (bind flow, which logs the user in directly).
func (h *Handler) handleTokenExchange(c *gin.Context, req *tokenRequest) {
	ctx := c.Request.Context()
	if req.SubjectToken == nil || *req.SubjectToken == "" {
		middleware.RespondError(c, apperror.BadRequest("Missing 'subject_token' parameter"))
		return
	}
	if req.SubjectTokenType == nil {
		middleware.RespondError(c, apperror.BadRequest("Missing 'subject_token_type' parameter"))
		return
	}
	if *req.SubjectTokenType != wechatSubjectTokenType {
		middleware.RespondError(c, apperror.BadRequest("Unsupported subject_token_type: "+*req.SubjectTokenType))
		return
	}

	app, err := h.resolveExchangeApp(c, req)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if app.WeChatAppID == "" || app.WeChatAppSecret == "" {
		middleware.RespondError(c, apperror.WeChatNotConfigured())
		return
	}

	client := wechat.NewClient(app.WeChatAppID, app.WeChatAppSecret, h.Cfg.WeChatCode2SessionURL)
	session, err := client.Code2Session(ctx, *req.SubjectToken)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}

	if req.Email != nil || req.Password != nil {
		h.handleWeChatBind(c, req, app, session)
		return
	}
	h.handleWeChatLogin(c, req, app, session)
}

// resolveExchangeApp determines the calling application: the Basic-authenticated
// app when the caller presented credentials, otherwise the client_id in the
// request body (public client).
func (h *Handler) resolveExchangeApp(c *gin.Context, req *tokenRequest) (*domain.Application, error) {
	ctx := c.Request.Context()
	if appID := middleware.AppID(c); appID != "" {
		return h.Repo.Applications().FindByID(ctx, appID)
	}
	if req.ClientID == nil || *req.ClientID == "" {
		return nil, apperror.New(http.StatusBadRequest, "missing_client_id", "Missing 'client_id' parameter")
	}
	app, err := h.Repo.Applications().FindByClientID(ctx, *req.ClientID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperror.ApplicationNotFound()
	}
	if !app.IsActive {
		return nil, apperror.ApplicationNotActive()
	}
	middleware.SetAppContext(c, app)
	return app, nil
}

// handleWeChatLogin logs in a user whose WeChat identity is already bound.
func (h *Handler) handleWeChatLogin(c *gin.Context, req *tokenRequest, app *domain.Application, session *wechat.SessionResult) {
	ctx := c.Request.Context()
	user, err := h.Repo.Users().FindByWeChatOpenID(ctx, app.WeChatAppID, session.OpenID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		middleware.RespondError(c, apperror.WeChatNeedsBinding())
		return
	}
	if !user.IsActive {
		middleware.RespondError(c, apperror.UserDisabled())
		return
	}
	h.respondTokenExchange(c, req, user, app)
}

// handleWeChatBind verifies email+password, then binds the exchanged WeChat
// identity to that account and logs it in.
func (h *Handler) handleWeChatBind(c *gin.Context, req *tokenRequest, app *domain.Application, session *wechat.SessionResult) {
	ctx := c.Request.Context()
	if req.Email == nil || *req.Email == "" || req.Password == nil || *req.Password == "" {
		middleware.RespondError(c, apperror.BadRequest("Both 'email' and 'password' are required to bind"))
		return
	}

	// Verify credentials first: nothing is bound unless the account is proven.
	user, err := h.Repo.Users().FindByEmail(ctx, *req.Email)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		middleware.RespondError(c, apperror.InvalidCredentials())
		return
	}
	if !user.IsActive {
		middleware.RespondError(c, apperror.UserDisabled())
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

	// The identity must not already belong to another account: openid within
	// this mini-program, and unionid as the cross-mini-program key.
	existing, err := h.Repo.Users().FindByWeChatOpenID(ctx, app.WeChatAppID, session.OpenID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if existing != nil && existing.ID != user.ID {
		middleware.RespondError(c, apperror.WeChatAlreadyBound())
		return
	}
	if session.UnionID != nil && *session.UnionID != "" {
		existing, err = h.Repo.Users().FindByWeChatUnionID(ctx, *session.UnionID)
		if err != nil {
			middleware.RespondError(c, err)
			return
		}
		if existing != nil && existing.ID != user.ID {
			middleware.RespondError(c, apperror.WeChatAlreadyBound())
			return
		}
	}

	// An account already bound to a DIFFERENT WeChat identity in this
	// mini-program may not silently rebind; the rebind flow is not designed yet.
	link, err := h.Repo.Users().FindWeChatLink(ctx, user.ID, app.WeChatAppID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if link != nil && link.OpenID != session.OpenID {
		middleware.RespondError(c, apperror.WeChatAlreadyBound())
		return
	}

	if link == nil {
		unionid := session.UnionID
		if unionid != nil && *unionid == "" {
			unionid = nil
		}
		if err := h.Repo.Users().LinkWeChat(ctx, user.ID, app.WeChatAppID, session.OpenID, unionid); err != nil {
			middleware.RespondError(c, err)
			return
		}
		user.WeChatBound = true
	}
	h.respondTokenExchange(c, req, user, app)
}

// respondTokenExchange issues the standard token pair for the exchanged user
// (shared by the login and bind flows).
func (h *Handler) respondTokenExchange(c *gin.Context, req *tokenRequest, user *domain.User, app *domain.Application) {
	ctx := c.Request.Context()

	_ = h.Repo.Users().RecordLogin(ctx, user.ID, middleware.ClientIP(c, "unknown"))

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

	membership := h.resolveMembership(ctx, user)
	accessToken, err := h.JWT.IssueAccessToken(user.ID, middleware.ClientID(c), scopes, user.Role, membership, user.UserType, user.Name)
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
