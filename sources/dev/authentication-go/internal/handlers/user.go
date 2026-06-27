package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/auth/providers"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/middleware"
)

// --- Request / Response types ---

type userProfileResponse struct {
	ID                  string                `json:"id"`
	Email               *string               `json:"email"`
	Name                *string               `json:"name"`
	AvatarURL           *string               `json:"avatar_url"`
	EmailVerified       bool                  `json:"email_verified"`
	Membership          domain.MembershipTier `json:"membership"`
	MembershipExpiresAt *string               `json:"membership_expires_at"`
	CreatedAt           string                `json:"created_at"`
}

type updateProfileRequest struct {
	Name      *string `json:"name"`
	AvatarURL *string `json:"avatar_url"`
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
		Name:                user.Name,
		AvatarURL:           user.AvatarURL,
		EmailVerified:       user.EmailVerified,
		Membership:          membership,
		MembershipExpiresAt: displayDTPtr(user.MembershipExpiresAt),
		CreatedAt:           displayDT(user.CreatedAt),
	})
}

// UpdateProfile updates the authenticated user's name/avatar.
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
	now := time.Now().UTC()
	user.UpdatedAt = now
	if err := h.Repo.Users().Update(ctx, user); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, userProfileResponse{
		ID:                  user.ID,
		Email:               user.Email,
		Name:                user.Name,
		AvatarURL:           user.AvatarURL,
		EmailVerified:       user.EmailVerified,
		Membership:          user.EffectiveMembership(now),
		MembershipExpiresAt: displayDTPtr(user.MembershipExpiresAt),
		CreatedAt:           displayDT(user.CreatedAt),
	})
}

// ListAccounts lists the authenticated user's linked accounts.
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
func (h *Handler) LinkAccount(c *gin.Context) {
	providerID := c.Param("provider_id")
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
	if err := h.Repo.Accounts().DeleteByID(ctx, target.ID); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "unlinked"})
}

// DeleteMe deletes the authenticated user's account.
func (h *Handler) DeleteMe(c *gin.Context) {
	if err := h.deleteUserAccount(c.Request.Context(), middleware.UserID(c)); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// deleteUserAccount removes a user and all dependent rows, refusing if the user
// still owns any team. Shared by self-delete and admin delete.
func (h *Handler) deleteUserAccount(ctx context.Context, userID string) error {
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
	if err := h.Repo.RefreshTokens().DeleteAllByUser(ctx, userID); err != nil {
		return err
	}
	if err := h.Repo.AuthCodes().DeleteAllByUser(ctx, userID); err != nil {
		return err
	}
	if err := h.Repo.Accounts().DeleteAllByUser(ctx, userID); err != nil {
		return err
	}
	if err := h.Repo.TeamMemberships().DeleteAllByUser(ctx, userID); err != nil {
		return err
	}
	return h.Repo.Users().DeleteByID(ctx, userID)
}
