package handlers

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/middleware"
	"github.com/zhaochy1990/auth-service/internal/repository"
	"github.com/zhaochy1990/auth-service/internal/wechat"
)

// grantWeChatPhoneBind is the wechat_phone_bind grant: a WeChat identity with
// no account (needs_binding) is bound to the account a phone number identifies
// — registering a 手机号账号 when nobody holds that number yet (ADR 0011). The
// verification code is the authorisation: it proves ownership of the number,
// so the number's account is the binding target. This is not account merging —
// nothing moves between accounts.
const grantWeChatPhoneBind = "wechat_phone_bind"

// handleWeChatPhoneBind implements the wechat_phone_bind grant. The request
// carries subject_token (the wx.login() code, subject_token_type
// wechat_mini_program) plus phone + code (a bind_phone-scene SMS verification
// code). Order matters: the format checks and the WeChat-identity dedup run
// before the one-time code is consumed, so a doomed request never burns it;
// the account lookup and invite gate come next (ADR 0006 — the gate rejection
// happens before the code is consumed too); then the code, then registration
// via the shared registerPhoneUser, then the WeChat bind checks that need the
// target account, the link write, and the standard token response. When this
// grant auto-registers, the response carries registered: true — the client's
// signal to enter post-registration onboarding once.
//
// The grant accepts no invite_code parameter: when the registration gate is
// on, a brand-new phone is rejected (invite_code is required), and closing
// the gate is a deployment configuration, not a per-request choice.
func (h *Handler) handleWeChatPhoneBind(c *gin.Context, req *tokenRequest) {
	ctx := c.Request.Context()
	if req.SubjectToken == nil || *req.SubjectToken == "" {
		middleware.RespondError(c, apperror.BadRequest("Missing 'subject_token' parameter"))
		return
	}
	if req.SubjectTokenType == nil {
		middleware.RespondError(c, apperror.BadRequest("Missing 'subject_token_type' parameter"))
		return
	}
	if *req.SubjectTokenType != wechatSubjectTokenType {
		middleware.RespondError(c, apperror.BadRequest("Unsupported subject_token_type: "+*req.SubjectTokenType))
		return
	}

	// The phone+code proof and the email+password proof are two different bind
	// flows on two different grants; a request carrying both is malformed.
	if req.Email != nil || (req.Password != nil && *req.Password != "") {
		middleware.RespondError(c, apperror.BadRequest("The wechat_phone_bind grant does not accept 'email' or 'password'"))
		return
	}
	if req.Phone == nil || *req.Phone == "" {
		middleware.RespondError(c, apperror.BadRequest("Missing 'phone' parameter"))
		return
	}
	if req.Code == nil || *req.Code == "" {
		middleware.RespondError(c, apperror.BadRequest("Missing 'code' parameter"))
		return
	}
	phone, err := domain.ParsePhoneNumber(*req.Phone)
	if err != nil {
		middleware.RespondError(c, apperror.BadRequest("Invalid phone number"))
		return
	}

	app, err := h.resolveExchangeApp(c, req)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	wechatCfg, err := h.resolveWeChatProviderConfig(ctx, app)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	client := wechat.NewClient(wechatCfg.AppID, wechatCfg.Secret, h.Cfg.WeChatCode2SessionURL)
	session, err := client.Code2Session(ctx, *req.SubjectToken)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}

	user, err := h.Repo.Users().FindByPhone(ctx, phone.String())
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if user != nil && !user.IsActive {
		middleware.RespondError(c, apperror.UserDisabled())
		return
	}

	// For a brand-new phone the invite gate is checked BEFORE the code is
	// consumed (ADR 0006), exactly like the SMS login-or-register flow.
	if user == nil {
		if _, err := h.resolveInviteGate(ctx, nil); err != nil {
			middleware.RespondError(c, err)
			return
		}
	}

	// The identity must not already belong to another account: openid within
	// this mini-program, and unionid as the cross-mini-program key. Checked
	// before the code is consumed so a conflict does not burn it. When the
	// identity already belongs to the phone's own account the grant is an
	// idempotent login.
	if err := h.ensureWeChatIdentityFree(ctx, wechatCfg.AppID, session, user); err != nil {
		middleware.RespondError(c, err)
		return
	}

	result, err := h.SMSStore.VerifyCode(ctx, repository.SmsSceneBindPhone, phone.String(), *req.Code, smsMaxAttempts)
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

	registered := false
	if user == nil {
		registeredUser, err := h.registerPhoneUser(ctx, phone.String(), nil, time.Now().UTC())
		if err != nil {
			middleware.RespondError(c, err)
			return
		}
		user, err = h.Repo.Users().FindByID(ctx, registeredUser)
		if err != nil {
			middleware.RespondError(c, err)
			return
		}
		registered = true
	}

	// An account already bound to a DIFFERENT WeChat identity in this
	// mini-program may not silently rebind; the rebind flow is not designed
	// yet. A fresh registration has no links, so this only guards logins.
	link, err := h.Repo.Users().FindWeChatLink(ctx, user.ID, wechatCfg.AppID)
	if err != nil {
		middleware.RespondError(c, err)
		return
	}
	if link != nil && link.OpenID != session.OpenID {
		middleware.RespondError(c, apperror.WeChatAlreadyBound())
		return
	}
	if link == nil {
		unionid := session.UnionID
		if unionid != nil && *unionid == "" {
			unionid = nil
		}
		if err := h.Repo.Users().LinkWeChat(ctx, user.ID, wechatCfg.AppID, session.OpenID, unionid); err != nil {
			middleware.RespondError(c, err)
			return
		}
		user.WeChatBound = true
	}

	h.respondTokenExchange(c, req, user, app, registered)
}

// ensureWeChatIdentityFree rejects the grant when the exchanged identity
// (openid within this mini-program, or the cross-mini-program unionid)
// already belongs to a different account than the one the phone identifies.
// target may be nil (brand-new phone); an identity bound to the target itself
// is fine — the grant then degrades to an idempotent login.
func (h *Handler) ensureWeChatIdentityFree(ctx context.Context, wechatAppID string, session *wechat.SessionResult, target *domain.User) error {
	existing, err := h.Repo.Users().FindByWeChatOpenID(ctx, wechatAppID, session.OpenID)
	if err != nil {
		return err
	}
	if existing != nil && (target == nil || existing.ID != target.ID) {
		return apperror.WeChatAlreadyBound()
	}
	if session.UnionID != nil && *session.UnionID != "" {
		existing, err = h.Repo.Users().FindByWeChatUnionID(ctx, *session.UnionID)
		if err != nil {
			return err
		}
		if existing != nil && (target == nil || existing.ID != target.ID) {
			return apperror.WeChatAlreadyBound()
		}
	}
	return nil
}
