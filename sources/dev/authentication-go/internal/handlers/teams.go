package handlers

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/middleware"
)

// --- Request / Response types ---

type createTeamRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type transferOwnerRequest struct {
	NewOwnerUserID string `json:"new_owner_user_id"`
}

type teamResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	OwnerUserID string  `json:"owner_user_id"`
	IsOpen      bool    `json:"is_open"`
	MemberCount uint64  `json:"member_count"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type teamMembershipResponse struct {
	TeamID   string `json:"team_id"`
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	JoinedAt string `json:"joined_at"`
}

type teamMemberInfo struct {
	UserID   string  `json:"user_id"`
	Name     *string `json:"name"`
	Email    *string `json:"email"`
	Role     string  `json:"role"`
	JoinedAt string  `json:"joined_at"`
}

type myTeamEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	JoinedAt string `json:"joined_at"`
}

func toTeamResponse(t *domain.Team, count uint64) teamResponse {
	return teamResponse{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		OwnerUserID: t.OwnerUserID,
		IsOpen:      t.IsOpen,
		MemberCount: count,
		CreatedAt:   displayDT(t.CreatedAt),
		UpdatedAt:   displayDT(t.UpdatedAt),
	}
}

// --- Handlers ---

// CreateTeam creates a team owned by the authenticated user.
func (h *Handler) CreateTeam(c *gin.Context) {
	var req createTeamRequest
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
	userID := middleware.UserID(c)
	now := time.Now().UTC()
	team := &domain.Team{
		ID:          uuid.NewString(),
		Name:        name,
		Description: req.Description,
		OwnerUserID: userID,
		IsOpen:      true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := h.Repo.Teams().Insert(ctx, team); err != nil {
		middleware.RespondError(c, err)
		return
	}
	if err := h.Repo.TeamMemberships().Insert(ctx, &domain.TeamMembership{
		TeamID: team.ID, UserID: userID, Role: "owner", JoinedAt: now,
	}); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, toTeamResponse(team, 1))
}

// ListTeams lists all open teams.
func (h *Handler) ListTeams(c *gin.Context) {
	ctx := c.Request.Context()
	teams, err := h.Repo.Teams().FindAllOpen(ctx)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	out := make([]teamResponse, 0, len(teams))
	for i := range teams {
		count, err := h.Repo.TeamMemberships().CountByTeam(ctx, teams[i].ID)
		if err != nil {
			middleware.RespondError(c, err)
			return
		}
		out = append(out, toTeamResponse(&teams[i], count))
	}
	c.JSON(http.StatusOK, gin.H{"teams": out})
}

// GetTeam returns a single team.
func (h *Handler) GetTeam(c *gin.Context) {
	ctx := c.Request.Context()
	team, err := h.Repo.Teams().FindByID(ctx, c.Param("team_id"))
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if team == nil {
		middleware.RespondError(c, apperror.TeamNotFound())
		return
	}
	count, err := h.Repo.TeamMemberships().CountByTeam(ctx, team.ID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, toTeamResponse(team, count))
}

// JoinTeam adds the authenticated user to an open team (idempotent).
func (h *Handler) JoinTeam(c *gin.Context) {
	ctx := c.Request.Context()
	teamID := c.Param("team_id")
	userID := middleware.UserID(c)

	team, err := h.Repo.Teams().FindByID(ctx, teamID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if team == nil {
		middleware.RespondError(c, apperror.TeamNotFound())
		return
	}
	if !team.IsOpen {
		middleware.RespondError(c, apperror.TeamNotOpen())
		return
	}
	existing, err := h.Repo.TeamMemberships().Find(ctx, teamID, userID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if existing != nil {
		c.JSON(http.StatusOK, teamMembershipResponse{
			TeamID: existing.TeamID, UserID: existing.UserID, Role: existing.Role, JoinedAt: displayDT(existing.JoinedAt),
		})
		return
	}
	now := time.Now().UTC()
	m := &domain.TeamMembership{TeamID: teamID, UserID: userID, Role: "member", JoinedAt: now}
	if err := h.Repo.TeamMemberships().Insert(ctx, m); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, teamMembershipResponse{
		TeamID: m.TeamID, UserID: m.UserID, Role: m.Role, JoinedAt: displayDT(m.JoinedAt),
	})
}

// LeaveTeam removes the authenticated user from a team.
func (h *Handler) LeaveTeam(c *gin.Context) {
	ctx := c.Request.Context()
	teamID := c.Param("team_id")
	userID := middleware.UserID(c)

	team, err := h.Repo.Teams().FindByID(ctx, teamID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if team == nil {
		middleware.RespondError(c, apperror.TeamNotFound())
		return
	}
	existing, err := h.Repo.TeamMemberships().Find(ctx, teamID, userID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if existing == nil {
		middleware.RespondError(c, apperror.BadRequest("Not a member of this team"))
		return
	}
	if team.OwnerUserID == userID {
		count, err := h.Repo.TeamMemberships().CountByTeam(ctx, teamID)
		if err != nil {
			middleware.RespondError(c, err)
			return
		}
		if count <= 1 {
			middleware.RespondError(c, apperror.OwnerCannotLeaveAsLastMember())
			return
		}
	}
	if err := h.Repo.TeamMemberships().Delete(ctx, teamID, userID); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "left"})
}

// TransferOwner transfers team ownership to another member.
func (h *Handler) TransferOwner(c *gin.Context) {
	var req transferOwnerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	ctx := c.Request.Context()
	teamID := c.Param("team_id")
	userID := middleware.UserID(c)

	team, err := h.Repo.Teams().FindByID(ctx, teamID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if team == nil {
		middleware.RespondError(c, apperror.TeamNotFound())
		return
	}
	if team.OwnerUserID != userID {
		middleware.RespondError(c, apperror.TeamOwnerRequired())
		return
	}
	newOwner, err := h.Repo.TeamMemberships().Find(ctx, teamID, req.NewOwnerUserID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if newOwner == nil {
		middleware.RespondError(c, apperror.TeamTransferTargetNotMember())
		return
	}
	if req.NewOwnerUserID == userID {
		count, err := h.Repo.TeamMemberships().CountByTeam(ctx, teamID)
		if err != nil {
			middleware.RespondError(c, err)
			return
		}
		c.JSON(http.StatusOK, toTeamResponse(team, count))
		return
	}
	currentOwner, err := h.Repo.TeamMemberships().Find(ctx, teamID, userID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if currentOwner == nil {
		middleware.RespondError(c, apperror.BadRequest("Current owner is not a member of this team"))
		return
	}
	currentOwner.Role = "member"
	newOwner.Role = "owner"
	if err := h.Repo.TeamMemberships().Insert(ctx, currentOwner); err != nil {
		middleware.RespondError(c, err)
		return
	}
	if err := h.Repo.TeamMemberships().Insert(ctx, newOwner); err != nil {
		middleware.RespondError(c, err)
		return
	}
	team.OwnerUserID = req.NewOwnerUserID
	team.UpdatedAt = time.Now().UTC()
	if err := h.Repo.Teams().Update(ctx, team); err != nil {
		middleware.RespondError(c, err)
		return
	}
	count, err := h.Repo.TeamMemberships().CountByTeam(ctx, teamID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, toTeamResponse(team, count))
}

// DeleteTeam deletes a team owned by the authenticated user.
func (h *Handler) DeleteTeam(c *gin.Context) {
	ctx := c.Request.Context()
	teamID := c.Param("team_id")
	team, err := h.Repo.Teams().FindByID(ctx, teamID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if team == nil {
		middleware.RespondError(c, apperror.TeamNotFound())
		return
	}
	if team.OwnerUserID != middleware.UserID(c) {
		middleware.RespondError(c, apperror.TeamOwnerRequired())
		return
	}
	if err := h.Repo.TeamMemberships().DeleteAllByTeam(ctx, teamID); err != nil {
		middleware.RespondError(c, err)
		return
	}
	if err := h.Repo.Teams().DeleteByID(ctx, teamID); err != nil {
		middleware.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// ListMembers lists a team's members with name/email.
func (h *Handler) ListMembers(c *gin.Context) {
	ctx := c.Request.Context()
	teamID := c.Param("team_id")
	team, err := h.Repo.Teams().FindByID(ctx, teamID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if team == nil {
		middleware.RespondError(c, apperror.TeamNotFound())
		return
	}
	memberships, err := h.Repo.TeamMemberships().FindAllByTeam(ctx, teamID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	out := make([]teamMemberInfo, 0, len(memberships))
	for _, m := range memberships {
		var name, email *string
		if u, err := h.Repo.Users().FindByID(ctx, m.UserID); err == nil && u != nil {
			name, email = u.Name, u.Email
		}
		out = append(out, teamMemberInfo{
			UserID: m.UserID, Name: name, Email: email, Role: m.Role, JoinedAt: displayDT(m.JoinedAt),
		})
	}
	c.JSON(http.StatusOK, gin.H{"members": out})
}

// ListMyTeams lists the teams the authenticated user belongs to.
func (h *Handler) ListMyTeams(c *gin.Context) {
	ctx := c.Request.Context()
	memberships, err := h.Repo.TeamMemberships().FindAllByUser(ctx, middleware.UserID(c))
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	out := make([]myTeamEntry, 0, len(memberships))
	for _, m := range memberships {
		if team, err := h.Repo.Teams().FindByID(ctx, m.TeamID); err == nil && team != nil {
			out = append(out, myTeamEntry{ID: team.ID, Name: team.Name, Role: m.Role, JoinedAt: displayDT(m.JoinedAt)})
		}
	}
	c.JSON(http.StatusOK, gin.H{"teams": out})
}
