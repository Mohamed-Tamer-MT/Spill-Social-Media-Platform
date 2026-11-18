package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/auth"
)

func TestRequireAuth(t *testing.T) {
	secret := []byte("very-secure-jwt-secret-with-minimum-32-characters!")
	userID := "user-12345"
	role := "user"
	validToken, err := auth.GenerateAccessToken(userID, role, secret, "social-api", 15*time.Minute)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	handler := RequireAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID := GetUserID(r.Context())
		gotRole := GetRole(r.Context())
		if gotUserID != userID {
			t.Errorf("GetUserID = %s, want %s", gotUserID, userID)
		}
		if gotRole != role {
			t.Errorf("GetRole = %s, want %s", gotRole, role)
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Case 1: Missing auth header -> 401
	req1 := httptest.NewRequest("GET", "/protected", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on missing auth, got %d", rr1.Code)
	}

	// Case 2: Malformed auth header -> 401
	req2 := httptest.NewRequest("GET", "/protected", nil)
	req2.Header.Set("Authorization", "Token "+validToken)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on non-Bearer, got %d", rr2.Code)
	}

	// Case 3: Valid Bearer token -> 200
	req3 := httptest.NewRequest("GET", "/protected", nil)
	req3.Header.Set("Authorization", "Bearer "+validToken)
	rr3 := httptest.NewRecorder()
	handler.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Errorf("expected 200 on valid token, got %d", rr3.Code)
	}

	// Case 4: Expired token -> 401
	expiredToken, _ := auth.GenerateAccessToken(userID, role, secret, "social-api", -1*time.Minute)
	req4 := httptest.NewRequest("GET", "/protected", nil)
	req4.Header.Set("Authorization", "Bearer "+expiredToken)
	rr4 := httptest.NewRecorder()
	handler.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on expired token, got %d", rr4.Code)
	}
}

func TestRequireRole(t *testing.T) {
	secret := []byte("very-secure-jwt-secret-with-minimum-32-characters!")
	userToken, _ := auth.GenerateAccessToken("user-1", "user", secret, "social-api", 15*time.Minute)
	adminToken, _ := auth.GenerateAccessToken("admin-1", "admin", secret, "social-api", 15*time.Minute)

	adminOnlyHandler := RequireAuth(secret)(RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	// User role accessing admin route -> 403 Forbidden
	req1 := httptest.NewRequest("GET", "/admin", nil)
	req1.Header.Set("Authorization", "Bearer "+userToken)
	rr1 := httptest.NewRecorder()
	adminOnlyHandler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusForbidden {
		t.Errorf("expected 403 for user role on admin endpoint, got %d", rr1.Code)
	}

	// Admin role accessing admin route -> 200 OK
	req2 := httptest.NewRequest("GET", "/admin", nil)
	req2.Header.Set("Authorization", "Bearer "+adminToken)
	rr2 := httptest.NewRecorder()
	adminOnlyHandler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("expected 200 for admin role on admin endpoint, got %d", rr2.Code)
	}
}
