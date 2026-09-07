package server_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

const avatarMaxBytes = 2 << 20 // 2 MB, mirrored from the handler's limit

// testJPEGBytes is a minimal byte slice whose leading magic makes
// http.DetectContentType report "image/jpeg"; testPNGBytes yields "image/png".
// Neither needs to be a decodable image — the COS client only streams bytes.
var testJPEGBytes = []byte{
	0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01,
}
var testPNGBytes = []byte{
	0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
}

// uploadAvatar posts a multipart file to POST /api/users/me/avatar.
func uploadAvatar(t *testing.T, ta *testApp, token, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/users/me/avatar", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	ta.engine.ServeHTTP(w, req)
	return w
}

type avatarURLResponse struct {
	AvatarURL string `json:"avatar_url"`
}

// Happy path: a JPEG upload lands in COS under avatars/{userID}.jpg and the
// response returns the public URL built from the configured base URL.
func TestAvatarUploadSuccess(t *testing.T) {
	ta := newTestApp(t)
	token := ta.registerUser(t, "avatar@example.com")

	w := uploadAvatar(t, ta, token, "me.jpg", testJPEGBytes)
	mustStatus(t, w, http.StatusOK)
	var resp avatarURLResponse
	decode(t, w, &resp)

	// Resolve the user id the middleware signs into the key from the token.
	claims, err := ta.jwt.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	userID := claims.Sub
	wantURL := ta.cfg.TencentCosBaseURL + "/avatars/" + userID + ".jpg"
	if resp.AvatarURL != wantURL {
		t.Fatalf("avatar_url = %q, want %q", resp.AvatarURL, wantURL)
	}

	key, ctype := ta.fakeCos.lastUpload()
	if key != "avatars/"+userID+".jpg" {
		t.Fatalf("object key = %q, want avatars/%s.jpg", key, userID)
	}
	if ctype != "image/jpeg" {
		t.Fatalf("stored content-type = %q, want image/jpeg", ctype)
	}
}

// The avatar URL then round-trips through PATCH + GET profile.
func TestAvatarUploadPersistsViaPatch(t *testing.T) {
	ta := newTestApp(t)
	token := ta.registerUser(t, "avatar-patch@example.com")

	w := uploadAvatar(t, ta, token, "me.png", testPNGBytes)
	mustStatus(t, w, http.StatusOK)
	var resp avatarURLResponse
	decode(t, w, &resp)

	key, ctype := ta.fakeCos.lastUpload()
	if !bytes.HasSuffix([]byte(key), []byte(".png")) {
		t.Fatalf("expected a .png object key, got %q", key)
	}
	if ctype != "image/png" {
		t.Fatalf("stored content-type = %q, want image/png", ctype)
	}

	patch := ta.do(http.MethodPatch, "/api/users/me", map[string]any{"avatar_url": resp.AvatarURL}, ta.bearer(token))
	mustStatus(t, patch, http.StatusOK)

	me := ta.do(http.MethodGet, "/api/users/me", nil, ta.bearer(token))
	mustStatus(t, me, http.StatusOK)
	var prof struct {
		AvatarURL string `json:"avatar_url"`
	}
	decode(t, me, &prof)
	if prof.AvatarURL != resp.AvatarURL {
		t.Fatalf("profile avatar_url = %q, want %q (PATCH→GET round-trip mismatch)", prof.AvatarURL, resp.AvatarURL)
	}
}

// A non-image upload is rejected with 400 before any COS call.
func TestAvatarUploadInvalidMIME(t *testing.T) {
	ta := newTestApp(t)
	token := ta.registerUser(t, "avatar-mime@example.com")

	w := uploadAvatar(t, ta, token, "notes.txt", []byte("this is not an image"))
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "bad_request" {
		t.Fatalf("error = %v, want bad_request", body["error"])
	}
	if k, _ := ta.fakeCos.lastUpload(); k != "" {
		t.Fatalf("no COS upload expected for invalid MIME, got key %q", k)
	}
}

// An oversized upload is rejected with 400.
func TestAvatarUploadTooLarge(t *testing.T) {
	ta := newTestApp(t)
	token := ta.registerUser(t, "avatar-large@example.com")

	big := make([]byte, avatarMaxBytes+1)
	w := uploadAvatar(t, ta, token, "big.jpg", big)
	mustStatus(t, w, http.StatusBadRequest)
	var body map[string]any
	decode(t, w, &body)
	if body["error"] != "bad_request" {
		t.Fatalf("error = %v, want bad_request", body["error"])
	}
}

// No Bearer token → 401, no COS call.
func TestAvatarUploadNoToken(t *testing.T) {
	ta := newTestApp(t)

	w := uploadAvatar(t, ta, "", "me.jpg", testJPEGBytes)
	mustStatus(t, w, http.StatusUnauthorized)
}
