package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// smsPhone returns a unique phone per test so Redis keys never collide across
// tests (Redis is shared; MySQL is cleared per test).
func smsPhone(n int) string {
	base := []byte("13800000000")
	// spread the variation into the last digits
	s := n
	for i := 9; i > 4; i-- {
		base[i] = byte('0' + s%10)
		s /= 10
	}
	return string(base)
}

func smsSend(t *testing.T, ta *testApp, phone string) *httptest.ResponseRecorder {
	t.Helper()
	return ta.do(http.MethodPost, "/api/auth/sms/send", map[string]any{"phone": phone}, ta.clientHeaders())
}

// The token shape returned by /api/auth/sms/verify.
type smsTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

func smsVerify(t *testing.T, ta *testApp, phone, code string, invite *string) *smsTokenResponse {
	t.Helper()
	body := map[string]any{"phone": phone, "code": code}
	if invite != nil {
		body["invite_code"] = *invite
	}
	w := ta.do(http.MethodPost, "/api/auth/sms/verify", body, ta.clientHeaders())
	mustStatus(t, w, http.StatusOK)
	var resp smsTokenResponse
	decode(t, w, &resp)
	if resp.AccessToken == "" || resp.RefreshToken == "" || resp.TokenType != "Bearer" || resp.ExpiresIn != 3600 {
		t.Fatalf("unexpected token response: %+v", resp)
	}
	return &resp
}

// seedCode writes a verification code directly through the store (test setup
// that bypasses the 60s cooldown, as if a fresh send had happened).
func seedCode(t *testing.T, ta *testApp, phone, code string) {
	t.Helper()
	if err := ta.smsStore.StoreCode(context.Background(), phone, code, 5*time.Minute); err != nil {
		t.Fatalf("seed code: %v", err)
	}
}

// The full flow: send → verify auto-registers the account; the profile carries
// the phone; the admin sees/search finds the user; login is recorded.
func TestSMSSendVerifyAutoRegister(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(1)

	w := smsSend(t, ta, phone)
	mustStatus(t, w, http.StatusOK)

	tok := smsVerify(t, ta, phone, "123456", nil)

	me := ta.do(http.MethodGet, "/api/users/me", nil, ta.bearer(tok.AccessToken))
	mustStatus(t, me, http.StatusOK)
	var prof struct {
		Phone *string `json:"phone"`
		Email *string `json:"email"`
		Name  *string `json:"name"`
	}
	decode(t, me, &prof)
	if prof.Phone == nil || *prof.Phone != phone {
		t.Fatalf("profile phone = %v, want %q", prof.Phone, phone)
	}
	if prof.Email != nil || prof.Name != nil {
		t.Fatalf("phone-only user should have no email/name, got %+v", prof)
	}

	// Admin can find the user by phone substring.
	list := ta.do(http.MethodGet, "/admin/users?search="+phone, nil, ta.bearer(ta.adminToken))
	mustStatus(t, list, http.StatusOK)
	var listed struct {
		Total uint64 `json:"total"`
		Users []struct {
			ID    string  `json:"id"`
			Phone *string `json:"phone"`
		} `json:"users"`
	}
	decode(t, list, &listed)
	if listed.Total != 1 || len(listed.Users) != 1 || listed.Users[0].Phone == nil || *listed.Users[0].Phone != phone {
		t.Fatalf("admin search by phone = %+v", listed)
	}

	// Admin get exposes the phone and the recorded login.
	get := ta.do(http.MethodGet, "/admin/users/"+listed.Users[0].ID, nil, ta.bearer(ta.adminToken))
	mustStatus(t, get, http.StatusOK)
	var user struct {
		Phone        *string `json:"phone"`
		RecentLogins []struct {
			At string `json:"at"`
		} `json:"recent_logins"`
	}
	decode(t, get, &user)
	if user.Phone == nil || *user.Phone != phone {
		t.Fatalf("admin get phone = %v", user.Phone)
	}
	if len(user.RecentLogins) != 1 {
		t.Fatalf("expected 1 recent login, got %d", len(user.RecentLogins))
	}
}

