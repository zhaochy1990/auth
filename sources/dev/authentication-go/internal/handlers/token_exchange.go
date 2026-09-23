package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/middleware"
	"github.com/zhaochy1990/auth-service/internal/wechat"
)

// grantTokenExchange is the RFC 8693 token_exchange grant.
const grantTokenExchange = "token_exchange"

// wechatSubjectTokenType is the subject_token_type value identifying a WeChat
// mini-program js_code.
const wechatSubjectTokenType = "wechat_mini_program"

// resolveWeChatSession resolves the WeChat identity behind a grant request:
// it validates the subject_token parameters, loads the calling application's
// WeChat provider config, and exchanges the wx.login() code for a session.
// Shared by the token_exchange and wechat_phone_bind grants so their request
// shapes cannot drift. Returns the calling application, its WeChat appid, and
// the session.
func (h *Handler) resolveWeChatSession(c *gin.Context, req *tokenRequest) (*domain.Application, string, *wechat.SessionResult, error) {
	ctx := c.Request.Context()
	if req.SubjectToken == nil || *req.SubjectToken == "" {
		return nil, "", nil, apperror.BadRequest("Missing 'subject_token' parameter")
	}
	if req.SubjectTokenType == nil {
		return nil, "", nil, apperror.BadRequest("Missing 'subject_token_type' parameter")
	}
	if *req.SubjectTokenType != wechatSubjectTokenType {
		return nil, "", nil, apperror.BadRequest("Unsupported subject_token_type: " + *req.SubjectTokenType)
	}

	app, err := h.resolveExchangeApp(c, req)
	if err != nil {
		return nil, "", nil, err
	}
	wechatCfg, err := h.resolveWeChatProviderConfig(ctx, app)
	if err != nil {
		return nil, "", nil, err
	}
	client := wechat.NewClient(wechatCfg.AppID, wechatCfg.Secret, h.Cfg.WeChatCode2SessionURL)
	session, err := client.Code2Session(ctx, *req.SubjectToken)
	if err != nil {
		return nil, "", nil, err
	}
	return app, wechatCfg.AppID, session, nil
}

// bindWeChatIdentity attaches the exchanged WeChat identity to the account:
// an existing link for a DIFFERENT identity in this mini-program is refused
// (no silent rebind — the rebind flow is not designed yet), the user's own
// identity is an idempotent no-op, and otherwise the link is written.
// Shared by the email+password bind flow and the wechat_phone_bind grant.
func (h *Handler) bindWeChatIdentity(ctx context.Context, user *domain.User, wechatAppID string, session *wechat.SessionResult) error {
	link, err := h.Repo.Users().FindWeChatLink(ctx, user.ID, wechatAppID)
	if err != nil {
		return err
	}
	if link != nil && link.OpenID != session.OpenID {
		return apperror.WeChatAlreadyBound()
	}
	if link == nil {
		unionid := session.UnionID
		if unionid != nil && *unionid == "" {
			unionid = nil
		}
		if err := h.Repo.Users().LinkWeChat(ctx, user.ID, wechatAppID, session.OpenID, unionid); err != nil {
			return err
		}
		user.WeChatBound = true
	}
	return nil
}

// handleTokenExchange implements the RFC 8693 token_exchange grant for WeChat
// mini-program login. subject_token is the wx.login() code; the WeChat
// mini-program credentials (appid/secret) are read from the calling
// application's WeChat provider config (per-app config, never env). With
// email+password present the exchanged identity is bound to that account
// instead (bind flow, which logs the user in directly).
func (h *Handler) handleTokenExchange(c *gin.Context, req *tokenRequest) {
	app, wechatAppID, session, err := h.resolveWeChatSession(c, req)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}

	if req.Email != nil || req.Password != nil {
		h.handleWeChatBind(c, req, app, wechatAppID, session)
		return
	}
	h.handleWeChatLogin(c, req, app, wechatAppID, session)
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

// wechatProviderConfig is the WeChat credential stored on the calling
// application's auth_app_providers row (JSON config with keys appid/secret).
type wechatProviderConfig struct {
	AppID  string `json:"appid"`
	Secret string `json:"secret"`
}

// resolveWeChatProviderConfig loads the calling application's WeChat provider
// config. A missing/inactive provider row or a config lacking appid or secret
// yields wechat_not_configured.
func (h *Handler) resolveWeChatProviderConfig(ctx context.Context, app *domain.Application) (wechatProviderConfig, error) {
	provider, err := h.Repo.AppProviders().FindByAppAndProvider(ctx, app.ID, "wechat")
	if err != nil {
		return wechatProviderConfig{}, err
	}
	if provider == nil || !provider.IsActive {
		return wechatProviderConfig{}, apperror.WeChatNotConfigured()
	}
	var cfg wechatProviderConfig
	if err := json.Unmarshal([]byte(provider.Config), &cfg); err != nil {
		return wechatProviderConfig{}, apperror.WeChatNotConfigured()
	}
	if cfg.AppID == "" || cfg.Secret == "" {
		return wechatProviderConfig{}, apperror.WeChatNotConfigured()
	}
	return cfg, nil
}

// handleWeChatLogin logs in a user whose WeChat identity is already bound.
func (h *Handler) handleWeChatLogin(c *gin.Context, req *tokenRequest, app *domain.Application, wechatAppID string, session *wechat.SessionResult) {
	ctx := c.Request.Context()
	user, err := h.Repo.Users().FindByWeChatOpenID(ctx, wechatAppID, session.OpenID)
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
	h.respondTokenExchange(c, req, user, app, false)
}

// handleWeChatBind verifies email+password, then binds the exchanged WeChat
// identity to that account and logs it in.
func (h *Handler) handleWeChatBind(c *gin.Context, req *tokenRequest, app *domain.Application, wechatAppID string, session *wechat.SessionResult) {
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
	// this mini-program, and unionid as the cross-mini-program key. Checked
	// after the credentials are proven but before anything is written.
	if err := h.ensureWeChatIdentityFree(ctx, wechatAppID, session, user); err != nil {
		middleware.RespondError(c, err)
		return
	}

	if err := h.bindWeChatIdentity(ctx, user, wechatAppID, session); err != nil {
		middleware.RespondError(c, err)
		return
	}
	h.respondTokenExchange(c, req, user, app, false)
}

// respondTokenExchange issues the standard token pair for the exchanged user
// (shared by the login and bind flows, and by the wechat_phone_bind grant,
// which passes registered=true when it auto-registered the account).
func (h *Handler) respondTokenExchange(c *gin.Context, req *tokenRequest, user *domain.User, app *domain.Application, registered bool) {
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
		Registered:   registered,
	})
}
