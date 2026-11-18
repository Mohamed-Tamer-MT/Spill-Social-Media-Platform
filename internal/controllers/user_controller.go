package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/middleware"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/services"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/views"
	"github.com/go-chi/chi/v5"
)

type UserController struct {
	userService *services.UserService
}

func NewUserController(userService *services.UserService) *UserController {
	return &UserController{userService: userService}
}

// GetProfile handles GET /users/{username}
func (c *UserController) GetProfile(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	if username == "" {
		views.WriteProblem(w, r, http.StatusBadRequest, "Username parameter is required.")
		return
	}

	res, err := c.userService.GetProfile(r.Context(), username)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, res)
}

// GetMe handles GET /me
func (c *UserController) GetMe(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		views.WriteProblem(w, r, http.StatusUnauthorized, "Authentication required.")
		return
	}

	res, err := c.userService.GetMe(r.Context(), userID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, res)
}

// UpdateMe handles PATCH /me
func (c *UserController) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		views.WriteProblem(w, r, http.StatusUnauthorized, "Authentication required.")
		return
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		views.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON request body.")
		return
	}

	var displayName *string
	var avatarAssetID *string
	clearAvatar := false

	if val, ok := raw["displayName"]; ok {
		var dn string
		if err := json.Unmarshal(val, &dn); err != nil {
			views.WriteProblem(w, r, http.StatusUnprocessableEntity, "displayName must be a string.")
			return
		}
		displayName = &dn
	}

	if val, ok := raw["avatarAssetId"]; ok {
		if string(val) == "null" {
			clearAvatar = true
		} else {
			var id string
			if err := json.Unmarshal(val, &id); err != nil {
				views.WriteProblem(w, r, http.StatusUnprocessableEntity, "avatarAssetId must be a valid UUID string or null.")
				return
			}
			avatarAssetID = &id
		}
	}

	res, err := c.userService.UpdateMe(r.Context(), userID, displayName, avatarAssetID, clearAvatar)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, res)
}

// Follow handles PUT /users/{id}/follow
func (c *UserController) Follow(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		views.WriteProblem(w, r, http.StatusUnauthorized, "Authentication required.")
		return
	}

	targetID := chi.URLParam(r, "id")
	if targetID == "" {
		views.WriteProblem(w, r, http.StatusBadRequest, "User ID parameter is required.")
		return
	}

	following, err := c.userService.FollowUser(r.Context(), userID, targetID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, views.FollowStateResponse{Following: following})
}

// Unfollow handles DELETE /users/{id}/follow
func (c *UserController) Unfollow(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		views.WriteProblem(w, r, http.StatusUnauthorized, "Authentication required.")
		return
	}

	targetID := chi.URLParam(r, "id")
	if targetID == "" {
		views.WriteProblem(w, r, http.StatusBadRequest, "User ID parameter is required.")
		return
	}

	following, err := c.userService.UnfollowUser(r.Context(), userID, targetID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, views.FollowStateResponse{Following: following})
}

// GetFollowers handles GET /users/{id}/followers
func (c *UserController) GetFollowers(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	if targetID == "" {
		views.WriteProblem(w, r, http.StatusBadRequest, "User ID parameter is required.")
		return
	}

	limit := parseLimit(r, 20, 50)
	cursor := r.URL.Query().Get("cursor")

	res, err := c.userService.GetFollowers(r.Context(), targetID, limit, cursor)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, res)
}

// GetFollowing handles GET /users/{id}/following
func (c *UserController) GetFollowing(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	if targetID == "" {
		views.WriteProblem(w, r, http.StatusBadRequest, "User ID parameter is required.")
		return
	}

	limit := parseLimit(r, 20, 50)
	cursor := r.URL.Query().Get("cursor")

	res, err := c.userService.GetFollowing(r.Context(), targetID, limit, cursor)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, res)
}
