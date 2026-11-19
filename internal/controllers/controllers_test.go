package controllers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/config"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/controllers"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/db"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/mailer"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/middleware"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/models"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/services"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/testhelper"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type mockStorage struct{}

func (m *mockStorage) CreateSignedUploadURL(ctx context.Context, bucket, key string) (string, error) {
	return "https://storage.supabase.co/" + bucket + "/" + key, nil
}
func (m *mockStorage) VerifyObject(ctx context.Context, bucket, key string, expectedSize int64) error {
	return nil
}
func (m *mockStorage) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	return nil
}

type testFixture struct {
	cfg        *config.Config
	repo       *models.Repository
	mockMailer *mailer.MockMailer
	router     *mux.Router

	authCtrl        *controllers.AuthController
	userCtrl        *controllers.UserController
	postCtrl        *controllers.PostController
	interactionCtrl *controllers.InteractionController
	commentCtrl     *controllers.CommentController
	assetCtrl       *controllers.AssetController
	searchCtrl      *controllers.SearchController
}

func setupFullTestApp(t *testing.T) *testFixture {
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
	storageClient := &mockStorage{}

	authService := services.NewAuthService(repo, mockMailer, cfg)
	userService := services.NewUserService(repo, cfg.StoragePublicBaseURL)
	postService := services.NewPostService(repo, cfg.StoragePublicBaseURL)
	commentService := services.NewCommentService(repo, cfg.StoragePublicBaseURL)
	assetService := services.NewAssetService(repo, storageClient, cfg.StorageBucket, cfg.StoragePublicBaseURL)
	searchService := services.NewSearchService(repo, cfg.StoragePublicBaseURL)

	authCtrl := controllers.NewAuthController(authService, cfg.JWTSecret)
	userCtrl := controllers.NewUserController(userService)
	postCtrl := controllers.NewPostController(postService)
	interactionCtrl := controllers.NewInteractionController(postService)
	commentCtrl := controllers.NewCommentController(commentService)
	assetCtrl := controllers.NewAssetController(assetService)
	searchCtrl := controllers.NewSearchController(searchService)

	r := mux.NewRouter()

	// Auth
	r.HandleFunc("/auth/request-code", authCtrl.RequestCode).Methods(http.MethodPost)
	r.HandleFunc("/auth/verify", authCtrl.Verify).Methods(http.MethodPost)
	r.HandleFunc("/auth/signup", authCtrl.Signup).Methods(http.MethodPost)
	r.HandleFunc("/auth/refresh", authCtrl.Refresh).Methods(http.MethodPost)
	r.HandleFunc("/auth/logout", authCtrl.Logout).Methods(http.MethodPost)

	// Users
	r.HandleFunc("/users/{username}", userCtrl.GetProfile).Methods(http.MethodGet)
	r.HandleFunc("/me", userCtrl.GetMe).Methods(http.MethodGet)
	r.HandleFunc("/me", userCtrl.UpdateMe).Methods(http.MethodPatch)
	r.HandleFunc("/users/{id}/follow", userCtrl.Follow).Methods(http.MethodPut)
	r.HandleFunc("/users/{id}/follow", userCtrl.Unfollow).Methods(http.MethodDelete)
	r.HandleFunc("/users/{id}/followers", userCtrl.GetFollowers).Methods(http.MethodGet)
	r.HandleFunc("/users/{id}/following", userCtrl.GetFollowing).Methods(http.MethodGet)

	// Posts
	r.HandleFunc("/posts", postCtrl.CreatePost).Methods(http.MethodPost)
	r.HandleFunc("/posts/{id}", postCtrl.GetPost).Methods(http.MethodGet)
	r.HandleFunc("/posts/{id}", postCtrl.UpdatePost).Methods(http.MethodPatch)
	r.HandleFunc("/posts/{id}", postCtrl.DeletePost).Methods(http.MethodDelete)
	r.HandleFunc("/feed", postCtrl.GetFeed).Methods(http.MethodGet)
	r.HandleFunc("/users/{id}/posts", postCtrl.GetUserPosts).Methods(http.MethodGet)

	// Interactions
	r.HandleFunc("/posts/{id}/like", interactionCtrl.Like).Methods(http.MethodPut)
	r.HandleFunc("/posts/{id}/like", interactionCtrl.Unlike).Methods(http.MethodDelete)
	r.HandleFunc("/posts/{id}/share", interactionCtrl.Share).Methods(http.MethodPost)

	// Comments
	r.HandleFunc("/posts/{id}/comments", commentCtrl.CreateComment).Methods(http.MethodPost)
	r.HandleFunc("/posts/{id}/comments", commentCtrl.GetComments).Methods(http.MethodGet)
	r.HandleFunc("/comments/{id}", commentCtrl.DeleteComment).Methods(http.MethodDelete)

	// Assets
	r.HandleFunc("/assets", assetCtrl.CreateAsset).Methods(http.MethodPost)
	r.HandleFunc("/assets/{id}/complete", assetCtrl.CompleteAsset).Methods(http.MethodPost)

	// Search
	r.HandleFunc("/search", searchCtrl.Search).Methods(http.MethodGet)

	return &testFixture{
		cfg:             cfg,
		repo:            repo,
		mockMailer:      mockMailer,
		router:          r,
		authCtrl:        authCtrl,
		userCtrl:        userCtrl,
		postCtrl:        postCtrl,
		interactionCtrl: interactionCtrl,
		commentCtrl:     commentCtrl,
		assetCtrl:       assetCtrl,
		searchCtrl:      searchCtrl,
	}
}

