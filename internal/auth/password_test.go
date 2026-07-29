package auth

import (
	"strings"
	"testing"
)

func TestVerifyPasswordRoundTrip(t *testing.T) {
	cases := []string{"correct horse battery staple", "shortish-1", "ünïcödé pässwörd"}

	for _, password := range cases {
		t.Run(password, func(t *testing.T) {
			hash, err := HashPassword(password)
			if err != nil {
				t.Fatalf("HashPassword: %v", err)
			}
			if !VerifyPassword(password, hash) {
				t.Fatal("expected the password to verify")
			}
			if VerifyPassword(password+"x", hash) {
				t.Fatal("expected a different password to fail")
			}
		})
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	cases := []struct {
		name   string
		stored string
	}{
		{"empty", ""},
		{"unknown scheme", "bcrypt$10$salt$digest"},
		{"missing fields", "pbkdf2-sha256$100000$deadbeef"},
		{"zero rounds", "pbkdf2-sha256$0$deadbeef$digest"},
		{"inflated rounds", "pbkdf2-sha256$100000000$deadbeef$digest"},
		{"invalid salt", "pbkdf2-sha256$100000$zzzz$digest"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if VerifyPassword("whatever", testCase.stored) {
				t.Fatal("expected verification to fail")
			}
		})
	}
}

func TestGenerateAPIToken(t *testing.T) {
	token, err := GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken: %v", err)
	}
	if !strings.HasPrefix(token, TokenPrefix) {
		t.Fatalf("token %q is missing the %q prefix", token, TokenPrefix)
	}

	other, err := GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken: %v", err)
	}
	if token == other {
		t.Fatal("expected two generated tokens to differ")
	}
}
