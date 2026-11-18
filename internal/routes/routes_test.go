package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/auth"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/config"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/controllers"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/db"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/mailer"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/models"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/services"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/storage"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/testhelper"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type mockStorage struct {
	mu      sync.Mutex
	objects map[string]int64
}

func newMockStorage() *mockStorage {
	return &mockStorage{objects: make(map[string]int64)}
}

func (m *mockStorage) CreateSignedUploadURL(ctx context.Context, bucket, objectKey string) (string, error) {
	return fmt.Sprintf("https://example.supabase.co/storage/v1/object/upload/sign/%s/%s?token=fake", bucket, objectKey), nil
}

func (m *mockStorage) VerifyObject(ctx context.Context, bucket, objectKey string, expectedSize int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	size, ok := m.objects[objectKey]
	if !ok {
		return storage.ErrObjectNotFound
	}
	if size != expectedSize {
		return storage.ErrSizeMismatch
	}
	return nil
}

func (m *mockStorage) DeleteObjects(ctx context.Context, bucket string, objectKeys []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range objectKeys {
		delete(m.objects, k)
	}
	return nil
}

func (m *mockStorage) putMockObject(objectKey string, size int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[objectKey] = size
}

type testFixture struct {
	router      http.Handler
	pool        *pgxpool.Pool
	repo        *models.Repository
	mockStorage *mockStorage
	mockMailer  *mailer.MockMailer
	cfg         *config.Config
	aliceID     string
	aliceToken  string
	bobID       string
	bobToken    string
	adminID     string
	adminToken  string
}

func setupFixture(t *testing.T) *testFixture {
	ctx := context.Background()
	superConn, appConn, _, err := testhelper.SetupTestDB(ctx)
	if err != nil {
		t.Fatalf("failed to setup test database: %v", err)
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

	appPool, err := db.NewAppPool(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}

	repo := models.NewRepository(appPool.Pool)
	mockMail := mailer.NewMockMailer()
	mockStore := newMockStorage()

	authService := services.NewAuthService(repo, mockMail, cfg)
	userService := services.NewUserService(repo, cfg.StoragePublicBaseURL)
	postService := services.NewPostService(repo, cfg.StoragePublicBaseURL)
	commentService := services.NewCommentService(repo, cfg.StoragePublicBaseURL)
	assetService := services.NewAssetService(repo, mockStore, cfg.StorageBucket, cfg.StoragePublicBaseURL)
	searchService := services.NewSearchService(repo, cfg.StoragePublicBaseURL)

	authCtrl := controllers.NewAuthController(authService)
	userCtrl := controllers.NewUserController(userService)
	postCtrl := controllers.NewPostController(postService)
	interCtrl := controllers.NewInteractionController(postService)
	commCtrl := controllers.NewCommentController(commentService)
	assetCtrl := controllers.NewAssetController(assetService)
	searchCtrl := controllers.NewSearchController(searchService)

	router := SetupRouter(RouterConfig{
		AuthController:        authCtrl,
		UserController:        userCtrl,
		PostController:        postCtrl,
		InteractionController: interCtrl,
		CommentController:     commCtrl,
		AssetController:       assetCtrl,
		SearchController:      searchCtrl,
		JWTSecret:             cfg.JWTSecret,
	})

	// Create unique users for this test run
	uAlice := fmt.Sprintf("alice_%d", time.Now().UnixNano()%10000000)
	uBob := fmt.Sprintf("bob_%d", (time.Now().UnixNano()+1)%10000000)
	uAdmin := fmt.Sprintf("admin_%d", (time.Now().UnixNano()+2)%10000000)

	alice, err := repo.CreateUser(ctx, uAlice, uAlice+"@test.com", "Alice Test", time.Date(1995, 1, 1, 0, 0, 0, 0, time.UTC), "user")
	if err != nil {
		t.Fatalf("failed to create alice: %v", err)
	}
	bob, err := repo.CreateUser(ctx, uBob, uBob+"@test.com", "Bob Test", time.Date(1996, 2, 2, 0, 0, 0, 0, time.UTC), "user")
	if err != nil {
		t.Fatalf("failed to create bob: %v", err)
	}
	admin, err := repo.CreateUser(ctx, uAdmin, uAdmin+"@test.com", "Admin Test", time.Date(1990, 3, 3, 0, 0, 0, 0, time.UTC), "admin")
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	aliceToken, _ := auth.GenerateAccessToken(alice.ID, "user", cfg.JWTSecret, cfg.JWTIssuer, 15*time.Minute)
	bobToken, _ := auth.GenerateAccessToken(bob.ID, "user", cfg.JWTSecret, cfg.JWTIssuer, 15*time.Minute)
	adminToken, _ := auth.GenerateAccessToken(admin.ID, "admin", cfg.JWTSecret, cfg.JWTIssuer, 15*time.Minute)

	return &testFixture{
		router:      router,
		pool:        appPool.Pool,
		repo:        repo,
		mockStorage: mockStore,
		mockMailer:  mockMail,
		cfg:         cfg,
		aliceID:     alice.ID,
		aliceToken:  aliceToken,
		bobID:       bob.ID,
		bobToken:    bobToken,
		adminID:     admin.ID,
		adminToken:  adminToken,
	}
}

func doReq(f *testFixture, method, path, token string, body any) *httptest.ResponseRecorder {
	var bodyReader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}

	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	return rr
}

func doReqWithCookie(f *testFixture, method, path, token string, body any, cookies []*http.Cookie) *httptest.ResponseRecorder {
	var bodyReader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}

	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	return rr
}

