package views_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/models"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/views"
)

func TestCursor_EncodeAndDecode(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	id := "550e8400-e29b-41d4-a716-446655440000"

	cursor := views.EncodeCursor(now, id)
	if cursor == "" {
		t.Fatal("expected non-empty cursor string")
	}

	decodedTime, decodedID, err := views.DecodeCursor(cursor)
	if err != nil {
		t.Fatalf("unexpected error decoding valid cursor: %v", err)
	}

	if !decodedTime.Equal(now) {
		t.Errorf("expected time %v, got %v", now, decodedTime)
	}
	if decodedID != id {
		t.Errorf("expected id %s, got %s", id, decodedID)
	}
}

func TestCursor_DecodeErrors(t *testing.T) {
	tests := []struct {
		name      string
		cursor    string
		expectErr string
	}{
		{
			name:      "Empty cursor",
			cursor:    "",
			expectErr: "empty cursor",
		},
		{
			name:      "Invalid base64 encoding",
			cursor:    "!!!not-base64!!!",
			expectErr: "invalid cursor encoding",
		},
		{
			name:      "Malformed cursor missing colon separator",
			cursor:    base64.RawURLEncoding.EncodeToString([]byte("123456789")),
			expectErr: "malformed cursor structure",
		},
		{
			name:      "Invalid timestamp before colon",
			cursor:    base64.RawURLEncoding.EncodeToString([]byte("abc:some-id")),
			expectErr: "invalid cursor timestamp",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := views.DecodeCursor(tc.cursor)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.expectErr)
			}
		})
	}
}

func TestToPublicUser_Security_NoEmailOrBirthdate(t *testing.T) {
	assetID := "asset-uuid-123"
	user := &models.User{
		ID:             "user-uuid-456",
		Username:       "testuser",
		Email:          "secret_user@example.com",
		DisplayName:    "Test Display",
		Birthdate:      time.Date(2000, 1, 15, 0, 0, 0, 0, time.UTC),
		AvatarAssetID:  &assetID,
		Role:           "user",
		FollowerCount:  10,
		FollowingCount: 5,
		CreatedAt:      time.Now().UTC(),
	}

	avatarURLFn := func(id string) string {
		return "https://cdn.example.com/" + id
	}

	pub := views.ToPublicUser(user, avatarURLFn)

	if pub.ID != user.ID || pub.Username != user.Username || pub.DisplayName != user.DisplayName {
		t.Errorf("mismatched user details: %+v", pub)
	}
	if pub.AvatarURL == nil || *pub.AvatarURL != "https://cdn.example.com/asset-uuid-123" {
		t.Errorf("expected avatar URL https://cdn.example.com/asset-uuid-123, got %v", pub.AvatarURL)
	}

	// Verify JSON marshaling does NOT expose email or birthdate
	bytes, err := json.Marshal(pub)
	if err != nil {
		t.Fatalf("failed to marshal PublicUserResponse: %v", err)
	}
	jsonStr := string(bytes)

	if contains(jsonStr, "email") || contains(jsonStr, "secret_user@example.com") {
		t.Errorf("CRITICAL SECURITY LEAK: PublicUserResponse contains email: %s", jsonStr)
	}
	if contains(jsonStr, "birthdate") || contains(jsonStr, "2000-01-15") {
		t.Errorf("CRITICAL SECURITY LEAK: PublicUserResponse contains birthdate: %s", jsonStr)
	}
}

func TestToPrivateUser(t *testing.T) {
	user := &models.User{
		ID:             "user-uuid-789",
		Username:       "privateuser",
		Email:          "private@example.com",
		DisplayName:    "Private Name",
		Birthdate:      time.Date(1995, 5, 20, 0, 0, 0, 0, time.UTC),
		AvatarAssetID:  nil,
		Role:           "admin",
		FollowerCount:  100,
		FollowingCount: 50,
		CreatedAt:      time.Now().UTC(),
	}

	avatarURLFn := func(id string) string { return "https://cdn/" + id }

	priv := views.ToPrivateUser(user, avatarURLFn)

	if priv.Email != "private@example.com" {
		t.Errorf("expected private user email 'private@example.com', got %s", priv.Email)
	}
	if priv.Birthdate != "1995-05-20" {
		t.Errorf("expected birthdate '1995-05-20', got %s", priv.Birthdate)
	}
	if priv.AvatarURL != nil {
		t.Errorf("expected nil avatar URL, got %v", priv.AvatarURL)
	}
}

func TestToPublicUsers(t *testing.T) {
	users := []*models.User{
		{ID: "1", Username: "user1", DisplayName: "One", CreatedAt: time.Now()},
		{ID: "2", Username: "user2", DisplayName: "Two", CreatedAt: time.Now()},
	}
	avatarURLFn := func(id string) string { return "" }

	res := views.ToPublicUsers(users, avatarURLFn)
	if len(res) != 2 {
		t.Fatalf("expected 2 users, got %d", len(res))
	}
	if res[0].Username != "user1" || res[1].Username != "user2" {
		t.Errorf("unexpected users list: %+v", res)
	}
}

func TestWriteProblem_RFC9457(t *testing.T) {
	t.Run("With existing request ID in context", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test-endpoint", nil)
		ctx := views.WithReqID(req.Context(), "custom-req-id-12345")
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		views.WriteProblem(w, req, http.StatusNotFound, "Item not found")

		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", w.Code)
		}
		if w.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("expected Content-Type application/problem+json, got %s", w.Header().Get("Content-Type"))
		}

		var problem views.ProblemDetail
		if err := json.NewDecoder(w.Body).Decode(&problem); err != nil {
			t.Fatalf("failed to decode problem detail: %v", err)
		}

		if problem.Status != 404 || problem.Detail != "Item not found" || problem.RequestID != "custom-req-id-12345" {
			t.Errorf("unexpected problem detail: %+v", problem)
		}
	})

	t.Run("Without request ID in context generates fallback ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test-endpoint", nil)
		w := httptest.NewRecorder()
		views.WriteProblem(w, req, http.StatusBadRequest, "Invalid input")

		var problem views.ProblemDetail
		if err := json.NewDecoder(w.Body).Decode(&problem); err != nil {
			t.Fatalf("failed to decode problem detail: %v", err)
		}

		if problem.RequestID == "" {
			t.Error("expected auto-generated request ID, got empty string")
		}
	})
}

func TestReqID_ContextHelper(t *testing.T) {
	ctx := context.Background()
	if id := views.GetReqID(ctx); id != "" {
		t.Errorf("expected empty string for empty context, got %s", id)
	}
	if id := views.GetReqID(nil); id != "" {
		t.Errorf("expected empty string for nil context, got %s", id)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
