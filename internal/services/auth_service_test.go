package services

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/auth"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/config"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/db"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/mailer"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/models"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/testhelper"
)

func getTestService(t *testing.T) (*AuthService, *mailer.MockMailer) {
	ctx := context.Background()
	superConn, appConn, _, err := testhelper.SetupTestDB(ctx)
	if err != nil {
		t.Fatalf("failed to setup testcontainer db: %v", err)
	}

	cfg := &config.Config{
		AppEnv:               "test",
		HTTPPort:             "8080",
		DatabaseURL:          appConn,
		MigrationDatabaseURL: superConn,
		StorageBucket:        "media",
		StoragePublicBaseURL: "https://test.local/storage",
		JWTSecret:            []byte("01234567890123456789012345678901"),
		JWTIssuer:            "social-api-test",
		AccessTokenTTL:       15 * time.Minute,
		RefreshTokenTTL:      720 * time.Hour,
		SignupTokenTTL:       10 * time.Minute,
		OTPHMACKey:           []byte("abcdefabcdefabcdefabcdefabcdefab"),
		OTPTTL:               10 * time.Minute,
		OTPResendCooldown:    60 * time.Second,
		OTPMaxAttempts:       5,
		Mailer:               "mock",
	}

	pool, err := db.NewAppPool(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("failed to connect to test db: %v", err)
	}

	repo := models.NewRepository(pool.Pool)
	mockMailer := mailer.NewMockMailer()
	svc := NewAuthService(repo, mockMailer, cfg)

	return svc, mockMailer
}

func TestAuthService_OTPFlow(t *testing.T) {
	svc, mockMailer := getTestService(t)
	ctx := context.Background()

	testEmail := fmt.Sprintf("test_otp_%d@example.com", time.Now().UnixNano())

	// 1. Request Code
	err := svc.RequestCode(ctx, testEmail)
	if err != nil {
		t.Fatalf("RequestCode failed: %v", err)
	}

	if len(mockMailer.SentMessages) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(mockMailer.SentMessages))
	}

	// 2. Cooldown check: second request within 60s should be silently dropped (no new email)
	err = svc.RequestCode(ctx, testEmail)
	if err != nil {
		t.Fatalf("second RequestCode within cooldown returned error: %v", err)
	}
	if len(mockMailer.SentMessages) != 1 {
		t.Errorf("cooldown failed: sent messages count increased to %d", len(mockMailer.SentMessages))
	}

	// 3. Failed verification with incorrect code
	_, err = svc.VerifyCode(ctx, testEmail, "000000", "test-agent")
	if err == nil {
		t.Errorf("expected wrong code to fail verification")
	}

	// 4. Verify with the actual code extracted from the mock mailer body
	// Subject: "Your Spill Verification Code"
	// Body contains: "Your login code is: 123456"
	msg := mockMailer.SentMessages[0].Body
	// Extract the 6 digits from body
	var code string
	for i := 0; i+6 <= len(msg); i++ {
		sub := msg[i : i+6]
		allDigits := true
		for _, ch := range sub {
			if ch < '0' || ch > '9' {
				allDigits = false
				break
			}
		}
		if allDigits && (i == 0 || msg[i-1] == ' ' || msg[i-1] == ':') {
			code = sub
			break
		}
	}

	if code == "" {
		t.Fatalf("failed to parse code from mock email body: %s", msg)
	}

	// Verify with correct code
	res, err := svc.VerifyCode(ctx, testEmail, code, "test-agent")
	if err != nil {
		t.Fatalf("VerifyCode failed with correct code: %v", err)
	}

	if !res.IsNewUser {
		t.Errorf("expected isNewUser = true for unregistered email")
	}
	if res.SignupToken == "" {
		t.Errorf("expected non-empty signup token")
	}

	// 5. Code replay: verifying the same consumed code must fail
	_, err = svc.VerifyCode(ctx, testEmail, code, "test-agent")
	if err == nil {
		t.Errorf("expected consumed code verification to fail")
	}
}

func TestAuthService_SignupAndRefreshFlow(t *testing.T) {
	svc, mockMailer := getTestService(t)
	ctx := context.Background()

	nano := time.Now().UnixNano()
	testEmail := fmt.Sprintf("test_refresh_%d@example.com", nano)

	// Request & verify code
	_ = svc.RequestCode(ctx, testEmail)
	if len(mockMailer.SentMessages) == 0 {
		t.Fatalf("no email sent")
	}

	msg := mockMailer.SentMessages[len(mockMailer.SentMessages)-1].Body
	var code string
	for i := 0; i+6 <= len(msg); i++ {
		sub := msg[i : i+6]
		allDigits := true
		for _, ch := range sub {
			if ch < '0' || ch > '9' {
				allDigits = false
				break
			}
		}
		if allDigits && (i == 0 || msg[i-1] == ' ' || msg[i-1] == ':') {
			code = sub
			break
		}
	}

	res, err := svc.VerifyCode(ctx, testEmail, code, "test-agent")
	if err != nil {
		t.Fatalf("VerifyCode failed: %v", err)
	}

	// Sign up
	username := fmt.Sprintf("rf_%d", nano%1000000000)
	tokens, user, err := svc.Signup(ctx, res.SignupToken, username, "Refresh User", "1998-05-10", "test-agent")
	if err != nil {
		t.Fatalf("Signup failed: %v", err)
	}

	if user.Username != username {
		t.Errorf("expected username %s, got %s", username, user.Username)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("expected access and refresh tokens")
	}

	// 1. Refresh Token Rotation
	oldRefreshToken := tokens.RefreshToken
	newTokens, err := svc.RefreshToken(ctx, oldRefreshToken, "test-agent-2")
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}

	if newTokens.RefreshToken == oldRefreshToken {
		t.Errorf("expected new refresh token after rotation")
	}
	if newTokens.AccessToken == "" {
		t.Errorf("expected new access token after rotation")
	}

	// 2. Reuse Attack Detection: Presenting the already-rotated (revoked) old token
	_, err = svc.RefreshToken(ctx, oldRefreshToken, "attacker-agent")
	if err == nil {
		t.Fatalf("expected reuse of revoked token to fail")
	}
	if !errors.Is(err, ErrTokenRevoked) {
		t.Errorf("expected ErrTokenRevoked on reuse, got: %v", err)
	}

	// 3. Since reuse attack was detected, all refresh tokens (including newTokens.RefreshToken) must now be revoked!
	_, err = svc.RefreshToken(ctx, newTokens.RefreshToken, "test-agent-3")
	if err == nil {
		t.Fatalf("expected all tokens to be invalidated following reuse attack")
	}
}

func TestAuthService_UnderageRejection(t *testing.T) {
	svc, _ := getTestService(t)
	ctx := context.Background()

	// Generate a signup token for a mock email
	signupToken, err := auth.GenerateSignupToken("kid@example.com", svc.cfg.JWTSecret, svc.cfg.JWTIssuer, 10*time.Minute)
	if err != nil {
		t.Fatalf("failed to generate signup token: %v", err)
	}

	// Attempt signup with a birthdate younger than 13 years old
	// Today is 2026-11-18, so 2015 is ~11 years old
	_, _, err = svc.Signup(ctx, signupToken, "young_kid", "Young Kid", "2015-01-01", "test-agent")
	if err == nil {
		t.Fatalf("expected underage user to be rejected")
	}
	if !errors.Is(err, ErrUnderage) {
		t.Errorf("expected ErrUnderage, got %v", err)
	}
}
