package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/models"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/views"
)

type PostRepository interface {
	GetUserByID(ctx context.Context, id string) (*models.UserDetail, error)
	CreatePost(ctx context.Context, authorID, body string, assetIDs []string) (*models.PostDetail, error)
	GetPostByID(ctx context.Context, id string, currentUserID *string) (*models.PostDetail, error)
	UpdatePost(ctx context.Context, id, body string) (*models.Post, error)
	SoftDeletePost(ctx context.Context, id string) error
	GetFeed(ctx context.Context, userID string, limit int, cursorTime *time.Time, cursorID string) ([]*models.PostDetail, error)
	GetUserPosts(ctx context.Context, targetUserID string, currentUserID *string, limit int, cursorTime *time.Time, cursorID string) ([]*models.PostDetail, error)
	LikePost(ctx context.Context, userID, postID string) (bool, error)
	UnlikePost(ctx context.Context, userID, postID string) (bool, error)
	CreateShare(ctx context.Context, userID, postID string, quote *string) (*models.Share, error)
}

type PostService struct {
	repo                 PostRepository
	storagePublicBaseURL string
}

func NewPostService(repo PostRepository, storagePublicBaseURL string) *PostService {
	return &PostService{
		repo:                 repo,
		storagePublicBaseURL: storagePublicBaseURL,
	}
}

func (s *PostService) ToPostResponse(p *models.PostDetail) views.PostResponse {
	var avatarURL *string
	if p.AuthorAvatarStorageKey != nil && *p.AuthorAvatarStorageKey != "" {
		u := fmt.Sprintf("%s/%s", s.storagePublicBaseURL, *p.AuthorAvatarStorageKey)
		avatarURL = &u
	}

	author := views.PublicUserResponse{
		ID:             p.AuthorID,
		Username:       p.AuthorUsername,
		DisplayName:    p.AuthorDisplayName,
		AvatarURL:      avatarURL,
		Role:           p.AuthorRole,
		FollowerCount:  p.AuthorFollowerCount,
		FollowingCount: p.AuthorFollowingCount,
		CreatedAt:      p.AuthorCreatedAt.Format(time.RFC3339),
	}

	var assets []views.AssetResponse
	for _, a := range p.Assets {
		assets = append(assets, views.AssetResponse{
			ID:        a.ID,
			URL:       fmt.Sprintf("%s/%s", s.storagePublicBaseURL, a.StorageKey),
			MimeType:  a.MimeType,
			SizeBytes: a.SizeBytes,
			Status:    a.Status,
		})
	}
	if assets == nil {
		assets = []views.AssetResponse{}
	}

	return views.PostResponse{
		ID:           p.ID,
		AuthorID:     p.AuthorID,
		Author:       &author,
		Body:         p.Body,
		Assets:       assets,
		LikeCount:    p.LikeCount,
		CommentCount: p.CommentCount,
		ShareCount:   p.ShareCount,
		Liked:        p.Liked,
		CreatedAt:    p.CreatedAt.Format(time.RFC3339),
	}
}

func (s *PostService) CreatePost(ctx context.Context, authorID, body string, assetIDs []string) (*views.PostResponse, error) {
	// Rule 4: post has body text or at least one image
	trimmed := strings.TrimSpace(body)
	if trimmed == "" && len(assetIDs) == 0 {
		return nil, fmt.Errorf("%w: post must have body text or at least one image", ErrValidation)
	}

	if len([]rune(body)) > 2000 {
		return nil, fmt.Errorf("%w: post body must not exceed 2000 characters", ErrValidation)
	}

	post, err := s.repo.CreatePost(ctx, authorID, body, assetIDs)
	if err != nil {
		var trgErr *models.TriggerError
		if errors.As(err, &trgErr) {
			return nil, fmt.Errorf("%w: %s", ErrValidation, trgErr.Message)
		}
		return nil, err
	}

	res := s.ToPostResponse(post)
	return &res, nil
}