func createUser(t *testing.T, f *testFixture, username, role string) *models.User {
	email := fmt.Sprintf("%s@example.com", username)
	u, err := f.repo.CreateUser(context.Background(), username, email, "Display "+username, time.Date(1998, 1, 1, 0, 0, 0, 0, time.UTC), role)
	if err != nil {
		t.Fatalf("failed to create user %s: %v", username, err)
	}
	return u
}

func executeRequest(r *mux.Router, method, path string, body any, userID, role string) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != nil {
		if b, ok := body.([]byte); ok {
			bodyReader = bytes.NewReader(b)
		} else if s, ok := body.(string); ok {
			bodyReader = strings.NewReader(s)
		} else {
			j, _ := json.Marshal(body)
			bodyReader = bytes.NewReader(j)
		}
	}

	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")

	ctx := req.Context()
	if userID != "" {
		ctx = context.WithValue(ctx, middleware.UserIDKey, userID)
	}
	if role != "" {
		ctx = context.WithValue(ctx, middleware.RoleKey, role)
	}
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

// ---------------------------------------------------------------------------
// AuthController Tests
// ---------------------------------------------------------------------------

func TestAuthController_AllEndpoints(t *testing.T) {
	f := setupFullTestApp(t)

	t.Run("RequestCode validation and accepted response", func(t *testing.T) {
		// Invalid JSON
		rr := executeRequest(f.router, "POST", "/auth/request-code", "invalid-json", "", "")
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for malformed JSON, got %d", rr.Code)
		}

		// Empty/invalid email
		rr = executeRequest(f.router, "POST", "/auth/request-code", map[string]string{"email": "not-an-email"}, "", "")
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for email without @, got %d", rr.Code)
		}

		// Valid email -> 202 Accepted
		rr = executeRequest(f.router, "POST", "/auth/request-code", map[string]string{"email": "realuser@example.com"}, "", "")
		if rr.Code != http.StatusAccepted {
			t.Errorf("expected 202 Accepted, got %d", rr.Code)
		}
	})

	t.Run("Verify and Signup Full Lifecycle", func(t *testing.T) {
		email := "newsignup@example.com"
		// Request code
		_ = executeRequest(f.router, "POST", "/auth/request-code", map[string]string{"email": email}, "", "")

		// Extract OTP from mock mailer
		if len(f.mockMailer.SentMessages) == 0 {
			t.Fatal("expected email to be sent")
		}
		lastMsg := f.mockMailer.SentMessages[len(f.mockMailer.SentMessages)-1]
		// Body contains code
		words := strings.Fields(lastMsg.Body)
		var otpCode string
		for _, w := range words {
			if len(w) == 6 && w[0] >= '0' && w[0] <= '9' {
				otpCode = w
				break
			}
		}

		// Verify invalid code
		rr := executeRequest(f.router, "POST", "/auth/verify", map[string]string{"email": email, "code": "000000"}, "", "")
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for wrong code, got %d", rr.Code)
		}

		// Verify valid code for new user
		rr = executeRequest(f.router, "POST", "/auth/verify", map[string]string{"email": email, "code": otpCode}, "", "")
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for valid code, got %d: %s", rr.Code, rr.Body.String())
		}

		var verifyRes struct {
			Action      string `json:"action"`
			SignupToken string `json:"signupToken"`
		}
		_ = json.NewDecoder(rr.Body).Decode(&verifyRes)
		if verifyRes.Action != "signup" || verifyRes.SignupToken == "" {
			t.Fatalf("expected signup action with signup token, got %+v", verifyRes)
		}

		// Signup under 13 years old -> 422
		rr = executeRequest(f.router, "POST", "/auth/signup", map[string]string{
			"signupToken": verifyRes.SignupToken,
			"username":    "younguser",
			"displayName": "Young User",
			"birthdate":   time.Now().Format("2006-01-02"), // today!
		}, "", "")
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for underage signup, got %d", rr.Code)
		}

		// Signup with invalid username format (e.g. spaces) -> 422
		rr = executeRequest(f.router, "POST", "/auth/signup", map[string]string{
			"signupToken": verifyRes.SignupToken,
			"username":    "invalid user!",
			"displayName": "Valid Display",
			"birthdate":   "2000-01-01",
		}, "", "")
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for invalid username regex, got %d", rr.Code)
		}

		// Signup success -> 201 Created
		rr = executeRequest(f.router, "POST", "/auth/signup", map[string]string{
			"signupToken": verifyRes.SignupToken,
			"username":    "validsignup",
			"displayName": "Valid Display",
			"birthdate":   "2000-01-01",
		}, "", "")
		if rr.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for valid signup, got %d: %s", rr.Code, rr.Body.String())
		}

		var signupRes struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
		}
		_ = json.NewDecoder(rr.Body).Decode(&signupRes)
		if signupRes.AccessToken == "" || signupRes.RefreshToken == "" {
			t.Fatalf("expected tokens in signup response, got %+v", signupRes)
		}

		// Refresh token success
		rr = executeRequest(f.router, "POST", "/auth/refresh", map[string]string{
			"refreshToken": signupRes.RefreshToken,
		}, "", "")
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for token refresh, got %d: %s", rr.Code, rr.Body.String())
		}

		// Logout without Authorization header -> 401
		reqLogout := httptest.NewRequest("POST", "/auth/logout", nil)
		rrLogout := httptest.NewRecorder()
		f.router.ServeHTTP(rrLogout, reqLogout)
		if rrLogout.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 on logout without auth header, got %d", rrLogout.Code)
		}

		// Logout with valid token -> 204 No Content
		reqLogout = httptest.NewRequest("POST", "/auth/logout", bytes.NewReader([]byte(`{}`)))
		reqLogout.Header.Set("Authorization", "Bearer "+signupRes.AccessToken)
		rrLogout = httptest.NewRecorder()
		f.router.ServeHTTP(rrLogout, reqLogout)
		if rrLogout.Code != http.StatusNoContent {
			t.Errorf("expected 204 No Content on logout, got %d", rrLogout.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// UserController Tests
// ---------------------------------------------------------------------------

func TestUserController_AllEndpoints(t *testing.T) {
	f := setupFullTestApp(t)
	u1 := createUser(t, f, "userctrl1", "user")
	u2 := createUser(t, f, "userctrl2", "user")

	t.Run("GetProfile", func(t *testing.T) {
		// Non-existent user -> 404
		rr := executeRequest(f.router, "GET", "/users/nonexistent", nil, "", "")
		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404 for non-existent profile, got %d", rr.Code)
		}

		// Existing user -> 200
		rr = executeRequest(f.router, "GET", "/users/userctrl1", nil, "", "")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for existing profile, got %d", rr.Code)
		}
	})

	t.Run("GetMe and UpdateMe", func(t *testing.T) {
		// GetMe without auth -> 401
		rr := executeRequest(f.router, "GET", "/me", nil, "", "")
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for unauthenticated GetMe, got %d", rr.Code)
		}

		// GetMe with auth -> 200
		rr = executeRequest(f.router, "GET", "/me", nil, u1.ID, "user")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for authenticated GetMe, got %d", rr.Code)
		}

		// UpdateMe with invalid JSON -> 400
		rr = executeRequest(f.router, "PATCH", "/me", "invalid-json", u1.ID, "user")
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid JSON in UpdateMe, got %d", rr.Code)
		}

		// UpdateMe with invalid displayName (>50 chars) -> 422
		longName := strings.Repeat("A", 51)
		rr = executeRequest(f.router, "PATCH", "/me", map[string]string{"displayName": longName}, u1.ID, "user")
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for displayName > 50 chars, got %d", rr.Code)
		}

		// UpdateMe with invalid UUID format for avatar -> 422
		rr = executeRequest(f.router, "PATCH", "/me", map[string]string{"avatarAssetId": "not-a-uuid"}, u1.ID, "user")
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for invalid avatar UUID, got %d", rr.Code)
		}

		// UpdateMe valid displayName -> 200
		rr = executeRequest(f.router, "PATCH", "/me", map[string]string{"displayName": "Updated Name"}, u1.ID, "user")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for valid UpdateMe, got %d", rr.Code)
		}
	})

	t.Run("Follow and Unfollow", func(t *testing.T) {
		// Follow without auth -> 401
		rr := executeRequest(f.router, "PUT", fmt.Sprintf("/users/%s/follow", u2.ID), nil, "", "")
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for unauthenticated Follow, got %d", rr.Code)
		}

		// Follow self -> 422
		rr = executeRequest(f.router, "PUT", fmt.Sprintf("/users/%s/follow", u1.ID), nil, u1.ID, "user")
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for self-follow, got %d", rr.Code)
		}

		// Follow non-existent user -> 404
		rr = executeRequest(f.router, "PUT", fmt.Sprintf("/users/%s/follow", uuid.NewString()), nil, u1.ID, "user")
		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404 following non-existent user, got %d", rr.Code)
		}

		// Follow valid user -> 200
		rr = executeRequest(f.router, "PUT", fmt.Sprintf("/users/%s/follow", u2.ID), nil, u1.ID, "user")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 following valid user, got %d", rr.Code)
		}

		// GetFollowers and GetFollowing
		rr = executeRequest(f.router, "GET", fmt.Sprintf("/users/%s/followers?limit=10", u2.ID), nil, "", "")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for GetFollowers, got %d", rr.Code)
		}

		rr = executeRequest(f.router, "GET", fmt.Sprintf("/users/%s/following?limit=10", u1.ID), nil, "", "")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for GetFollowing, got %d", rr.Code)
		}

		// Unfollow -> 200
		rr = executeRequest(f.router, "DELETE", fmt.Sprintf("/users/%s/follow", u2.ID), nil, u1.ID, "user")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for Unfollow, got %d", rr.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// PostController & InteractionController Tests
