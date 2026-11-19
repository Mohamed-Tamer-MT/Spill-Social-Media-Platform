package models_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/models"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/testhelper"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type dbFixture struct {
	superPool *pgxpool.Pool
	appPool   *pgxpool.Pool
	anonPool  *pgxpool.Pool
	repo      *models.Repository
	ctx       context.Context
}

func setupDB(t *testing.T) *dbFixture {
	ctx := context.Background()
	superConn, appConn, anonConn, err := testhelper.SetupTestDB(ctx)
	if err != nil {
		t.Fatalf("failed to setup test database: %v", err)
	}

	superPool, err := pgxpool.New(ctx, superConn)
	if err != nil {
		t.Fatalf("failed to connect to super db: %v", err)
	}

	appPool, err := pgxpool.New(ctx, appConn)
	if err != nil {
		t.Fatalf("failed to connect to app db: %v", err)
	}

	anonPool, err := pgxpool.New(ctx, anonConn)
	if err != nil {
		t.Fatalf("failed to connect to anon db: %v", err)
	}

	repo := models.NewRepository(appPool)

	t.Cleanup(func() {
		anonPool.Close()
		appPool.Close()
		superPool.Close()
	})

	return &dbFixture{
		superPool: superPool,
		appPool:   appPool,
		anonPool:  anonPool,
		repo:      repo,
		ctx:       ctx,
	}
}

func makeTestUser(t *testing.T, f *dbFixture, prefix string) *models.User {
	uid := uuid.New().String()[:8]
	username := fmt.Sprintf("%s_%s", prefix, uid)
	email := fmt.Sprintf("%s_%s@test.com", prefix, uid)
	u, err := f.repo.CreateUser(f.ctx, username, email, "Test "+prefix, time.Date(1995, 1, 1, 0, 0, 0, 0, time.UTC), "user")
	if err != nil {
		t.Fatalf("failed to make test user %s: %v", prefix, err)
	}
	return u
}

// ---------------------------------------------------------------------------
// Acceptance Criteria: Database rule tests
// ---------------------------------------------------------------------------

// 1. A duplicate like does not change like_count.
func TestDBRule_DuplicateLikeDoesNotChangeLikeCount(t *testing.T) {
	f := setupDB(t)

	author := makeTestUser(t, f, "aut1")
	liker := makeTestUser(t, f, "lik1")

	p, err := f.repo.CreatePost(f.ctx, author.ID, "Post for duplicate like test", nil)
	if err != nil {
		t.Fatalf("failed to create post: %v", err)
	}

	// Initial like
	liked, err := f.repo.LikePost(f.ctx, liker.ID, p.ID)
	if err != nil || !liked {
		t.Fatalf("expected first like to succeed, got liked=%v, err=%v", liked, err)
	}

	var count1 int
	err = f.appPool.QueryRow(f.ctx, "SELECT like_count FROM public.posts WHERE id = $1", p.ID).Scan(&count1)
	if err != nil || count1 != 1 {
		t.Fatalf("expected like_count=1 after first like, got %d (err: %v)", count1, err)
	}

	// Duplicate like
	liked2, err := f.repo.LikePost(f.ctx, liker.ID, p.ID)
	if err != nil {
		t.Fatalf("duplicate like returned unexpected err: %v", err)
	}
	if liked2 {
		t.Errorf("expected duplicate like to return liked=false")
	}

	var count2 int
	err = f.appPool.QueryRow(f.ctx, "SELECT like_count FROM public.posts WHERE id = $1", p.ID).Scan(&count2)
	if err != nil || count2 != 1 {
		t.Fatalf("expected like_count to remain 1, got %d (err: %v)", count2, err)
	}
}