// A returning phone logs into the SAME account (no duplicate registration).
func TestSMSExistingUserLogin(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(2)

	// Register on first verification.
	smsSend(t, ta, phone)
	first := smsVerify(t, ta, phone, "123456", nil)
	w := ta.do(http.MethodGet, "/api/users/me", nil, ta.bearer(first.AccessToken))
	mustStatus(t, w, http.StatusOK)

	// A fresh code (bypassing the 60s cooldown) logs the same account in.
	seedCode(t, ta, phone, "123456")
	second := smsVerify(t, ta, phone, "123456", nil)

	me := ta.do(http.MethodGet, "/api/users/me", nil, ta.bearer(second.AccessToken))
	mustStatus(t, me, http.StatusOK)
	var prof struct {
		ID string `json:"id"`
	}
	decode(t, me, &prof)

	list := ta.do(http.MethodGet, "/admin/users?search="+phone, nil, ta.bearer(ta.adminToken))
	mustStatus(t, list, http.StatusOK)
	var listed struct {
		Total uint64 `json:"total"`
		Users []struct {
			ID string `json:"id"`
		} `json:"users"`
	}
	decode(t, list, &listed)
	if listed.Total != 1 || listed.Users[0].ID != prof.ID {
		t.Fatalf("expected one account (%s), got %+v", prof.ID, listed)
	}
}

// Wrong code → sms_code_invalid; correct code consumes (single-use) so a replay
// is rejected.
func TestSMSVerifyInvalidAndSingleUse(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(3)

	smsSend(t, ta, phone)

	wrong := ta.do(http.MethodPost, "/api/auth/sms/verify", map[string]any{"phone": phone, "code": "000000"}, ta.clientHeaders())
	mustStatus(t, wrong, http.StatusBadRequest)
	var body map[string]any
	decode(t, wrong, &body)
	if body["error"] != "sms_code_invalid" {
		t.Fatalf("error = %v, want sms_code_invalid", body["error"])
	}

	smsVerify(t, ta, phone, "123456", nil)

	// Replay of the consumed code is rejected.
	replay := ta.do(http.MethodPost, "/api/auth/sms/verify", map[string]any{"phone": phone, "code": "123456"}, ta.clientHeaders())
	mustStatus(t, replay, http.StatusBadRequest)
	decode(t, replay, &body)
	if body["error"] != "sms_code_expired" {
		t.Fatalf("replay error = %v, want sms_code_expired", body["error"])
	}
}

// An expired code (TTL elapsed) is rejected with sms_code_expired.
func TestSMSVerifyExpiredCode(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(4)

	// Store a code with a 1-second TTL, then wait past it.
	if err := ta.smsStore.StoreCode(context.Background(), phone, "123456", time.Second); err != nil {
		t.Fatalf("seed code: %v", err)
	}
	time.Sleep(1200 * time.Millisecond)

	w := ta.do(http.MethodPost, "/api/auth/sms/verify", map[string]any{"phone": phone, "code": "123456"}, ta.clientHeaders())
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "sms_code_expired" {
		t.Fatalf("error = %v, want sms_code_expired", body["error"])
	}
}

// 5 failed attempts invalidate the code; the correct code then no longer works.
func TestSMSVerifyAttemptCap(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(5)

	smsSend(t, ta, phone)

	for i := 0; i < 4; i++ {
		w := ta.do(http.MethodPost, "/api/auth/sms/verify", map[string]any{"phone": phone, "code": "000000"}, ta.clientHeaders())
		mustStatus(t, w, http.StatusBadRequest)
		var body map[string]any
		decode(t, w, &body)
		if body["error"] != "sms_code_invalid" {
			t.Fatalf("attempt %d error = %v, want sms_code_invalid", i+1, body["error"])
		}
	}

	w := ta.do(http.MethodPost, "/api/auth/sms/verify", map[string]any{"phone": phone, "code": "000000"}, ta.clientHeaders())
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "sms_attempts_exceeded" {
		t.Fatalf("error = %v, want sms_attempts_exceeded", body["error"])
	}

	// The code was invalidated by the cap.
	w = ta.do(http.MethodPost, "/api/auth/sms/verify", map[string]any{"phone": phone, "code": "123456"}, ta.clientHeaders())
	mustStatus(t, w, http.StatusBadRequest)
	decode(t, w, &body)
	if body["error"] != "sms_code_expired" {
		t.Fatalf("post-cap error = %v, want sms_code_expired", body["error"])
	}
}

