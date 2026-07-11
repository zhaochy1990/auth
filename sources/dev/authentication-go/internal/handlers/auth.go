package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/auth/providers"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/middleware"
)

// --- Request / Response types ---

type registerRequest struct {
	Email      string  `json:"email"`
	Password   string  `json:"password"`
	Name       *string `json:"name"`
	InviteCode *string `json:"invite_code"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type providerLoginRequest struct {
	Credential json.RawMessage `json:"credential"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

type registerResponse struct {
	UserID       string `json:"user_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// --- Handlers ---

// Register creates a password user (optionally invite-gated), and returns tokens.
func (h *Handler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	ctx := c.Request.Context()

	if err := auth.ValidatePassword(req.Password); err != nil {
		middleware.RespondError(c, err)
		return
	}

	var inviteRecord *domain.InviteCode
	if requireInviteCode() {
		if req.InviteCode == nil || *req.InviteCode == "" {
			middleware.RespondError(c, apperror.BadRequest("invite_code is required"))
			return
		}
		record, err := h.Repo.InviteCodes().GetByCode(ctx, *req.InviteCode)
		if err != nil {
			middleware.RespondError(c, err)
			return
		}
		if record == nil || record.IsRevoked {
			middleware.RespondError(c, apperror.InviteCodeNotFound())
			return
		}
		if record.Kind == domain.InviteSingleUse && record.UsedAt != nil {
			middleware.RespondError(c, apperror.InviteCodeAlreadyUsed())
			return
		}
		inviteRecord = record
	}

	existing, err := h.Repo.Users().FindByEmail(ctx, req.Email)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if existing != nil {
		middleware.RespondError(c, apperror.UserAlreadyExists())
		return
	}

	now := time.Now().UTC()
	userID := uuid.NewString()

	// Claim a single-use invite code first (ETag-atomic) so a race leaves no orphan rows.
	if inviteRecord != nil && inviteRecord.Kind == domain.InviteSingleUse {
		if err := h.Repo.InviteCodes().MarkUsed(ctx, inviteRecord.Code, userID); err != nil {
			middleware.RespondError(c, err)
			return
		}
	}

	// Derive any granted membership.
	membership := domain.MembershipRegular
	var membershipExpires *time.Time
	if inviteRecord != nil && inviteRecord.GrantsMembership != nil && inviteRecord.GrantsMembership.IsPaid() {
		membership = *inviteRecord.GrantsMembership
		if inviteRecord.GrantsMembershipDays != nil {
			e := now.Add(time.Duration(*inviteRecord.GrantsMembershipDays) * 24 * time.Hour)
			membershipExpires = &e
		}
	}

	var invitedWith *string
	if inviteRecord != nil {
		invitedWith = strPtr(inviteRecord.Code)
	}

	user := &domain.User{
		ID:                  userID,
		Email:               strPtr(req.Email),
		Name:                req.Name,
		EmailVerified:       false,
		Role:                "user",
		UserType:            domain.UserTypeRegular,
		IsActive:            true,
		CustomAttributes:    map[string]any{},
		CreatedAt:           now,
		UpdatedAt:           now,
		InviteCode:          invitedWith,
		Membership:          membership,
		MembershipExpiresAt: membershipExpires,
	}
	if err := h.Repo.Users().Insert(ctx, user); err != nil {
		middleware.RespondError(c, err)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	accountID := uuid.NewString()
	account := &domain.Account{
		ID:                accountID,
		UserID:            userID,
		ProviderID:        "password",
		ProviderAccountID: strPtr(req.Email),
		Credential:        strPtr(hash),
		ProviderMetadata:  "{}",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := h.Repo.Accounts().Insert(ctx, account); err != nil {
		_ = h.Repo.Accounts().DeleteByID(ctx, accountID)
		_ = h.Repo.Users().DeleteByID(ctx, userID)
		middleware.RespondError(c, err)
		return
	}

	// Record initial login (best-effort).
	_ = h.Repo.Users().RecordLogin(ctx, userID, middleware.ClientIP(c, "unknown"))

	scopes := middleware.AllowedScopes(c)
	accessToken, err := h.JWT.IssueAccessToken(userID, middleware.ClientID(c), scopes, "user", user.Membership, user.UserType, user.Name)
	if err != nil {
		_ = h.Repo.Accounts().DeleteByID(ctx, accountID)
		_ = h.Repo.Users().DeleteByID(ctx, userID)
		middleware.RespondError(c, err)
		return
	}
	refreshToken := auth.GenerateRefreshToken()
	if err := auth.StoreRefreshToken(ctx, h.Repo, userID, middleware.AppID(c), refreshToken, scopes, nil, h.Cfg.JWTRefreshTokenExpiryDays); err != nil {
		_ = h.Repo.Accounts().DeleteByID(ctx, accountID)
		_ = h.Repo.Users().DeleteByID(ctx, userID)
		middleware.RespondError(c, err)
		return
	}

	c.JSON(http.StatusCreated, registerResponse{
		UserID:       userID,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    h.Cfg.JWTAccessTokenExpirySecs,
	})
}

// Login authenticates a password user and returns tokens.
func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	ctx := c.Request.Context()

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

	c.JSON(http.StatusOK, tokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    h.Cfg.JWTAccessTokenExpirySecs,
	})
}

// ProviderLogin authenticates via an external provider, creating the user on
// first sign-in.
func (h *Handler) ProviderLogin(c *gin.Context) {
	providerID := c.Param("provider_id")
	var req providerLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	ctx := c.Request.Context()

	appProvider, err := h.Repo.AppProviders().FindByAppAndProvider(ctx, middleware.AppID(c), providerID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if appProvider == nil || !appProvider.IsActive {
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

	now := time.Now().UTC()

	var userID, userRole string
	var userName *string
	var membership domain.MembershipTier
	userType := domain.UserTypeRegular

	existingAccount, err := h.Repo.Accounts().FindByProviderAccount(ctx, providerID, info.ProviderAccountID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if existingAccount != nil {
		existingAccount.ProviderMetadata = string(info.Metadata)
		existingAccount.UpdatedAt = now
		if err := h.Repo.Accounts().Update(ctx, existingAccount); err != nil {
			middleware.RespondError(c, err)
			return
		}
		user, err := h.Repo.Users().FindByID(ctx, existingAccount.UserID)
		if err != nil {
			middleware.RespondError(c, err)
			return
		}
		if user == nil {
			middleware.RespondError(c, apperror.UserNotFound())
			return
		}
		if !user.IsActive {
			middleware.RespondError(c, apperror.UserDisabled())
			return
		}
		membership = h.resolveMembership(ctx, user)
		userID, userRole, userName, userType = user.ID, user.Role, user.Name, domain.UserTypeFromString(string(user.UserType))
	} else {
		userID = uuid.NewString()
		user := &domain.User{
			ID:               userID,
			Email:            info.Email,
			Name:             info.Name,
			AvatarURL:        info.AvatarURL,
			EmailVerified:    false,
			Role:             "user",
			UserType:         domain.UserTypeRegular,
			IsActive:         true,
			CustomAttributes: map[string]any{},
			CreatedAt:        now,
			UpdatedAt:        now,
			Membership:       domain.MembershipRegular,
		}
		if err := h.Repo.Users().Insert(ctx, user); err != nil {
			middleware.RespondError(c, err)
			return
		}
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
		userRole, userName, membership = "user", info.Name, domain.MembershipRegular
	}

	_ = h.Repo.Users().RecordLogin(ctx, userID, middleware.ClientIP(c, "unknown"))

	scopes := middleware.AllowedScopes(c)
	accessToken, err := h.JWT.IssueAccessToken(userID, middleware.ClientID(c), scopes, userRole, membership, userType, userName)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	refreshToken := auth.GenerateRefreshToken()
	if err := auth.StoreRefreshToken(ctx, h.Repo, userID, middleware.AppID(c), refreshToken, scopes, nil, h.Cfg.JWTRefreshTokenExpiryDays); err != nil {
		middleware.RespondError(c, err)
		return
	}

	c.JSON(http.StatusOK, tokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    h.Cfg.JWTAccessTokenExpirySecs,
	})
}

// Refresh rotates a refresh token and issues a new access token.
func (h *Handler) Refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	ctx := c.Request.Context()

	userID, newRefreshToken, scopes, err := auth.RotateRefreshToken(ctx, h.Repo, req.RefreshToken, middleware.AppID(c), h.Cfg.JWTRefreshTokenExpiryDays)
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
		middleware.RespondError(c, apperror.UserDisabled())
		return
	}
	membership := h.resolveMembership(ctx, user)
	accessToken, err := h.JWT.IssueAccessToken(userID, middleware.ClientID(c), scopes, user.Role, membership, user.UserType, user.Name)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, tokenResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    h.Cfg.JWTAccessTokenExpirySecs,
	})
}

// Logout revokes a refresh token.
func (h *Handler) Logout(c *gin.Context) {
	var req logoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	if err := auth.RevokeRefreshToken(c.Request.Context(), h.Repo, req.RefreshToken); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
