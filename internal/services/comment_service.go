package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/models"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/views"
)

type CommentRepository interface {
	GetPostByID(ctx context.Context, id string, currentUserID *string) (*models.PostDetail, error)
	CreateComment(ctx context.Context, postID, authorID, body string) (*models.CommentDetail, error)
	GetCommentByID(ctx context.Context, id string) (*models.Comment, error)
	GetCommentsByPostID(ctx context.Context, postID string, limit int, cursorTime *time.Time, cursorID string) ([]*models.CommentDetail, error)
	SoftDeleteComment(ctx context.Context, id string) error
}

type CommentService struct {
	repo                 CommentRepository
	storagePublicBaseURL string
}

func NewCommentService(repo CommentRepository, storagePublicBaseURL string) *CommentService {
	return &CommentService{
		repo:                 repo,
		storagePublicBaseURL: storagePublicBaseURL,
	}
}

func (s *CommentService) ToCommentResponse(c *models.CommentDetail) views.CommentResponse {
	var avatarURL *string
	if c.AuthorAvatarStorageKey != nil && *c.AuthorAvatarStorageKey != "" {
		u := fmt.Sprintf("%s/%s", s.storagePublicBaseURL, *c.AuthorAvatarStorageKey)
		avatarURL = &u
	}

	author := views.PublicUserResponse{
		ID:             c.AuthorID,
		Username:       c.AuthorUsername,
		DisplayName:    c.AuthorDisplayName,
		AvatarURL:      avatarURL,
		Role:           c.AuthorRole,
		FollowerCount:  c.AuthorFollowerCount,
		FollowingCount: c.AuthorFollowingCount,
		CreatedAt:      c.AuthorCreatedAt.Format(time.RFC3339),
	}

	return views.CommentResponse{
		ID:        c.ID,
		PostID:    c.PostID,
		AuthorID:  c.AuthorID,
		Author:    &author,
		Body:      c.Body,
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
	}
}

func (s *CommentService) CreateComment(ctx context.Context, currentUserID, postID, body string) (*views.CommentResponse, error) {
	l := len([]rune(body))
	if l < 1 || l > 500 {
		return nil, fmt.Errorf("%w: comment body must be between 1 and 500 characters", ErrValidation)
	}

	// Verify post exists and is not soft-deleted
	if _, err := s.repo.GetPostByID(ctx, postID, nil); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	comment, err := s.repo.CreateComment(ctx, postID, currentUserID, body)
	if err != nil {
		return nil, err
	}

	res := s.ToCommentResponse(comment)
	return &res, nil
}

func (s *CommentService) GetComments(ctx context.Context, postID string, limit int, cursorStr string) (*views.CommentListResponse, error) {
	// Verify post exists and is not soft-deleted
	if _, err := s.repo.GetPostByID(ctx, postID, nil); err != nil {
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

	comments, err := s.repo.GetCommentsByPostID(ctx, postID, limit+1, cursorTime, cursorID)
	if err != nil {
		return nil, err
	}

	hasMore := len(comments) > limit
	if hasMore {
		comments = comments[:limit]
	}

	var data []views.CommentResponse
	for _, c := range comments {
		data = append(data, s.ToCommentResponse(c))
	}

	var nextCursor *string
	if hasMore && len(comments) > 0 {
		last := comments[len(comments)-1]
		c := views.EncodeCursor(last.CreatedAt, last.ID)
		nextCursor = &c
	}

	return &views.CommentListResponse{
		Data: data,
		Pagination: views.PaginationMeta{
			NextCursor: nextCursor,
			Limit:      limit,
			HasMore:    hasMore,
		},
	}, nil
}

func (s *CommentService) DeleteComment(ctx context.Context, commentID, currentUserID, userRole string) error {
	comment, err := s.repo.GetCommentByID(ctx, commentID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	if comment.DeletedAt != nil {
		return ErrNotFound
	}

	if comment.AuthorID != currentUserID && userRole != "admin" {
		return ErrForbidden
	}

	return s.repo.SoftDeleteComment(ctx, commentID)
}