// ---------------------------------------------------------------------------

func TestPostAndInteractionController(t *testing.T) {
	f := setupFullTestApp(t)
	u1 := createUser(t, f, "postauthor", "user")
	u2 := createUser(t, f, "otheruser", "user")
	admin := createUser(t, f, "adminuser", "admin")

	t.Run("CreatePost validation and success", func(t *testing.T) {
		// Unauthenticated -> 401
		rr := executeRequest(f.router, "POST", "/posts", map[string]string{"body": "Hello"}, "", "")
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for unauthenticated CreatePost, got %d", rr.Code)
		}

		// Invalid JSON -> 400
		rr = executeRequest(f.router, "POST", "/posts", "invalid-json", u1.ID, "user")
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid JSON, got %d", rr.Code)
		}

		// Body > 2000 chars -> 422
		longBody := strings.Repeat("B", 2001)
		rr = executeRequest(f.router, "POST", "/posts", map[string]string{"body": longBody}, u1.ID, "user")
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for body > 2000 chars, got %d", rr.Code)
		}

		// Valid post -> 201
		rr = executeRequest(f.router, "POST", "/posts", map[string]string{"body": "Valid post body"}, u1.ID, "user")
		if rr.Code != http.StatusCreated {
			t.Fatalf("expected 201 for valid CreatePost, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("GetPost, UpdatePost, Like, Share, DeletePost", func(t *testing.T) {
		// Create a post
		rr := executeRequest(f.router, "POST", "/posts", map[string]string{"body": "Post to interact"}, u1.ID, "user")
		var postRes struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(rr.Body).Decode(&postRes)
		postID := postRes.ID

		// GetPost -> 200
		rr = executeRequest(f.router, "GET", "/posts/"+postID, nil, "", "")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for GetPost, got %d", rr.Code)
		}

		// Get non-existent post -> 404
		rr = executeRequest(f.router, "GET", "/posts/"+uuid.NewString(), nil, "", "")
		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404 for non-existent post, got %d", rr.Code)
		}

		// UpdatePost by non-author -> 403 Forbidden
		rr = executeRequest(f.router, "PATCH", "/posts/"+postID, map[string]string{"body": "Hacked body"}, u2.ID, "user")
		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403 for non-author UpdatePost, got %d", rr.Code)
		}

		// UpdatePost by author -> 200 OK
		rr = executeRequest(f.router, "PATCH", "/posts/"+postID, map[string]string{"body": "Updated body by author"}, u1.ID, "user")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for author UpdatePost, got %d", rr.Code)
		}

		// Like post -> 200 OK
		rr = executeRequest(f.router, "PUT", "/posts/"+postID+"/like", nil, u2.ID, "user")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for Like, got %d", rr.Code)
		}

		// Unlike post -> 200 OK
		rr = executeRequest(f.router, "DELETE", "/posts/"+postID+"/like", nil, u2.ID, "user")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for Unlike, got %d", rr.Code)
		}

		// Share post with valid quote -> 201 Created
		quote := "Great post!"
		rr = executeRequest(f.router, "POST", "/posts/"+postID+"/share", map[string]*string{"quote": &quote}, u2.ID, "user")
		if rr.Code != http.StatusCreated {
			t.Errorf("expected 201 for SharePost, got %d", rr.Code)
		}

		// Share post with quote > 500 chars -> 422
		longQuote := strings.Repeat("Q", 501)
		rr = executeRequest(f.router, "POST", "/posts/"+postID+"/share", map[string]*string{"quote": &longQuote}, u2.ID, "user")
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for quote > 500 chars, got %d", rr.Code)
		}

		// DeletePost by non-author -> 403 Forbidden
		rr = executeRequest(f.router, "DELETE", "/posts/"+postID, nil, u2.ID, "user")
		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403 for non-author DeletePost, got %d", rr.Code)
		}

		// DeletePost by admin -> 204 No Content
		rr = executeRequest(f.router, "DELETE", "/posts/"+postID, nil, admin.ID, "admin")
		if rr.Code != http.StatusNoContent {
			t.Errorf("expected 204 for admin DeletePost, got %d", rr.Code)
		}
	})

	t.Run("GetFeed and GetUserPosts", func(t *testing.T) {
		// Feed without auth -> 401
		rr := executeRequest(f.router, "GET", "/feed", nil, "", "")
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for unauthenticated feed, got %d", rr.Code)
		}

		// Feed with auth -> 200
		rr = executeRequest(f.router, "GET", "/feed?limit=10", nil, u1.ID, "user")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for authenticated feed, got %d", rr.Code)
		}

		// User posts -> 200
		rr = executeRequest(f.router, "GET", fmt.Sprintf("/users/%s/posts?limit=10", u1.ID), nil, "", "")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for GetUserPosts, got %d", rr.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// CommentController Tests
