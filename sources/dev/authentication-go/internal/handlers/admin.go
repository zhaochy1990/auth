package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/middleware"
)

// --- Request / Response types ---

type createApplicationRequest struct {
	Name          string   `json:"name"`
	RedirectURIs  []string `json:"redirect_uris"`
	AllowedScopes []string `json:"allowed_scopes"`
}

type createApplicationResponse struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ClientID      string   `json:"client_id"`
	ClientSecret  string   `json:"client_secret"`
	RedirectURIs  []string `json:"redirect_uris"`
	AllowedScopes []string `json:"allowed_scopes"`
}

type updateApplicationRequest struct {
	Name          *string   `json:"name"`
	RedirectURIs  *[]string `json:"redirect_uris"`
	AllowedScopes *[]string `json:"allowed_scopes"`
	IsActive      *bool     `json:"is_active"`
}

type applicationResponse struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ClientID      string   `json:"client_id"`
	RedirectURIs  []string `json:"redirect_uris"`
	AllowedScopes []string `json:"allowed_scopes"`
	IsActive      bool     `json:"is_active"`
	CreatedAt     string   `json:"created_at"`
}

type addProviderRequest struct {
	ProviderID string          `json:"provider_id"`
	Config     json.RawMessage `json:"config"`
}

type providerResponse struct {
	ID         string          `json:"id"`
	ProviderID string          `json:"provider_id"`
	Config     json.RawMessage `json:"config"`
	IsActive   bool            `json:"is_active"`
	CreatedAt  string          `json:"created_at"`
}

type rotateSecretResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type loginRecordResponse struct {
	At string `json:"at"`
	IP string `json:"ip"`
}

type userResponse struct {
	ID                  string                `json:"id"`
	Email               *string               `json:"email"`
	Name                *string               `json:"name"`
	AvatarURL           *string               `json:"avatar_url"`
	EmailVerified       bool                  `json:"email_verified"`
	Role                string                `json:"role"`
	Membership          domain.MembershipTier `json:"membership"`
	MembershipExpiresAt *string               `json:"membership_expires_at"`
	IsActive            bool                  `json:"is_active"`
	Note                *string               `json:"note"`
	CustomAttributes    map[string]any        `json:"custom_attributes"`
	CreatedAt           string                `json:"created_at"`
	UpdatedAt           string                `json:"updated_at"`
	LastLoginAt         *string               `json:"last_login_at"`
	RecentLogins        []loginRecordResponse `json:"recent_logins"`
}

func toUserResponse(u *domain.User) userResponse {
	logins := make([]loginRecordResponse, 0, len(u.RecentLogins))
	for _, r := range u.RecentLogins {
		logins = append(logins, loginRecordResponse{At: displayDT(r.At), IP: r.IP})
	}
	return userResponse{
		ID:                  u.ID,
		Email:               u.Email,
		Name:                u.Name,
		AvatarURL:           u.AvatarURL,
		EmailVerified:       u.EmailVerified,
		Role:                u.Role,
		Membership:          u.Membership,
		MembershipExpiresAt: displayDTPtr(u.MembershipExpiresAt),
		IsActive:            u.IsActive,
		Note:                u.Note,
		CustomAttributes:    customAttributesOrEmpty(u.CustomAttributes),
		CreatedAt:           displayDT(u.CreatedAt),
		UpdatedAt:           displayDT(u.UpdatedAt),
		LastLoginAt:         displayDTPtr(u.LastLoginAt),
		RecentLogins:        logins,
	}
}

type userListResponse struct {
	Users   []userResponse `json:"users"`
	Total   uint64         `json:"total"`
	Page    uint64         `json:"page"`
	PerPage uint64         `json:"per_page"`
}

type updateUserRequest struct {
	Name                *string                `json:"name"`
	Role                *string                `json:"role"`
	Membership          *domain.MembershipTier `json:"membership"`
	MembershipExpiresAt *string                `json:"membership_expires_at"`
	IsActive            *bool                  `json:"is_active"`
	Note                *string                `json:"note"`
	CustomAttributes    map[string]any         `json:"custom_attributes"`
}

type createUserRequest struct {
	Email            string                 `json:"email"`
	Password         string                 `json:"password"`
	Name             *string                `json:"name"`
	Role             *string                `json:"role"`
	Membership       *domain.MembershipTier `json:"membership"`
	CustomAttributes map[string]any         `json:"custom_attributes"`
}