// 2. Deleting a like decrements like_count, never below zero.
func TestDBRule_DeletingLikeDecrementsLikeCountNeverBelowZero(t *testing.T) {
	f := setupDB(t)

	author := makeTestUser(t, f, "aut2")
	liker := makeTestUser(t, f, "lik2")

	p, err := f.repo.CreatePost(f.ctx, author.ID, "Post for delete like test", nil)
	if err != nil {
		t.Fatalf("failed to create post: %v", err)
	}

	// Like then unlike
	_, _ = f.repo.LikePost(f.ctx, liker.ID, p.ID)
	unliked, err := f.repo.UnlikePost(f.ctx, liker.ID, p.ID)
	if err != nil || !unliked {
		t.Fatalf("failed to unlike: %v", err)
	}

	var count int
	_ = f.appPool.QueryRow(f.ctx, "SELECT like_count FROM public.posts WHERE id = $1", p.ID).Scan(&count)
	if count != 0 {
		t.Fatalf("expected like_count=0 after delete, got %d", count)
	}

	// Unlike again when no like exists
	unliked2, err := f.repo.UnlikePost(f.ctx, liker.ID, p.ID)
	if err != nil || unliked2 {
		t.Fatalf("expected unliked2=false, got %v, err=%v", unliked2, err)
	}

	// Direct delete statement simulation to test trigger 'greatest(like_count - 1, 0)'
	_, _ = f.superPool.Exec(f.ctx, "DELETE FROM public.likes WHERE post_id = $1", p.ID)
	_ = f.appPool.QueryRow(f.ctx, "SELECT like_count FROM public.posts WHERE id = $1", p.ID).Scan(&count)
	if count < 0 {
		t.Fatalf("like_count fell below zero: %d", count)
	}
}

// 3. Soft-deleting a comment decrements comment_count, and restoring it increments it again.
func TestDBRule_SoftDeletingCommentDecrementsAndRestoringIncrements(t *testing.T) {
	f := setupDB(t)

	author := makeTestUser(t, f, "caut")
	p, err := f.repo.CreatePost(f.ctx, author.ID, "Post for comment count rule", nil)
	if err != nil {
		t.Fatalf("failed to create post: %v", err)
	}

	c, err := f.repo.CreateComment(f.ctx, p.ID, author.ID, "Comment 1")
	if err != nil {
		t.Fatalf("failed to create comment: %v", err)
	}

	var count1 int
	_ = f.appPool.QueryRow(f.ctx, "SELECT comment_count FROM public.posts WHERE id = $1", p.ID).Scan(&count1)
	if count1 != 1 {
		t.Fatalf("expected comment_count=1 after insert, got %d", count1)
	}

	// Soft delete comment
	if err := f.repo.SoftDeleteComment(f.ctx, c.ID); err != nil {
		t.Fatalf("failed to soft delete comment: %v", err)
	}

	var count2 int
	_ = f.appPool.QueryRow(f.ctx, "SELECT comment_count FROM public.posts WHERE id = $1", p.ID).Scan(&count2)
	if count2 != 0 {
		t.Fatalf("expected comment_count=0 after soft delete, got %d", count2)
	}

	// Restore comment by setting deleted_at = NULL
	_, err = f.appPool.Exec(f.ctx, "UPDATE public.comments SET deleted_at = NULL WHERE id = $1", c.ID)
	if err != nil {
		t.Fatalf("failed to restore comment: %v", err)
	}

	var count3 int
	_ = f.appPool.QueryRow(f.ctx, "SELECT comment_count FROM public.posts WHERE id = $1", p.ID).Scan(&count3)
	if count3 != 1 {
		t.Fatalf("expected comment_count=1 after restoration, got %d", count3)
	}
}

// 4. A share can be inserted only once per user and post.
func TestDBRule_ShareInsertedOnlyOncePerUserAndPost(t *testing.T) {
	f := setupDB(t)

	author := makeTestUser(t, f, "shaut")
	sharer := makeTestUser(t, f, "shuser")

	p, err := f.repo.CreatePost(f.ctx, author.ID, "Post for share unique rule", nil)
	if err != nil {
		t.Fatalf("failed to create post: %v", err)
	}

	// 1. Direct insertion to test the DB unique constraint shares_user_post_unique
	_, err = f.appPool.Exec(f.ctx, "INSERT INTO public.shares (id, user_id, post_id, quote) VALUES ($1, $2, $3, $4)",
		uuid.New().String(), sharer.ID, p.ID, "First share")
	if err != nil {
		t.Fatalf("expected first share insert to succeed: %v", err)
	}

	// Second insert must fail with unique constraint violation
	_, err = f.appPool.Exec(f.ctx, "INSERT INTO public.shares (id, user_id, post_id, quote) VALUES ($1, $2, $3, $4)",
		uuid.New().String(), sharer.ID, p.ID, "Duplicate share")
	if err == nil {
		t.Fatalf("expected duplicate share insert to fail with unique constraint, but it succeeded")
	}
	if !strings.Contains(err.Error(), "shares_user_post_unique") {
		t.Errorf("expected error to mention 'shares_user_post_unique', got: %v", err)
	}
}

