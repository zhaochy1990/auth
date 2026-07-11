// Package server wires the Gin engine: route groups, per-group rate limiters,
// CORS, and the auth middlewares. The /api/users and /api/teams groups
// intentionally share one rate-limiter instance.
package server

import (
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/config"
	"github.com/zhaochy1990/auth-service/internal/handlers"
	"github.com/zhaochy1990/auth-service/internal/middleware"
	"github.com/zhaochy1990/auth-service/internal/repository"
)

// NewRouter builds the fully wired Gin engine.
func NewRouter(repo repository.Repository, jwt *auth.JWTManager, cfg *config.Config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.CORS(cfg.CORSAllowedOrigins))

	h := handlers.New(repo, jwt, cfg)
	am := &middleware.Auth{Repo: repo, JWT: jwt}

	// Per-IP sliding-window rate limiters.
	authLimiter := middleware.NewRateLimiter(20, 60*time.Second)  // brute-force protection
	oauthLimiter := middleware.NewRateLimiter(30, 60*time.Second) // OAuth2
	userLimiter := middleware.NewRateLimiter(60, 60*time.Second)  // shared by /api/users + /api/teams
	adminLimiter := middleware.NewRateLimiter(60, 60*time.Second) // admin

	r.GET("/health", func(c *gin.Context) {
		version := os.Getenv("APP_VERSION")
		if version == "" {
			version = "dev"
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "version": version})
	})

	// OAuth2 endpoints (Basic-auth client).
	oauth := r.Group("/oauth")
	oauth.Use(oauthLimiter.Middleware(), am.AuthenticatedApp())
	{
		oauth.POST("/token", h.Token)
		oauth.POST("/revoke", h.Revoke)
		oauth.POST("/introspect", h.Introspect)
	}

	// Auth endpoints (X-Client-Id, except logout which is Bearer).
	authGroup := r.Group("/api/auth")
	authGroup.Use(authLimiter.Middleware())
	{
		authGroup.POST("/register", am.ClientApp(), h.Register)
		authGroup.POST("/login", am.ClientApp(), h.Login)
		authGroup.POST("/provider/:provider_id/login", am.ClientApp(), h.ProviderLogin)
		authGroup.POST("/refresh", am.ClientApp(), h.Refresh)
		authGroup.POST("/logout", am.AuthenticatedUser(), h.Logout)
	}

	// User endpoints (Bearer).
	users := r.Group("/api/users")
	users.Use(userLimiter.Middleware(), am.AuthenticatedUser())
	{
		users.GET("/me", h.GetProfile)
		users.PATCH("/me", h.UpdateProfile)
		users.DELETE("/me", h.DeleteMe)
		users.GET("/me/accounts", h.ListAccounts)
		users.POST("/me/accounts/:provider_id/link", h.LinkAccount)
		users.DELETE("/me/accounts/:provider_id", h.UnlinkAccount)
		users.GET("/me/teams", h.ListMyTeams)
	}

	// Team endpoints (Bearer; shares the user limiter instance).
	teams := r.Group("/api/teams")
	teams.Use(userLimiter.Middleware(), am.AuthenticatedUser())
	{
		teams.POST("", h.CreateTeam)
		teams.GET("", h.ListTeams)
		teams.GET("/:team_id", h.GetTeam)
		teams.DELETE("/:team_id", h.DeleteTeam)
		teams.POST("/:team_id/join", h.JoinTeam)
		teams.POST("/:team_id/leave", h.LeaveTeam)
		teams.POST("/:team_id/transfer-owner", h.TransferOwner)
		teams.GET("/:team_id/members", h.ListMembers)
	}

	// Admin endpoints (Bearer with admin role).
	admin := r.Group("/admin")
	admin.Use(adminLimiter.Middleware(), am.AdminAuth())
	{
		admin.POST("/applications", h.CreateApplication)
		admin.GET("/applications", h.ListApplications)
		admin.PATCH("/applications/:id", h.UpdateApplication)
		admin.GET("/applications/:id/providers", h.ListProviders)
		admin.POST("/applications/:id/providers", h.AddProvider)
		admin.DELETE("/applications/:id/providers/:provider_id", h.RemoveProvider)
		admin.POST("/applications/:id/rotate-secret", h.RotateSecret)
		admin.GET("/users", h.ListUsers)
		admin.POST("/users", h.CreateUser)
		admin.GET("/users/:id", h.GetUser)
		admin.PATCH("/users/:id", h.UpdateUser)
		admin.DELETE("/users/:id", h.DeleteUser)
		admin.GET("/users/:id/accounts", h.GetUserAccounts)
		admin.DELETE("/users/:id/accounts/:provider_id", h.AdminUnlinkAccount)
		admin.POST("/users/:id/reset-password", h.ResetUserPassword)
		admin.GET("/stats", h.Stats)
		admin.GET("/invite-codes", h.ListInviteCodes)
		admin.POST("/invite-codes", h.CreateInviteCode)
		admin.DELETE("/invite-codes/:code", h.RevokeInviteCode)
		admin.POST("/teams", h.AdminCreateTeam)
		admin.POST("/teams/:id/members", h.AdminAddTeamMember)
		admin.DELETE("/teams/:id/members/:user_id", h.AdminRemoveTeamMember)
	}

	return r
}
