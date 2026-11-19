package models

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type UserDetail struct {
	ID               string
	Username         string
	Email            string
	DisplayName      string
	Birthdate        time.Time
	AvatarAssetID    *string
	AvatarStorageKey *string
	Role             string
	FollowerCount    int
	FollowingCount   int
	CreatedAt        time.Time
	EdgeCreatedAt    *time.Time // for paginated follow edges
}

// GetUserByUsername retrieves a public/private profile by case-insensitive username.
func (r *Repository) GetUserByUsername(ctx context.Context, username string) (*UserDetail, error) {
	query := `
SELECT u.id, u.username, u.email, u.display_name, u.birthdate, u.avatar_asset_id,
       a.storage_key, u.role, u.follower_count, u.following_count, u.created_at
FROM public.users u
LEFT JOIN public.assets a ON a.id = u.avatar_asset_id
WHERE u.username = $1;`

	var u UserDetail
	err := r.db.QueryRow(ctx, query, username).Scan(
		&u.ID,
		&u.Username,
		&u.Email,
		&u.DisplayName,
		&u.Birthdate,
		&u.AvatarAssetID,
		&u.AvatarStorageKey,
		&u.Role,
		&u.FollowerCount,
		&u.FollowingCount,
		&u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to query user by username: %w", err)
	}
	return &u, nil
}

// GetUserByID retrieves a user by UUID including the avatar storage key.
func (r *Repository) GetUserByID(ctx context.Context, id string) (*UserDetail, error) {
	query := `
SELECT u.id, u.username, u.email, u.display_name, u.birthdate, u.avatar_asset_id,
       a.storage_key, u.role, u.follower_count, u.following_count, u.created_at
FROM public.users u
LEFT JOIN public.assets a ON a.id = u.avatar_asset_id
WHERE u.id = $1;`

	var u UserDetail
	err := r.db.QueryRow(ctx, query, id).Scan(
		&u.ID,
		&u.Username,
		&u.Email,
		&u.DisplayName,
		&u.Birthdate,
		&u.AvatarAssetID,
		&u.AvatarStorageKey,
		&u.Role,
		&u.FollowerCount,
		&u.FollowingCount,
		&u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to query user by id: %w", err)
	}
	return &u, nil
}

// UpdateUserProfile updates display_name and/or avatar_asset_id.
func (r *Repository) UpdateUserProfile(ctx context.Context, id string, displayName *string, avatarAssetID *string, clearAvatar bool) (*UserDetail, error) {
	query := `
UPDATE public.users
SET display_name = COALESCE($2, display_name),
    avatar_asset_id = CASE
      WHEN $3::boolean THEN NULL
      WHEN $4::uuid IS NOT NULL THEN $4::uuid
      ELSE avatar_asset_id
    END
WHERE id = $1
RETURNING id, username, email, display_name, birthdate, avatar_asset_id, role, follower_count, following_count, created_at;`

	var u UserDetail
	err := r.db.QueryRow(ctx, query, id, displayName, clearAvatar, avatarAssetID).Scan(
		&u.ID,
		&u.Username,
		&u.Email,
		&u.DisplayName,
		&u.Birthdate,
		&u.AvatarAssetID,
		&u.Role,
		&u.FollowerCount,
		&u.FollowingCount,
		&u.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "P0001" {
				return nil, fmt.Errorf("trigger validation error: %s", pgErr.Message)
			}
			if pgErr.Code == "22P02" {
				return nil, fmt.Errorf("invalid uuid syntax: %s", pgErr.Message)
			}
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to update user profile: %w", err)
	}

	// Fetch avatar storage key if present
	if u.AvatarAssetID != nil {
		if err := r.db.QueryRow(ctx, `SELECT storage_key FROM public.assets WHERE id = $1`, *u.AvatarAssetID).Scan(&u.AvatarStorageKey); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("failed to load avatar storage key: %w", err)
		}
	}

	return &u, nil
}

