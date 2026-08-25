package handlers

import (
	"crypto/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/middleware"
	"github.com/zhaochy1990/auth-service/internal/repository"
)

const (
	// smsCodeLifetime is how long a verification code stays valid (5 minutes).
	smsCodeLifetime = 5 * time.Minute
	// smsMaxAttempts is the failed-verify cap before a code is invalidated.
	smsMaxAttempts = 5
	// smsProviderID is the account provider id recorded for phone identities.
	smsProviderID = "sms"
)

// --- Request types ---

type smsSendRequest struct {
	Phone string `json:"phone"`
}

type smsVerifyRequest struct {
	Phone      string  `json:"phone"`
	Code       string  `json:"code"`
	InviteCode *string `json:"invite_code"`
}

// SendSmsCode sends a one-time verification code to a mainland-China phone
// number. The flow is login-or-register: verification later auto-creates the
// account on first use. Enforces the 60-second send cooldown and the 10-per-day
// cap per phone, and fails closed (503) when Redis is unavailable. In
// AUTH_SMS_TEST_MODE the fixed code 123456 is stored and the Tencent Cloud call
// is skipped.
//
// @Summary		Send an SMS verification code
// @Description	Sends a one-time SMS verification code to a mainland-China phone number (11 digits starting 1[3-9]). The response never contains the code.
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body		smsSendRequest	true	"Phone number"
// @Success		200		{object}	StatusResponse
// @Failure		400		{object}	ErrorResponse
// @Failure		429		{object}	ErrorResponse
// @Failure		503		{object}	ErrorResponse
// @Security		ClientID
// @Router			/api/auth/sms/send [post]
func (h *Handler) SendSmsCode(c *gin.Context) {
	var req smsSendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	phone, err := domain.ParsePhoneNumber(req.Phone)
	if err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid phone number"))
		return
	}
	ctx := c.Request.Context()

	if !h.Cfg.SMSTestMode && !h.SMSClient.Configured() {
		middleware.RespondError(c, apperror.SmsNotConfigured())
		return
	}

	// Reserve the cooldown before the upstream call so accidental double-taps
	// cannot double-send; a failed send releases it (ReleaseSend).
	reserved, err := h.SMSStore.ReserveCooldown(ctx, phone.String())
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if !reserved {
		middleware.RespondError(c, apperror.SmsSendCooldown())
		return
	}
	if err := h.SMSStore.ReserveDailyCount(ctx, phone.String()); err != nil {
		_ = h.SMSStore.ReleaseSend(ctx, phone.String())
		middleware.RespondError(c, err)
		return
	}

	code := "123456"
	if !h.Cfg.SMSTestMode {
		code = randomSixDigits()
		if err := h.SMSClient.SendCode(ctx, phone.String(), code); err != nil {
			_ = h.SMSStore.ReleaseSend(ctx, phone.String())
			middleware.RespondError(c, err)
			return
		}
	}
	if err := h.SMSStore.StoreCode(ctx, phone.String(), code, smsCodeLifetime); err != nil {
		_ = h.SMSStore.ReleaseSend(ctx, phone.String())
		middleware.RespondError(c, err)
		return
	}

	c.JSON(http.StatusOK, StatusResponse{Status: "ok"})
}

