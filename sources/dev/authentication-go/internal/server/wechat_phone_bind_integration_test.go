package server_test

// Integration tests for the wechat_phone_bind grant (ADR 0011) and the
// scene-scoped SMS codes it consumes (ADR 0010). Everything goes through the
// HTTP surface: the token endpoint and the SMS send/verify endpoints, with
// the fake WeChat jscode2session server mapping js_code → openid.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/zhaochy1990/auth-service/internal/repository"
)

// phoneBindGrant posts a wechat_phone_bind grant request as a public client
// (client_id in the body). extra merges into the form (phone, code, and any
// stray parameters a test wants to include).
func (ta *testApp) phoneBindGrant(t *testing.T, subjectToken string, extra url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{
		"grant_type":         {"wechat_phone_bind"},
		"client_id":          {ta.clientID},
		"subject_token":      {subjectToken},
		"subject_token_type": {"wechat_mini_program"},
	}
	for k, vs := range extra {
		for _, v := range vs {
			form.Add(k, v)
		}
	}
	return ta.doForm(http.MethodPost, "/oauth/token", form, nil)
}

// smsSendScene posts a scene-carrying SMS send.
func smsSendScene(t *testing.T, ta *testApp, phone string, scene repository.SmsScene) *httptest.ResponseRecorder {
	t.Helper()
	return ta.do(http.MethodPost, "/api/auth/sms/send", map[string]any{"phone": phone, "scene": string(scene)}, ta.clientHeaders())
}

type phoneBindTokenResp struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken *string `json:"refresh_token"`
	TokenType    string  `json:"token_type"`
	ExpiresIn    int64   `json:"expires_in"`
	Registered   bool    `json:"registered"`
}

// A brand-new phone + a fresh WeChat identity registers the 手机号账号, binds the
// identity to it, and answers with the standard token pair plus registered:
// true — the client's once-only onboarding signal.
func TestWeChatPhoneBindAutoRegister(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(100)

	if w := smsSendScene(t, ta, phone, repository.SmsSceneBindPhone); w.Code != http.StatusOK {
		t.Fatalf("send status = %d, want 200", w.Code)
	}

	w := ta.phoneBindGrant(t, "code-bindable", url.Values{"phone": {phone}, "code": {"123456"}})
	mustStatus(t, w, http.StatusOK)
	var resp phoneBindTokenResp
	decode(t, w, &resp)
	if resp.AccessToken == "" || resp.RefreshToken == nil || *resp.RefreshToken == "" || resp.TokenType != "Bearer" {
		t.Fatalf("unexpected token response: %+v", resp)
	}
	if !resp.Registered {
		t.Fatalf("expected registered=true on auto-register, got %+v", resp)
	}

	me := ta.do(http.MethodGet, "/api/users/me", nil, ta.bearer(resp.AccessToken))
	mustStatus(t, me, http.StatusOK)
	var prof struct {
		Phone       *string `json:"phone"`
		Email       *string `json:"email"`
		WeChatBound bool    `json:"wechat_bound"`
	}
	decode(t, me, &prof)
	if prof.Phone == nil || *prof.Phone != phone {
		t.Fatalf("profile phone = %v, want %q", prof.Phone, phone)
	}
	if prof.Email != nil {
		t.Fatalf("phone-only account should have no email, got %+v", prof)
	}
	if !prof.WeChatBound {
		t.Fatalf("expected wechat_bound=true after the grant, got %+v", prof)
	}
}

// An existing 手机号账号 logging in through the grant gets no registered field —
// it is a login (plus first-time WeChat bind), not a registration.
func TestWeChatPhoneBindLoginExistingPhoneUser(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(101)

	// Create the phone account through the regular SMS login-or-register flow.
	if w := smsSend(t, ta, phone); w.Code != http.StatusOK {
		t.Fatalf("send status = %d, want 200", w.Code)
	}
	smsVerify(t, ta, phone, "123456", nil)

	seedCode(t, ta, repository.SmsSceneBindPhone, phone, "123456")
	w := ta.phoneBindGrant(t, "code-bindable", url.Values{"phone": {phone}, "code": {"123456"}})
	mustStatus(t, w, http.StatusOK)
	var body map[string]any
	decode(t, w, &body)
	if body["access_token"] == "" {
		t.Fatalf("expected access_token, got %+v", body)
	}
	if _, present := body["registered"]; present {
		t.Fatalf("registered must be absent for an existing account, got %+v", body)
	}
}