// ---------------------------------------------------------------------------

func TestCommentController_AllEndpoints(t *testing.T) {
	f := setupFullTestApp(t)
	author := createUser(t, f, "cmtuser1", "user")
	commenter := createUser(t, f, "cmtuser2", "user")
	other := createUser(t, f, "cmtuser3", "user")

	// Create post
	rr := executeRequest(f.router, "POST", "/posts", map[string]string{"body": "Post for comments"}, author.ID, "user")
	var postRes struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&postRes)
	postID := postRes.ID

	t.Run("CreateComment", func(t *testing.T) {
		// Unauthenticated -> 401
		rr := executeRequest(f.router, "POST", "/posts/"+postID+"/comments", map[string]string{"body": "cmt"}, "", "")
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for unauthenticated CreateComment, got %d", rr.Code)
		}

		// Empty body -> 422
		rr = executeRequest(f.router, "POST", "/posts/"+postID+"/comments", map[string]string{"body": ""}, commenter.ID, "user")
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for empty comment body, got %d", rr.Code)
		}

		// Body > 1000 chars -> 422
		longBody := strings.Repeat("C", 1001)
		rr = executeRequest(f.router, "POST", "/posts/"+postID+"/comments", map[string]string{"body": longBody}, commenter.ID, "user")
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for comment body > 1000 chars, got %d", rr.Code)
		}

		// Valid comment -> 201 Created
		rr = executeRequest(f.router, "POST", "/posts/"+postID+"/comments", map[string]string{"body": "Nice post!"}, commenter.ID, "user")
		if rr.Code != http.StatusCreated {
			t.Fatalf("expected 201 for valid CreateComment, got %d", rr.Code)
		}
	})

	t.Run("GetComments and DeleteComment", func(t *testing.T) {
		// Create comment
		rr := executeRequest(f.router, "POST", "/posts/"+postID+"/comments", map[string]string{"body": "To be deleted"}, commenter.ID, "user")
		var cmtRes struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(rr.Body).Decode(&cmtRes)
		commentID := cmtRes.ID

		// GetComments -> 200
		rr = executeRequest(f.router, "GET", "/posts/"+postID+"/comments?limit=10", nil, "", "")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for GetComments, got %d", rr.Code)
		}

		// DeleteComment by non-author -> 403 Forbidden
		rr = executeRequest(f.router, "DELETE", "/comments/"+commentID, nil, other.ID, "user")
		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403 for non-author DeleteComment, got %d", rr.Code)
		}

		// DeleteComment by author -> 204 No Content
		rr = executeRequest(f.router, "DELETE", "/comments/"+commentID, nil, commenter.ID, "user")
		if rr.Code != http.StatusNoContent {
			t.Errorf("expected 204 for author DeleteComment, got %d", rr.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// SearchController Tests
// ---------------------------------------------------------------------------

func TestSearchController_AllEndpoints(t *testing.T) {
	f := setupFullTestApp(t)

	// Missing query 'q' -> 422
	rr := executeRequest(f.router, "GET", "/search?type=users", nil, "", "")
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 when q is missing, got %d", rr.Code)
	}

	// Missing type -> 422
	rr = executeRequest(f.router, "GET", "/search?q=test", nil, "", "")
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 when type is missing, got %d", rr.Code)
	}

	// Invalid type -> 422
	rr = executeRequest(f.router, "GET", "/search?q=test&type=invalid", nil, "", "")
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 when type is invalid, got %d", rr.Code)
	}

	// Valid searches -> 200
	for _, searchType := range []string{"users", "posts", "comments"} {
		rr = executeRequest(f.router, "GET", fmt.Sprintf("/search?q=hello&type=%s&limit=10", searchType), nil, "", "")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for type=%s, got %d", searchType, rr.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// AssetController Tests
// ---------------------------------------------------------------------------

func TestAssetController_AllEndpoints(t *testing.T) {
	f := setupFullTestApp(t)
	u := createUser(t, f, "assetctrl", "user")

	t.Run("CreateAsset", func(t *testing.T) {
		// Unauthenticated -> 401
		rr := executeRequest(f.router, "POST", "/assets", map[string]any{"mimeType": "image/png", "sizeBytes": 1000}, "", "")
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for unauthenticated CreateAsset, got %d", rr.Code)
		}

		// Size > 5MB -> 413
		rr = executeRequest(f.router, "POST", "/assets", map[string]any{"mimeType": "image/png", "sizeBytes": 6000000}, u.ID, "user")
		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("expected 413 for asset size > 5MB, got %d", rr.Code)
		}

		// Unsupported MIME -> 422
		rr = executeRequest(f.router, "POST", "/assets", map[string]any{"mimeType": "application/pdf", "sizeBytes": 1000}, u.ID, "user")
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for unsupported MIME type, got %d", rr.Code)
		}

		// Valid asset -> 201
		rr = executeRequest(f.router, "POST", "/assets", map[string]any{"mimeType": "image/png", "sizeBytes": 2048}, u.ID, "user")
		if rr.Code != http.StatusCreated {
			t.Fatalf("expected 201 for valid CreateAsset, got %d", rr.Code)
		}

		var res struct {
			AssetID string `json:"assetId"`
		}
		_ = json.NewDecoder(rr.Body).Decode(&res)

		// CompleteAsset -> 200
		rr = executeRequest(f.router, "POST", fmt.Sprintf("/assets/%s/complete", res.AssetID), nil, u.ID, "user")
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for CompleteAsset, got %d: %s", rr.Code, rr.Body.String())
		}

		// CompleteAsset again -> 409 Conflict (already ready)
		rr = executeRequest(f.router, "POST", fmt.Sprintf("/assets/%s/complete", res.AssetID), nil, u.ID, "user")
		if rr.Code != http.StatusConflict {
			t.Errorf("expected 409 for already completed asset, got %d", rr.Code)
		}
	})
}