func (s *PostService) GetPost(ctx context.Context, id string, currentUserID *string) (*views.PostResponse, error) {
	post, err := s.repo.GetPostByID(ctx, id, currentUserID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	res := s.ToPostResponse(post)
	return &res, nil
}

func (s *PostService) UpdatePost(ctx context.Context, id, currentUserID, userRole, body string) (*views.PostResponse, error) {
	// Fetch post to check ownership (Rule 1: author or admin)
	existing, err := s.repo.GetPostByID(ctx, id, nil)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if existing.AuthorID != currentUserID && userRole != "admin" {
		return nil, ErrForbidden
	}

	l := len([]rune(body))
	if l < 1 || l > 2000 {
		return nil, fmt.Errorf("%w: post body must be between 1 and 2000 characters", ErrValidation)
	}

	if _, err := s.repo.UpdatePost(ctx, id, body); err != nil {
		return nil, err
	}

	return s.GetPost(ctx, id, &currentUserID)
}

func (s *PostService) DeletePost(ctx context.Context, id, currentUserID, userRole string) error {
	// Fetch post to check ownership (Rule 1: author or admin)
	existing, err := s.repo.GetPostByID(ctx, id, nil)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	if existing.AuthorID != currentUserID && userRole != "admin" {
		return ErrForbidden
	}

	return s.repo.SoftDeletePost(ctx, id)
}

func (s *PostService) GetFeed(ctx context.Context, currentUserID string, limit int, cursorStr string) (*views.PostListResponse, error) {
	var cursorTime *time.Time
	var cursorID string
	if cursorStr != "" {
		t, id, err := views.DecodeCursor(cursorStr)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid cursor", ErrValidation)
		}
		cursorTime = &t
		cursorID = id
	}

	posts, err := s.repo.GetFeed(ctx, currentUserID, limit+1, cursorTime, cursorID)
	if err != nil {
		return nil, err
	}

	hasMore := len(posts) > limit
	if hasMore {
		posts = posts[:limit]
	}

	var data []views.PostResponse
	for _, p := range posts {
		data = append(data, s.ToPostResponse(p))
	}

	var nextCursor *string
	if hasMore && len(posts) > 0 {
		last := posts[len(posts)-1]
		c := views.EncodeCursor(last.CreatedAt, last.ID)
		nextCursor = &c
	}

	return &views.PostListResponse{
		Data: data,
		Pagination: views.PaginationMeta{
			NextCursor: nextCursor,
			Limit:      limit,
			HasMore:    hasMore,
		},
	}, nil
}

func (s *PostService) GetUserPosts(ctx context.Context, targetUserID string, currentUserID *string, limit int, cursorStr string) (*views.PostListResponse, error) {
	// Verify user exists
	if _, err := s.repo.GetUserByID(ctx, targetUserID); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	var cursorTime *time.Time
	var cursorID string
	if cursorStr != "" {
		t, id, err := views.DecodeCursor(cursorStr)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid cursor", ErrValidation)
		}
		cursorTime = &t
		cursorID = id
	}

	posts, err := s.repo.GetUserPosts(ctx, targetUserID, currentUserID, limit+1, cursorTime, cursorID)
	if err != nil {
		return nil, err
	}

	hasMore := len(posts) > limit
	if hasMore {
		posts = posts[:limit]
	}

	var data []views.PostResponse
	for _, p := range posts {
		data = append(data, s.ToPostResponse(p))
	}

	var nextCursor *string
	if hasMore && len(posts) > 0 {
		last := posts[len(posts)-1]
		c := views.EncodeCursor(last.CreatedAt, last.ID)
		nextCursor = &c
	}

	return &views.PostListResponse{
		Data: data,
		Pagination: views.PaginationMeta{
			NextCursor: nextCursor,
			Limit:      limit,
			HasMore:    hasMore,
		},
	}, nil
}

func (s *PostService) LikePost(ctx context.Context, currentUserID, postID string) (bool, error) {
	// Verify post exists and not soft-deleted
	if _, err := s.repo.GetPostByID(ctx, postID, nil); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return false, ErrNotFound
		}
		return false, err
	}

	_, err := s.repo.LikePost(ctx, currentUserID, postID)
	if err != nil {
		return false, err
	}
	// PUT /posts/{id}/like is idempotent and returns current state (liked: true)
	return true, nil
}

func (s *PostService) UnlikePost(ctx context.Context, currentUserID, postID string) (bool, error) {
	// Verify post exists and not soft-deleted
	if _, err := s.repo.GetPostByID(ctx, postID, nil); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return false, ErrNotFound
		}
		return false, err
	}

	_, err := s.repo.UnlikePost(ctx, currentUserID, postID)
	if err != nil {
		return false, err
	}
	// DELETE /posts/{id}/like returns current state (liked: false)
	return false, nil
}

func (s *PostService) SharePost(ctx context.Context, currentUserID, postID string, quote *string) (*views.ShareResponse, error) {
	if quote != nil && len([]rune(*quote)) > 500 {
		return nil, fmt.Errorf("%w: quote must not exceed 500 characters", ErrValidation)
	}

	// Verify post exists and not soft-deleted
	if _, err := s.repo.GetPostByID(ctx, postID, nil); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	sh, err := s.repo.CreateShare(ctx, currentUserID, postID, quote)
	if err != nil {
		return nil, err
	}

	return &views.ShareResponse{
		ID:        sh.ID,
		UserID:    sh.UserID,
		PostID:    sh.PostID,
		Quote:     sh.Quote,
		CreatedAt: sh.CreatedAt.Format(time.RFC3339),
	}, nil
}