// GetFollowers returns users following followeeID, ordered by edge created_at DESC, follower_id DESC.
func (r *Repository) GetFollowers(ctx context.Context, followeeID string, limit int, cursorTime *time.Time, cursorID string) ([]*UserDetail, error) {
	var query string
	var rows pgx.Rows
	var err error

	if cursorTime != nil && cursorID != "" {
		query = `
SELECT u.id, u.username, u.email, u.display_name, u.birthdate, u.avatar_asset_id,
       a.storage_key, u.role, u.follower_count, u.following_count, u.created_at, f.created_at
FROM public.follows f
JOIN public.users u ON u.id = f.follower_id
LEFT JOIN public.assets a ON a.id = u.avatar_asset_id
WHERE f.followee_id = $1
  AND (f.created_at, f.follower_id) < ($2, $3::uuid)
ORDER BY f.created_at DESC, f.follower_id DESC
LIMIT $4;`
		rows, err = r.db.Query(ctx, query, followeeID, *cursorTime, cursorID, limit)
	} else {
		query = `
SELECT u.id, u.username, u.email, u.display_name, u.birthdate, u.avatar_asset_id,
       a.storage_key, u.role, u.follower_count, u.following_count, u.created_at, f.created_at
FROM public.follows f
JOIN public.users u ON u.id = f.follower_id
LEFT JOIN public.assets a ON a.id = u.avatar_asset_id
WHERE f.followee_id = $1
ORDER BY f.created_at DESC, f.follower_id DESC
LIMIT $2;`
		rows, err = r.db.Query(ctx, query, followeeID, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query followers: %w", err)
	}
	defer rows.Close()

	var users []*UserDetail
	for rows.Next() {
		var u UserDetail
		var edgeTime time.Time
		if err := rows.Scan(
			&u.ID,
			&u.Username,
			&u.Email,
			&u.DisplayName,
			&u.Birthdate,
			&u.AvatarAssetID,
			&u.AvatarStorageKey,
			&u.Role,
			&u.FollowerCount,
			&u.FollowingCount,
			&u.CreatedAt,
			&edgeTime,
		); err != nil {
			return nil, fmt.Errorf("failed to scan follower row: %w", err)
		}
		u.EdgeCreatedAt = &edgeTime
		users = append(users, &u)
	}
	return users, rows.Err()
}

// GetFollowing returns users followed by followerID, ordered by edge created_at DESC, followee_id DESC.
func (r *Repository) GetFollowing(ctx context.Context, followerID string, limit int, cursorTime *time.Time, cursorID string) ([]*UserDetail, error) {
	var query string
	var rows pgx.Rows
	var err error

	if cursorTime != nil && cursorID != "" {
		query = `
SELECT u.id, u.username, u.email, u.display_name, u.birthdate, u.avatar_asset_id,
       a.storage_key, u.role, u.follower_count, u.following_count, u.created_at, f.created_at
FROM public.follows f
JOIN public.users u ON u.id = f.followee_id
LEFT JOIN public.assets a ON a.id = u.avatar_asset_id
WHERE f.follower_id = $1
  AND (f.created_at, f.followee_id) < ($2, $3::uuid)
ORDER BY f.created_at DESC, f.followee_id DESC
LIMIT $4;`
		rows, err = r.db.Query(ctx, query, followerID, *cursorTime, cursorID, limit)
	} else {
		query = `
SELECT u.id, u.username, u.email, u.display_name, u.birthdate, u.avatar_asset_id,
       a.storage_key, u.role, u.follower_count, u.following_count, u.created_at, f.created_at
FROM public.follows f
JOIN public.users u ON u.id = f.followee_id
LEFT JOIN public.assets a ON a.id = u.avatar_asset_id
WHERE f.follower_id = $1
ORDER BY f.created_at DESC, f.followee_id DESC
LIMIT $2;`
		rows, err = r.db.Query(ctx, query, followerID, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query following: %w", err)
	}
	defer rows.Close()

	var users []*UserDetail
	for rows.Next() {
		var u UserDetail
		var edgeTime time.Time
		if err := rows.Scan(
			&u.ID,
			&u.Username,
			&u.Email,
			&u.DisplayName,
			&u.Birthdate,
			&u.AvatarAssetID,
			&u.AvatarStorageKey,
			&u.Role,
			&u.FollowerCount,
			&u.FollowingCount,
			&u.CreatedAt,
			&edgeTime,
		); err != nil {
			return nil, fmt.Errorf("failed to scan following row: %w", err)
		}
		u.EdgeCreatedAt = &edgeTime
		users = append(users, &u)
	}
	return users, rows.Err()
}
