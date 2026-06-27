package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
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
