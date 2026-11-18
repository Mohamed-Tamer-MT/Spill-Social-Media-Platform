package models

import (
	"context"
	"fmt"
	"time"
)

type Share struct {
	ID        string
	UserID    string
	PostID    string
	Quote     *string
	CreatedAt time.Time
}

// Follow inserts a follow edge idempotently. Returns whether a new row was inserted.
func (r *Repository) Follow(ctx context.Context, followerID, followeeID string) (bool, error) {
	query := `
INSERT INTO public.follows (follower_id, followee_id, created_at)
VALUES ($1, $2, now())
ON CONFLICT (follower_id, followee_id) DO NOTHING;`

	tag, err := r.db.Exec(ctx, query, followerID, followeeID)
	if err != nil {
		return false, fmt.Errorf("failed to follow user: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Unfollow deletes a follow edge. Returns whether a row was deleted.
func (r *Repository) Unfollow(ctx context.Context, followerID, followeeID string) (bool, error) {
	query := `DELETE FROM public.follows WHERE follower_id = $1 AND followee_id = $2;`
	tag, err := r.db.Exec(ctx, query, followerID, followeeID)
	if err != nil {
		return false, fmt.Errorf("failed to unfollow user: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// LikePost inserts a like row idempotently. Returns whether a new row was inserted.
func (r *Repository) LikePost(ctx context.Context, userID, postID string) (bool, error) {
	query := `
INSERT INTO public.likes (user_id, post_id, created_at)
VALUES ($1, $2, now())
ON CONFLICT (user_id, post_id) DO NOTHING;`

	tag, err := r.db.Exec(ctx, query, userID, postID)
	if err != nil {
		return false, fmt.Errorf("failed to like post: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// UnlikePost deletes a like row. Returns whether a row was deleted.
func (r *Repository) UnlikePost(ctx context.Context, userID, postID string) (bool, error) {
	query := `DELETE FROM public.likes WHERE user_id = $1 AND post_id = $2;`
	tag, err := r.db.Exec(ctx, query, userID, postID)
	if err != nil {
		return false, fmt.Errorf("failed to unlike post: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// CreateShare inserts or updates a post share.
func (r *Repository) CreateShare(ctx context.Context, userID, postID string, quote *string) (*Share, error) {
	query := `
INSERT INTO public.shares (id, user_id, post_id, quote, created_at)
VALUES (gen_random_uuid(), $1, $2, $3, now())
ON CONFLICT (user_id, post_id) DO UPDATE
SET quote = COALESCE(EXCLUDED.quote, shares.quote)
RETURNING id, user_id, post_id, quote, created_at;`

	var s Share
	err := r.db.QueryRow(ctx, query, userID, postID, quote).Scan(
		&s.ID,
		&s.UserID,
		&s.PostID,
		&s.Quote,
		&s.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create share: %w", err)
	}
	return &s, nil
}
