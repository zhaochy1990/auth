package handlers

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/middleware"
	"github.com/zhaochy1990/auth-service/internal/wechat"
)

// urllinkQueryMaxRunes caps the query parameter (visible chars). WeChat
// itself allows 1024; share pages carry a handful of ids, so 256 leaves
// ample room while keeping abuse room tight.
const urllinkQueryMaxRunes = 256

// urllinkQueryChars is the allowed query charset: the URL-safe visible
// characters a k=v&k2=v2 pair can be built from. Anything else (spaces,
// CJK, control chars, quotes…) is rejected before WeChat is called.
const urllinkQueryChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_=&%.-"

// urllinkAllowedPages is the share-page whitelist: every published
// mini-program page a URL Link may open. A page is added here when its
// share feature ships; anything else is rejected before WeChat is called
// (the public endpoint must not become an arbitrary-link minter).
var urllinkAllowedPages = map[string]bool{
	"pages/workout-share/workout-share":               true,
	"pages/coach-share/coach-share":                   true,
	"pages/race-center/strategy-share/strategy-share": true,
	"pages/race-center/race-center":                   true,
	"pages/race-center/detail":                        true,
	"pages/index/index":                               true,
}

// urllinkRequest is the POST /api/internal/urllink and POST
// /api/public/mp-link body (same shape on both planes).
type urllinkRequest struct {
	// Page is the published mini-program page the link opens (must be on
	// the share whitelist).
	Page string `json:"page" example:"pages/race-center/detail"`
	// Query is the parameter string the link carries (k=v&k2=v2 form,
	// ≤256 visible chars, URL-safe charset).
	Query string `json:"query" example:"id=abc123"`
	// EnvVersion optionally selects the mini-program version: release
	// (default) / trial / develop.
	EnvVersion *string `json:"env_version" example:"release"`
}

// urllinkResponse is the JSON body both URL Link endpoints return.
type urllinkResponse struct {
	URLLink string `json:"url_link"`
}

// GenerateURLLink mints a WeChat mini-program URL Link (generate_urllink)
// for the given page+query. Mirrors the wxacode internal endpoint: appid and
// secret come from the WeChat provider config row (no calling-application
// context on the internal plane) and the plane is gated by the
// X-Internal-Token shared secret.
//
// @Summary		Generate a mini-program URL Link (internal)
// @Description	Mints a WeChat URL Link (generate_urllink) opening the whitelisted page with the given query. Requires the X-Internal-Token header.
// @Tags			internal
// @Accept			json
// @Produce			json
// @Param			body	body	urllinkRequest	true	"Link parameters"
// @Success		200	{object}	urllinkResponse
// @Failure		400	{object}	ErrorResponse
// @Failure		401	{object}	ErrorResponse
// @Failure		429	{object}	ErrorResponse
// @Failure		502	{object}	ErrorResponse
// @Security		InternalToken
// @Router			/api/internal/urllink [post]
func (h *Handler) GenerateURLLink(c *gin.Context) {
	h.generateURLLink(c)
}

// MiniProgramLink is the public plane's URL Link endpoint. Same minting
// logic as GenerateURLLink, but for the browser-run share pages (the /s/*
// splitter on stride-running.cn): no shared secret — a dedicated per-IP
// rate limiter and the page whitelist carry the abuse protection instead.
//
// @Summary		Generate a mini-program URL Link (public, rate-limited)
// @Description	Mints a WeChat URL Link opening the whitelisted page with the given query, for the public share pages. Rate-limited per IP (~10 req/min).
// @Tags			public
// @Accept			json
// @Produce			json
// @Param			body	body	urllinkRequest	true	"Link parameters"
// @Success		200	{object}	urllinkResponse
// @Failure		400	{object}	ErrorResponse
// @Failure		429	{object}	ErrorResponse
// @Failure		502	{object}	ErrorResponse
// @Router			/api/public/mp-link [post]
func (h *Handler) MiniProgramLink(c *gin.Context) {
	h.generateURLLink(c)
}

// generateURLLink is the shared body of both URL Link endpoints: validate
// the whitelisted page and bounded query, load the WeChat provider config,
// mint the link.
func (h *Handler) generateURLLink(c *gin.Context) {
	var req urllinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("body must be JSON with 'page'"))
		return
	}
	if err := validateURLLinkRequest(&req); err != nil {
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
	link, err := h.URLLink.Generate(ctx, wechatCfg.AppID, wechatCfg.Secret, wechat.URLLinkParams{
		Path:       req.Page,
		Query:      req.Query,
		EnvVersion: envVersion,
	})
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, urllinkResponse{URLLink: link})
}

// validateURLLinkRequest checks the caller-attributable request fields: the
// page must be on the share whitelist and the query must fit the length and
// charset caps.
func validateURLLinkRequest(req *urllinkRequest) error {
	if req.Page == "" {
		return apperror.BadRequest("page is required")
	}
	if !urllinkAllowedPages[req.Page] {
		return apperror.BadRequest("page is not allowed for share links")
	}
	if n := utf8.RuneCountInString(req.Query); n > urllinkQueryMaxRunes {
		return apperror.BadRequest("query must be at most 256 characters")
	}
	for _, r := range req.Query {
		if !strings.ContainsRune(urllinkQueryChars, r) {
			return apperror.BadRequest("query contains unsupported characters")
		}
	}
	if req.EnvVersion != nil && *req.EnvVersion != "" && !wxacodeEnvVersions[*req.EnvVersion] {
		return apperror.BadRequest("env_version must be release, trial or develop")
	}
	return nil
}
