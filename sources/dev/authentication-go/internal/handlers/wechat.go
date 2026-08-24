package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/middleware"
)

// --- Request / Response types ---

type wechatLoginRequest struct {
	Code string `json:"code"`
}

type wechatBindRequest struct {
	Code     string `json:"code"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type wechatLoginResponse struct {
	AccessToken  string              `json:"access_token,omitempty"`
	RefreshToken string              `json:"refresh_token,omitempty"`
	TokenType    string              `json:"token_type,omitempty"`
	ExpiresIn    int64               `json:"expires_in,omitempty"`
	User         *wechatUserResponse `json:"user,omitempty"`
	NeedsBinding bool                `json:"needs_binding"`
}

type wechatUserResponse struct {
	ID          string  `json:"id"`
	Email       *string `json:"email"`
	Name        *string `json:"name"`
	WeChatBound bool    `json:"wechat_bound"`
}

// WeChatLogin logs in a user via WeChat mini-program code.
//
// @Summary		Log in with WeChat mini-program code
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body		wechatLoginRequest	true	"WeChat login code"
// @Success		200		{object}	wechatLoginResponse
// @Failure		400		{object}	ErrorResponse
// @Failure		403		{object}	ErrorResponse
// @Failure		500		{object}	ErrorResponse
// @Security		ClientID
// @Router			/api/auth/wechat-login [post]
func (h *Handler) WeChatLogin(c *gin.Context) {
	var req wechatLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	ctx := c.Request.Context()

	session, err := h.WeChat.Code2Session(ctx, req.Code)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}

	user, err := h.Repo.Users().FindByWeChatOpenID(ctx, session.OpenID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		c.JSON(http.StatusOK, wechatLoginResponse{NeedsBinding: true})
		return
	}
	if !user.IsActive {
		middleware.RespondError(c, apperror.UserDisabled())
		return
	}

	h.respondWeChatLogin(c, user)
}

// WeChatBind binds a WeChat openid to an existing account via email+password.
//
// @Summary		Bind WeChat to an existing account
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body		wechatBindRequest	true	"WeChat code + email + password"
// @Success		200		{object}	wechatLoginResponse
// @Failure		400		{object}	ErrorResponse
// @Failure		401		{object}	ErrorResponse
// @Failure		403		{object}	ErrorResponse
// @Failure		409		{object}	ErrorResponse
// @Failure		500		{object}	ErrorResponse
// @Security		ClientID
// @Router			/api/auth/wechat-bind [post]
func (h *Handler) WeChatBind(c *gin.Context) {
	var req wechatBindRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	ctx := c.Request.Context()

	// Verify email + password first.
	user, err := h.Repo.Users().FindByEmail(ctx, req.Email)
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
	ok, err := auth.VerifyPassword(req.Password, *account.Credential)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if !ok {
		middleware.RespondError(c, apperror.InvalidCredentials())
		return
	}

	// Exchange code for openid.
	session, err := h.WeChat.Code2Session(ctx, req.Code)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}

	// Check openid already bound to another account.
	existing, err := h.Repo.Users().FindByWeChatOpenID(ctx, session.OpenID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if existing != nil && existing.ID != user.ID {
		middleware.RespondError(c, apperror.New(
			http.StatusConflict,
			"wechat_already_bound",
			"This WeChat account is already bound to another user",
		))
		return
	}
	// Reject a collision on the (also unique) unionid — a second mini-program's
	// openid can share the same WeChat identity.
	if session.UnionID != nil && *session.UnionID != "" {
		existing, err = h.Repo.Users().FindByWeChatUnionID(ctx, *session.UnionID)
		if err != nil {
			middleware.RespondError(c, err)
			return
		}
		if existing != nil && existing.ID != user.ID {
			middleware.RespondError(c, apperror.New(
				http.StatusConflict,
				"wechat_already_bound",
				"This WeChat account is already bound to another user",
			))
			return
		}
	}

	// Bind openid to this user.
	now := time.Now().UTC()
	user.WeChatOpenID = &session.OpenID
	if session.UnionID != nil {
		user.WeChatUnionID = session.UnionID
	}
	user.UpdatedAt = now
	if err := h.Repo.Users().Update(ctx, user); err != nil {
		middleware.RespondError(c, err)
		return
	}

	h.respondWeChatLogin(c, user)
}

// respondWeChatLogin issues the standard token pair for an authenticated WeChat
// user and writes the wechat-login success response. Shared by WeChatLogin and
// WeChatBind (bind logs the user in directly).
func (h *Handler) respondWeChatLogin(c *gin.Context, user *domain.User) {
	ctx := c.Request.Context()

	_ = h.Repo.Users().RecordLogin(ctx, user.ID, middleware.ClientIP(c, "unknown"))

	membership := h.resolveMembership(ctx, user)
	scopes := middleware.AllowedScopes(c)
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

	c.JSON(http.StatusOK, wechatLoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    h.Cfg.JWTAccessTokenExpirySecs,
		User: &wechatUserResponse{
			ID:          user.ID,
			Email:       user.Email,
			Name:        user.Name,
			WeChatBound: true,
		},
		NeedsBinding: false,
	})
}
