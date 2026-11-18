package controllers

import (
	"net/http"
	"strings"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/middleware"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/services"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/views"
)

type SearchController struct {
	searchService *services.SearchService
}

func NewSearchController(searchService *services.SearchService) *SearchController {
	return &SearchController{searchService: searchService}
}

// Search handles GET /search
func (c *SearchController) Search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		views.WriteProblem(w, r, http.StatusUnprocessableEntity, "Query parameter 'q' is required.")
		return
	}

	searchType := strings.TrimSpace(r.URL.Query().Get("type"))
	if searchType == "" {
		views.WriteProblem(w, r, http.StatusUnprocessableEntity, "Query parameter 'type' is required (users, posts, comments).")
		return
	}

	if searchType != "users" && searchType != "posts" && searchType != "comments" {
		views.WriteProblem(w, r, http.StatusUnprocessableEntity, "Query parameter 'type' must be one of: users, posts, comments.")
		return
	}

	limit := parseLimit(r, 20, 50)
	cursor := r.URL.Query().Get("cursor")

	var currentUserID *string
	if uid := middleware.GetUserID(r.Context()); uid != "" {
		currentUserID = &uid
	}

	res, err := c.searchService.Search(r.Context(), q, searchType, currentUserID, limit, cursor)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, res)
}