// 5. Attaching a fifth image to a post fails.
func TestDBRule_AttachingFifthImageFails(t *testing.T) {
	f := setupDB(t)

	author := makeTestUser(t, f, "imguser")
	p, err := f.repo.CreatePost(f.ctx, author.ID, "Post for 5 images rule", nil)
	if err != nil {
		t.Fatalf("failed to create post: %v", err)
	}

	// Create and complete 5 assets for author
	var assetIDs []string
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("u/%s/img_%d.jpg", author.ID, i)
		a, err := f.repo.CreateAsset(f.ctx, uuid.New().String(), author.ID, "post_image", "media", key, "image/jpeg", 1024)
		if err != nil {
			t.Fatalf("failed to create asset: %v", err)
		}
		_, err = f.repo.CompleteAsset(f.ctx, a.ID)
		if err != nil {
			t.Fatalf("failed to complete asset: %v", err)
		}
		assetIDs = append(assetIDs, a.ID)
	}

	// Attach 4 images: should succeed
	for i := 0; i < 4; i++ {
		_, err = f.appPool.Exec(f.ctx, "INSERT INTO public.post_assets (post_id, asset_id, position) VALUES ($1, $2, $3)", p.ID, assetIDs[i], i)
		if err != nil {
			t.Fatalf("attaching image %d failed: %v", i+1, err)
		}
	}

	// Attach 5th image: must fail by trigger
	_, err = f.appPool.Exec(f.ctx, "INSERT INTO public.post_assets (post_id, asset_id, position) VALUES ($1, $2, $3)", p.ID, assetIDs[4], 4)
	if err == nil {
		t.Fatalf("expected 5th image attachment to fail, but it succeeded")
	}
	if !strings.Contains(err.Error(), "already has 4 images") {
		t.Errorf("expected trigger error message to mention 'already has 4 images', got: %v", err)
	}
}

// 6. Attaching another user's asset to a post fails.
func TestDBRule_AttachingAnotherUsersAssetFails(t *testing.T) {
	f := setupDB(t)

	user1 := makeTestUser(t, f, "u1asset")
	user2 := makeTestUser(t, f, "u2asset")

	p, err := f.repo.CreatePost(f.ctx, user1.ID, "Post by user 1", nil)
	if err != nil {
		t.Fatalf("failed to create post: %v", err)
	}

	// User2's asset
	key := fmt.Sprintf("u/%s/other.jpg", user2.ID)
	a, err := f.repo.CreateAsset(f.ctx, uuid.New().String(), user2.ID, "post_image", "media", key, "image/jpeg", 1024)
	if err != nil {
		t.Fatalf("failed to create asset: %v", err)
	}
	_, err = f.repo.CompleteAsset(f.ctx, a.ID)
	if err != nil {
		t.Fatalf("failed to complete asset: %v", err)
	}

	// Attach User2's asset to User1's post
	_, err = f.appPool.Exec(f.ctx, "INSERT INTO public.post_assets (post_id, asset_id, position) VALUES ($1, $2, 0)", p.ID, a.ID)
	if err == nil {
		t.Fatalf("expected attaching another user's asset to fail, but it succeeded")
	}
	if !strings.Contains(err.Error(), "not owned by the post author") {
		t.Errorf("expected error to mention 'not owned by the post author', got: %v", err)
	}
}

