// Package middleware holds the Gin middleware that ports the Axum extractors:
// bearer-token user auth, X-Client-Id app resolution, Basic-auth client auth,
// and admin-role gating — plus the per-IP rate limiter, CORS, and the shared
// error responder. Handlers read the values these middlewares stash on the
// gin.Context via the typed getters below.
package middleware

import (
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/repository"
)

// Context keys.
const (
	ctxUserID        = "auth.user_id"
	ctxClientID      = "auth.client_id"
	ctxScopes        = "auth.scopes"
	ctxAppID         = "auth.app_id"
	ctxAllowedScopes = "auth.allowed_scopes"
)

// RespondError writes a typed application error as a JSON response and aborts.
func RespondError(c *gin.Context, err error) {
	ae, _ := apperror.As(err)
	c.AbortWithStatusJSON(ae.Status, gin.H{"error": ae.Type, "message": ae.Message})
}

// --- Context getters ---

func UserID(c *gin.Context) string          { return getString(c, ctxUserID) }
func ClientID(c *gin.Context) string        { return getString(c, ctxClientID) }
func AppID(c *gin.Context) string           { return getString(c, ctxAppID) }
func Scopes(c *gin.Context) []string        { return getStrings(c, ctxScopes) }
func AllowedScopes(c *gin.Context) []string { return getStrings(c, ctxAllowedScopes) }

func getString(c *gin.Context, key string) string {
	if v, ok := c.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getStrings(c *gin.Context, key string) []string {
	if v, ok := c.Get(key); ok {
		if s, ok := v.([]string); ok {
			return s
		}
	}
	return nil
}

// ClientIP extracts the originating client IP from forwarding headers, using
// fallback when none is present.
func ClientIP(c *gin.Context, fallback string) string {
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if xr := c.GetHeader("X-Real-IP"); xr != "" {
		return xr
	}
	return fallback
}

func bearer(c *gin.Context) (string, bool) {
	h := c.GetHeader("Authorization")
	return strings.CutPrefix(h, "Bearer ")
}

// Auth bundles the dependencies the auth middlewares need.
type Auth struct {
	Repo repository.Repository
	JWT  *auth.JWTManager
}

// AuthenticatedUser validates a Bearer token and loads the active user.
func (a *Auth) AuthenticatedUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearer(c)
		if !ok {
			RespondError(c, apperror.Unauthorized())
			return
		}
		claims, err := a.JWT.VerifyAccessToken(token)
		if err != nil {
			RespondError(c, err)
			return
		}
		user, err := a.Repo.Users().FindByID(c.Request.Context(), claims.Sub)
		if err != nil {
			RespondError(c, err)
			return
		}
		if user == nil {
			RespondError(c, apperror.Unauthorized())
			return
		}
		if !user.IsActive {
			RespondError(c, apperror.UserDisabled())
			return
		}
		c.Set(ctxUserID, claims.Sub)
		c.Set(ctxClientID, claims.Aud)
		c.Set(ctxScopes, claims.Scopes)
		c.Next()
	}
}

// ClientApp resolves the active application from the X-Client-Id header.
func (a *Auth) ClientApp() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientID := c.GetHeader("X-Client-Id")
		if clientID == "" {
			RespondError(c, apperror.MissingClientID())
			return
		}
		app, err := a.Repo.Applications().FindByClientID(c.Request.Context(), clientID)
		if err != nil {
			RespondError(c, err)
			return
		}
		if app == nil {
			RespondError(c, apperror.ApplicationNotFound())
			return
		}
		if !app.IsActive {
			RespondError(c, apperror.ApplicationNotActive())
			return
		}
		c.Set(ctxAppID, app.ID)
		c.Set(ctxClientID, app.ClientID)
		c.Set(ctxAllowedScopes, auth.DecodeStringArray(app.AllowedScopes))
		c.Next()
	}
}