// A second send to the same phone within 60 seconds is rejected.
func TestSMSSendCooldown(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(6)

	w := smsSend(t, ta, phone)
	mustStatus(t, w, http.StatusOK)

	w = smsSend(t, ta, phone)
	mustStatus(t, w, http.StatusTooManyRequests)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "sms_send_cooldown" {
		t.Fatalf("error = %v, want sms_send_cooldown", body["error"])
	}
}

// Ten sends per phone per day; the 11th is rejected (cooldown bypassed via the
// store for setup).
func TestSMSSendDailyLimit(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(7)

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if err := ta.smsStore.ReserveDailyCount(ctx, phone); err != nil {
			t.Fatalf("seed daily #%d: %v", i+1, err)
		}
	}

	w := smsSend(t, ta, phone)
	mustStatus(t, w, http.StatusTooManyRequests)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "sms_daily_limit" {
		t.Fatalf("error = %v, want sms_daily_limit", body["error"])
	}
}

// An invalid phone format is rejected before anything else.
func TestSMSInvalidPhoneFormat(t *testing.T) {
	ta := newTestApp(t)

	for _, bad := range []string{"12345", "23812345678", "1381234567", "+8613812345678", ""} {
		w := ta.do(http.MethodPost, "/api/auth/sms/send", map[string]any{"phone": bad}, ta.clientHeaders())
		mustStatus(t, w, http.StatusBadRequest)
		var body map[string]any
		decode(t, w, &body)
		if body["error"] != "bad_request" {
			t.Fatalf("send %q error = %v, want bad_request", bad, body["error"])
		}

		w = ta.do(http.MethodPost, "/api/auth/sms/verify", map[string]any{"phone": bad, "code": "123456"}, ta.clientHeaders())
		mustStatus(t, w, http.StatusBadRequest)
		decode(t, w, &body)
		if body["error"] != "bad_request" {
			t.Fatalf("verify %q error = %v, want bad_request", bad, body["error"])
		}
	}
}

// Without Tencent credentials and outside test mode, send reports
// sms_not_configured instead of failing startup or hanging on an upstream call.
func TestSMSNotConfigured(t *testing.T) {
	ta := newTestApp(t)
	ta.cfg.SMSTestMode = false // unconfigured client in the harness

	w := smsSend(t, ta, smsPhone(8))
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "sms_not_configured" {
		t.Fatalf("error = %v, want sms_not_configured", body["error"])
	}
}

// Invite gate on: a NEW phone must present a valid invite code; an existing
// phone user is never asked for one.
func TestSMSVerifyInviteGateOn(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(9)
	t.Setenv("STRIDE_REQUIRE_INVITE_CODE", "true")

	mk := ta.do(http.MethodPost, "/admin/invite-codes", nil, ta.bearer(ta.adminToken))
	mustStatus(t, mk, http.StatusOK)
	var code struct {
		Code string `json:"code"`
	}
	decode(t, mk, &code)

	smsSend(t, ta, phone)

	// No invite code → 400.
	noCode := ta.do(http.MethodPost, "/api/auth/sms/verify", map[string]any{"phone": phone, "code": "123456"}, ta.clientHeaders())
	mustStatus(t, noCode, http.StatusBadRequest)
	var body map[string]any
	decode(t, noCode, &body)
	if body["error"] != "bad_request" || body["message"] != "invite_code is required" {
		t.Fatalf("unexpected gate rejection: %+v", body)
	}

	// With the invite code → registered.
	smsVerify(t, ta, phone, "123456", &code.Code)

	// A returning user logs in without an invite code even while the gate is on.
	seedCode(t, ta, phone, "123456")
	smsVerify(t, ta, phone, "123456", nil)

	// A fresh phone with an already-used invite → 409.
	phone2 := smsPhone(10)
	smsSend(t, ta, phone2)
	reuse := ta.do(http.MethodPost, "/api/auth/sms/verify", map[string]any{"phone": phone2, "code": "123456", "invite_code": code.Code}, ta.clientHeaders())
	mustStatus(t, reuse, http.StatusConflict)
	decode(t, reuse, &body)
	if body["error"] != "invite_code_already_used" {
		t.Fatalf("error = %v, want invite_code_already_used", body["error"])
	}
}

