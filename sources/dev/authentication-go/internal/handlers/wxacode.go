package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/middleware"
	"github.com/zhaochy1990/auth-service/internal/wechat"
)

// wxacodeSceneMaxRunes is WeChat's scene limit: at most 32 visible chars.
const wxacodeSceneMaxRunes = 32

// wxacodeMinWidth / wxacodeMaxWidth bound the optional width parameter (px).
// Outside them the request is rejected; the WeChat default when unset.
const (
	wxacodeMinWidth     = 280
	wxacodeMaxWidth     = 1280
	wxacodeDefaultWidth = 430
)

// wxacodeEnvVersions are the getwxacodeunlimit env_version values.
var wxacodeEnvVersions = map[string]bool{"release": true, "trial": true, "develop": true}

// wxacodeRequest is the POST /api/internal/wxacode body.
type wxacodeRequest struct {
	// Scene is the parameter the code carries, read back by the
	// mini-program via wx.getEnterOptions (1-32 visible chars).
	Scene string `json:"scene" example:"c=abc123"`
	// Page is the mini-program page the code opens (must exist in the
	// published version, else WeChat rejects with 41030).
	Page string `json:"page" example:"pages/coach/index"`
	// EnvVersion optionally selects the mini-program version:
	// release (default) / trial / develop.
	EnvVersion *string `json:"env_version" example:"release"`
	// Width optionally sets the code width in px (280-1280, default 430).
	Width *int `json:"width" example:"430"`
}

// wxacodeResponse is the JSON body returned by POST /api/internal/wxacode.
type wxacodeResponse struct {
	URL string `json:"url"`
}

// GenerateWxACode renders a WeChat mini-program code (getwxacodeunlimit) for
// the given scene/page and stores it in Tencent Cloud COS, returning the
// public URL. Every generated code is stored (one image per share, per the
// share-page design), keyed by scene+width so repeat scenes serve the same
// object. Appid/secret come from the WeChat provider config row (no calling-
// application context on the internal plane), and the whole endpoint is gated
// by the X-Internal-Token shared secret.
//
// @Summary		Generate a mini-program code (internal)
// @Description	Renders an unlimited WeChat mini-program code for scene+page, uploads the PNG to Tencent Cloud COS, and returns its public URL. Requires the X-Internal-Token header.
// @Tags			internal
// @Accept			json
// @Produce		json
// @Param			body	body	wxacodeRequest	true	"Code parameters"
// @Success		200	{object}	wxacodeResponse
// @Failure		400	{object}	ErrorResponse
// @Failure		401	{object}	ErrorResponse
// @Failure		429	{object}	ErrorResponse
// @Failure		502	{object}	ErrorResponse
// @Security		InternalToken
// @Router			/api/internal/wxacode [post]
func (h *Handler) GenerateWxACode(c *gin.Context) {
	var req wxacodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("body must be JSON with 'scene' and 'page'"))
		return
	}
	width, err := validateWxACodeRequest(&req)
	if err != nil {
		middleware.RespondError(c, err)
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

	envVersion := ""
	if req.EnvVersion != nil {
		envVersion = *req.EnvVersion
	}
	png, err := h.WxACode.GetUnlimitedQRCode(ctx, wechatCfg.AppID, wechatCfg.Secret, wechat.WxACodeParams{
		Page:       req.Page,
		Scene:      req.Scene,
		EnvVersion: envVersion,
		Width:      width,
	})
	if err != nil {
		middleware.RespondError(c, err)
		return
	}

	// PathEscape keeps the object key a single safe segment for any scene
	// WeChat accepts (slashes, '&', '?', '%' … are all escaped; repeat
	// scenes map to the same key and same image).
	key := fmt.Sprintf("wxacode/%s.%d.png", url.PathEscape(req.Scene), width)
	if err := h.CosClient.Upload(ctx, key, bytes.NewReader(png), "image/png"); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, wxacodeResponse{URL: h.CosClient.PublicURL(key)})
}

// validateWxACodeRequest checks the caller-attributable request fields and
// returns the effective code width (default when unset).
func validateWxACodeRequest(req *wxacodeRequest) (int, error) {
	if n := utf8.RuneCountInString(req.Scene); n < 1 || n > wxacodeSceneMaxRunes {
		return 0, apperror.BadRequest("scene must be 1-32 characters")
	}
	for _, r := range req.Scene {
		if !unicode.IsPrint(r) {
			return 0, apperror.BadRequest("scene must contain only visible characters")
		}
	}
	if req.Page == "" {
		return 0, apperror.BadRequest("page is required")
	}
	if req.EnvVersion != nil && *req.EnvVersion != "" && !wxacodeEnvVersions[*req.EnvVersion] {
		return 0, apperror.BadRequest("env_version must be release, trial or develop")
	}
	width := wxacodeDefaultWidth
	if req.Width != nil {
		if *req.Width < wxacodeMinWidth || *req.Width > wxacodeMaxWidth {
			return 0, apperror.BadRequest("width must be between 280 and 1280")
		}
		width = *req.Width
	}
	return width, nil
}
