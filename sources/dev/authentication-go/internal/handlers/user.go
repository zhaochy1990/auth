package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/auth/providers"
	"github.com/zhaochy1990/auth-service/internal/brandoauth"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/middleware"
)

// --- Request / Response types ---

type userProfileResponse struct {
	ID                  string                `json:"id"`
	Email               *string               `json:"email"`
	Phone               *string               `json:"phone"`
	Name                *string               `json:"name"`
	AvatarURL           *string               `json:"avatar_url"`
	EmailVerified       bool                  `json:"email_verified"`
	UserType            domain.UserType       `json:"user_type"`
	Membership          domain.MembershipTier `json:"membership"`
	MembershipExpiresAt *string               `json:"membership_expires_at"`
	WeChatBound         bool                  `json:"wechat_bound"`
	CustomAttributes    map[string]any        `json:"custom_attributes"`
	CreatedAt           string                `json:"created_at"`
}

type updateProfileRequest struct {
	Name             *string        `json:"name"`
	AvatarURL        *string        `json:"avatar_url"`
	CustomAttributes map[string]any `json:"custom_attributes"`
}

type accountResponse struct {
	ProviderID        string  `json:"provider_id"`
	ProviderAccountID *string `json:"provider_account_id"`
	CreatedAt         string  `json:"created_at"`
}

type linkAccountRequest struct {
	Credential json.RawMessage `json:"credential"`
}

// --- Handlers ---

// GetProfile returns the authenticated user's profile.
//
// @Summary		Get the current user's profile
// @Tags			users
// @Produce		json
// @Success		200	{object}	userProfileResponse
// @Failure		401	{object}	ErrorResponse
// @Failure		404	{object}	ErrorResponse
// @Failure		500	{object}	ErrorResponse
// @Security		BearerAuth
// @Router			/api/users/me [get]
func (h *Handler) GetProfile(c *gin.Context) {
	ctx := c.Request.Context()
	user, err := h.Repo.Users().FindByID(ctx, middleware.UserID(c))
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		middleware.RespondError(c, apperror.UserNotFound())
		return
	}
	membership := h.resolveMembership(ctx, user)
	c.JSON(http.StatusOK, userProfileResponse{
		ID:                  user.ID,
		Email:               user.Email,
		Phone:               user.Phone,
		Name:                user.Name,
		AvatarURL:           user.AvatarURL,
		EmailVerified:       user.EmailVerified,
		UserType:            domain.UserTypeFromString(string(user.UserType)),
		Membership:          membership,
		MembershipExpiresAt: displayDTPtr(user.MembershipExpiresAt),
		WeChatBound:         user.WeChatBound,
		CustomAttributes:    customAttributesOrEmpty(user.CustomAttributes),
		CreatedAt:           displayDT(user.CreatedAt),
	})
}

// UpdateProfile updates the authenticated user's name/avatar.
//
// @Summary		Update the current user's profile
// @Tags			users
// @Accept			json
// @Produce		json
// @Param			body	body		updateProfileRequest	true	"Fields to update"
// @Success		200		{object}	userProfileResponse
// @Failure		400		{object}	ErrorResponse
// @Failure		401		{object}	ErrorResponse
// @Failure		404		{object}	ErrorResponse
// @Failure		500		{object}	ErrorResponse
// @Security		BearerAuth
// @Router			/api/users/me [patch]
func (h *Handler) UpdateProfile(c *gin.Context) {
	var req updateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	ctx := c.Request.Context()
	user, err := h.Repo.Users().FindByID(ctx, middleware.UserID(c))
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		middleware.RespondError(c, apperror.UserNotFound())
		return
	}
	if req.Name != nil {
		user.Name = req.Name
	}
	if req.AvatarURL != nil {
		user.AvatarURL = req.AvatarURL
	}
	if req.CustomAttributes != nil {
		user.CustomAttributes = mergeCustomAttributes(user.CustomAttributes, req.CustomAttributes)
	}
	now := time.Now().UTC()
	user.UpdatedAt = now
	if err := h.Repo.Users().Update(ctx, user); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, userProfileResponse{
		ID:                  user.ID,
		Email:               user.Email,
		Phone:               user.Phone,
		Name:                user.Name,
		AvatarURL:           user.AvatarURL,
		EmailVerified:       user.EmailVerified,
		UserType:            domain.UserTypeFromString(string(user.UserType)),
		Membership:          user.EffectiveMembership(now),
		MembershipExpiresAt: displayDTPtr(user.MembershipExpiresAt),
		WeChatBound:         user.WeChatBound,
		CustomAttributes:    customAttributesOrEmpty(user.CustomAttributes),
		CreatedAt:           displayDT(user.CreatedAt),
	})
}

const avatarMaxBytes = 2 << 20 // 2 MB