// Re-running the grant for an account that already holds this exact identity
// is an idempotent login (no duplicate link, no registered flag).
func TestWeChatPhoneBindIdempotentLogin(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(102)

	seedCode(t, ta, repository.SmsSceneBindPhone, phone, "123456")
	first := ta.phoneBindGrant(t, "code-bindable", url.Values{"phone": {phone}, "code": {"123456"}})
	mustStatus(t, first, http.StatusOK)

	seedCode(t, ta, repository.SmsSceneBindPhone, phone, "123456")
	second := ta.phoneBindGrant(t, "code-bindable", url.Values{"phone": {phone}, "code": {"123456"}})
	mustStatus(t, second, http.StatusOK)
	var resp phoneBindTokenResp
	decode(t, second, &resp)
	if resp.Registered {
		t.Fatalf("re-login must not carry registered=true, got %+v", resp)
	}
}

// Wrong code → sms_code_invalid (and the code survives wrong attempts under
// the cap).
func TestWeChatPhoneBindWrongCode(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(103)
	seedCode(t, ta, repository.SmsSceneBindPhone, phone, "123456")

	w := ta.phoneBindGrant(t, "code-bindable", url.Values{"phone": {phone}, "code": {"000000"}})
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "sms_code_invalid" {
		t.Fatalf("error = %v, want sms_code_invalid", body["error"])
	}
}

// Five wrong attempts invalidate the code: sms_attempts_exceeded.
func TestWeChatPhoneBindAttemptCap(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(104)
	seedCode(t, ta, repository.SmsSceneBindPhone, phone, "123456")

	var body map[string]any
	for i := 0; i < 4; i++ {
		w := ta.phoneBindGrant(t, "code-bindable", url.Values{"phone": {phone}, "code": {"000000"}})
		mustStatus(t, w, http.StatusBadRequest)
		decode(t, w, &body)
		if body["error"] != "sms_code_invalid" {
			t.Fatalf("attempt %d: error = %v, want sms_code_invalid", i+1, body["error"])
		}
	}
	w := ta.phoneBindGrant(t, "code-bindable", url.Values{"phone": {phone}, "code": {"000000"}})
	mustStatus(t, w, http.StatusBadRequest)
	decode(t, w, &body)
	if body["error"] != "sms_attempts_exceeded" {
		t.Fatalf("5th wrong attempt: error = %v, want sms_attempts_exceeded", body["error"])
	}
}

// No code stored (never sent / already consumed) → sms_code_expired.
func TestWeChatPhoneBindExpiredCode(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(105)

	w := ta.phoneBindGrant(t, "code-bindable", url.Values{"phone": {phone}, "code": {"123456"}})
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "sms_code_expired" {
		t.Fatalf("error = %v, want sms_code_expired", body["error"])
	}
}

// A code minted for the login scene can never drive the bind grant (ADR 0010:
// scene-scoped codes close the social-engineering gap). The wrong-scene code
// is indistinguishable from no code at all: sms_code_expired, not invalid.
func TestWeChatPhoneBindSceneMismatchRejected(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(106)
	seedCode(t, ta, repository.SmsSceneLogin, phone, "123456") // login-scene code

	w := ta.phoneBindGrant(t, "code-bindable", url.Values{"phone": {phone}, "code": {"123456"}})
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "sms_code_expired" {
		t.Fatalf("error = %v, want sms_code_expired", body["error"])
	}
}

// The identity is already bound to a different account → 409
// wechat_already_bound, and the one-time code is NOT consumed (a doomed
// request must not burn it): a later attempt with a conflict-free identity
// and the same code succeeds, which only works if the record survived.
func TestWeChatPhoneBindIdentityBoundToAnotherUser(t *testing.T) {
	ta := newTestApp(t)
	ta.bindUser(t, "wx-holder@example.com") // openid wx_bindable now bound
	phone := smsPhone(107)
	seedCode(t, ta, repository.SmsSceneBindPhone, phone, "123456")

	w := ta.phoneBindGrant(t, "code-bindable", url.Values{"phone": {phone}, "code": {"123456"}})
	mustStatus(t, w, http.StatusConflict)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "wechat_already_bound" {
		t.Fatalf("error = %v, want wechat_already_bound", body["error"])
	}

	// A conflict-free identity + the surviving code completes the flow.
	ok := ta.phoneBindGrant(t, "code-unionid", url.Values{"phone": {phone}, "code": {"123456"}})
	mustStatus(t, ok, http.StatusOK)
}

