package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/middleware"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/services"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/views"
	"github.com/gorilla/mux"
)

type AssetController struct {
	assetService *services.AssetService
}

func NewAssetController(assetService *services.AssetService) *AssetController {
	return &AssetController{assetService: assetService}
}

type CreateAssetRequest struct {
	MimeType  string `json:"mimeType"`
	SizeBytes int64  `json:"sizeBytes"`
	Kind      string `json:"kind,omitempty"`
}

// CreateAsset handles POST /assets
func (c *AssetController) CreateAsset(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		views.WriteProblem(w, r, http.StatusUnauthorized, "Authentication required.")
		return
	}

	var req CreateAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		views.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON request body.")
		return
	}

	assetID, presignedURL, err := c.assetService.CreateAsset(r.Context(), userID, req.Kind, req.MimeType, req.SizeBytes)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, views.CreateAssetResponse{
		AssetID:         assetID,
		PresignedPutURL: presignedURL,
	})
}

// CompleteAsset handles POST /assets/{id}/complete
func (c *AssetController) CompleteAsset(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	role := middleware.GetRole(r.Context())
	if userID == "" {
		views.WriteProblem(w, r, http.StatusUnauthorized, "Authentication required.")
		return
	}

	assetID := mux.Vars(r)["id"]
	if assetID == "" {
		views.WriteProblem(w, r, http.StatusBadRequest, "Asset ID parameter is required.")
		return
	}

	publicURL, err := c.assetService.CompleteAsset(r.Context(), assetID, userID, role)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, views.CompleteAssetResponse{
		AssetID: assetID,
		URL:     publicURL,
	})
}
