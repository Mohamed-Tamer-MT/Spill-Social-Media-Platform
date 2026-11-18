package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/middleware"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/services"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/views"
	"github.com/go-chi/chi/v5"
)

type PostController struct {
	postService *services.PostService
}

func NewPostController(postService *services.PostService) *PostController {
	return &PostController{postService: postService}
}

type CreatePostRequest struct {
	Body     string   `json:"body"`
	AssetIDs []string `json:"assetIds"`
}

type UpdatePostRequest struct {
	Body string `json:"body"`
}

// CreatePost handles POST /posts
func (c *PostController) CreatePost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		views.WriteProblem(w, r, http.StatusUnauthorized, "Authentication required.")
		return
	}

	var req CreatePostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		views.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON request body.")
		return
	}

	res, err := c.postService.CreatePost(r.Context(), userID, req.Body, req.AssetIDs)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, res)
}

// GetPost handles GET /posts/{id}
func (c *PostController) GetPost(w http.ResponseWriter, r *http.Request) {
	postID := chi.URLParam(r, "id")
	if postID == "" {
		views.WriteProblem(w, r, http.StatusBadRequest, "Post ID parameter is required.")
		return
	}

	var currentUserID *string
	if uid := middleware.GetUserID(r.Context()); uid != "" {
		currentUserID = &uid
	}

	res, err := c.postService.GetPost(r.Context(), postID, currentUserID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, res)
}

// UpdatePost handles PATCH /posts/{id}
func (c *PostController) UpdatePost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	role := middleware.GetRole(r.Context())
	if userID == "" {
		views.WriteProblem(w, r, http.StatusUnauthorized, "Authentication required.")
		return
	}

	postID := chi.URLParam(r, "id")
	if postID == "" {
		views.WriteProblem(w, r, http.StatusBadRequest, "Post ID parameter is required.")
		return
	}

	var req UpdatePostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		views.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON request body.")
		return
	}

	res, err := c.postService.UpdatePost(r.Context(), postID, userID, role, req.Body)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, res)
}

// DeletePost handles DELETE /posts/{id}
func (c *PostController) DeletePost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	role := middleware.GetRole(r.Context())
	if userID == "" {
		views.WriteProblem(w, r, http.StatusUnauthorized, "Authentication required.")
		return
	}

	postID := chi.URLParam(r, "id")
	if postID == "" {
		views.WriteProblem(w, r, http.StatusBadRequest, "Post ID parameter is required.")
		return
	}

	if err := c.postService.DeletePost(r.Context(), postID, userID, role); err != nil {
		handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetFeed handles GET /feed
func (c *PostController) GetFeed(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		views.WriteProblem(w, r, http.StatusUnauthorized, "Authentication required.")
		return
	}

	limit := parseLimit(r, 20, 50)
	cursor := r.URL.Query().Get("cursor")

	res, err := c.postService.GetFeed(r.Context(), userID, limit, cursor)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, res)
}

// GetUserPosts handles GET /users/{id}/posts
func (c *PostController) GetUserPosts(w http.ResponseWriter, r *http.Request) {
	targetUserID := chi.URLParam(r, "id")
	if targetUserID == "" {
		views.WriteProblem(w, r, http.StatusBadRequest, "User ID parameter is required.")
		return
	}

	var currentUserID *string
	if uid := middleware.GetUserID(r.Context()); uid != "" {
		currentUserID = &uid
	}

	limit := parseLimit(r, 20, 50)
	cursor := r.URL.Query().Get("cursor")

	res, err := c.postService.GetUserPosts(r.Context(), targetUserID, currentUserID, limit, cursor)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, res)
}