// 7. Attaching an avatar asset as a post image fails.
func TestDBRule_AttachingAvatarAssetAsPostImageFails(t *testing.T) {
	f := setupDB(t)

	user := makeTestUser(t, f, "uavatar")
	p, err := f.repo.CreatePost(f.ctx, user.ID, "Post by user", nil)
	if err != nil {
		t.Fatalf("failed to create post: %v", err)
	}

	// User creates an avatar asset (kind = 'avatar')
	key := fmt.Sprintf("u/%s/avatar.png", user.ID)
	a, err := f.repo.CreateAsset(f.ctx, uuid.New().String(), user.ID, "avatar", "media", key, "image/png", 2048)
	if err != nil {
		t.Fatalf("failed to create asset: %v", err)
	}
	_, err = f.repo.CompleteAsset(f.ctx, a.ID)
	if err != nil {
		t.Fatalf("failed to complete asset: %v", err)
	}

	// Attempt to attach avatar asset as post image
	_, err = f.appPool.Exec(f.ctx, "INSERT INTO public.post_assets (post_id, asset_id, position) VALUES ($1, $2, 0)", p.ID, a.ID)
	if err == nil {
		t.Fatalf("expected attaching avatar asset to post to fail, but it succeeded")
	}
	if !strings.Contains(err.Error(), "must be a ready post_image") {
		t.Errorf("expected error to mention 'must be a ready post_image', got: %v", err)
	}
}

// 8. A user cannot follow themselves.
func TestDBRule_UserCannotFollowThemselves(t *testing.T) {
	f := setupDB(t)

	user := makeTestUser(t, f, "selfuser")

	// Attempt self-follow via SQL
	_, err := f.appPool.Exec(f.ctx, "INSERT INTO public.follows (follower_id, followee_id) VALUES ($1, $1)", user.ID)
	if err == nil {
		t.Fatalf("expected self-follow to fail check constraint, but it succeeded")
	}
	if !strings.Contains(err.Error(), "no_self_follow") {
		t.Errorf("expected check constraint error 'no_self_follow', got: %v", err)
	}

	// Also via repo method
	_, err = f.repo.Follow(f.ctx, user.ID, user.ID)
	if err == nil {
		t.Fatalf("expected repo.Follow self-follow to fail, but got nil")
	}
}

// 9. An anon connection cannot read public.users.
func TestDBRule_AnonCannotReadPublicUsers(t *testing.T) {
	f := setupDB(t)

	// Create user as api_app
	u := makeTestUser(t, f, "anoncheck")

	// api_app can read public.users
	var countApp int
	err := f.appPool.QueryRow(f.ctx, "SELECT count(*) FROM public.users WHERE id = $1", u.ID).Scan(&countApp)
	if err != nil || countApp != 1 {
		t.Fatalf("api_app failed to read user: count=%d, err=%v", countApp, err)
	}

	// anon connects and attempts to read public.users
	var countAnon int
	err = f.anonPool.QueryRow(f.ctx, "SELECT count(*) FROM public.users WHERE id = $1", u.ID).Scan(&countAnon)
	// Due to RLS, anon gets 0 rows (count=0) without error or permission denied
	if countAnon != 0 {
		t.Fatalf("RLS breach: anon was able to read %d rows from public.users", countAnon)
	}
}

// ---------------------------------------------------------------------------
// Additional Repository Integration Tests (Users, Posts, Search, Auth, Assets)
// ---------------------------------------------------------------------------

