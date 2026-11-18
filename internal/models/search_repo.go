package models

import (
	"context"
	"fmt"
	"strings"
)

// SearchUsers searches users by username prefix or display_name text search.
func (r *Repository) SearchUsers(ctx context.Context, q string, limit, offset int) ([]*UserDetail, error) {
	query := `
SELECT u.id, u.username, u.email, u.display_name, u.birthdate, u.avatar_asset_id,
       a.storage_key, u.role, u.follower_count, u.following_count, u.created_at
FROM public.users u
LEFT JOIN public.assets a ON a.id = u.avatar_asset_id
WHERE ((u.username::text) LIKE lower($1) || '%'
   OR (websearch_to_tsquery('simple', $1) != ''::tsquery AND u.search_vector @@ websearch_to_tsquery('simple', $1)))
ORDER BY u.created_at DESC
LIMIT $2 OFFSET $3;`

	cleanQ := strings.TrimSpace(q)
	rows, err := r.db.Query(ctx, query, cleanQ, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to search users: %w", err)
	}
	defer rows.Close()

	var users []*UserDetail
	for rows.Next() {
		var u UserDetail
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
		); err != nil {
			return nil, fmt.Errorf("failed to scan search user row: %w", err)
		}
		users = append(users, &u)
	}
	return users, rows.Err()
}

// SearchPosts searches non-deleted posts using full-text search with limit and offset.
func (r *Repository) SearchPosts(ctx context.Context, q string, currentUserID *string, limit, offset int) ([]*PostDetail, error) {
	query := `
SELECT p.id, p.author_id, p.body, p.like_count, p.comment_count, p.share_count, p.created_at, p.edited_at,
       u.username, u.display_name, a_u.storage_key, u.role, u.follower_count, u.following_count, u.created_at,
       CASE WHEN $2::uuid IS NOT NULL THEN
         EXISTS (SELECT 1 FROM public.likes l WHERE l.user_id = $2::uuid AND l.post_id = p.id)
       ELSE NULL END AS liked,
       ts_rank(p.search_vector, query) AS rank
FROM public.posts p
JOIN public.users u ON u.id = p.author_id
LEFT JOIN public.assets a_u ON a_u.id = u.avatar_asset_id
CROSS JOIN websearch_to_tsquery('english', $1) query
WHERE p.search_vector @@ query AND p.deleted_at IS NULL
ORDER BY rank DESC, p.created_at DESC
LIMIT $3 OFFSET $4;`

	cleanQ := strings.TrimSpace(q)
	rows, err := r.db.Query(ctx, query, cleanQ, currentUserID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to search posts: %w", err)
	}
	defer rows.Close()

	var posts []*PostDetail
	var postIDs []string
	for rows.Next() {
		var p PostDetail
		var rank float32
		if err := rows.Scan(
			&p.ID,
			&p.AuthorID,
			&p.Body,
			&p.LikeCount,
			&p.CommentCount,
			&p.ShareCount,
			&p.CreatedAt,
			&p.EditedAt,
			&p.AuthorUsername,
			&p.AuthorDisplayName,
			&p.AuthorAvatarStorageKey,
			&p.AuthorRole,
			&p.AuthorFollowerCount,
			&p.AuthorFollowingCount,
			&p.AuthorCreatedAt,
			&p.Liked,
			&rank,
		); err != nil {
			return nil, fmt.Errorf("failed to scan search post row: %w", err)
		}
		p.Assets = []PostAssetInfo{}
		posts = append(posts, &p)
		postIDs = append(postIDs, p.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(postIDs) > 0 {
		assetsMap, err := r.getAssetsForPosts(ctx, postIDs)
		if err != nil {
			return nil, err
		}
		for _, p := range posts {
			if a, ok := assetsMap[p.ID]; ok {
				p.Assets = a
			}
		}
	}

	return posts, nil
}

// SearchComments searches non-deleted comments using full-text search with limit and offset.
func (r *Repository) SearchComments(ctx context.Context, q string, limit, offset int) ([]*CommentDetail, error) {
	query := `
SELECT c.id, c.post_id, c.author_id, c.body, c.created_at, c.edited_at,
       u.username, u.display_name, a_u.storage_key, u.role, u.follower_count, u.following_count, u.created_at,
       ts_rank(c.search_vector, query) AS rank
FROM public.comments c
JOIN public.users u ON u.id = c.author_id
LEFT JOIN public.assets a_u ON a_u.id = u.avatar_asset_id
CROSS JOIN websearch_to_tsquery('english', $1) query
WHERE c.search_vector @@ query AND c.deleted_at IS NULL
ORDER BY rank DESC, c.created_at DESC
LIMIT $2 OFFSET $3;`

	cleanQ := strings.TrimSpace(q)
	rows, err := r.db.Query(ctx, query, cleanQ, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to search comments: %w", err)
	}
	defer rows.Close()

	var comments []*CommentDetail
	for rows.Next() {
		var c CommentDetail
		var rank float32
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
			&rank,
		); err != nil {
			return nil, fmt.Errorf("failed to scan search comment row: %w", err)
		}
		comments = append(comments, &c)
	}

	return comments, rows.Err()
}
