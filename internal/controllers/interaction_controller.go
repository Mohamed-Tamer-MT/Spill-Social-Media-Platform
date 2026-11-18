package controllers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/middleware"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/services"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/views"
	"github.com/go-chi/chi/v5"
)

type InteractionController struct {
	postService *services.PostService
}

func NewInteractionController(postService *services.PostService) *InteractionController {
	return &InteractionController{postService: postService}
}

type CreateShareRequest struct {
	Quote *string `json:"quote"`
}

// Like handles PUT /posts/{id}/like
func (c *InteractionController) Like(w http.ResponseWriter, r *http.Request) {
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

	liked, err := c.postService.LikePost(r.Context(), userID, postID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, views.LikeStateResponse{Liked: liked})
}

// Unlike handles DELETE /posts/{id}/like
func (c *InteractionController) Unlike(w http.ResponseWriter, r *http.Request) {
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

	liked, err := c.postService.UnlikePost(r.Context(), userID, postID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, views.LikeStateResponse{Liked: liked})
}

// Share handles POST /posts/{id}/share
func (c *InteractionController) Share(w http.ResponseWriter, r *http.Request) {
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

	var quote *string
	if r.Body != nil {
		var req CreateShareRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil && err != io.EOF {
			views.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON request body.")
			return
		}
		quote = req.Quote
	}

	res, err := c.postService.SharePost(r.Context(), userID, postID, quote)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, res)
}