// The unionid is the cross-mini-program key: a fresh openid sharing a
// unionid with another account's identity is still a conflict.
func TestWeChatPhoneBindUnionIDCollision(t *testing.T) {
	ta := newTestApp(t)
	// code-bound → openid wx_bound + unionid wx_union_bound.
	ta.registerUser(t, "unionid-holder@example.com")
	bind := ta.wechatExchange(t, "code-bound", url.Values{
		"email": {"unionid-holder@example.com"}, "password": {"Password1!"},
	})
	mustStatus(t, bind, http.StatusOK)

	phone := smsPhone(108)
	seedCode(t, ta, repository.SmsSceneBindPhone, phone, "123456")
	// code-unionid → openid wx_unionids, SAME unionid wx_union_bound.
	w := ta.phoneBindGrant(t, "code-unionid", url.Values{"phone": {phone}, "code": {"123456"}})
	mustStatus(t, w, http.StatusConflict)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "wechat_already_bound" {
		t.Fatalf("error = %v, want wechat_already_bound", body["error"])
	}
}

// An EXISTING phone account binding a WeChat identity that belongs to a
// different account is refused — the phone identifies the target, and the
// identity belongs to someone else (issue Testing Decisions: 手机号被其它账号
// 持有/身份冲突的既有账号分支).
func TestWeChatPhoneBindExistingPhoneIdentityBoundToOther(t *testing.T) {
	ta := newTestApp(t)
	// Account B binds the code-bindable identity via email+password.
	ta.bindUser(t, "identity-owner@example.com")

	// Account A is an existing phone account with no WeChat link.
	phone := smsPhone(108)
	if w := smsSend(t, ta, phone); w.Code != http.StatusOK {
		t.Fatalf("send status = %d, want 200", w.Code)
	}
	accountA := smsVerify(t, ta, phone, "123456", nil)

	seedCode(t, ta, repository.SmsSceneBindPhone, phone, "123456")
	w := ta.phoneBindGrant(t, "code-bindable", url.Values{"phone": {phone}, "code": {"123456"}})
	mustStatus(t, w, http.StatusConflict)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "wechat_already_bound" {
		t.Fatalf("error = %v, want wechat_already_bound", body["error"])
	}

	// Nothing was bound to A.
	me := ta.do(http.MethodGet, "/api/users/me", nil, ta.bearer(accountA.AccessToken))
	mustStatus(t, me, http.StatusOK)
	var prof struct {
		WeChatBound bool `json:"wechat_bound"`
	}
	decode(t, me, &prof)
	if prof.WeChatBound {
		t.Fatalf("conflicting bind must not link the identity to account A")
	}
}

// An account already bound to a DIFFERENT WeChat identity in this
// mini-program may not silently rebind (same guard as the email bind flow).
func TestWeChatPhoneBindDifferentIdentityRejected(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(109)
	seedCode(t, ta, repository.SmsSceneBindPhone, phone, "123456")
	first := ta.phoneBindGrant(t, "code-bindable", url.Values{"phone": {phone}, "code": {"123456"}})
	mustStatus(t, first, http.StatusOK)

	seedCode(t, ta, repository.SmsSceneBindPhone, phone, "123456")
	// code-unionid is a different openid, and its unionid is free here.
	w := ta.phoneBindGrant(t, "code-unionid", url.Values{"phone": {phone}, "code": {"123456"}})
	mustStatus(t, w, http.StatusConflict)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "wechat_already_bound" {
		t.Fatalf("error = %v, want wechat_already_bound", body["error"])
	}
}

// Parameter validation: missing subject_token / phone / code, an invalid
// phone, and the email+password proof are all 400; email+password and
// phone+code are mutually exclusive.
func TestWeChatPhoneBindParamValidation(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(110)

	cases := []struct {
		name  string
		token string
		extra url.Values
	}{
		{"missing subject_token", "", url.Values{"phone": {phone}, "code": {"123456"}}},
		{"missing phone", "code-bindable", url.Values{"code": {"123456"}}},
		{"missing code", "code-bindable", url.Values{"phone": {phone}}},
		{"invalid phone", "code-bindable", url.Values{"phone": {"12345"}, "code": {"123456"}}},
		{"email+password present", "code-bindable", url.Values{"phone": {phone}, "code": {"123456"}, "email": {"a@b.co"}, "password": {"Password1!"}}},
	}
	for _, tc := range cases {
		w := ta.phoneBindGrant(t, tc.token, tc.extra)
		mustStatus(t, w, http.StatusBadRequest)
	}
}