func TestRepository_UserLifecycle(t *testing.T) {
	f := setupDB(t)

	u := makeTestUser(t, f, "lifecycle")

	// Find by email
	byEmail, err := f.repo.FindUserByEmail(f.ctx, u.Email)
	if err != nil || byEmail.ID != u.ID {
		t.Fatalf("FindUserByEmail failed: %v", err)
	}

	// Find by ID
	byID, err := f.repo.FindUserByID(f.ctx, u.ID)
	if err != nil || byID.Username != u.Username {
		t.Fatalf("FindUserByID failed: %v", err)
	}

	// Get by username
	byName, err := f.repo.GetUserByUsername(f.ctx, u.Username)
	if err != nil || byName.ID != u.ID {
		t.Fatalf("GetUserByUsername failed: %v", err)
	}

	// IsUsernameTaken
	taken, err := f.repo.IsUsernameTaken(f.ctx, u.Username)
	if err != nil || !taken {
		t.Fatalf("expected username to be taken: %v", err)
	}

	// Update user profile
	newName := "New Name"
	updated, err := f.repo.UpdateUserProfile(f.ctx, u.ID, &newName, nil, false)
	if err != nil || updated.DisplayName != newName {
		t.Fatalf("UpdateUserProfile failed: %v", err)
	}

	// Create a valid avatar asset for user
	avatarKey := fmt.Sprintf("u/%s/avatar.png", u.ID)
	avatarAsset, err := f.repo.CreateAsset(f.ctx, uuid.New().String(), u.ID, "avatar", "media", avatarKey, "image/png", 2048)
	if err != nil {
		t.Fatalf("CreateAsset failed: %v", err)
	}
	_, err = f.repo.CompleteAsset(f.ctx, avatarAsset.ID)
	if err != nil {
		t.Fatalf("CompleteAsset failed: %v", err)
	}

	// Update user profile with valid ready avatar
	updatedAvatar, err := f.repo.UpdateUserProfile(f.ctx, u.ID, nil, &avatarAsset.ID, false)
	if err != nil || updatedAvatar.AvatarAssetID == nil || *updatedAvatar.AvatarAssetID != avatarAsset.ID {
		t.Fatalf("UpdateUserProfile with valid avatar failed: %v", err)
	}

	// Clear avatar
	clearedAvatar, err := f.repo.UpdateUserProfile(f.ctx, u.ID, nil, nil, true)
	if err != nil || clearedAvatar.AvatarAssetID != nil {
		t.Fatalf("UpdateUserProfile clear avatar failed: %v", err)
	}

	// Attempt setting another user's asset as avatar -> trigger must reject
	otherUser := makeTestUser(t, f, "otheravatar")
	otherKey := fmt.Sprintf("u/%s/avatar.png", otherUser.ID)
	otherAsset, err := f.repo.CreateAsset(f.ctx, uuid.New().String(), otherUser.ID, "avatar", "media", otherKey, "image/png", 2048)
	if err != nil {
		t.Fatalf("CreateAsset failed: %v", err)
	}
	_, _ = f.repo.CompleteAsset(f.ctx, otherAsset.ID)

	_, err = f.repo.UpdateUserProfile(f.ctx, u.ID, nil, &otherAsset.ID, false)
	if err == nil || !strings.Contains(err.Error(), "trigger validation error") {
		t.Fatalf("expected trigger validation error when using another user's asset as avatar, got: %v", err)
	}
}

func TestRepository_FollowLifecycle(t *testing.T) {
	f := setupDB(t)

	u1 := makeTestUser(t, f, "flw1")
	u2 := makeTestUser(t, f, "flw2")

	// Follow
	followed, err := f.repo.Follow(f.ctx, u1.ID, u2.ID)
	if err != nil || !followed {
		t.Fatalf("Follow failed: %v", err)
	}

	// Duplicate follow
	followed2, err := f.repo.Follow(f.ctx, u1.ID, u2.ID)
	if err != nil || followed2 {
		t.Fatalf("Duplicate follow should return false: %v", err)
	}

	// Check followers / following
	followers, err := f.repo.GetFollowers(f.ctx, u2.ID, 10, nil, "")
	if err != nil || len(followers) != 1 || followers[0].ID != u1.ID {
		t.Fatalf("GetFollowers failed: %v", err)
	}

	following, err := f.repo.GetFollowing(f.ctx, u1.ID, 10, nil, "")
	if err != nil || len(following) != 1 || following[0].ID != u2.ID {
		t.Fatalf("GetFollowing failed: %v", err)
	}

	// Test cursor pagination for followers & following
	cursorTime := followers[0].CreatedAt
	cursorID := followers[0].ID
	pagedFollowers, err := f.repo.GetFollowers(f.ctx, u2.ID, 10, &cursorTime, cursorID)
	if err != nil {
		t.Fatalf("GetFollowers with cursor failed: %v", err)
	}
	if len(pagedFollowers) != 0 {
		t.Errorf("expected 0 followers after last item cursor, got %d", len(pagedFollowers))
	}

	fCursorTime := following[0].CreatedAt
	fCursorID := following[0].ID
	pagedFollowing, err := f.repo.GetFollowing(f.ctx, u1.ID, 10, &fCursorTime, fCursorID)
	if err != nil {
		t.Fatalf("GetFollowing with cursor failed: %v", err)
	}
	if len(pagedFollowing) != 0 {
		t.Errorf("expected 0 following after last item cursor, got %d", len(pagedFollowing))
	}

	// Unfollow
	unfollowed, err := f.repo.Unfollow(f.ctx, u1.ID, u2.ID)
	if err != nil || !unfollowed {
		t.Fatalf("Unfollow failed: %v", err)
	}
}

