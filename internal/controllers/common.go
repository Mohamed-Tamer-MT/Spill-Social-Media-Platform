package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/services"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/views"
)

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func parseLimit(r *http.Request, defaultVal, maxVal int) int {
	lStr := r.URL.Query().Get("limit")
	if lStr == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(lStr)
	if err != nil || val < 1 {
		return defaultVal
	}
	if val > maxVal {
		return maxVal
	}
	return val
}

func handleServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, services.ErrNotFound) {
		views.WriteProblem(w, r, http.StatusNotFound, "The requested resource was not found.")
		return
	}
	if errors.Is(err, services.ErrForbidden) {
		views.WriteProblem(w, r, http.StatusForbidden, "You do not have permission to perform this action.")
		return
	}
	if errors.Is(err, services.ErrUnauthorized) {
		views.WriteProblem(w, r, http.StatusUnauthorized, "Authentication required.")
		return
	}
	if errors.Is(err, services.ErrConflict) {
		views.WriteProblem(w, r, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, services.ErrAlreadyReady) {
		views.WriteProblem(w, r, http.StatusConflict, "Asset is already marked as ready.")
		return
	}
	if errors.Is(err, services.ErrPayloadTooLarge) {
		views.WriteProblem(w, r, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	if errors.Is(err, services.ErrSelfFollow) {
		views.WriteProblem(w, r, http.StatusUnprocessableEntity, "Users cannot follow themselves.")
		return
	}
	if errors.Is(err, services.ErrValidation) {
		views.WriteProblem(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	views.WriteProblem(w, r, http.StatusInternalServerError, "An unexpected server error occurred.")
}