// The AUTH_REQUIRE_INVITE_CODE env alias gates SMS registration identically.
func TestSMSVerifyInviteGateEnvAlias(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(11)
	t.Setenv("AUTH_REQUIRE_INVITE_CODE", "true")

	smsSend(t, ta, phone)
	w := ta.do(http.MethodPost, "/api/auth/sms/verify", map[string]any{"phone": phone, "code": "123456"}, ta.clientHeaders())
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "bad_request" {
		t.Fatalf("error = %v, want bad_request under AUTH_REQUIRE_INVITE_CODE", body["error"])
	}
}

// Invite gate off: verification never requires an invite code.
func TestSMSVerifyInviteGateOff(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(12)

	smsSend(t, ta, phone)
	smsVerify(t, ta, phone, "123456", nil)
}

// The dedicated send limiter caps per-IP sends.
func TestSMSSendRateLimit(t *testing.T) {
	ta := newTestAppWithRateLimits(t, 3, 60)

	for i := 1; i <= 3; i++ {
		w := smsSend(t, ta, smsPhone(20+i))
		mustStatus(t, w, http.StatusOK)
	}
	w := smsSend(t, ta, smsPhone(30))
	mustStatus(t, w, http.StatusTooManyRequests)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "rate_limited" {
		t.Fatalf("error = %v, want rate_limited", body["error"])
	}
}

// The dedicated verify limiter caps per-IP verifies.
func TestSMSVerifyRateLimit(t *testing.T) {
	ta := newTestAppWithRateLimits(t, 60, 3)

	for i := 1; i <= 3; i++ {
		phone := smsPhone(40 + i)
		seedCode(t, ta, phone, "123456")
		smsVerify(t, ta, phone, "123456", nil)
	}
	phone := smsPhone(50)
	seedCode(t, ta, phone, "123456")
	w := ta.do(http.MethodPost, "/api/auth/sms/verify", map[string]any{"phone": phone, "code": "123456"}, ta.clientHeaders())
	mustStatus(t, w, http.StatusTooManyRequests)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "rate_limited" {
		t.Fatalf("error = %v, want rate_limited", body["error"])
	}
}

// Phone-only users get a masked-phone display fallback in the admin list and
// sort alongside named users.
func TestSMSAdminListIncludesPhoneUsers(t *testing.T) {
	ta := newTestApp(t)
	phone := smsPhone(13)

	smsSend(t, ta, phone)
	smsVerify(t, ta, phone, "123456", nil)

	list := ta.do(http.MethodGet, "/admin/users?search="+phone, nil, ta.bearer(ta.adminToken))
	mustStatus(t, list, http.StatusOK)
	var body struct {
		Total uint64 `json:"total"`
		Users []struct {
			ID    string  `json:"id"`
			Phone *string `json:"phone"`
			Name  *string `json:"name"`
		} `json:"users"`
	}
	decode(t, list, &body)
	if body.Total != 1 || body.Users[0].Phone == nil || *body.Users[0].Phone != phone {
		t.Fatalf("expected phone user in admin list, got %+v", body)
	}
	if body.Users[0].Name != nil {
		t.Fatalf("phone-only user must not fabricate a display name, got %v", *body.Users[0].Name)
	}
}