func TestRepository_PostAndCommentLifecycle(t *testing.T) {
	f := setupDB(t)

	author := makeTestUser(t, f, "pclifecycle")

	// Create post
	p, err := f.repo.CreatePost(f.ctx, author.ID, "Post Lifecycle Body", nil)
	if err != nil {
		t.Fatalf("CreatePost failed: %v", err)
	}

	// Get post by ID
	pDetail, err := f.repo.GetPostByID(f.ctx, p.ID, &author.ID)
	if err != nil || pDetail.Body != "Post Lifecycle Body" {
		t.Fatalf("GetPostByID failed: %v", err)
	}

	// Update post
	upd, err := f.repo.UpdatePost(f.ctx, p.ID, "Updated Body")
	if err != nil || upd.Body != "Updated Body" {
		t.Fatalf("UpdatePost failed: %v", err)
	}

	// Comments
	c, err := f.repo.CreateComment(f.ctx, p.ID, author.ID, "Comment Body")
	if err != nil {
		t.Fatalf("CreateComment failed: %v", err)
	}

	cDetail, err := f.repo.GetCommentByID(f.ctx, c.ID)
	if err != nil || cDetail.Body != "Comment Body" {
		t.Fatalf("GetCommentByID failed: %v", err)
	}

	cList, err := f.repo.GetCommentsByPostID(f.ctx, p.ID, 10, nil, "")
	if err != nil || len(cList) != 1 {
		t.Fatalf("GetCommentsByPostID failed: %v", err)
	}

	// Soft delete comment
	if err := f.repo.SoftDeleteComment(f.ctx, c.ID); err != nil {
		t.Fatalf("SoftDeleteComment failed: %v", err)
	}

	// Soft delete post
	if err := f.repo.SoftDeletePost(f.ctx, p.ID); err != nil {
		t.Fatalf("SoftDeletePost failed: %v", err)
	}
}

func TestRepository_AssetLifecycle(t *testing.T) {
	f := setupDB(t)

	user := makeTestUser(t, f, "assetuser")

	key := fmt.Sprintf("u/%s/photo.jpg", user.ID)
	a, err := f.repo.CreateAsset(f.ctx, uuid.New().String(), user.ID, "post_image", "media", key, "image/jpeg", 5000)
	if err != nil {
		t.Fatalf("CreateAsset failed: %v", err)
	}

	// GetAssetByID
	got, err := f.repo.GetAssetByID(f.ctx, a.ID)
	if err != nil || got.Status != "pending" {
		t.Fatalf("GetAssetByID failed: %v", err)
	}

	// CompleteAsset
	completed, err := f.repo.CompleteAsset(f.ctx, a.ID)
	if err != nil || completed.Status != "ready" {
		t.Fatalf("CompleteAsset failed: %v", err)
	}

	// DeleteAssets
	if err := f.repo.DeleteAssets(f.ctx, []string{a.ID}); err != nil {
		t.Fatalf("DeleteAssets failed: %v", err)
	}

	// Test GetExpiredPendingAssets
	expiredKey := fmt.Sprintf("u/%s/expired.jpg", user.ID)
	expiredID := uuid.New().String()
	_, err = f.appPool.Exec(f.ctx, `
		INSERT INTO public.assets (id, owner_id, kind, storage_bucket, storage_key, mime_type, size_bytes, status, upload_expires_at, created_at)
		VALUES ($1, $2, 'post_image', 'media', $3, 'image/jpeg', 1000, 'pending', now() - interval '1 hour', now() - interval '2 hours');
	`, expiredID, user.ID, expiredKey)
	if err != nil {
		t.Fatalf("failed to insert expired pending asset: %v", err)
	}

	expiredAssets, err := f.repo.GetExpiredPendingAssets(f.ctx)
	if err != nil {
		t.Fatalf("GetExpiredPendingAssets failed: %v", err)
	}
	foundExpired := false
	for _, ea := range expiredAssets {
		if ea.ID == expiredID {
			foundExpired = true
			break
		}
	}
	if !foundExpired {
		t.Fatalf("expected to find expired asset %s in GetExpiredPendingAssets results", expiredID)
	}

	// Clean up expired asset
	if err := f.repo.DeleteAssets(f.ctx, []string{expiredID}); err != nil {
		t.Fatalf("DeleteAssets on expired asset failed: %v", err)
	}
}