// AuthenticatedApp authenticates a client application via Basic auth.
func (a *Auth) AuthenticatedApp() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		encoded, ok := strings.CutPrefix(header, "Basic ")
		if !ok {
			RespondError(c, apperror.InvalidCredentials())
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			RespondError(c, apperror.InvalidCredentials())
			return
		}
		clientID, secret, ok := strings.Cut(string(decoded), ":")
		if !ok {
			RespondError(c, apperror.InvalidCredentials())
			return
		}
		app, err := a.Repo.Applications().FindByClientID(c.Request.Context(), clientID)
		if err != nil {
			RespondError(c, err)
			return
		}
		if app == nil {
			RespondError(c, apperror.ApplicationNotFound())
			return
		}
		if !app.IsActive {
			RespondError(c, apperror.ApplicationNotActive())
			return
		}
		valid, err := auth.VerifyClientSecret(secret, app.ClientSecretHash)
		if err != nil {
			RespondError(c, err)
			return
		}
		if !valid {
			RespondError(c, apperror.InvalidCredentials())
			return
		}
		c.Set(ctxAppID, app.ID)
		c.Set(ctxClientID, app.ClientID)
		c.Next()
	}
}

// AdminAuth requires an active admin user with a Bearer token carrying the
// admin role.
func (a *Auth) AdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearer(c)
		if !ok {
			RespondError(c, apperror.Unauthorized())
			return
		}
		claims, err := a.JWT.VerifyAccessToken(token)
		if err != nil {
			RespondError(c, err)
			return
		}
		if claims.Role != "admin" {
			RespondError(c, apperror.Forbidden())
			return
		}
		user, err := a.Repo.Users().FindByID(c.Request.Context(), claims.Sub)
		if err != nil {
			RespondError(c, err)
			return
		}
		if user == nil {
			RespondError(c, apperror.Unauthorized())
			return
		}
		if !user.IsActive {
			RespondError(c, apperror.UserDisabled())
			return
		}
		if user.Role != "admin" {
			RespondError(c, apperror.Forbidden())
			return
		}
		c.Set(ctxUserID, claims.Sub)
		c.Next()
	}
}

// --- Rate limiter (per-key sliding window) ---

// RateLimiter is a per-key sliding-window rate limiter.
type RateLimiter struct {
	mu          sync.Mutex
	buckets     map[string][]time.Time
	lastCleanup time.Time
	max         int
	window      time.Duration
}

// NewRateLimiter builds a limiter allowing max requests per window.
func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	return &RateLimiter{buckets: make(map[string][]time.Time), lastCleanup: time.Now(), max: max, window: window}
}

func (l *RateLimiter) check(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()

	if now.Sub(l.lastCleanup) > 60*time.Second {
		for k, ts := range l.buckets {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) >= l.window {
				delete(l.buckets, k)
			}
		}
		l.lastCleanup = now
	}

	ts := l.buckets[key]
	kept := ts[:0]
	for _, t := range ts {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.buckets[key] = kept
		return false
	}
	kept = append(kept, now)
	l.buckets[key] = kept
	return true
}

// Middleware rate-limits by client IP.
func (l *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := ClientIP(c, "global")
		if !l.check(key) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":   "rate_limited",
				"message": "Too many requests. Please try again later.",
			})
			return
		}
		c.Next()
	}
}

// --- CORS ---

// CORS mirrors the tower-http CorsLayer: echo allowed origins (or "*"), allow
// any method/header, and short-circuit preflight requests.
func CORS(allowedOrigins string) gin.HandlerFunc {
	wildcard := strings.TrimSpace(allowedOrigins) == "*"
	set := map[string]bool{}
	if !wildcard {
		for _, o := range strings.Split(allowedOrigins, ",") {
			if t := strings.TrimSpace(o); t != "" {
				set[t] = true
			}
		}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		switch {
		case wildcard:
			c.Header("Access-Control-Allow-Origin", "*")
		case origin != "" && set[origin]:
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "*")
		c.Header("Access-Control-Allow-Headers", "*")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
