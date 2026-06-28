package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/zhaochy1990/auth-service/internal/domain"
)

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("Password1!"); err != nil {
		t.Fatalf("expected valid password, got %v", err)
	}
	cases := []struct {
		pw   string
		want string
	}{
		{"Ab1!", "at least 8"},
		{strings.Repeat("A", 100) + "a1!" + strings.Repeat("x", 26), "not exceed 128"},
		{"password1!", "uppercase"},
		{"PASSWORD1!", "lowercase"},
		{"Password!!", "digit"},
		{"Password11", "special"},
	}
	for _, c := range cases {
		err := ValidatePassword(c.pw)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("ValidatePassword(%q) = %v, want error containing %q", c.pw, err, c.want)
		}
	}
}

func TestClientSecretSHA256(t *testing.T) {
	secret := "test_secret_value_12345"
	hash := HashClientSecret(secret)
	if !strings.HasPrefix(hash, "sha256:") {
		t.Fatalf("hash missing sha256 prefix: %s", hash)
	}
	if ok, _ := VerifyClientSecret(secret, hash); !ok {
		t.Fatal("expected secret to verify")
	}
	if ok, _ := VerifyClientSecret("wrong", hash); ok {
		t.Fatal("expected wrong secret to fail")
	}
}

func TestClientSecretLegacyArgon2(t *testing.T) {
	secret := "test_secret"
	h, err := HashPassword(secret)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(h, "$argon2") {
		t.Fatalf("expected argon2 hash, got %s", h)
	}
	if ok, _ := VerifyClientSecret(secret, h); !ok {
		t.Fatal("expected legacy argon2 secret to verify")
	}
	if ok, _ := VerifyClientSecret("wrong", h); ok {
		t.Fatal("expected wrong secret to fail")
	}
}

func TestPasswordRoundtrip(t *testing.T) {
	h, err := HashPassword("Password1!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if ok, _ := VerifyPassword("Password1!", h); !ok {
		t.Fatal("expected password to verify")
	}
	if ok, _ := VerifyPassword("Nope", h); ok {
		t.Fatal("expected wrong password to fail")
	}
}

func TestVerifyAccessTokenRequiredClaims(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	m := &JWTManager{priv: priv, pub: &priv.PublicKey, issuer: "auth-service", accessExpirySecs: 3600}

	// A fully-formed token verifies.
	good, err := m.IssueAccessToken("user-1", "client-1", []string{"openid"}, "user", domain.MembershipRegular, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.VerifyAccessToken(good); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}

	// A correctly-signed token missing any required claim (sub/aud/iat) is
	// rejected — parity with the Rust set_required_spec_claims guard.
	now := time.Now()
	base := jwt.MapClaims{
		"sub": "user-1", "aud": "client-1", "iss": "auth-service",
		"exp": now.Add(time.Hour).Unix(), "iat": now.Unix(),
	}
	for _, missing := range []string{"sub", "aud", "iat"} {
		claims := jwt.MapClaims{}
		for k, v := range base {
			claims[k] = v
		}
		delete(claims, missing)
		signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(priv)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.VerifyAccessToken(signed); err == nil {
			t.Errorf("token missing %q was accepted", missing)
		}
	}
}

func TestVerifyPKCE(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	if !VerifyPKCE(verifier, challenge, "S256") {
		t.Fatal("expected S256 to verify")
	}
	if VerifyPKCE("wrong", challenge, "S256") {
		t.Fatal("expected wrong verifier to fail")
	}
	if !VerifyPKCE("abc", "abc", "plain") {
		t.Fatal("expected plain to verify")
	}
	if VerifyPKCE("abc", "abc", "unknown") {
		t.Fatal("expected unknown method to fail")
	}
}