func TestRepository_AuthLifecycle(t *testing.T) {
	f := setupDB(t)

	email := "auth_test@example.com"
	codeHash := fmt.Sprintf("%x", sha256.Sum256([]byte("code_seed_1")))

	// CreateLoginCode
	err := f.repo.CreateLoginCode(f.ctx, email, codeHash, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("CreateLoginCode failed: %v", err)
	}

	// CheckLoginCodeCooldown
	inCooldown, err := f.repo.CheckLoginCodeCooldown(f.ctx, email, 60*time.Second)
	if err != nil || !inCooldown {
		t.Fatalf("expected cooldown=true, got %v (err: %v)", inCooldown, err)
	}

	// GetLatestActiveCode
	code, err := f.repo.GetLatestActiveCode(f.ctx, email)
	if err != nil || code.CodeHash != codeHash {
		t.Fatalf("GetLatestActiveCode failed: %v", err)
	}

	// IncrementAttempts
	if err := f.repo.IncrementAttempts(f.ctx, code.ID); err != nil {
		t.Fatalf("IncrementAttempts failed: %v", err)
	}

	// MarkCodeConsumed
	if err := f.repo.MarkCodeConsumed(f.ctx, code.ID); err != nil {
		t.Fatalf("MarkCodeConsumed failed: %v", err)
	}

	// Refresh token operations
	u := makeTestUser(t, f, "refauth")
	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte("refresh_token_1")))
	err = f.repo.CreateRefreshToken(f.ctx, u.ID, tokenHash, nil, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}

	rt, err := f.repo.FindRefreshTokenByHash(f.ctx, tokenHash)
	if err != nil || rt.UserID != u.ID {
		t.Fatalf("FindRefreshTokenByHash failed: %v", err)
	}

	// Rotate
	newTokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte("refresh_token_2")))
	err = f.repo.RotateRefreshToken(f.ctx, rt.ID, u.ID, newTokenHash, nil, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("RotateRefreshToken failed: %v", err)
	}

	// RevokeAll
	if err := f.repo.RevokeAllUserRefreshTokens(f.ctx, u.ID); err != nil {
		t.Fatalf("RevokeAllUserRefreshTokens failed: %v", err)
	}

	// RevokeRefreshTokenByHash
	singleTokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte("single_refresh_token")))
	err = f.repo.CreateRefreshToken(f.ctx, u.ID, singleTokenHash, nil, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}

	if err := f.repo.RevokeRefreshTokenByHash(f.ctx, singleTokenHash); err != nil {
		t.Fatalf("RevokeRefreshTokenByHash failed: %v", err)
	}

	revokedRT, err := f.repo.FindRefreshTokenByHash(f.ctx, singleTokenHash)
	if err != nil {
		t.Fatalf("FindRefreshTokenByHash failed: %v", err)
	}
	if revokedRT.RevokedAt == nil {
		t.Fatal("expected RevokedAt to be set after RevokeRefreshTokenByHash, got nil")
	}
}
