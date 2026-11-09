package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/middleware"
	"github.com/zhaochy1990/auth-service/internal/wechat"
)

// code2sessionRequest is the POST /api/internal/wechat/code2session body.
type code2sessionRequest struct {
	// Code is the miniprogram's one-time wx.login() code the caller (e.g.
	// stride-api's prepay) wants resolved into a payer openid.
	Code string `json:"code" example:"0a3Xxx000Qwabcd1DxV000"`
}

// code2sessionResponse is the JSON body returned by POST
// /api/internal/wechat/code2session. Only the openid: session_key is the
// caller's own login-session secret, unnecessary here and never relayed.
type code2sessionResponse struct {
	OpenID string `json:"openid"`
}

// WeChatCode2Session exchanges a wx.login() code for the payer openid
// (devops#904/#905 prepay fix: the appsecret stays in this service, the
// miniprogram's pay endpoint now sends the code and the API resolves it
// here). Mirrors the wxacode/urllink internal endpoints: appid and secret
// come from the active WeChat provider config row (no calling-application
// context on the internal plane) and the plane is gated by the
// X-Internal-Token shared secret. Nothing about the call is logged.
//
// @Summary		Exchange a wx.login code for the openid (internal)
// @Description	Resolves a miniprogram wx.login() code into the payer openid via WeChat's code2Session, using the configured WeChat provider credentials. Requires the X-Internal-Token header; returns only the openid.
// @Tags			internal
// @Accept			json
// @Produce			json
// @Param			body	body	code2sessionRequest	true	"Login code"
// @Success		200		{object}	code2sessionResponse
// @Failure		400		{object}	ErrorResponse
// @Failure		401		{object}	ErrorResponse
// @Security		InternalToken
// @Router			/api/internal/wechat/code2session [post]
func (h *Handler) WeChatCode2Session(c *gin.Context) {
	var req code2sessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("body must be JSON with 'code'"))
		return
	}

	ctx := c.Request.Context()
	provider, err := h.Repo.AppProviders().FindActiveByProvider(ctx, "wechat")
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	wechatCfg, err := parseWeChatProviderConfig(provider)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}

	client := wechat.NewClient(wechatCfg.AppID, wechatCfg.Secret, h.Cfg.WeChatCode2SessionURL)
	session, err := client.Code2Session(ctx, req.Code)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, code2sessionResponse{OpenID: session.OpenID})
}
