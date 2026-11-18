package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/middleware"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/services"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/views"
	"github.com/go-chi/chi/v5"
)

type CommentController struct {
	commentService *services.CommentService
}

func NewCommentController(commentService *services.CommentService) *CommentController {
	return &CommentController{commentService: commentService}
}

type CreateCommentRequest struct {
	Body string `json:"body"`
}

// CreateComment handles POST /posts/{id}/comments
func (c *CommentController) CreateComment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		views.WriteProblem(w, r, http.StatusUnauthorized, "Authentication required.")
		return
	}

	postID := chi.URLParam(r, "id")
	if postID == "" {
		views.WriteProblem(w, r, http.StatusBadRequest, "Post ID parameter is required.")
		return
	}

	var req CreateCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		views.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON request body.")
		return
	}

	res, err := c.commentService.CreateComment(r.Context(), userID, postID, req.Body)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, res)
}

// GetComments handles GET /posts/{id}/comments
func (c *CommentController) GetComments(w http.ResponseWriter, r *http.Request) {
	postID := chi.URLParam(r, "id")
	if postID == "" {
		views.WriteProblem(w, r, http.StatusBadRequest, "Post ID parameter is required.")
		return
	}

	limit := parseLimit(r, 20, 50)
	cursor := r.URL.Query().Get("cursor")

	res, err := c.commentService.GetComments(r.Context(), postID, limit, cursor)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, res)
}

// DeleteComment handles DELETE /comments/{id}
func (c *CommentController) DeleteComment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	role := middleware.GetRole(r.Context())
	if userID == "" {
		views.WriteProblem(w, r, http.StatusUnauthorized, "Authentication required.")
		return
	}

	commentID := chi.URLParam(r, "id")
	if commentID == "" {
		views.WriteProblem(w, r, http.StatusBadRequest, "Comment ID parameter is required.")
		return
	}

	if err := c.commentService.DeleteComment(r.Context(), commentID, userID, role); err != nil {
		handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
