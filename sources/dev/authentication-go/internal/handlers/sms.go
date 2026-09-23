package handlers

import (
	"context"
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
	// Scene selects which flow the code is for (login / bind_phone /
	// reset_password). A code can only be consumed by the scene it was sent
	// for (ADR 0010); omitted defaults to login for clients predating scenes.
	Scene string `json:"scene"`
	// LoginOnly restricts the send to already-registered phones (the web login
	// form). Omitted (false) keeps the login-or-register behavior for clients
	// that still auto-create the account on first verification. Only valid
	// with the login scene.
	LoginOnly bool `json:"login_only"`
}

type smsVerifyRequest struct {
	Phone      string  `json:"phone"`
	Code       string  `json:"code"`
	InviteCode *string `json:"invite_code"`
}

// SendSmsCode sends a one-time verification code to a mainland-China phone
// number. The flow is login-or-register: verification later auto-creates the
// account on first use. With login_only the phone must already be bound to a
// user, otherwise the request is rejected with phone_not_registered and no SMS
// is sent (the web login form uses this so an unregistered phone is guided to
// registration). Enforces the per-phone send caps — at most
// sms_send_window_max sends per 60-second window (default 5) and sms_daily_max
// per day (default 20) — and fails closed (503) when Redis is unavailable. In
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
// @Failure		404		{object}	ErrorResponse
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
	scene, ok := repository.SmsSceneFromRequest(req.Scene)
	if !ok {
		middleware.RespondError(c, apperror.BadRequest("Invalid scene: unknown verification-code scene"))
		return
	}
	// reset_password is a reserved scene (ADR 0010) with no consuming endpoint
	// yet — refusing the send keeps anyone from burning SMS budget on codes
	// that can never be used. Remove this guard when the 找回密码 endpoint
	// lands.
	if scene == repository.SmsSceneResetPassword {
		middleware.RespondError(c, apperror.BadRequest("reset_password codes cannot be sent yet"))
		return
	}
	if req.LoginOnly && scene != repository.SmsSceneLogin {
		middleware.RespondError(c, apperror.BadRequest("login_only is only valid with the login scene"))
		return
	}
	ctx := c.Request.Context()

	// login_only: reject phones with no bound user BEFORE reserving the cooldown
	// or calling the SMS provider, so a failed login attempt sends nothing.
	if req.LoginOnly {
		user, err := h.Repo.Users().FindByPhone(ctx, phone.String())
		if err != nil {
			middleware.RespondError(c, err)
			return
		}
		if user == nil {
			middleware.RespondError(c, apperror.PhoneNotRegistered())
			return
		}
	}

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
		// The over-limit call leaves the daily counter untouched, so there is
		// nothing to give back: releasing here would refund quota the phone
		// never spent, and alternating attempts would dodge the cap entirely.
		// The reserved cooldown slot simply expires with its window.
		middleware.RespondError(c, err)
		return
	}

	code := "123456"
	if !h.Cfg.SMSTestMode {
		code = randomSixDigits()
		// NOTE(ADR 0010): per-scene Tencent templates (one per scene, so the
		// message the user reads agrees with the action it authorises) are a
		// planned follow-up — the bind_phone / reset_password templates are
		// still awaiting approval, so every scene currently sends the login
		// template. Key isolation is already enforced at the store level.
		if err := h.SMSClient.SendCode(ctx, phone.String(), code); err != nil {
			_ = h.SMSStore.ReleaseSend(ctx, phone.String())
			middleware.RespondError(c, err)
			return
		}
	}
	if err := h.SMSStore.StoreCode(ctx, scene, phone.String(), code, smsCodeLifetime); err != nil {
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

	result, err := h.SMSStore.VerifyCode(ctx, repository.SmsSceneLogin, phone.String(), req.Code, smsMaxAttempts)
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
		userID, err = h.registerPhoneUser(ctx, phone.String(), inviteRecord, now)
		if err != nil {
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

// registerPhoneUser creates the 手机号账号 for a first-time phone: the user row
// plus its sms provider account row, claimed invite code first (ETag-atomic,
// so a race leaves no orphan rows) and compensating deletes when the second
// insert fails. Shared by the SMS login-or-register flow and the
// wechat_phone_bind grant so the two auto-registrations cannot drift.
func (h *Handler) registerPhoneUser(ctx context.Context, phone string, inviteRecord *domain.InviteCode, now time.Time) (string, error) {
	userID := uuid.NewString()
	membership, membershipExpires, invitedWith, userType := registrationGrants(inviteRecord, now)

	if inviteRecord != nil && inviteRecord.Kind == domain.InviteSingleUse {
		if err := h.Repo.InviteCodes().MarkUsed(ctx, inviteRecord.Code, userID); err != nil {
			return "", err
		}
	}

	newUser := &domain.User{
		ID:                  userID,
		Phone:               strPtr(phone),
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
		// A concurrent auto-registration of the same phone (SMS verify vs the
		// wechat_phone_bind grant) can win the users.phone unique index race.
		// The winner's account is a perfectly good target: adopt it instead of
		// surfacing a 500.
		if winner, findErr := h.Repo.Users().FindByPhone(ctx, phone); findErr == nil && winner != nil {
			return winner.ID, nil
		}
		return "", err
	}
	account := &domain.Account{
		ID:                uuid.NewString(),
		UserID:            userID,
		ProviderID:        smsProviderID,
		ProviderAccountID: strPtr(phone),
		ProviderMetadata:  "{}",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := h.Repo.Accounts().Insert(ctx, account); err != nil {
		_ = h.Repo.Accounts().DeleteByID(ctx, account.ID) // compensate
		_ = h.Repo.Users().DeleteByID(ctx, userID)        // compensate
		if winner, findErr := h.Repo.Users().FindByPhone(ctx, phone); findErr == nil && winner != nil {
			return winner.ID, nil
		}
		return "", err
	}
	return userID, nil
}

// phoneBindRequest binds a verified phone number to the current user.
type phoneBindRequest struct {
	Phone string `json:"phone"`
	Code  string `json:"code"`
}

// phoneBindError maps an accounts-row duplicate (the same sms identity raced
// onto another user) to the phone-specific conflict code, so the client
// surface stays phone-shaped instead of leaking account_linking vocabulary.
func phoneBindError(err error) *apperror.Error {
	ae, ok := apperror.As(err)
	if ok && ae.Type == "account_already_linked" {
		return apperror.PhoneAlreadyBound()
	}
	return ae
}

// BindPhone verifies a one-time SMS code and binds (or rebinds) the phone to
// the authenticated user. The code is issued by POST /api/auth/sms/send with
// scene=bind_phone (ADR 0010) and consumed under the same scene key; a login
// code can never drive a binding. A phone already held by a
// different account is rejected with phone_already_bound — there is no account
// merge. Rebinding replaces the user's current phone after the new phone's
// code is verified (the old phone is not re-verified, matching the "verify the
// new identifier" convention). Binding the phone the user already owns is an
// idempotent no-op. Writes users.phone and the sms account row together (with
// compensating deletes/reverts on a mid-flow failure) so the phone becomes a
// usable login method immediately.
//
// @Summary		Bind (or rebind) the current user's phone
// @Description	Verifies a phone + one-time SMS code, then binds the phone to the authenticated user. If the user already has a phone it is replaced (rebind); binding the same phone is a no-op.
// @Tags			users
// @Accept			json
// @Produce		json
// @Param			body	body		phoneBindRequest	true	"Phone and verification code"
// @Success		200		{object}	StatusResponse
// @Failure		400		{object}	ErrorResponse
// @Failure		401		{object}	ErrorResponse
// @Failure		409		{object}	ErrorResponse
// @Failure		500		{object}	ErrorResponse
// @Security		BearerAuth
// @Router			/api/users/me/phone [post]
func (h *Handler) BindPhone(c *gin.Context) {
	var req phoneBindRequest
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
	userID := middleware.UserID(c)

	user, err := h.Repo.Users().FindByID(ctx, userID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		middleware.RespondError(c, apperror.UserNotFound())
		return
	}

	// A phone held by a different account can never be bound (no merge), and
	// the user's own phone is already bound — both rejected before the code is
	// consumed so a doomed attempt does not burn a one-time code.
	holder, err := h.Repo.Users().FindByPhone(ctx, phone.String())
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if holder != nil && holder.ID != userID {
		middleware.RespondError(c, apperror.PhoneAlreadyBound())
		return
	}
	if holder != nil && holder.ID == userID {
		c.JSON(http.StatusOK, StatusResponse{Status: "ok"})
		return
	}

	// The code must have been sent for the bind_phone scene (ADR 0010): a
	// login code can never drive a binding.
	result, err := h.SMSStore.VerifyCode(ctx, repository.SmsSceneBindPhone, phone.String(), req.Code, smsMaxAttempts)
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

	now := time.Now().UTC()
	phoneStr := phone.String()

	// First bind inserts the sms account then stamps users.phone; rebind
	// repoints the existing sms account then replaces users.phone. Either way
	// the account row and the phone column stay consistent, and the
	// (provider_id, provider_account_id) unique index is the final guard
	// against a concurrent bind of the same phone by another user.
	existingAccount, err := h.Repo.Accounts().FindByUserAndProvider(ctx, userID, smsProviderID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}

	if existingAccount == nil {
		account := &domain.Account{
			ID:                uuid.NewString(),
			UserID:            userID,
			ProviderID:        smsProviderID,
			ProviderAccountID: strPtr(phoneStr),
			ProviderMetadata:  "{}",
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := h.Repo.Accounts().Insert(ctx, account); err != nil {
			middleware.RespondError(c, phoneBindError(err))
			return
		}
		user.Phone = &phoneStr
		user.UpdatedAt = now
		if err := h.Repo.Users().Update(ctx, user); err != nil {
			_ = h.Repo.Accounts().DeleteByID(ctx, account.ID) // compensate
			middleware.RespondError(c, err)
			return
		}
	} else {
		oldPhone := existingAccount.ProviderAccountID
		existingAccount.ProviderAccountID = &phoneStr
		existingAccount.UpdatedAt = now
		if err := h.Repo.Accounts().Update(ctx, existingAccount); err != nil {
			middleware.RespondError(c, phoneBindError(err))
			return
		}
		user.Phone = &phoneStr
		user.UpdatedAt = now
		if err := h.Repo.Users().Update(ctx, user); err != nil {
			existingAccount.ProviderAccountID = oldPhone // compensate
			_ = h.Repo.Accounts().Update(ctx, existingAccount)
			middleware.RespondError(c, err)
			return
		}
	}

	c.JSON(http.StatusOK, StatusResponse{Status: "ok"})
}

// UnbindPhone removes the phone from the authenticated user. It refuses when
// the sms account is the user's last remaining login method (mirrors
// UnlinkAccount's last-account guard), so a phone-only account can never be
// stranded. No re-verification is required, matching the existing unlink
// surface. The sms account row is deleted and users.phone cleared together,
// with a compensating re-insert if the phone clear fails.
//
// @Summary		Unbind the current user's phone
// @Description	Removes the phone from the authenticated user. Refused when the phone is the user's last remaining login method.
// @Tags			users
// @Produce		json
// @Success		200		{object}	StatusResponse
// @Failure		401		{object}	ErrorResponse
// @Failure		404		{object}	ErrorResponse
// @Failure		409		{object}	ErrorResponse
// @Failure		500		{object}	ErrorResponse
// @Security		BearerAuth
// @Router			/api/users/me/phone [delete]
func (h *Handler) UnbindPhone(c *gin.Context) {
	ctx := c.Request.Context()
	userID := middleware.UserID(c)

	user, err := h.Repo.Users().FindByID(ctx, userID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user == nil {
		middleware.RespondError(c, apperror.UserNotFound())
		return
	}
	if user.Phone == nil {
		middleware.RespondError(c, apperror.PhoneNotBound())
		return
	}

	account, err := h.Repo.Accounts().FindByUserAndProvider(ctx, userID, smsProviderID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}

	if account != nil {
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
	}

	now := time.Now().UTC()
	user.Phone = nil
	user.UpdatedAt = now
	if err := h.Repo.Users().Update(ctx, user); err != nil {
		if account != nil {
			_ = h.Repo.Accounts().Insert(ctx, account) // compensate
		}
		middleware.RespondError(c, err)
		return
	}

	c.JSON(http.StatusOK, StatusResponse{Status: "ok"})
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
