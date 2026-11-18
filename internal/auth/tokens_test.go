package auth

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestOTPGenerationAndVerification(t *testing.T) {
	key := []byte("super-secret-hmac-key-at-least-32-bytes-long!")

	code, err := GenerateOTP()
	if err != nil {
		t.Fatalf("GenerateOTP failed: %v", err)
	}

	if len(code) != 6 {
		t.Errorf("expected 6-digit code, got %s", code)
	}

	for _, ch := range code {
		if ch < '0' || ch > '9' {
			t.Errorf("expected only digits in code, got %s", code)
		}
	}

	hash := HashOTP(code, key)
	if len(hash) != 64 {
		t.Errorf("expected 64-character hex hash, got %d chars: %s", len(hash), hash)
	}

	// Correct code verification
	if !VerifyOTP(code, key, hash) {
		t.Errorf("expected OTP %s to verify against hash %s", code, hash)
	}

	// Incorrect code verification
	wrongCode := "000000"
	if wrongCode == code {
		wrongCode = "111111"
	}
	if VerifyOTP(wrongCode, key, hash) {
		t.Errorf("expected wrong code %s to fail verification", wrongCode)
	}

	// Wrong key verification
	wrongKey := []byte("different-secret-key-that-is-also-32-bytes-long!")
	if VerifyOTP(code, wrongKey, hash) {
		t.Errorf("expected verification with wrong key to fail")
	}
}

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"User@Example.COM", "user@example.com", false},
		{"   alice.smith@test.org   ", "alice.smith@test.org", false},
		{"not-an-email", "", true},
		{"@missinguser.com", "", true},
		{"missingdomain@", "", true},
		{"", "", true},
		{"user@domain", "", true},
	}

	for _, tc := range tests {
		got, err := NormalizeEmail(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("NormalizeEmail(%q) err = %v, wantErr = %v", tc.input, err, tc.wantErr)
		}
		if got != tc.expected {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestAccessTokenGenerationAndParsing(t *testing.T) {
	secret := []byte("very-secure-jwt-secret-with-minimum-32-characters!")
	userID := "11111111-2222-3333-4444-555555555555"
	role := "user"
	issuer := "social-api"

	tokenStr, err := GenerateAccessToken(userID, role, secret, issuer, 15*time.Minute)
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}

	claims, err := ParseAndVerifyAccessToken(tokenStr, secret)
	if err != nil {
		t.Fatalf("ParseAndVerifyAccessToken failed: %v", err)
	}

	if claims.Subject != userID {
		t.Errorf("claims.Subject = %s, want %s", claims.Subject, userID)
	}
	if claims.Role != role {
		t.Errorf("claims.Role = %s, want %s", claims.Role, role)
	}
	if claims.Issuer != issuer {
		t.Errorf("claims.Issuer = %s, want %s", claims.Issuer, issuer)
	}

	// Pinning check: Algorithm tampering (e.g. signing with "none" or RS256)
	tamperedHeader := base64Encode([]byte(`{"alg":"none","typ":"JWT"}`))
	parts := strings.Split(tokenStr, ".")
	noneToken := fmt.Sprintf("%s.%s.", tamperedHeader, parts[1])

	_, err = ParseAndVerifyAccessToken(noneToken, secret)
	if err == nil {
		t.Errorf("expected algorithm mismatch to be rejected")
	}

	// Secret mismatch
	wrongSecret := []byte("completely-different-secret-key-at-least-32-bytes!")
	_, err = ParseAndVerifyAccessToken(tokenStr, wrongSecret)
	if err == nil {
		t.Errorf("expected wrong secret to fail verification")
	}
}

func TestSignupToken(t *testing.T) {
	secret := []byte("very-secure-jwt-secret-with-minimum-32-characters!")
	email := "newuser@example.com"
	issuer := "social-api"

	tokenStr, err := GenerateSignupToken(email, secret, issuer, 10*time.Minute)
	if err != nil {
		t.Fatalf("GenerateSignupToken failed: %v", err)
	}

	claims, err := ParseAndVerifySignupToken(tokenStr, secret)
	if err != nil {
		t.Fatalf("ParseAndVerifySignupToken failed: %v", err)
	}

	if claims.Email != email {
		t.Errorf("claims.Email = %s, want %s", claims.Email, email)
	}
	if claims.Purpose != "signup" {
		t.Errorf("claims.Purpose = %s, want signup", claims.Purpose)
	}

	// Verify that an Access Token is rejected as a Signup Token (purpose != signup)
	accessTokenStr, _ := GenerateAccessToken("user-id", "user", secret, issuer, 15*time.Minute)
	_, err = ParseAndVerifySignupToken(accessTokenStr, secret)
	if err == nil {
		t.Errorf("expected access token to be rejected when parsed as signup token")
	}
}

func TestRefreshToken(t *testing.T) {
	token1, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken failed: %v", err)
	}
	token2, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken failed: %v", err)
	}

	if token1 == token2 {
		t.Errorf("expected distinct random refresh tokens, got identical tokens")
	}

	hash1 := HashRefreshToken(token1)
	hash2 := HashRefreshToken(token2)

	if len(hash1) != 64 || len(hash2) != 64 {
		t.Errorf("expected 64 hex characters for SHA-256 hash")
	}
	if hash1 == hash2 {
		t.Errorf("expected distinct hashes for distinct tokens")
	}

	// Idempotent hashing
	if HashRefreshToken(token1) != hash1 {
		t.Errorf("hash function must be deterministic")
	}
}

func TestCalculateAge(t *testing.T) {
	today := time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		birthdate time.Time
		wantAge   int
	}{
		// Turned 13 today
		{time.Date(2013, time.November, 18, 0, 0, 0, 0, time.UTC), 13},
		// Will turn 13 tomorrow (currently 12)
		{time.Date(2013, time.November, 19, 0, 0, 0, 0, time.UTC), 12},
		// Turned 13 yesterday
		{time.Date(2013, time.November, 17, 0, 0, 0, 0, time.UTC), 13},
		// Born in 2000
		{time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC), 26},
		// Born in December 2013 (currently 12)
		{time.Date(2013, time.December, 31, 0, 0, 0, 0, time.UTC), 12},
	}

	for _, tc := range tests {
		got := CalculateAge(tc.birthdate, today)
		if got != tc.wantAge {
			t.Errorf("CalculateAge(%s, %s) = %d, want %d", tc.birthdate.Format("2006-01-02"), today.Format("2006-01-02"), got, tc.wantAge)
		}
	}
}

func base64Encode(b []byte) string {
	return strings.TrimRight(jwt.SigningMethodHS256.Alg(), "=")
}
