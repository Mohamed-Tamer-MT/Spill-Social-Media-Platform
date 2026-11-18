package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/models"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/views"
)

type SearchRepository interface {
	SearchUsers(ctx context.Context, q string, limit, offset int) ([]*models.UserDetail, error)
	SearchPosts(ctx context.Context, q string, currentUserID *string, limit, offset int) ([]*models.PostDetail, error)
	SearchComments(ctx context.Context, q string, limit, offset int) ([]*models.CommentDetail, error)
}

type SearchService struct {
	repo                 SearchRepository
	storagePublicBaseURL string
}

func NewSearchService(repo SearchRepository, storagePublicBaseURL string) *SearchService {
	return &SearchService{
		repo:                 repo,
		storagePublicBaseURL: storagePublicBaseURL,
	}
}

func (s *SearchService) parseOffset(cursorStr string) int {
	if cursorStr == "" {
		return 0
	}
	if val, err := strconv.Atoi(cursorStr); err == nil && val >= 0 {
		return val
	}
	if b, err := base64.RawURLEncoding.DecodeString(cursorStr); err == nil {
		if val, err := strconv.Atoi(string(b)); err == nil && val >= 0 {
			return val
		}
	}
	if b, err := base64.StdEncoding.DecodeString(cursorStr); err == nil {
		if val, err := strconv.Atoi(string(b)); err == nil && val >= 0 {
			return val
		}
	}
	return 0
}

func (s *SearchService) Search(ctx context.Context, q, searchType string, currentUserID *string, limit int, cursorStr string) (*views.SearchResponse, error) {
	cleanQ := strings.TrimSpace(q)
	if cleanQ == "" {
		return nil, fmt.Errorf("%w: query parameter 'q' must not be empty", ErrValidation)
	}
	if len([]rune(cleanQ)) > 200 {
		return nil, fmt.Errorf("%w: query parameter 'q' must not exceed 200 characters", ErrValidation)
	}

	offset := s.parseOffset(cursorStr)

	switch searchType {
	case "users":
		users, err := s.repo.SearchUsers(ctx, cleanQ, limit+1, offset)
		if err != nil {
			return nil, err
		}

		hasMore := len(users) > limit
		if hasMore {
			users = users[:limit]
		}

		var data []views.PublicUserResponse
		for _, u := range users {
			var avatarURL *string
			if u.AvatarStorageKey != nil && *u.AvatarStorageKey != "" {
				url := fmt.Sprintf("%s/%s", s.storagePublicBaseURL, *u.AvatarStorageKey)
				avatarURL = &url
			}
			data = append(data, views.PublicUserResponse{
				ID:             u.ID,
				Username:       u.Username,
				DisplayName:    u.DisplayName,
				AvatarURL:      avatarURL,
				Role:           u.Role,
				FollowerCount:  u.FollowerCount,
				FollowingCount: u.FollowingCount,
				CreatedAt:      u.CreatedAt.Format(time.RFC3339),
			})
		}
		if data == nil {
			data = []views.PublicUserResponse{}
		}

		var nextCursor *string
		if hasMore {
			c := base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset + limit)))
			nextCursor = &c
		}

		return &views.SearchResponse{
			Data: data,
			Pagination: views.PaginationMeta{
				NextCursor: nextCursor,
				Limit:      limit,
				HasMore:    hasMore,
			},
		}, nil

	case "posts":
		posts, err := s.repo.SearchPosts(ctx, cleanQ, currentUserID, limit+1, offset)
		if err != nil {
			return nil, err
		}

		hasMore := len(posts) > limit
		if hasMore {
			posts = posts[:limit]
		}

		var data []views.PostResponse
		for _, p := range posts {
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

			data = append(data, views.PostResponse{
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
			})
		}
		if data == nil {
			data = []views.PostResponse{}
		}

		var nextCursor *string
		if hasMore {
			c := base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset + limit)))
			nextCursor = &c
		}

		return &views.SearchResponse{
			Data: data,
			Pagination: views.PaginationMeta{
				NextCursor: nextCursor,
				Limit:      limit,
				HasMore:    hasMore,
			},
		}, nil

	case "comments":
		comments, err := s.repo.SearchComments(ctx, cleanQ, limit+1, offset)
		if err != nil {
			return nil, err
		}

		hasMore := len(comments) > limit
		if hasMore {
			comments = comments[:limit]
		}

		var data []views.CommentResponse
		for _, c := range comments {
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

			data = append(data, views.CommentResponse{
				ID:        c.ID,
				PostID:    c.PostID,
				AuthorID:  c.AuthorID,
				Author:    &author,
				Body:      c.Body,
				CreatedAt: c.CreatedAt.Format(time.RFC3339),
			})
		}
		if data == nil {
			data = []views.CommentResponse{}
		}

		var nextCursor *string
		if hasMore {
			c := base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset + limit)))
			nextCursor = &c
		}

		return &views.SearchResponse{
			Data: data,
			Pagination: views.PaginationMeta{
				NextCursor: nextCursor,
				Limit:      limit,
				HasMore:    hasMore,
			},
		}, nil

	default:
		return nil, fmt.Errorf("%w: invalid search type '%s' (allowed: users, posts, comments)", ErrValidation, searchType)
	}
}