type resetUserPasswordRequest struct {
	Password       string `json:"password"`
	RevokeSessions *bool  `json:"revoke_sessions"`
}

type resetUserPasswordResponse struct {
	UserID          string `json:"user_id"`
	RevokedSessions bool   `json:"revoked_sessions"`
}

type userAccountResponse struct {
	ID                string  `json:"id"`
	ProviderID        string  `json:"provider_id"`
	ProviderAccountID *string `json:"provider_account_id"`
	CreatedAt         string  `json:"created_at"`
}

type statsResponse struct {
	Applications appStats  `json:"applications"`
	Users        userStats `json:"users"`
}

type appStats struct {
	Total    uint64 `json:"total"`
	Active   uint64 `json:"active"`
	Inactive uint64 `json:"inactive"`
}

type userStats struct {
	Total  uint64 `json:"total"`
	Recent uint64 `json:"recent"`
}

type inviteCodeResponse struct {
	ID                   string                `json:"id"`
	Code                 string                `json:"code"`
	CreatedBy            string                `json:"created_by"`
	CreatedAt            string                `json:"created_at"`
	UsedAt               *string               `json:"used_at"`
	UsedBy               *string               `json:"used_by"`
	IsRevoked            bool                  `json:"is_revoked"`
	Kind                 domain.InviteCodeKind `json:"kind"`
	GrantsMembership     *string               `json:"grants_membership"`
	GrantsMembershipDays *int64                `json:"grants_membership_days"`
}

func toInviteCodeResponse(ic *domain.InviteCode) inviteCodeResponse {
	var grants *string
	if ic.GrantsMembership != nil {
		v := string(*ic.GrantsMembership)
		grants = &v
	}
	return inviteCodeResponse{
		ID:                   ic.ID,
		Code:                 ic.Code,
		CreatedBy:            ic.CreatedBy,
		CreatedAt:            displayDT(ic.CreatedAt),
		UsedAt:               displayDTPtr(ic.UsedAt),
		UsedBy:               ic.UsedBy,
		IsRevoked:            ic.IsRevoked,
		Kind:                 ic.Kind,
		GrantsMembership:     grants,
		GrantsMembershipDays: ic.GrantsMembershipDays,
	}
}

type adminCreateTeamRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	OwnerUserID string  `json:"owner_user_id"`
	IsOpen      *bool   `json:"is_open"`
}

type adminAddMemberRequest struct {
	UserID string  `json:"user_id"`
	Role   *string `json:"role"`
}

type adminTeamMembershipResponse struct {
	TeamID   string `json:"team_id"`
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	JoinedAt string `json:"joined_at"`
}

// --- Application handlers ---

// CreateApplication registers a new OAuth2 application.
func (h *Handler) CreateApplication(c *gin.Context) {
	var req createApplicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	if req.RedirectURIs == nil {
		req.RedirectURIs = []string{}
	}
	if req.AllowedScopes == nil {
		req.AllowedScopes = []string{}
	}
	clientID := auth.GenerateClientID()
	secret := auth.RandomHex(32)
	now := time.Now().UTC()
	id := uuid.NewString()
	redirectJSON, _ := json.Marshal(req.RedirectURIs)
	scopesJSON, _ := json.Marshal(req.AllowedScopes)
	app := &domain.Application{
		ID:               id,
		Name:             req.Name,
		ClientID:         clientID,
		ClientSecretHash: auth.HashClientSecret(secret),
		RedirectURIs:     string(redirectJSON),
		AllowedScopes:    string(scopesJSON),
		IsActive:         true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := h.Repo.Applications().Insert(c.Request.Context(), app); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, createApplicationResponse{
		ID: id, Name: req.Name, ClientID: clientID, ClientSecret: secret,
		RedirectURIs: req.RedirectURIs, AllowedScopes: req.AllowedScopes,
	})
}

// ListApplications lists all applications.
func (h *Handler) ListApplications(c *gin.Context) {
	apps, err := h.Repo.Applications().FindAll(c.Request.Context())
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	out := make([]applicationResponse, 0, len(apps))
	for i := range apps {
		out = append(out, toApplicationResponse(&apps[i]))
	}
	c.JSON(http.StatusOK, out)
}

