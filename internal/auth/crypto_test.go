package auth

import (
	"testing"
	"time"

	"instalbloc/internal/domain"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("ParolaSigura123!")
	if err != nil {
		t.Fatalf("unexpected error hashing password: %v", err)
	}
	if !VerifyPassword("ParolaSigura123!", hash) {
		t.Fatalf("expected VerifyPassword to succeed for matching password")
	}
	if VerifyPassword("ParolaGresita999!", hash) {
		t.Fatalf("expected VerifyPassword to fail for mismatched password")
	}
}

func TestOTPGenerationAndVerification(t *testing.T) {
	code, err := GenerateOTPCode()
	if err != nil {
		t.Fatalf("failed to generate OTP: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("expected 6-digit OTP, got %q", code)
	}

	secret := "test-hmac-secret-minimum-16-chars"
	digest := HashOTPCode(code, secret)
	if !VerifyOTPCode(code, digest, secret) {
		t.Fatalf("expected VerifyOTPCode to succeed")
	}
	if VerifyOTPCode("000000", digest, secret) && code != "000000" {
		t.Fatalf("expected VerifyOTPCode to fail for wrong code")
	}
}

func TestSignAndVerifySessionToken(t *testing.T) {
	secret := "test-hmac-secret-minimum-16-chars"
	now := time.Now().Unix()

	token := SignSessionToken(42, domain.RoleAsociatie, now, secret)
	uid, role, err := VerifySessionToken(token, secret, SessionMaxAgeSec)
	if err != nil {
		t.Fatalf("expected valid session token, got error: %v", err)
	}
	if uid != 42 || role != domain.RoleAsociatie {
		t.Fatalf("expected uid=42, role=asociatie; got uid=%d, role=%s", uid, role)
	}

	if _, _, err := VerifySessionToken(token+"tampered", secret, SessionMaxAgeSec); err == nil {
		t.Fatalf("expected tampered token to fail verification")
	}
}
