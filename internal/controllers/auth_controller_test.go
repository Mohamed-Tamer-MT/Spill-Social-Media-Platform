package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/config"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/db"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/mailer"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/models"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/services"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/testhelper"
	"github.com/go-chi/chi/v5"
)

func setupTestApp(t *testing.T) (http.Handler, *mailer.MockMailer) {
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
		t.Fatalf("failed to connect to db: %v", err)
	}

	repo := models.NewRepository(pool.Pool)
	mockMailer := mailer.NewMockMailer()
	authService := services.NewAuthService(repo, mockMailer, cfg)
	authController := NewAuthController(authService)

	r := chi.NewRouter()
	r.Post("/auth/request-code", authController.RequestCode)
	r.Post("/auth/verify", authController.Verify)
	r.Post("/auth/signup", authController.Signup)
	r.Post("/auth/refresh", authController.Refresh)
	r.Post("/auth/logout", authController.Logout)

	return r, mockMailer
}

func TestAuthController_RequestCode(t *testing.T) {
	router, _ := setupTestApp(t)

	body, _ := json.Marshal(map[string]string{
		"email": "ctrl_test@example.com",
	})
	req := httptest.NewRequest("POST", "/auth/request-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	// Always returns 202 Accepted
	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202 Accepted, got %d", rr.Code)
	}
}

func TestAuthController_VerifyInvalid(t *testing.T) {
	router, _ := setupTestApp(t)

	body, _ := json.Marshal(map[string]string{
		"email": "nonexistent@example.com",
		"code":  "000000",
	})
	req := httptest.NewRequest("POST", "/auth/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for invalid code, got %d", rr.Code)
	}
}
