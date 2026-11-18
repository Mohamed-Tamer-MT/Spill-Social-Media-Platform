package models

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Comment struct {
	ID        string
	PostID    string
	AuthorID  string
	Body      string
	CreatedAt time.Time
	EditedAt  *time.Time
	DeletedAt *time.Time
}

type CommentDetail struct {
	Comment
	AuthorUsername         string
	AuthorDisplayName      string
	AuthorAvatarStorageKey *string
	AuthorRole             string
	AuthorFollowerCount    int
	AuthorFollowingCount   int
	AuthorCreatedAt        time.Time
}

// CreateComment inserts a new comment on a post.
func (r *Repository) CreateComment(ctx context.Context, postID, authorID, body string) (*CommentDetail, error) {
	query := `
INSERT INTO public.comments (id, post_id, author_id, body, created_at)
VALUES (gen_random_uuid(), $1, $2, $3, now())
RETURNING id, post_id, author_id, body, created_at;`

	var c CommentDetail
	err := r.db.QueryRow(ctx, query, postID, authorID, body).Scan(
		&c.ID,
		&c.PostID,
		&c.AuthorID,
		&c.Body,
		&c.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert comment: %w", err)
	}

	// Fetch author details
	author, err := r.GetUserByID(ctx, authorID)
	if err == nil {
		c.AuthorUsername = author.Username
		c.AuthorDisplayName = author.DisplayName
		c.AuthorAvatarStorageKey = author.AvatarStorageKey
		c.AuthorRole = author.Role
		c.AuthorFollowerCount = author.FollowerCount
		c.AuthorFollowingCount = author.FollowingCount
		c.AuthorCreatedAt = author.CreatedAt
	}

	return &c, nil
}

// GetCommentByID retrieves a comment by ID (even if soft-deleted, for authorization checks).
func (r *Repository) GetCommentByID(ctx context.Context, id string) (*Comment, error) {
	query := `
SELECT id, post_id, author_id, body, created_at, edited_at, deleted_at
FROM public.comments
WHERE id = $1;`

	var c Comment
	err := r.db.QueryRow(ctx, query, id).Scan(
		&c.ID,
		&c.PostID,
		&c.AuthorID,
		&c.Body,
		&c.CreatedAt,
		&c.EditedAt,
		&c.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to query comment by id: %w", err)
	}
	return &c, nil
}

// GetCommentsByPostID returns non-deleted comments on a post in chronological order.
func (r *Repository) GetCommentsByPostID(ctx context.Context, postID string, limit int, cursorTime *time.Time, cursorID string) ([]*CommentDetail, error) {
	var query string
	var rows pgx.Rows
	var err error

	if cursorTime != nil && cursorID != "" {
		query = `
SELECT c.id, c.post_id, c.author_id, c.body, c.created_at, c.edited_at,
       u.username, u.display_name, a_u.storage_key, u.role, u.follower_count, u.following_count, u.created_at
FROM public.comments c
JOIN public.users u ON u.id = c.author_id
LEFT JOIN public.assets a_u ON a_u.id = u.avatar_asset_id
WHERE c.post_id = $1 AND c.deleted_at IS NULL
  AND (c.created_at, c.id) > ($2, $3::uuid)
ORDER BY c.created_at ASC, c.id ASC
LIMIT $4;`
		rows, err = r.db.Query(ctx, query, postID, *cursorTime, cursorID, limit)
	} else {
		query = `
SELECT c.id, c.post_id, c.author_id, c.body, c.created_at, c.edited_at,
       u.username, u.display_name, a_u.storage_key, u.role, u.follower_count, u.following_count, u.created_at
FROM public.comments c
JOIN public.users u ON u.id = c.author_id
LEFT JOIN public.assets a_u ON a_u.id = u.avatar_asset_id
WHERE c.post_id = $1 AND c.deleted_at IS NULL
ORDER BY c.created_at ASC, c.id ASC
LIMIT $2;`
		rows, err = r.db.Query(ctx, query, postID, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query comments: %w", err)
	}
	defer rows.Close()

	var comments []*CommentDetail
	for rows.Next() {
		var c CommentDetail
		if err := rows.Scan(
			&c.ID,
			&c.PostID,
			&c.AuthorID,
			&c.Body,
			&c.CreatedAt,
			&c.EditedAt,
			&c.AuthorUsername,
			&c.AuthorDisplayName,
			&c.AuthorAvatarStorageKey,
			&c.AuthorRole,
			&c.AuthorFollowerCount,
			&c.AuthorFollowingCount,
			&c.AuthorCreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan comment row: %w", err)
		}
		comments = append(comments, &c)
	}

	return comments, rows.Err()
}

// SoftDeleteComment marks a comment as soft-deleted.
func (r *Repository) SoftDeleteComment(ctx context.Context, id string) error {
	query := `UPDATE public.comments SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL;`
	tag, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to soft delete comment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