// With the invite gate on, a brand-new phone is rejected (the grant accepts
// no invite_code parameter — closing the gate is a deployment decision), and
// an existing phone user still logs in. With the gate off, stray invite_code
// parameters are ignored (ADR 0006).
func TestWeChatPhoneBindInviteGate(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(111)
	ta.cfg.RequireInviteCode = true
	seedCode(t, ta, repository.SmsSceneBindPhone, phone, "123456")

	w := ta.phoneBindGrant(t, "code-bindable", url.Values{"phone": {phone}, "code": {"123456"}})
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "bad_request" || body["message"] != "invite_code is required" {
		t.Fatalf("gate rejection = %+v", body)
	}

	// Existing phone user: the gate never applies to login. Stray invite_code
	// parameters are accepted and ignored either way.
	ta.cfg.RequireInviteCode = false
	smsSend(t, ta, phone)
	smsVerify(t, ta, phone, "123456", nil)

	seedCode(t, ta, repository.SmsSceneBindPhone, phone, "123456")
	login := ta.phoneBindGrant(t, "code-bindable", url.Values{"phone": {phone}, "code": {"123456"}, "invite_code": {"STRAY"}})
	mustStatus(t, login, http.StatusOK)

	// Gate off + brand-new phone + stray invite_code: the parameter is ignored
	// (ADR 0006) and the auto-register goes through with registered=true.
	fresh := smsPhone(109)
	seedCode(t, ta, repository.SmsSceneBindPhone, fresh, "123456")
	// code-unionid is conflict-free here: code-bindable is already bound to the
	// login account above.
	reg := ta.phoneBindGrant(t, "code-unionid", url.Values{"phone": {fresh}, "code": {"123456"}, "invite_code": {"STRAY"}})
	mustStatus(t, reg, http.StatusOK)
	var resp phoneBindTokenResp
	decode(t, reg, &resp)
	if !resp.Registered {
		t.Fatalf("expected registered=true on auto-register with gate off, got %+v", resp)
	}
}

// Scene isolation on the send side: a bind_phone code cannot be consumed by
// the login verify endpoint; the 60-second cooldown is shared across scenes
// (per phone, not per scene); a bind_phone code drives /api/users/me/phone.
func TestSMSSendSceneIsolationAndSharedCooldown(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(112)

	// bind_phone code ≠ login code: the login verify endpoint cannot consume
	// a bind_phone-scene code (it sees no login-scene record at all).
	if w := smsSendScene(t, ta, phone, repository.SmsSceneBindPhone); w.Code != http.StatusOK {
		t.Fatalf("send status = %d, want 200", w.Code)
	}
	w := ta.do(http.MethodPost, "/api/auth/sms/verify", map[string]any{"phone": phone, "code": "123456"}, ta.clientHeaders())
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "sms_code_expired" {
		t.Fatalf("error = %v, want sms_code_expired", body["error"])
	}

	// Cooldown is per phone across scenes: the send window (5 per 60 s) counts
	// login and bind_phone sends against the same budget. Exhaust it with
	// alternating scenes; the 6th send — of the other scene — is throttled.
	if err := ta.smsStore.ReleaseSend(context.Background(), phone); err != nil {
		t.Fatalf("release: %v", err)
	}
	for i := 0; i < 5; i++ {
		scene := repository.SmsSceneLogin
		if i%2 == 0 {
			scene = repository.SmsSceneBindPhone
		}
		if w := smsSendScene(t, ta, phone, scene); w.Code != http.StatusOK {
			t.Fatalf("send #%d status = %d, want 200", i+1, w.Code)
		}
	}
	limited := smsSend(t, ta, phone) // login scene, budget already spent by both
	mustStatus(t, limited, http.StatusTooManyRequests)
	decode(t, limited, &body)
	if body["error"] != "sms_send_cooldown" {
		t.Fatalf("error = %v, want sms_send_cooldown", body["error"])
	}
}

// An unknown scene value is rejected at send time.
func TestSMSSendUnknownSceneRejected(t *testing.T) {
	ta := newTestApp(t)
	w := smsSendScene(t, ta, smsPhone(113), repository.SmsScene("pickup"))
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "bad_request" {
		t.Fatalf("error = %v, want bad_request", body["error"])
	}
}