func toApplicationResponse(a *domain.Application) applicationResponse {
	return applicationResponse{
		ID:            a.ID,
		Name:          a.Name,
		ClientID:      a.ClientID,
		RedirectURIs:  auth.DecodeStringArray(a.RedirectURIs),
		AllowedScopes: auth.DecodeStringArray(a.AllowedScopes),
		IsActive:      a.IsActive,
		CreatedAt:     displayDT(a.CreatedAt),
	}
}

// UpdateApplication patches an application.
func (h *Handler) UpdateApplication(c *gin.Context) {
	var req updateApplicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	ctx := c.Request.Context()
	app, err := h.Repo.Applications().FindByID(ctx, c.Param("id"))
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if app == nil {
		middleware.RespondError(c, apperror.ApplicationNotFound())
		return
	}
	if req.Name != nil {
		app.Name = *req.Name
	}
	if req.RedirectURIs != nil {
		b, _ := json.Marshal(*req.RedirectURIs)
		app.RedirectURIs = string(b)
	}
	if req.AllowedScopes != nil {
		b, _ := json.Marshal(*req.AllowedScopes)
		app.AllowedScopes = string(b)
	}
	if req.IsActive != nil {
		app.IsActive = *req.IsActive
	}
	app.UpdatedAt = time.Now().UTC()
	if err := h.Repo.Applications().Update(ctx, app); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, toApplicationResponse(app))
}