// VerifySmsCode validates a verification code and returns tokens. A verified
// phone logs the existing account in, or auto-registers it on first use
// (invite-gated when AUTH_REQUIRE_INVITE_CODE / STRIDE_REQUIRE_INVITE_CODE is
// on — the code is never required for existing users). On success a login
// record is appended and the response matches /api/auth/login.
//
// @Summary		Verify an SMS code and log in (or auto-register)
// @Description	Exchanges a phone + one-time SMS verification code for tokens. First successful verification auto-creates the account; later verifications log the existing account in.
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body		smsVerifyRequest	true	"Verification details"
// @Success		200		{object}	tokenResponse
// @Failure		400		{object}	ErrorResponse
// @Failure		403		{object}	ErrorResponse
// @Failure		409		{object}	ErrorResponse
// @Failure		500		{object}	ErrorResponse
// @Security		ClientID
// @Router			/api/auth/sms/verify [post]
func (h *Handler) VerifySmsCode(c *gin.Context) {
	var req smsVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid request body"))
		return
	}
	phone, err := domain.ParsePhoneNumber(req.Phone)
	if err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid phone number"))
		return
	}
	ctx := c.Request.Context()

	now := time.Now().UTC()
	user, err := h.Repo.Users().FindByPhone(ctx, phone.String())
	if err != nil {
		middleware.RespondError(c, err)
		return
	}

	// For a brand-new phone, the invite gate is checked BEFORE the code is
	// verified (and consumed): a gate rejection must not burn the one-time
	// code. Existing users are never asked for an invite.
	var inviteRecord *domain.InviteCode
	if user == nil {
		inviteRecord, err = h.resolveInviteGate(ctx, req.InviteCode)
		if err != nil {
			middleware.RespondError(c, err)
			return
		}
	}

	result, err := h.SMSStore.VerifyCode(ctx, phone.String(), req.Code, smsMaxAttempts)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	switch result {
	case repository.SmsVerifyInvalid:
		middleware.RespondError(c, apperror.SmsCodeInvalid())
		return
	case repository.SmsVerifyExpired:
		middleware.RespondError(c, apperror.SmsCodeExpired())
		return
	case repository.SmsVerifyAttemptsExceeded:
		middleware.RespondError(c, apperror.SmsAttemptsExceeded())
		return
	}

	var (
		userID     string
		role       string
		name       *string
		membership domain.MembershipTier
		userType   domain.UserType
	)

	if user == nil {
		// First successful verification auto-registers (login-or-register).
		userID = uuid.NewString()
		membership, membershipExpires, invitedWith, userType := registrationGrants(inviteRecord, now)

		// Claim a single-use invite code first (ETag-atomic) so a race leaves
		// no orphan rows.
		if inviteRecord != nil && inviteRecord.Kind == domain.InviteSingleUse {
			if err := h.Repo.InviteCodes().MarkUsed(ctx, inviteRecord.Code, userID); err != nil {
				middleware.RespondError(c, err)
				return
			}
		}

		newUser := &domain.User{
			ID:                  userID,
			Phone:               strPtr(phone.String()),
			EmailVerified:       false,
			Role:                "user",
			UserType:            userType,
			IsActive:            true,
			CustomAttributes:    map[string]any{},
			CreatedAt:           now,
			UpdatedAt:           now,
			InviteCode:          invitedWith,
			Membership:          membership,
			MembershipExpiresAt: membershipExpires,
		}
		if err := h.Repo.Users().Insert(ctx, newUser); err != nil {
			middleware.RespondError(c, err)
			return
		}
		account := &domain.Account{
			ID:                uuid.NewString(),
			UserID:            userID,
			ProviderID:        smsProviderID,
			ProviderAccountID: strPtr(phone.String()),
			ProviderMetadata:  "{}",
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := h.Repo.Accounts().Insert(ctx, account); err != nil {
			_ = h.Repo.Accounts().DeleteByID(ctx, account.ID)
			_ = h.Repo.Users().DeleteByID(ctx, userID)
			middleware.RespondError(c, err)
			return
		}
		role = "user"
	} else {
		if !user.IsActive {
			middleware.RespondError(c, apperror.UserDisabled())
			return
		}
		membership = h.resolveMembership(ctx, user)
		userID, role, name = user.ID, user.Role, user.Name
		userType = domain.UserTypeFromString(string(user.UserType))
	}

	_ = h.Repo.Users().RecordLogin(ctx, userID, middleware.ClientIP(c, "unknown"))

	scopes := middleware.AllowedScopes(c)
	accessToken, err := h.JWT.IssueAccessToken(userID, middleware.ClientID(c), scopes, role, membership, userType, name)
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

// randomSixDigits returns a cryptographically random 6-digit decimal string.
func randomSixDigits() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	digits := make([]byte, 6)
	for i, v := range b {
		digits[i] = '0' + v%10
	}
	return string(digits)
}
