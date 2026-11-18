package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/models"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/views"
)

type UserRepository interface {
	GetUserByUsername(ctx context.Context, username string) (*models.UserDetail, error)
	GetUserByID(ctx context.Context, id string) (*models.UserDetail, error)
	UpdateUserProfile(ctx context.Context, id string, displayName *string, avatarAssetID *string, clearAvatar bool) (*models.UserDetail, error)
	Follow(ctx context.Context, followerID, followeeID string) (bool, error)
	Unfollow(ctx context.Context, followerID, followeeID string) (bool, error)
	GetFollowers(ctx context.Context, followeeID string, limit int, cursorTime *time.Time, cursorID string) ([]*models.UserDetail, error)
	GetFollowing(ctx context.Context, followerID string, limit int, cursorTime *time.Time, cursorID string) ([]*models.UserDetail, error)
}

type UserService struct {
	repo                 UserRepository
	storagePublicBaseURL string
}

func NewUserService(repo UserRepository, storagePublicBaseURL string) *UserService {
	return &UserService{
		repo:                 repo,
		storagePublicBaseURL: storagePublicBaseURL,
	}
}

func (s *UserService) AssetURL(storageKey *string) *string {
	if storageKey == nil || *storageKey == "" {
		return nil
	}
	url := fmt.Sprintf("%s/%s", s.storagePublicBaseURL, *storageKey)
	return &url
}

func (s *UserService) GetProfile(ctx context.Context, username string) (*views.PublicUserResponse, error) {
	u, err := s.repo.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	res := views.PublicUserResponse{
		ID:             u.ID,
		Username:       u.Username,
		DisplayName:    u.DisplayName,
		AvatarURL:      s.AssetURL(u.AvatarStorageKey),
		Role:           u.Role,
		FollowerCount:  u.FollowerCount,
		FollowingCount: u.FollowingCount,
		CreatedAt:      u.CreatedAt.Format(time.RFC3339),
	}
	return &res, nil
}

func (s *UserService) GetMe(ctx context.Context, userID string) (*views.PrivateUserResponse, error) {
	u, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	res := views.PrivateUserResponse{
		ID:             u.ID,
		Username:       u.Username,
		Email:          u.Email,
		DisplayName:    u.DisplayName,
		Birthdate:      u.Birthdate.Format("2006-01-02"),
		AvatarURL:      s.AssetURL(u.AvatarStorageKey),
		Role:           u.Role,
		FollowerCount:  u.FollowerCount,
		FollowingCount: u.FollowingCount,
		CreatedAt:      u.CreatedAt.Format(time.RFC3339),
	}
	return &res, nil
}

func (s *UserService) UpdateMe(ctx context.Context, userID string, displayName *string, avatarAssetID *string, clearAvatar bool) (*views.PrivateUserResponse, error) {
	if displayName != nil {
		l := len([]rune(*displayName))
		if l < 1 || l > 50 {
			return nil, fmt.Errorf("%w: displayName must be between 1 and 50 characters", ErrValidation)
		}
	}

	u, err := s.repo.UpdateUserProfile(ctx, userID, displayName, avatarAssetID, clearAvatar)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}

	res := views.PrivateUserResponse{
		ID:             u.ID,
		Username:       u.Username,
		Email:          u.Email,
		DisplayName:    u.DisplayName,
		Birthdate:      u.Birthdate.Format("2006-01-02"),
		AvatarURL:      s.AssetURL(u.AvatarStorageKey),
		Role:           u.Role,
		FollowerCount:  u.FollowerCount,
		FollowingCount: u.FollowingCount,
		CreatedAt:      u.CreatedAt.Format(time.RFC3339),
	}
	return &res, nil
}

func (s *UserService) FollowUser(ctx context.Context, currentUserID, targetUserID string) (bool, error) {
	if currentUserID == targetUserID {
		return false, ErrSelfFollow
	}

	// Verify target user exists
	if _, err := s.repo.GetUserByID(ctx, targetUserID); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return false, ErrNotFound
		}
		return false, err
	}

	_, err := s.repo.Follow(ctx, currentUserID, targetUserID)
	if err != nil {
		return false, err
	}
	// PUT is idempotent and returns current state (following: true)
	return true, nil
}

func (s *UserService) UnfollowUser(ctx context.Context, currentUserID, targetUserID string) (bool, error) {
	// Verify target user exists
	if _, err := s.repo.GetUserByID(ctx, targetUserID); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return false, ErrNotFound
		}
		return false, err
	}

	_, err := s.repo.Unfollow(ctx, currentUserID, targetUserID)
	if err != nil {
		return false, err
	}
	// DELETE returns current state (following: false)
	return false, nil
}

func (s *UserService) GetFollowers(ctx context.Context, targetUserID string, limit int, cursorStr string) (*views.UserListResponse, error) {
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

	// Fetch limit + 1 to detect hasMore
	users, err := s.repo.GetFollowers(ctx, targetUserID, limit+1, cursorTime, cursorID)
	if err != nil {
		return nil, err
	}

	hasMore := len(users) > limit
	if hasMore {
		users = users[:limit]
	}

	var data []views.PublicUserResponse
	for _, u := range users {
		data = append(data, views.PublicUserResponse{
			ID:             u.ID,
			Username:       u.Username,
			DisplayName:    u.DisplayName,
			AvatarURL:      s.AssetURL(u.AvatarStorageKey),
			Role:           u.Role,
			FollowerCount:  u.FollowerCount,
			FollowingCount: u.FollowingCount,
			CreatedAt:      u.CreatedAt.Format(time.RFC3339),
		})
	}

	var nextCursor *string
	if hasMore && len(users) > 0 {
		last := users[len(users)-1]
		if last.EdgeCreatedAt != nil {
			c := views.EncodeCursor(*last.EdgeCreatedAt, last.ID)
			nextCursor = &c
		}
	}

	return &views.UserListResponse{
		Data: data,
		Pagination: views.PaginationMeta{
			NextCursor: nextCursor,
			Limit:      limit,
			HasMore:    hasMore,
		},
	}, nil
}

func (s *UserService) GetFollowing(ctx context.Context, targetUserID string, limit int, cursorStr string) (*views.UserListResponse, error) {
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

	users, err := s.repo.GetFollowing(ctx, targetUserID, limit+1, cursorTime, cursorID)
	if err != nil {
		return nil, err
	}

	hasMore := len(users) > limit
	if hasMore {
		users = users[:limit]
	}

	var data []views.PublicUserResponse
	for _, u := range users {
		data = append(data, views.PublicUserResponse{
			ID:             u.ID,
			Username:       u.Username,
			DisplayName:    u.DisplayName,
			AvatarURL:      s.AssetURL(u.AvatarStorageKey),
			Role:           u.Role,
			FollowerCount:  u.FollowerCount,
			FollowingCount: u.FollowingCount,
			CreatedAt:      u.CreatedAt.Format(time.RFC3339),
		})
	}

	var nextCursor *string
	if hasMore && len(users) > 0 {
		last := users[len(users)-1]
		if last.EdgeCreatedAt != nil {
			c := views.EncodeCursor(*last.EdgeCreatedAt, last.ID)
			nextCursor = &c
		}
	}

	return &views.UserListResponse{
		Data: data,
		Pagination: views.PaginationMeta{
			NextCursor: nextCursor,
			Limit:      limit,
			HasMore:    hasMore,
		},
	}, nil
}