// UploadAvatar stores a mini-program avator image in Tencent Cloud COS and
// returns its public URL. The mini-program then PATCHes avatar_url with that
// URL (the existing profile flow). COS credentials stay server-side; the file
// is never stored in MySQL. Objects are keyed by the user UUID so the key is
// not guessable and does not leak personal info.
//
// @Summary		Upload the current user's avatar
// @Description	Uploads an avatar image (multipart/form-data, field `file`, \u2264 2MB, image/*) to Tencent Cloud COS and returns its public URL.
// @Tags			users
// @Accept			multipart/form-data
// @Produce		json
// @Param			file	formData	file	true	"Avatar image"
// @Success		200			{object}	avatarUploadResponse
// @Failure		400			{object}	ErrorResponse
// @Failure		401			{object}	ErrorResponse
// @Failure		502			{object}	ErrorResponse
// @Security		BearerAuth
// @Router			/api/users/me/avatar [post]
func (h *Handler) UploadAvatar(c *gin.Context) {
	if !h.CosClient.Configured() {
		middleware.RespondError(c, apperror.CosNotConfigured())
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		middleware.RespondError(c, apperror.BadRequest("file is required"))
		return
	}
	if file.Size > avatarMaxBytes {
		middleware.RespondError(c, apperror.BadRequest("avatar file exceeds 2MB"))
		return
	}
	f, err := file.Open()
	if err != nil {
		middleware.RespondError(c, apperror.BadRequest("unable to read uploaded file"))
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		middleware.RespondError(c, apperror.BadRequest("unable to read uploaded file"))
		return
	}
	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		middleware.RespondError(c, apperror.BadRequest("uploaded file must be an image"))
		return
	}
	key := "avatars/" + middleware.UserID(c) + "." + avatarExt(contentType)
	if err := h.CosClient.Upload(c.Request.Context(), key, bytes.NewReader(data), contentType); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, avatarUploadResponse{AvatarURL: h.CosClient.PublicURL(key)})
}

func avatarExt(contentType string) string {
	switch strings.ToLower(contentType) {
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	default:
		return "jpg"
	}
}

// avatarUploadResponse is the JSON body returned by POST /api/users/me/avatar.
type avatarUploadResponse struct {
	AvatarURL string `json:"avatar_url"`
}

// ListAccounts lists the authenticated user's linked accounts.
//
// @Summary		List the current user's linked accounts
// @Tags			users
// @Produce		json
// @Success		200	{array}		accountResponse
// @Failure		401	{object}	ErrorResponse
// @Failure		500	{object}	ErrorResponse
// @Security		BearerAuth
// @Router			/api/users/me/accounts [get]
func (h *Handler) ListAccounts(c *gin.Context) {
	accounts, err := h.Repo.Accounts().FindAllByUser(c.Request.Context(), middleware.UserID(c))
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	out := make([]accountResponse, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, accountResponse{
			ProviderID:        a.ProviderID,
			ProviderAccountID: a.ProviderAccountID,
			CreatedAt:         displayDT(a.CreatedAt),
		})
	}
	c.JSON(http.StatusOK, out)
}