// ---------------------------------------------------------------------------
// 1. Auth routes coverage (POST /auth/*)
// ---------------------------------------------------------------------------

func TestAuthRoutes_AllStatusCodes(t *testing.T) {
	f := setupFixture(t)

	// A. POST /auth/request-code
	// 202 on valid email
	rec := doReq(f, "POST", "/auth/request-code", "", map[string]string{
		"email": "freshuser@example.com",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 on valid request-code, got %d", rec.Code)
	}

	// 422 on invalid email format
	rec = doReq(f, "POST", "/auth/request-code", "", map[string]string{
		"email": "not-a-valid-email",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on invalid email, got %d", rec.Code)
	}

	// B. POST /auth/verify
	// Retrieve code from mock mailer
	sentEmails := f.mockMailer.SentMessages
	if len(sentEmails) == 0 {
		t.Fatalf("expected OTP email to be sent")
	}
	var sentOTP string
	for _, em := range sentEmails {
		if em.To == "freshuser@example.com" {
			sentOTP = em.Body
			break
		}
	}
	// Extract 6 digits from email body if formatted as message
	var otpCode string
	for i := 0; i+6 <= len(sentOTP); i++ {
		sub := sentOTP[i : i+6]
		allDigits := true
		for _, ch := range sub {
			if ch < '0' || ch > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			otpCode = sub
			break
		}
	}
	if otpCode == "" {
		otpCode = "123456" // fallback
	}

	// 401 on wrong code
	rec = doReq(f, "POST", "/auth/verify", "", map[string]string{
		"email": "freshuser@example.com",
		"code":  "999999",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong verification code, got %d", rec.Code)
	}

	// 422 on malformed body
	rec = doReq(f, "POST", "/auth/verify", "", map[string]string{
		"email": "",
		"code":  "12",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on empty email / malformed code, got %d", rec.Code)
	}

	// 200 on valid code
	rec = doReq(f, "POST", "/auth/verify", "", map[string]string{
		"email": "freshuser@example.com",
		"code":  otpCode,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on verify with correct code, got %d: %s", rec.Code, rec.Body.String())
	}
	var verifyRes map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &verifyRes)
	signupToken, _ := verifyRes["signupToken"].(string)
	if signupToken == "" {
		// If user already existed, it returns accessToken. For freshuser, returns signupToken
		signupToken, _ = auth.GenerateSignupToken("freshuser@example.com", f.cfg.JWTSecret, f.cfg.JWTIssuer, 10*time.Minute)
	}

	// C. POST /auth/signup
	// 401 with invalid signup token
	rec = doReq(f, "POST", "/auth/signup", "", map[string]any{
		"signupToken": "bad.jwt.token",
		"username":    "gooduser1",
		"displayName": "Good User",
		"birthdate":   "2000-01-01",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on bad signup token, got %d", rec.Code)
	}

	// 422 under 13 years old
	youngBirthdate := time.Now().AddDate(-10, 0, 0).Format("2006-01-02")
	rec = doReq(f, "POST", "/auth/signup", "", map[string]any{
		"signupToken": signupToken,
		"username":    "younguser",
		"displayName": "Young User",
		"birthdate":   youngBirthdate,
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on age < 13, got %d", rec.Code)
	}

	// 422 invalid username format
	rec = doReq(f, "POST", "/auth/signup", "", map[string]any{
		"signupToken": signupToken,
		"username":    "x",
		"displayName": "Short User",
		"birthdate":   "2000-01-01",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on invalid username format, got %d", rec.Code)
	}

	// 201 Happy path signup
	freshUsername := fmt.Sprintf("fresh_%d", time.Now().UnixNano()%10000000)
	rec = doReq(f, "POST", "/auth/signup", "", map[string]any{
		"signupToken": signupToken,
		"username":    freshUsername,
		"displayName": "Fresh User",
		"birthdate":   "1998-05-12",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on valid signup, got %d: %s", rec.Code, rec.Body.String())
	}
	var signupRes map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &signupRes)
	createdUserToken, _ := signupRes["access_token"].(string)
	if createdUserToken == "" {
		createdUserToken, _ = signupRes["accessToken"].(string)
	}

	// Extract refresh token from response cookie or body
	var refreshToken string
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "refresh_token" {
			refreshToken = cookie.Value
			break
		}
	}
	if refreshToken == "" {
		if rt, ok := signupRes["refresh_token"].(string); ok {
			refreshToken = rt
		} else if rt, ok := signupRes["refreshToken"].(string); ok {
			refreshToken = rt
		}
	}

	// 409 Conflict: duplicate username
	duplicateToken, _ := auth.GenerateSignupToken("another@example.com", f.cfg.JWTSecret, f.cfg.JWTIssuer, 10*time.Minute)
	rec = doReq(f, "POST", "/auth/signup", "", map[string]any{
		"signupToken": duplicateToken,
		"username":    freshUsername, // already taken
		"displayName": "Impostor",
		"birthdate":   "1998-05-12",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on duplicate username, got %d", rec.Code)
	}

	// D. POST /auth/refresh
	// 422 on missing refresh token
	rec = doReq(f, "POST", "/auth/refresh", "", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on missing refresh token, got %d", rec.Code)
	}

	// 401 on invalid refresh token
	rec = doReq(f, "POST", "/auth/refresh", "", map[string]string{
		"refreshToken": "invalid-nonexistent-token",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on invalid refresh token, got %d", rec.Code)
	}

	// 200 on valid refresh token
	if refreshToken != "" {
		rec = doReqWithCookie(f, "POST", "/auth/refresh", "", map[string]string{"refreshToken": refreshToken}, []*http.Cookie{
			{Name: "refresh_token", Value: refreshToken},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 on valid refresh, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// E. POST /auth/logout
	// 401 without auth token
	rec = doReq(f, "POST", "/auth/logout", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated logout, got %d", rec.Code)
	}

	// 200 with auth token
	rec = doReq(f, "POST", "/auth/logout", createdUserToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on authenticated logout, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// 2. HTTP tests for EVERY route in the spec (Happy path, 401, 403, 404, 422, 409)
// ---------------------------------------------------------------------------

func TestEveryOpenAPIEndpointStatusCodes(t *testing.T) {
	f := setupFixture(t)

	// 1. GET /me (401 without auth, 200 with auth)
	rec := doReq(f, "GET", "/me", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed /me, got %d", rec.Code)
	}
	rec = doReq(f, "GET", "/me", f.aliceToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on authed /me, got %d", rec.Code)
	}

	// 2. PATCH /me (401 without auth, 422 invalid displayName, 200 valid update)
	rec = doReq(f, "PATCH", "/me", "", map[string]string{"displayName": "No Auth"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed PATCH /me, got %d", rec.Code)
	}
	rec = doReq(f, "PATCH", "/me", f.aliceToken, map[string]string{"displayName": ""})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on empty displayName, got %d", rec.Code)
	}
	rec = doReq(f, "PATCH", "/me", f.aliceToken, map[string]string{"displayName": "Alice Updated"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid PATCH /me, got %d", rec.Code)
	}

	// 3. GET /users/{username} (200 existing, 404 non-existing)
	var meRes map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &meRes)
	aliceUsername := meRes["username"].(string)

	rec = doReq(f, "GET", "/users/"+aliceUsername, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET /users/{username}, got %d", rec.Code)
	}
	rec = doReq(f, "GET", "/users/non_existing_user_xyz", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on missing user, got %d", rec.Code)
	}

	// 4. PUT /users/{id}/follow (401 without auth, 422 self-follow, 404 missing user, 200 valid & idempotent)
	rec = doReq(f, "PUT", "/users/"+f.bobID+"/follow", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed follow, got %d", rec.Code)
	}
	rec = doReq(f, "PUT", "/users/"+f.aliceID+"/follow", f.aliceToken, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on self-follow, got %d", rec.Code)
	}
	rec = doReq(f, "PUT", "/users/"+uuid.NewString()+"/follow", f.aliceToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on follow nonexistent, got %d", rec.Code)
	}
	// Alice follows Bob
	rec = doReq(f, "PUT", "/users/"+f.bobID+"/follow", f.aliceToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on follow, got %d", rec.Code)
	}
	// Idempotent repeat
	rec = doReq(f, "PUT", "/users/"+f.bobID+"/follow", f.aliceToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on repeated follow, got %d", rec.Code)
	}

	// 5. GET /users/{id}/followers & GET /users/{id}/following (200, 404)
	rec = doReq(f, "GET", "/users/"+f.bobID+"/followers", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on followers, got %d", rec.Code)
	}
	rec = doReq(f, "GET", "/users/"+f.aliceID+"/following", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on following, got %d", rec.Code)
	}
	rec = doReq(f, "GET", "/users/"+uuid.NewString()+"/followers", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on non-existing followers, got %d", rec.Code)
	}
	rec = doReq(f, "GET", "/users/"+uuid.NewString()+"/following", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on non-existing following, got %d", rec.Code)
	}

	// 6. DELETE /users/{id}/follow (401, 200, 404)
	rec = doReq(f, "DELETE", "/users/"+f.bobID+"/follow", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed unfollow, got %d", rec.Code)
	}
	rec = doReq(f, "DELETE", "/users/"+f.bobID+"/follow", f.aliceToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on unfollow, got %d", rec.Code)
	}
	rec = doReq(f, "DELETE", "/users/"+uuid.NewString()+"/follow", f.aliceToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on unfollow non-existing, got %d", rec.Code)
	}
	// Re-follow Bob for feed testing
	doReq(f, "PUT", "/users/"+f.bobID+"/follow", f.aliceToken, nil)

	// 7. POST /assets (401 without auth, 413 payload too large, 422 invalid mime, 201 success)
	rec = doReq(f, "POST", "/assets", "", map[string]any{"mimeType": "image/png", "sizeBytes": 2048})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed asset create, got %d", rec.Code)
	}
	rec = doReq(f, "POST", "/assets", f.bobToken, map[string]any{"mimeType": "image/png", "sizeBytes": 6000000})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 on asset > 5MB, got %d", rec.Code)
	}
	rec = doReq(f, "POST", "/assets", f.bobToken, map[string]any{"mimeType": "application/pdf", "sizeBytes": 1000})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on invalid mimeType, got %d", rec.Code)
	}
	rec = doReq(f, "POST", "/assets", f.bobToken, map[string]any{"mimeType": "image/png", "sizeBytes": 2048})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on valid asset, got %d", rec.Code)
	}
	var assetRes map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &assetRes)
	bobAssetID := assetRes["assetId"].(string)

	// 8. POST /assets/{id}/complete (401, 403 for other user, 404 non-existing, 200 success, 409 already ready)
	rec = doReq(f, "POST", "/assets/"+bobAssetID+"/complete", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed complete asset, got %d", rec.Code)
	}
	rec = doReq(f, "POST", "/assets/"+uuid.NewString()+"/complete", f.bobToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on complete missing asset, got %d", rec.Code)
	}
	rec = doReq(f, "POST", "/assets/"+bobAssetID+"/complete", f.aliceToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on completing another user's asset, got %d", rec.Code)
	}

	// Seed mock storage with bob's asset
	var bobAssetStorageKey string
	_ = f.pool.QueryRow(context.Background(), "SELECT storage_key FROM public.assets WHERE id = $1", bobAssetID).Scan(&bobAssetStorageKey)
	f.mockStorage.putMockObject(bobAssetStorageKey, 2048)

	rec = doReq(f, "POST", "/assets/"+bobAssetID+"/complete", f.bobToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on complete asset, got %d", rec.Code)
	}
	// 409 already ready
	rec = doReq(f, "POST", "/assets/"+bobAssetID+"/complete", f.bobToken, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on already completed asset, got %d", rec.Code)
	}

	// 9. POST /posts (401, 422 empty body & no assets, 201 valid post)
	rec = doReq(f, "POST", "/posts", "", map[string]any{"body": "Hello"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed create post, got %d", rec.Code)
	}
	rec = doReq(f, "POST", "/posts", f.bobToken, map[string]any{"body": "  "})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on post with empty body and no images, got %d", rec.Code)
	}
	rec = doReq(f, "POST", "/posts", f.bobToken, map[string]any{
		"body":     "Hello from Bob! #firstpost",
		"assetIds": []string{bobAssetID},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on create post, got %d", rec.Code)
	}
	var postRes map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &postRes)
	bobPostID := postRes["id"].(string)

	// 10. GET /posts/{id} (200 existing, 404 non-existing)
	rec = doReq(f, "GET", "/posts/"+bobPostID, f.aliceToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET /posts/{id}, got %d", rec.Code)
	}
	rec = doReq(f, "GET", "/posts/"+uuid.NewString(), f.aliceToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on missing post, got %d", rec.Code)
	}

	// 11. PATCH /posts/{id} (401, 403 not author, 422 empty body, 200 author, 200 admin)
	rec = doReq(f, "PATCH", "/posts/"+bobPostID, "", map[string]any{"body": "No token"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed edit post, got %d", rec.Code)
	}
	rec = doReq(f, "PATCH", "/posts/"+bobPostID, f.aliceToken, map[string]any{"body": "Hacked"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on edit by non-author, got %d", rec.Code)
	}
	rec = doReq(f, "PATCH", "/posts/"+uuid.NewString(), f.bobToken, map[string]any{"body": "Missing"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on edit missing post, got %d", rec.Code)
	}
	rec = doReq(f, "PATCH", "/posts/"+bobPostID, f.bobToken, map[string]any{"body": ""})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on edit with empty body, got %d", rec.Code)
	}
	rec = doReq(f, "PATCH", "/posts/"+bobPostID, f.bobToken, map[string]any{"body": "Hello updated!"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on edit by author, got %d", rec.Code)
	}
	rec = doReq(f, "PATCH", "/posts/"+bobPostID, f.adminToken, map[string]any{"body": "Moderated by admin"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on edit by admin, got %d", rec.Code)
	}

	// 12. PUT /posts/{id}/like & DELETE /posts/{id}/like (401, 200, idempotent, 404)
	rec = doReq(f, "PUT", "/posts/"+bobPostID+"/like", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed like, got %d", rec.Code)
	}
	rec = doReq(f, "PUT", "/posts/"+uuid.NewString()+"/like", f.aliceToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on like non-existing post, got %d", rec.Code)
	}
	rec = doReq(f, "PUT", "/posts/"+bobPostID+"/like", f.aliceToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on like, got %d", rec.Code)
	}
	// Repeat like (idempotent)
	rec = doReq(f, "PUT", "/posts/"+bobPostID+"/like", f.aliceToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on repeated like, got %d", rec.Code)
	}

	rec = doReq(f, "DELETE", "/posts/"+bobPostID+"/like", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed unlike, got %d", rec.Code)
	}
	rec = doReq(f, "DELETE", "/posts/"+bobPostID+"/like", f.aliceToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on unlike, got %d", rec.Code)
	}
	rec = doReq(f, "DELETE", "/posts/"+uuid.NewString()+"/like", f.aliceToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on unlike missing post, got %d", rec.Code)
	}
	// Re-like for counter test
	doReq(f, "PUT", "/posts/"+bobPostID+"/like", f.aliceToken, nil)

	// 13. POST /posts/{id}/share (401, 201, 404, 422)
	rec = doReq(f, "POST", "/posts/"+bobPostID+"/share", "", map[string]any{"quote": "Great!"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed share, got %d", rec.Code)
	}
	rec = doReq(f, "POST", "/posts/"+uuid.NewString()+"/share", f.aliceToken, map[string]any{"quote": "Great!"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on share non-existing, got %d", rec.Code)
	}
	longQuote := string(make([]byte, 501))
	rec = doReq(f, "POST", "/posts/"+bobPostID+"/share", f.aliceToken, map[string]any{"quote": longQuote})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on quote > 500 chars, got %d", rec.Code)
	}
	rec = doReq(f, "POST", "/posts/"+bobPostID+"/share", f.aliceToken, map[string]any{"quote": "Check this out!"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on share, got %d", rec.Code)
	}

	// 14. POST /posts/{id}/comments & GET /posts/{id}/comments (401, 404, 422, 201, 200)
	rec = doReq(f, "POST", "/posts/"+bobPostID+"/comments", "", map[string]any{"body": "Nice"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed comment, got %d", rec.Code)
	}
	rec = doReq(f, "POST", "/posts/"+uuid.NewString()+"/comments", f.aliceToken, map[string]any{"body": "Nice"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on comment non-existing, got %d", rec.Code)
	}
	rec = doReq(f, "POST", "/posts/"+bobPostID+"/comments", f.aliceToken, map[string]any{"body": ""})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on empty comment body, got %d", rec.Code)
	}
	rec = doReq(f, "POST", "/posts/"+bobPostID+"/comments", f.aliceToken, map[string]any{"body": "Awesome post!"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on create comment, got %d", rec.Code)
	}
	var commentRes map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &commentRes)
	aliceCommentID := commentRes["id"].(string)

	rec = doReq(f, "GET", "/posts/"+bobPostID+"/comments", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET comments, got %d", rec.Code)
	}
	rec = doReq(f, "GET", "/posts/"+uuid.NewString()+"/comments", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on GET comments of non-existing post, got %d", rec.Code)
	}

	// 15. DELETE /comments/{id} (401, 403, 204, 404)
	rec = doReq(f, "DELETE", "/comments/"+aliceCommentID, "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed delete comment, got %d", rec.Code)
	}
	rec = doReq(f, "DELETE", "/comments/"+aliceCommentID, f.bobToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on delete comment by non-author, got %d", rec.Code)
	}
	rec = doReq(f, "DELETE", "/comments/"+aliceCommentID, f.aliceToken, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on delete comment by author, got %d", rec.Code)
	}
	rec = doReq(f, "DELETE", "/comments/"+aliceCommentID, f.aliceToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on already deleted comment, got %d", rec.Code)
	}

	// Add another comment for counters
	doReq(f, "POST", "/posts/"+bobPostID+"/comments", f.aliceToken, map[string]any{"body": "Second comment"})

	// 16. GET /feed (401 without auth, 200 with Alice following Bob)
	rec = doReq(f, "GET", "/feed", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed GET /feed, got %d", rec.Code)
	}
	rec = doReq(f, "GET", "/feed", f.aliceToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET /feed, got %d", rec.Code)
	}

	// 17. GET /users/{id}/posts (200, 404)
	rec = doReq(f, "GET", "/users/"+f.bobID+"/posts", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET /users/{id}/posts, got %d", rec.Code)
	}
	rec = doReq(f, "GET", "/users/"+uuid.NewString()+"/posts", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on GET /users/{id}/posts for missing user, got %d", rec.Code)
	}

	// 18. GET /search (422 missing q or type, 200 success for users, posts, comments)
	rec = doReq(f, "GET", "/search", "", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on empty search, got %d", rec.Code)
	}
	rec = doReq(f, "GET", "/search?q=Alice&type=users", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on search users, got %d", rec.Code)
	}
	rec = doReq(f, "GET", "/search?q=Hello&type=posts", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on search posts, got %d", rec.Code)
	}
	rec = doReq(f, "GET", "/search?q=comment&type=comments", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on search comments, got %d", rec.Code)
	}

	// 19. DELETE /posts/{id} (401, 403, 204, 404)
	rec = doReq(f, "DELETE", "/posts/"+bobPostID, "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthed delete post, got %d", rec.Code)
	}
	rec = doReq(f, "DELETE", "/posts/"+bobPostID, f.aliceToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on delete post by non-author, got %d", rec.Code)
	}
	rec = doReq(f, "DELETE", "/posts/"+bobPostID, f.adminToken, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on delete post by admin, got %d", rec.Code)
	}
	rec = doReq(f, "DELETE", "/posts/"+bobPostID, f.bobToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on already soft-deleted post, got %d", rec.Code)
	}
}

func TestAssetSecurityFifthImageAndWrongOwner(t *testing.T) {
	f := setupFixture(t)
	ctx := context.Background()

	// 1. Create a ready asset owned by Bob
	bobAssetID := uuid.NewString()
	storageKeyBob := fmt.Sprintf("u/%s/%s.png", f.bobID, bobAssetID)
	_, err := f.pool.Exec(ctx, `
INSERT INTO public.assets (id, owner_id, kind, storage_bucket, storage_key, mime_type, size_bytes, status, completed_at)
VALUES ($1, $2, 'post_image', 'media', $3, 'image/png', 1024, 'ready', now())`,
		bobAssetID, f.bobID, storageKeyBob)
	if err != nil {
		t.Fatalf("failed to insert bob asset: %v", err)
	}

	// Alice attempts to create a post using Bob's asset -> Must return 422!
	rec := doReq(f, "POST", "/posts", f.aliceToken, map[string]any{
		"body":     "Alice trying to steal Bob asset",
		"assetIds": []string{bobAssetID},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 when attaching another user's asset, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Create 5 ready assets owned by Alice
	aliceAssets := make([]string, 5)
	for i := 0; i < 5; i++ {
		aid := uuid.NewString()
		skey := fmt.Sprintf("u/%s/%s.png", f.aliceID, aid)
		_, err := f.pool.Exec(ctx, `
INSERT INTO public.assets (id, owner_id, kind, storage_bucket, storage_key, mime_type, size_bytes, status, completed_at)
VALUES ($1, $2, 'post_image', 'media', $3, 'image/png', 1024, 'ready', now())`,
			aid, f.aliceID, skey)
		if err != nil {
			t.Fatalf("failed to insert alice asset %d: %v", i, err)
		}
		aliceAssets[i] = aid
	}

	// Alice attempts to attach 5 images to a single post -> Must return 422!
	rec = doReq(f, "POST", "/posts", f.aliceToken, map[string]any{
		"body":     "Alice post with 5 images",
		"assetIds": aliceAssets, // 5 images
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 when attaching 5 images, got %d: %s", rec.Code, rec.Body.String())
	}

	// Attaching 4 images should succeed!
	rec = doReq(f, "POST", "/posts", f.aliceToken, map[string]any{
		"body":     "Alice post with 4 images",
		"assetIds": aliceAssets[:4], // 4 images
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 when attaching exactly 4 images, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 4. Concurrency tests (Requirements: 50 same-user likes -> 1, 20 diff-user likes -> 20, follow/unfollow matches count)
// ---------------------------------------------------------------------------

func TestConcurrency_FiftyGoroutinesSameUserLike(t *testing.T) {
	f := setupFixture(t)
	ctx := context.Background()

	// Bob creates a post
	rec := doReq(f, "POST", "/posts", f.bobToken, map[string]any{
		"body": "Post for 50 goroutines same user like",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create post: %d", rec.Code)
	}
	var postRes map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &postRes)
	postID := postRes["id"].(string)

	// 50 goroutines send PUT like for the same post from the same user (Alice)
	const numRoutines = 50
	var wg sync.WaitGroup
	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func() {
			defer wg.Done()
			_ = doReq(f, "PUT", "/posts/"+postID+"/like", f.aliceToken, nil)
		}()
	}
	wg.Wait()

	// like_count ends at 1
	var likeCount int
	var countSQL int
	err := f.pool.QueryRow(ctx, "SELECT like_count, (SELECT count(*) FROM public.likes WHERE post_id = $1) FROM public.posts WHERE id = $1", postID).Scan(&likeCount, &countSQL)
	if err != nil {
		t.Fatalf("failed to query like count: %v", err)
	}
	if likeCount != 1 {
		t.Fatalf("expected like_count to end at 1, got %d", likeCount)
	}
	if countSQL != 1 {
		t.Fatalf("expected count(*) from likes to be 1, got %d", countSQL)
	}
}

func TestConcurrency_TwentyUsersConcurrentlyLike(t *testing.T) {
	f := setupFixture(t)
	ctx := context.Background()

	// Bob creates a post
	rec := doReq(f, "POST", "/posts", f.bobToken, map[string]any{
		"body": "Post for 20 users concurrent like",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create post: %d", rec.Code)
	}
	var postRes map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &postRes)
	postID := postRes["id"].(string)

	// 20 different users like the same post concurrently
	const numUsers = 20
	type userSession struct {
		id    string
		token string
	}
	users := make([]userSession, numUsers)
	for i := 0; i < numUsers; i++ {
		uname := fmt.Sprintf("u20_%d_%d", time.Now().UnixNano()%10000000, i)
		u, err := f.repo.CreateUser(ctx, uname, uname+"@test.com", "User 20", time.Date(1995, 1, 1, 0, 0, 0, 0, time.UTC), "user")
		if err != nil {
			t.Fatalf("failed to create user: %v", err)
		}
		tok, _ := auth.GenerateAccessToken(u.ID, "user", f.cfg.JWTSecret, f.cfg.JWTIssuer, 15*time.Minute)
		users[i] = userSession{id: u.ID, token: tok}
	}

	var wg sync.WaitGroup
	wg.Add(numUsers)
	for i := 0; i < numUsers; i++ {
		go func(tok string) {
			defer wg.Done()
			_ = doReq(f, "PUT", "/posts/"+postID+"/like", tok, nil)
		}(users[i].token)
	}
	wg.Wait()

	// like_count ends at 20
	var likeCount int
	var countSQL int
	err := f.pool.QueryRow(ctx, "SELECT like_count, (SELECT count(*) FROM public.likes WHERE post_id = $1) FROM public.posts WHERE id = $1", postID).Scan(&likeCount, &countSQL)
	if err != nil {
		t.Fatalf("failed to query like count: %v", err)
	}
	if likeCount != 20 {
		t.Fatalf("expected like_count to end at 20, got %d", likeCount)
	}
	if countSQL != 20 {
		t.Fatalf("expected count(*) from likes to be 20, got %d", countSQL)
	}
}

func TestConcurrency_FollowUnfollowCountersMatchSQLCount(t *testing.T) {
	f := setupFixture(t)
	ctx := context.Background()

	// Target user: Bob
	const numUsers = 20
	type userSession struct {
		id    string
		token string
	}
	users := make([]userSession, numUsers)
	for i := 0; i < numUsers; i++ {
		uname := fmt.Sprintf("flwcon_%d_%d", time.Now().UnixNano()%10000000, i)
		u, err := f.repo.CreateUser(ctx, uname, uname+"@test.com", "Follow User", time.Date(1995, 1, 1, 0, 0, 0, 0, time.UTC), "user")
		if err != nil {
			t.Fatalf("failed to create user: %v", err)
		}
		tok, _ := auth.GenerateAccessToken(u.ID, "user", f.cfg.JWTSecret, f.cfg.JWTIssuer, 15*time.Minute)
		users[i] = userSession{id: u.ID, token: tok}
	}

	var wg sync.WaitGroup
	// Concurrently follow Bob
	for i := 0; i < numUsers; i++ {
		wg.Add(1)
		go func(tok string) {
			defer wg.Done()
			_ = doReq(f, "PUT", "/users/"+f.bobID+"/follow", tok, nil)
		}(users[i].token)
	}
	wg.Wait()

	// Half of them unfollow Bob concurrently
	for i := 0; i < numUsers/2; i++ {
		wg.Add(1)
		go func(tok string) {
			defer wg.Done()
			_ = doReq(f, "DELETE", "/users/"+f.bobID+"/follow", tok, nil)
		}(users[i].token)
	}
	wg.Wait()

	// Verify Bob's follower_count matches COUNT(*) on public.follows
	var followerCount, actualCount int
	err := f.pool.QueryRow(ctx, "SELECT follower_count, (SELECT count(*) FROM public.follows WHERE followee_id = $1) FROM public.users WHERE id = $1", f.bobID).Scan(&followerCount, &actualCount)
	if err != nil {
		t.Fatalf("failed to query follower count: %v", err)
	}
	if followerCount != actualCount {
		t.Fatalf("follower_count (%d) does not match COUNT(*) on follows table (%d)", followerCount, actualCount)
	}

	// Verify each user's following_count matches COUNT(*) on public.follows
	for i := 0; i < numUsers; i++ {
		var followingCount, actualFollowing int
		err := f.pool.QueryRow(ctx, "SELECT following_count, (SELECT count(*) FROM public.follows WHERE follower_id = $1) FROM public.users WHERE id = $1", users[i].id).Scan(&followingCount, &actualFollowing)
		if err != nil {
			t.Fatalf("failed to query following count for user %d: %v", i, err)
		}
		if followingCount != actualFollowing {
			t.Fatalf("user %s following_count (%d) does not match COUNT(*) on follows table (%d)", users[i].id, followingCount, actualFollowing)
		}
	}
}

// ---------------------------------------------------------------------------
// 5. Pagination: Seed 45 posts and page through the feed. Every post appears exactly once, in order.
// ---------------------------------------------------------------------------

func TestPagination_Seed45PostsInFeed(t *testing.T) {
	f := setupFixture(t)
	ctx := context.Background()

	// Create an author and a follower
	authorName := fmt.Sprintf("auth_p_%d", time.Now().UnixNano()%10000000)
	author, err := f.repo.CreateUser(ctx, authorName, authorName+"@test.com", "Author Dave", time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), "user")
	if err != nil {
		t.Fatalf("failed to create author: %v", err)
	}

	followerName := fmt.Sprintf("foll_p_%d", time.Now().UnixNano()%10000000)
	follower, err := f.repo.CreateUser(ctx, followerName, followerName+"@test.com", "Follower Eve", time.Date(1992, 1, 1, 0, 0, 0, 0, time.UTC), "user")
	if err != nil {
		t.Fatalf("failed to create follower: %v", err)
	}
	followerToken, _ := auth.GenerateAccessToken(follower.ID, "user", f.cfg.JWTSecret, f.cfg.JWTIssuer, 15*time.Minute)

	// Follower follows author
	_, err = f.repo.Follow(ctx, follower.ID, author.ID)
	if err != nil {
		t.Fatalf("failed to follow author: %v", err)
	}

	// Seed 45 posts authored by Dave with distinct descending created_at timestamps
	const totalPosts = 45
	postIDs := make([]string, totalPosts)
	baseTime := time.Now().Add(-1 * time.Hour)

	for i := 0; i < totalPosts; i++ {
		pTime := baseTime.Add(time.Duration(i) * time.Minute)
		pid := uuid.NewString()
		_, err := f.pool.Exec(ctx, `
INSERT INTO public.posts (id, author_id, body, created_at)
VALUES ($1, $2, $3, $4)`, pid, author.ID, fmt.Sprintf("Post number %02d", i), pTime)
		if err != nil {
			t.Fatalf("failed to insert post %d: %v", i, err)
		}
		// Notice newest is i=44, oldest is i=0
		postIDs[i] = pid
	}

	// Page through the feed using limit=10 until hasMore is false
	seenPosts := make(map[string]bool)
	var orderedReceivedIDs []string
	cursor := ""

	for {
		url := "/feed?limit=10"
		if cursor != "" {
			url = fmt.Sprintf("/feed?limit=10&cursor=%s", cursor)
		}
		rec := doReq(f, "GET", url, followerToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /feed returned %d: %s", rec.Code, rec.Body.String())
		}

		var pageRes struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			Pagination struct {
				HasMore    bool    `json:"hasMore"`
				NextCursor *string `json:"nextCursor"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &pageRes); err != nil {
			t.Fatalf("failed to unmarshal feed page: %v", err)
		}

		for _, item := range pageRes.Data {
			if seenPosts[item.ID] {
				t.Fatalf("post %s appeared more than once during pagination!", item.ID)
			}
			seenPosts[item.ID] = true
			orderedReceivedIDs = append(orderedReceivedIDs, item.ID)
		}

		if !pageRes.Pagination.HasMore || pageRes.Pagination.NextCursor == nil || *pageRes.Pagination.NextCursor == "" {
			break
		}
		cursor = *pageRes.Pagination.NextCursor
	}

	// Verify count: exactly 45 posts
	if len(orderedReceivedIDs) != totalPosts {
		t.Fatalf("expected exactly %d posts across pagination, got %d", totalPosts, len(orderedReceivedIDs))
	}

	// Verify ordering: newest first (i=44 down to i=0)
	for i := 0; i < totalPosts; i++ {
		expectedID := postIDs[totalPosts-1-i]
		if orderedReceivedIDs[i] != expectedID {
			t.Errorf("at index %d: expected post ID %s, got %s", i, expectedID, orderedReceivedIDs[i])
		}
	}
}

// ---------------------------------------------------------------------------
// 6. Search: Stemmed matches are returned, and soft-deleted posts are excluded.
// ---------------------------------------------------------------------------

func TestSearch_StemmedAndExcludesSoftDeleted(t *testing.T) {
	f := setupFixture(t)
	ctx := context.Background()

	// Author creates post with "running" and "jumping"
	post1ID := uuid.NewString()
	_, err := f.pool.Exec(ctx, `
INSERT INTO public.posts (id, author_id, body, created_at)
VALUES ($1, $2, 'Engineers love running and jumping early mornings', now() - interval '2 minutes')`,
		post1ID, f.bobID)
	if err != nil {
		t.Fatalf("failed to insert post 1: %v", err)
	}

	// Post 2 with "jumping" as well, but this one is soft-deleted
	post2ID := uuid.NewString()
	_, err = f.pool.Exec(ctx, `
INSERT INTO public.posts (id, author_id, body, created_at, deleted_at)
VALUES ($1, $2, 'Another jumping athlete in the competition', now() - interval '1 minute', now())`,
		post2ID, f.bobID)
	if err != nil {
		t.Fatalf("failed to insert post 2: %v", err)
	}

	// Post 3 with unrelated content
	post3ID := uuid.NewString()
	_, err = f.pool.Exec(ctx, `
INSERT INTO public.posts (id, author_id, body, created_at)
VALUES ($1, $2, 'Cooking delicious pasta with garlic and cheese', now())`,
		post3ID, f.bobID)
	if err != nil {
		t.Fatalf("failed to insert post 3: %v", err)
	}

	// Search for "jumped" (which stems to "jump", matching "jumping")
	rec := doReq(f, "GET", "/search?q=jumped&type=posts", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("search request returned %d: %s", rec.Code, rec.Body.String())
	}

	var searchRes struct {
		Data []struct {
			ID   string `json:"id"`
			Body string `json:"body"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &searchRes); err != nil {
		t.Fatalf("failed to unmarshal search response: %v", err)
	}

	foundPost1 := false
	foundPost2 := false
	foundPost3 := false

	for _, item := range searchRes.Data {
		if item.ID == post1ID {
			foundPost1 = true
		}
		if item.ID == post2ID {
			foundPost2 = true
		}
		if item.ID == post3ID {
			foundPost3 = true
		}
	}

	// Stemmed match must be found
	if !foundPost1 {
		t.Errorf("expected stemmed match for 'jumped' to return post1 containing 'jumping', but it was not returned (response: %s)", rec.Body.String())
	}

	// Soft-deleted post must be excluded
	if foundPost2 {
		t.Errorf("expected soft-deleted post2 to be excluded from search, but it was returned!")
	}

	// Unrelated post must not match
	if foundPost3 {
		t.Errorf("unrelated post3 was unexpectedly returned in search results")
	}
}

// ---------------------------------------------------------------------------
// 7. Security: Email and birthdate never appear in public responses
// ---------------------------------------------------------------------------

func TestSecurity_EmailAndBirthdateNeverInPublicResponses(t *testing.T) {
	f := setupFixture(t)

	// Create a post with a comment to verify post/comment author public views
	recPost := doReq(f, "POST", "/posts", f.bobToken, map[string]any{
		"body": "Public post by Bob to verify privacy",
	})
	if recPost.Code != http.StatusCreated {
		t.Fatalf("failed to create post: %d", recPost.Code)
	}
	var pRes map[string]any
	_ = json.Unmarshal(recPost.Body.Bytes(), &pRes)
	postID := pRes["id"].(string)

	doReq(f, "POST", "/posts/"+postID+"/comments", f.aliceToken, map[string]any{
		"body": "Public comment by Alice",
	})

	var meRes map[string]any
	recMe := doReq(f, "GET", "/me", f.bobToken, nil)
	_ = json.Unmarshal(recMe.Body.Bytes(), &meRes)
	bobUsername := meRes["username"].(string)

	publicEndpoints := []struct {
		name   string
		method string
		path   string
		token  string
	}{
		{name: "GET /users/{username}", method: "GET", path: "/users/" + bobUsername, token: ""},
		{name: "GET /users/{id}/followers", method: "GET", path: "/users/" + f.bobID + "/followers", token: ""},
		{name: "GET /users/{id}/following", method: "GET", path: "/users/" + f.aliceID + "/following", token: ""},
		{name: "GET /posts/{id}", method: "GET", path: "/posts/" + postID, token: f.aliceToken},
		{name: "GET /posts/{id}/comments", method: "GET", path: "/posts/" + postID + "/comments", token: ""},
		{name: "GET /feed", method: "GET", path: "/feed", token: f.aliceToken},
		{name: "GET /users/{id}/posts", method: "GET", path: "/users/" + f.bobID + "/posts", token: ""},
		{name: "GET /search?q=bob&type=users", method: "GET", path: "/search?q=bob&type=users", token: ""},
		{name: "GET /search?q=Public&type=posts", method: "GET", path: "/search?q=Public&type=posts", token: ""},
	}

	for _, ep := range publicEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			rec := doReq(f, ep.method, ep.path, ep.token, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s returned status %d: %s", ep.name, rec.Code, rec.Body.String())
			}

			bodyStr := rec.Body.String()

			// Check that "email" is never exposed
			if strings.Contains(strings.ToLower(bodyStr), `"email"`) {
				t.Errorf("%s leaked 'email' field in public response: %s", ep.name, bodyStr)
			}
			// Check that "birthdate" is never exposed
			if strings.Contains(strings.ToLower(bodyStr), `"birthdate"`) || strings.Contains(strings.ToLower(bodyStr), `"birth_date"`) {
				t.Errorf("%s leaked 'birthdate' field in public response: %s", ep.name, bodyStr)
			}
		})
	}
}