// AddProvider attaches an auth provider to an application.
func (h *Handler) AddProvider(c *gin.Context) {
	var req addProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	ctx := c.Request.Context()
	appID := c.Param("id")
	app, err := h.Repo.Applications().FindByID(ctx, appID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if app == nil {
		middleware.RespondError(c, apperror.ApplicationNotFound())
		return
	}
	existing, err := h.Repo.AppProviders().FindByAppAndProvider(ctx, appID, req.ProviderID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if existing != nil {
		middleware.RespondError(c, apperror.BadRequest("Provider already configured for this application"))
		return
	}
	cfg := string(req.Config)
	if cfg == "" {
		cfg = "{}"
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	ap := &domain.AppProvider{ID: id, AppID: appID, ProviderID: req.ProviderID, Config: cfg, IsActive: true, CreatedAt: now}
	if err := h.Repo.AppProviders().Insert(ctx, ap); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, providerResponse{
		ID: id, ProviderID: req.ProviderID, Config: json.RawMessage(cfg), IsActive: true, CreatedAt: displayDT(now),
	})
}

// RemoveProvider detaches a provider from an application.
func (h *Handler) RemoveProvider(c *gin.Context) {
	ctx := c.Request.Context()
	provider, err := h.Repo.AppProviders().FindByAppAndProvider(ctx, c.Param("id"), c.Param("provider_id"))
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if provider == nil {
		middleware.RespondError(c, apperror.ProviderNotConfigured())
		return
	}
	if err := h.Repo.AppProviders().DeleteByID(ctx, provider.ID); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// RotateSecret rotates an application's client secret.
func (h *Handler) RotateSecret(c *gin.Context) {
	ctx := c.Request.Context()
	app, err := h.Repo.Applications().FindByID(ctx, c.Param("id"))
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if app == nil {
		middleware.RespondError(c, apperror.ApplicationNotFound())
		return
	}
	secret := auth.RandomHex(32)
	app.ClientSecretHash = auth.HashClientSecret(secret)
	app.UpdatedAt = time.Now().UTC()
	if err := h.Repo.Applications().Update(ctx, app); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, rotateSecretResponse{ClientID: app.ClientID, ClientSecret: secret})
}

// ListProviders lists an application's providers.
func (h *Handler) ListProviders(c *gin.Context) {
	ctx := c.Request.Context()
	appID := c.Param("id")
	app, err := h.Repo.Applications().FindByID(ctx, appID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if app == nil {
		middleware.RespondError(c, apperror.ApplicationNotFound())
		return
	}
	providers, err := h.Repo.AppProviders().FindAllByApp(ctx, appID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	out := make([]providerResponse, 0, len(providers))
	for _, p := range providers {
		cfg := p.Config
		if cfg == "" {
			cfg = "{}"
		}
		out = append(out, providerResponse{
			ID: p.ID, ProviderID: p.ProviderID, Config: json.RawMessage(cfg), IsActive: p.IsActive, CreatedAt: displayDT(p.CreatedAt),
		})
	}
	c.JSON(http.StatusOK, out)
}

// --- User handlers ---

// ListUsers lists users with pagination and optional search.
func (h *Handler) ListUsers(c *gin.Context) {
	page := parseUintDefault(c.Query("page"), 1)
	if page < 1 {
		page = 1
	}
	perPage := parseUintDefault(c.Query("per_page"), 20)
	if perPage > 100 {
		perPage = 100
	}
	offset := (page - 1) * perPage

	users, total, err := h.Repo.Users().ListPaginated(c.Request.Context(), c.Query("search"), offset, perPage)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	out := make([]userResponse, 0, len(users))
	for i := range users {
		out = append(out, toUserResponse(&users[i]))
	}
	c.JSON(http.StatusOK, userListResponse{Users: out, Total: total, Page: page, PerPage: perPage})
}

// GetUser returns a single user.
func (h *Handler) GetUser(c *gin.Context) {
	user, err := h.Repo.Users().FindByID(c.Request.Context(), c.Param("id"))
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		middleware.RespondError(c, apperror.UserNotFound())
		return
	}
	c.JSON(http.StatusOK, toUserResponse(user))
}

// GetUserAccounts lists a user's linked accounts.
func (h *Handler) GetUserAccounts(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.Param("id")
	user, err := h.Repo.Users().FindByID(ctx, userID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		middleware.RespondError(c, apperror.UserNotFound())
		return
	}
	accounts, err := h.Repo.Accounts().FindAllByUser(ctx, userID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	out := make([]userAccountResponse, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, userAccountResponse{
			ID: a.ID, ProviderID: a.ProviderID, ProviderAccountID: a.ProviderAccountID, CreatedAt: displayDT(a.CreatedAt),
		})
	}
	c.JSON(http.StatusOK, out)
}

// CreateUser creates a user with a password account.
func (h *Handler) CreateUser(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		middleware.RespondError(c, err)
		return
	}
	role := "user"
	if req.Role != nil {
		role = *req.Role
	}
	if role != "user" && role != "admin" {
		middleware.RespondError(c, apperror.BadRequest("Role must be 'user' or 'admin'"))
		return
	}
	membership := domain.MembershipRegular
	if req.Membership != nil {
		membership = domain.MembershipFromString(string(*req.Membership))
	}
	ctx := c.Request.Context()
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
	user := &domain.User{
		ID: userID, Email: strPtr(req.Email), Name: req.Name, EmailVerified: false,
		Role: role, IsActive: true, CustomAttributes: req.CustomAttributes,
		CreatedAt: now, UpdatedAt: now, Membership: membership,
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
	account := &domain.Account{
		ID: uuid.NewString(), UserID: userID, ProviderID: "password",
		ProviderAccountID: strPtr(req.Email), Credential: strPtr(hash),
		ProviderMetadata: "{}", CreatedAt: now, UpdatedAt: now,
	}
	if err := h.Repo.Accounts().Insert(ctx, account); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, toUserResponse(user))
}

// UpdateUser patches a user.
func (h *Handler) UpdateUser(c *gin.Context) {
	var req updateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	ctx := c.Request.Context()
	user, err := h.Repo.Users().FindByID(ctx, c.Param("id"))
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
	if req.Role != nil {
		if *req.Role != "user" && *req.Role != "admin" {
			middleware.RespondError(c, apperror.BadRequest("Role must be 'user' or 'admin'"))
			return
		}
		user.Role = *req.Role
	}
	if req.Membership != nil {
		m := domain.MembershipFromString(string(*req.Membership))
		user.Membership = m
		if !m.IsPaid() {
			user.MembershipExpiresAt = nil
		}
	}
	if req.MembershipExpiresAt != nil {
		if strings.TrimSpace(*req.MembershipExpiresAt) == "" {
			user.MembershipExpiresAt = nil
		} else if user.Membership.IsPaid() {
			t, err := parseMembershipExpiry(*req.MembershipExpiresAt)
			if err != nil {
				middleware.RespondError(c, err)
				return
			}
			user.MembershipExpiresAt = &t
		}
	}
	if req.IsActive != nil {
		user.IsActive = *req.IsActive
	}
	if req.Note != nil {
		if *req.Note == "" {
			user.Note = nil
		} else {
			user.Note = req.Note
		}
	}
	if req.CustomAttributes != nil {
		user.CustomAttributes = mergeCustomAttributes(user.CustomAttributes, req.CustomAttributes)
	}
	user.UpdatedAt = time.Now().UTC()
	if err := h.Repo.Users().Update(ctx, user); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, toUserResponse(user))
}

// DeleteUser deletes a user account (admin).
func (h *Handler) DeleteUser(c *gin.Context) {
	if err := h.deleteUserAccount(c.Request.Context(), c.Param("id")); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ResetUserPassword sets a new password for a user, optionally revoking sessions.
func (h *Handler) ResetUserPassword(c *gin.Context) {
	var req resetUserPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	revoke := true
	if req.RevokeSessions != nil {
		revoke = *req.RevokeSessions
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		middleware.RespondError(c, err)
		return
	}
	ctx := c.Request.Context()
	id := c.Param("id")
	user, err := h.Repo.Users().FindByID(ctx, id)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		middleware.RespondError(c, apperror.UserNotFound())
		return
	}
	account, err := h.Repo.Accounts().FindByUserAndProvider(ctx, id, "password")
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if account == nil {
		middleware.RespondError(c, apperror.BadRequest("User has no password provider account"))
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	account.Credential = strPtr(hash)
	account.UpdatedAt = time.Now().UTC()
	if err := h.Repo.Accounts().Update(ctx, account); err != nil {
		middleware.RespondError(c, err)
		return
	}
	if revoke {
		if err := h.Repo.RefreshTokens().DeleteAllByUser(ctx, id); err != nil {
			middleware.RespondError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, resetUserPasswordResponse{UserID: id, RevokedSessions: revoke})
}

// AdminUnlinkAccount unlinks a provider account from a user (never the last).
func (h *Handler) AdminUnlinkAccount(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.Param("id")
	providerID := c.Param("provider_id")
	user, err := h.Repo.Users().FindByID(ctx, userID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		middleware.RespondError(c, apperror.UserNotFound())
		return
	}
	account, err := h.Repo.Accounts().FindByUserAndProvider(ctx, userID, providerID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if account == nil {
		middleware.RespondError(c, apperror.BadRequest("Account not linked"))
		return
	}
	count, err := h.Repo.Accounts().CountByUser(ctx, userID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if count <= 1 {
		middleware.RespondError(c, apperror.CannotUnlinkLastAccount())
		return
	}
	if err := h.Repo.Accounts().DeleteByID(ctx, account.ID); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "unlinked"})
}

// Stats returns application and user counts.
func (h *Handler) Stats(c *gin.Context) {
	ctx := c.Request.Context()
	totalApps, err := h.Repo.Applications().CountAll(ctx)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	activeApps, err := h.Repo.Applications().CountActive(ctx)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	totalUsers, err := h.Repo.Users().CountAll(ctx)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	recentUsers, err := h.Repo.Users().CountSince(ctx, time.Now().UTC().Add(-7*24*time.Hour))
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, statsResponse{
		Applications: appStats{Total: totalApps, Active: activeApps, Inactive: totalApps - activeApps},
		Users:        userStats{Total: totalUsers, Recent: recentUsers},
	})
}

// --- Invite code handlers ---

// CreateInviteCode mints an invite code.
func (h *Handler) CreateInviteCode(c *gin.Context) {
	kind := domain.InviteKindFromString(c.Query("kind"))
	var grants *domain.MembershipTier
	if g := c.Query("grants_membership"); g != "" {
		t := domain.MembershipFromString(g)
		if t.IsPaid() {
			grants = &t
		}
	}
	var days *int64
	if grants != nil {
		if d := c.Query("grants_membership_days"); d != "" {
			if n, err := strconv.ParseInt(d, 10, 64); err == nil {
				days = &n
			}
		}
	}
	code, err := h.Repo.InviteCodes().Create(c.Request.Context(), middleware.UserID(c), kind, grants, days)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, toInviteCodeResponse(code))
}

// ListInviteCodes lists invite codes, optionally filtered by used status.
func (h *Handler) ListInviteCodes(c *gin.Context) {
	var used *bool
	if u := c.Query("used"); u == "true" || u == "false" {
		b := u == "true"
		used = &b
	}
	codes, err := h.Repo.InviteCodes().List(c.Request.Context(), used)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	out := make([]inviteCodeResponse, 0, len(codes))
	for i := range codes {
		out = append(out, toInviteCodeResponse(&codes[i]))
	}
	c.JSON(http.StatusOK, out)
}

// RevokeInviteCode revokes an invite code.
func (h *Handler) RevokeInviteCode(c *gin.Context) {
	if err := h.Repo.InviteCodes().Revoke(c.Request.Context(), c.Param("code")); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

// --- Admin team management ---

// AdminCreateTeam creates a team on behalf of a user.
func (h *Handler) AdminCreateTeam(c *gin.Context) {
	var req adminCreateTeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		middleware.RespondError(c, apperror.BadRequest("Team name must be 1-100 characters"))
		return
	}
	ctx := c.Request.Context()
	owner, err := h.Repo.Users().FindByID(ctx, req.OwnerUserID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if owner == nil {
		middleware.RespondError(c, apperror.UserNotFound())
		return
	}
	isOpen := true
	if req.IsOpen != nil {
		isOpen = *req.IsOpen
	}
	now := time.Now().UTC()
	team := &domain.Team{
		ID: uuid.NewString(), Name: name, Description: req.Description,
		OwnerUserID: req.OwnerUserID, IsOpen: isOpen, CreatedAt: now, UpdatedAt: now,
	}
	if err := h.Repo.Teams().Insert(ctx, team); err != nil {
		middleware.RespondError(c, err)
		return
	}
	if err := h.Repo.TeamMemberships().Insert(ctx, &domain.TeamMembership{
		TeamID: team.ID, UserID: req.OwnerUserID, Role: "owner", JoinedAt: now,
	}); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, toTeamResponse(team, 1))
}

// AdminAddTeamMember adds a user to a team.
func (h *Handler) AdminAddTeamMember(c *gin.Context) {
	var req adminAddMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	ctx := c.Request.Context()
	teamID := c.Param("id")
	team, err := h.Repo.Teams().FindByID(ctx, teamID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if team == nil {
		middleware.RespondError(c, apperror.TeamNotFound())
		return
	}
	user, err := h.Repo.Users().FindByID(ctx, req.UserID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		middleware.RespondError(c, apperror.UserNotFound())
		return
	}
	role := "member"
	if req.Role != nil {
		role = *req.Role
	}
	if role != "member" && role != "owner" {
		middleware.RespondError(c, apperror.BadRequest("Role must be 'member' or 'owner'"))
		return
	}
	existing, err := h.Repo.TeamMemberships().Find(ctx, teamID, req.UserID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if existing != nil {
		c.JSON(http.StatusOK, adminTeamMembershipResponse{
			TeamID: existing.TeamID, UserID: existing.UserID, Role: existing.Role, JoinedAt: displayDT(existing.JoinedAt),
		})
		return
	}
	now := time.Now().UTC()
	m := &domain.TeamMembership{TeamID: teamID, UserID: req.UserID, Role: role, JoinedAt: now}
	if err := h.Repo.TeamMemberships().Insert(ctx, m); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, adminTeamMembershipResponse{
		TeamID: m.TeamID, UserID: m.UserID, Role: m.Role, JoinedAt: displayDT(m.JoinedAt),
	})
}

// AdminRemoveTeamMember removes a non-owner member from a team.
func (h *Handler) AdminRemoveTeamMember(c *gin.Context) {
	ctx := c.Request.Context()
	teamID := c.Param("id")
	userID := c.Param("user_id")
	team, err := h.Repo.Teams().FindByID(ctx, teamID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if team == nil {
		middleware.RespondError(c, apperror.TeamNotFound())
		return
	}
	if team.OwnerUserID == userID {
		middleware.RespondError(c, apperror.BadRequest("Cannot remove team owner; delete the team or transfer ownership first"))
		return
	}
	existing, err := h.Repo.TeamMemberships().Find(ctx, teamID, userID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if existing == nil {
		middleware.RespondError(c, apperror.BadRequest("User is not a member of this team"))
		return
	}
	if err := h.Repo.TeamMemberships().Delete(ctx, teamID, userID); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "removed"})
}

// --- Helpers ---

func parseMembershipExpiry(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, apperror.BadRequest("membership_expires_at must be an ISO 8601 date or datetime")
}

func parseUintDefault(s string, def uint64) uint64 {
	if s == "" {
		return def
	}
	if n, err := strconv.ParseUint(s, 10, 64); err == nil {
		return n
	}
	return def
}