// The daily cap is per phone, not per scene (ADR 0010): once the phone's
// sms:daily budget is spent, sends of EVERY scene are refused.
func TestSMSSendDailyLimitSharedAcrossScenes(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(110)

	// One real send per scene: both increment the same per-phone daily counter.
	if w := smsSendScene(t, ta, phone, repository.SmsSceneBindPhone); w.Code != http.StatusOK {
		t.Fatalf("bind send status = %d, want 200", w.Code)
	}
	if w := smsSendScene(t, ta, phone, repository.SmsSceneLogin); w.Code != http.StatusOK {
		t.Fatalf("login send status = %d, want 200", w.Code)
	}
	// Fill the rest of the daily budget (cap 20) directly through the store
	// (setup seam, mirroring seedCode's cooldown bypass).
	for i := 0; i < 18; i++ {
		if err := ta.smsStore.ReserveDailyCount(context.Background(), phone); err != nil {
			t.Fatalf("reserve daily #%d: %v", i+1, err)
		}
	}

	var body map[string]any
	for _, scene := range []repository.SmsScene{repository.SmsSceneBindPhone, repository.SmsSceneLogin} {
		w := smsSendScene(t, ta, phone, scene)
		mustStatus(t, w, http.StatusTooManyRequests)
		decode(t, w, &body)
		if body["error"] != "sms_daily_limit" {
			t.Fatalf("scene %s: error = %v, want sms_daily_limit", scene, body["error"])
		}
	}
}

// Regression guard for the web bind flow (PhoneBindModal → /api/auth/sms/send
// → POST /api/users/me/phone): the code the web client sends WITHOUT a scene
// (defaulting to login) must NOT satisfy the phone-bind endpoint, which
// consumes the bind_phone scene — only a code sent with scene=bind_phone does.
// This exercises send→consume scene consistency over the real HTTP endpoints,
// which seedCode-based tests bypass.
func TestPhoneBindSendConsumeSceneConsistency(t *testing.T) {
	ta := newTestApp(t)
	userTok := ta.registerUser(t, "scene-consistency@example.com")
	phone := smsPhone(115)

	// Exactly the web client's send payload: no scene field at all.
	webSend := ta.do(http.MethodPost, "/api/auth/sms/send", map[string]any{"phone": phone, "login_only": false}, ta.clientHeaders())
	mustStatus(t, webSend, http.StatusOK)

	w := ta.do(http.MethodPost, "/api/users/me/phone", map[string]any{"phone": phone, "code": "123456"}, ta.bearer(userTok))
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "sms_code_expired" {
		t.Fatalf("error = %v, want sms_code_expired (sceneless code must not drive a bind)", body["error"])
	}

	// The bind_phone-scene send is what makes the bind succeed.
	if w := smsSendScene(t, ta, phone, repository.SmsSceneBindPhone); w.Code != http.StatusOK {
		t.Fatalf("bind send status = %d, want 200", w.Code)
	}
	w = ta.do(http.MethodPost, "/api/users/me/phone", map[string]any{"phone": phone, "code": "123456"}, ta.bearer(userTok))
	mustStatus(t, w, http.StatusOK)
}

// The reset_password scene is reserved (ADR 0010) with no consuming endpoint
// yet — the send endpoint refuses it so SMS budget cannot be burned on codes
// that can never be consumed.
func TestSMSSendResetPasswordSceneRefused(t *testing.T) {
	ta := newTestApp(t)
	w := smsSendScene(t, ta, smsPhone(116), repository.SmsSceneResetPassword)
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "bad_request" {
		t.Fatalf("error = %v, want bad_request", body["error"])
	}
}

// The bind_phone scene also drives the authenticated phone-bind endpoint
// (POST /api/users/me/phone), which consumes under the same scene key.
func TestPhoneBindConsumesBindPhoneScene(t *testing.T) {
	ta := newTestApp(t)
	userTok := ta.registerUser(t, "phonebind-scene@example.com")
	phone := smsPhone(114)

	seedCode(t, ta, repository.SmsSceneBindPhone, phone, "123456")
	w := ta.do(http.MethodPost, "/api/users/me/phone", map[string]any{"phone": phone, "code": "123456"}, ta.bearer(userTok))
	mustStatus(t, w, http.StatusOK)
}