// LinkAccount links an external provider account to the authenticated user.
//
// @Summary		Link an external provider account
// @Tags			users
// @Accept			json
// @Produce		json
// @Param			provider_id	path		string				true	"Provider id"
// @Param			body		body		linkAccountRequest	true	"Provider credential"
// @Success		200			{object}	accountResponse
// @Failure		400			{object}	ErrorResponse
// @Failure		401			{object}	ErrorResponse
// @Failure		404			{object}	ErrorResponse
// @Failure		409			{object}	ErrorResponse
// @Failure		500			{object}	ErrorResponse
// @Security		BearerAuth
// @Router			/api/users/me/accounts/{provider_id}/link [post]
func (h *Handler) LinkAccount(c *gin.Context) {
	providerID := c.Param("provider_id")
	// Providers on the OAuth2 linking layer are driven by a browser redirect,
	// not by a submitted credential; point callers at the right endpoint
	// instead of the unhelpful "provider not supported".
	if _, ok := brandoauth.Default().Lookup(providerID); ok {
		middleware.RespondError(c, apperror.BadRequest(
			"This provider requires OAuth authorization; use POST /api/users/me/accounts/"+providerID+"/authorize"))
		return
	}
	var req linkAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	ctx := c.Request.Context()
	userID := middleware.UserID(c)

	existing, err := h.Repo.Accounts().FindByUserAndProvider(ctx, userID, providerID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if existing != nil {
		middleware.RespondError(c, apperror.AccountAlreadyLinked())
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
	appProvider, err := h.Repo.AppProviders().FindByAppAndProvider(ctx, app.ID, providerID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if appProvider == nil {
		middleware.RespondError(c, apperror.ProviderNotConfigured())
		return
	}

	provider, err := providers.Create(providerID, json.RawMessage(appProvider.Config), h.Cfg.EnableTestProviders)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	info, err := provider.Authenticate(ctx, req.Credential)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}

	alreadyLinked, err := h.Repo.Accounts().FindByProviderAccount(ctx, providerID, info.ProviderAccountID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if alreadyLinked != nil {
		middleware.RespondError(c, apperror.AccountAlreadyLinked())
		return
	}

	now := time.Now().UTC()
	account := &domain.Account{
		ID:                uuid.NewString(),
		UserID:            userID,
		ProviderID:        providerID,
		ProviderAccountID: strPtr(info.ProviderAccountID),
		ProviderMetadata:  string(info.Metadata),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := h.Repo.Accounts().Insert(ctx, account); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, accountResponse{
		ProviderID:        providerID,
		ProviderAccountID: strPtr(info.ProviderAccountID),
		CreatedAt:         displayDT(now),
	})
}

// UnlinkAccount unlinks a provider account (never the last one).
//
// @Summary		Unlink a provider account
// @Description	Unlinks a linked provider account. Refuses to remove the user's last remaining account.
// @Tags			users
// @Produce		json
// @Param			provider_id	path		string	true	"Provider id"
// @Success		200			{object}	StatusResponse
// @Failure		400			{object}	ErrorResponse
// @Failure		401			{object}	ErrorResponse
// @Failure		500			{object}	ErrorResponse
// @Security		BearerAuth
// @Router			/api/users/me/accounts/{provider_id} [delete]
func (h *Handler) UnlinkAccount(c *gin.Context) {
	providerID := c.Param("provider_id")
	ctx := c.Request.Context()
	userID := middleware.UserID(c)

	accounts, err := h.Repo.Accounts().FindAllByUser(ctx, userID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if len(accounts) <= 1 {
		middleware.RespondError(c, apperror.CannotUnlinkLastAccount())
		return
	}
	var target *domain.Account
	for i := range accounts {
		if accounts[i].ProviderID == providerID {
			target = &accounts[i]
			break
		}
	}
	if target == nil {
		middleware.RespondError(c, apperror.BadRequest("Account not linked"))
		return
	}
	// Best-effort: ask the brand to revoke the grant before dropping the local
	// row. A failure here must not block the unlink.
	h.revokeOAuthAccount(ctx, target, middleware.ClientID(c))
	if err := h.Repo.Accounts().DeleteByID(ctx, target.ID); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "unlinked"})
}

// DeleteMe deletes the authenticated user's account.
//
// @Summary		Delete the current user's account
// @Description	Deletes the authenticated user and all dependent rows. Refuses if the user still owns any team.
// @Tags			users
// @Produce		json
// @Success		204	"No Content"
// @Failure		401	{object}	ErrorResponse
// @Failure		404	{object}	ErrorResponse
// @Failure		409	{object}	ErrorResponse
// @Failure		500	{object}	ErrorResponse
// @Security		BearerAuth
// @Router			/api/users/me [delete]
func (h *Handler) DeleteMe(c *gin.Context) {
	if err := h.deleteUserAccount(c.Request.Context(), middleware.UserID(c), middleware.ClientID(c)); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// deleteUserAccount removes a user and all dependent rows, refusing if the user
// still owns any team. Shared by self-delete and admin delete. appClientID
// identifies the calling application whose provider config is used for the
// best-effort brand deauthorization that precedes deleting the account rows.
func (h *Handler) deleteUserAccount(ctx context.Context, userID, appClientID string) error {
	user, err := h.Repo.Users().FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return apperror.UserNotFound()
	}
	owned, err := h.Repo.Teams().FindAllOwnedByUser(ctx, userID)
	if err != nil {
		return err
	}
	if len(owned) > 0 {
		return apperror.UserOwnsTeams(len(owned))
	}
	accounts, err := h.Repo.Accounts().FindAllByUser(ctx, userID)
	if err != nil {
		return err
	}
	for i := range accounts {
		h.revokeOAuthAccount(ctx, &accounts[i], appClientID)
	}
	if err := h.Repo.RefreshTokens().DeleteAllByUser(ctx, userID); err != nil {
		return err
	}
	if err := h.Repo.AuthCodes().DeleteAllByUser(ctx, userID); err != nil {
		return err
	}
	if err := h.Repo.Accounts().DeleteAllByUser(ctx, userID); err != nil {
		return err
	}
	if err := h.Repo.Users().DeleteWeChatLinksByUser(ctx, userID); err != nil {
		return err
	}
	if err := h.Repo.TeamMemberships().DeleteAllByUser(ctx, userID); err != nil {
		return err
	}
	return h.Repo.Users().DeleteByID(ctx, userID)
}
