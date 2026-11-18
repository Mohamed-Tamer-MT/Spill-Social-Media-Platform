package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/models"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/services"
)

type AuthController struct {
	authService *services.AuthService
}

func NewAuthController(authService *services.AuthService) *AuthController {
	return &AuthController{
		authService: authService,
	}
}

type RequestCodeRequest struct {
	Email string `json:"email"`
}

type VerifyRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type SignupRequest struct {
	SignupToken      string `json:"signup_token"`
	SignupTokenCamel string `json:"signupToken"`
	Username         string `json:"username"`
	DisplayName      string `json:"display_name"`
	DisplayNameCamel string `json:"displayName"`
	Birthdate        string `json:"birthdate"`
}

type RefreshRequest struct {
	RefreshToken      string `json:"refresh_token"`
	RefreshTokenCamel string `json:"refreshToken"`
}

type LogoutRequest struct {
	RefreshToken      string `json:"refresh_token"`
	RefreshTokenCamel string `json:"refreshToken"`
}

type UserResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

type VerifyResponse struct {
	NewUser      bool          `json:"new_user"`
	AccessToken  string        `json:"access_token,omitempty"`
	RefreshToken string        `json:"refresh_token,omitempty"`
	SignupToken  string        `json:"signup_token,omitempty"`
	User         *UserResponse `json:"user,omitempty"`
}

type TokensResponse struct {
	User         *UserResponse `json:"user,omitempty"`
	AccessToken  string        `json:"access_token"`
	RefreshToken string        `json:"refresh_token"`
}

// RequestCode handles POST /auth/request-code.
// Always returns 202 Accepted for syntactically valid emails to prevent email enumeration.
func (c *AuthController) RequestCode(w http.ResponseWriter, r *http.Request) {
	var req RequestCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid request body"})
		return
	}

	trimmedEmail := strings.TrimSpace(req.Email)
	if trimmedEmail == "" || !strings.Contains(trimmedEmail, "@") {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "valid email is required"})
		return
	}

	_ = c.authService.RequestCode(r.Context(), trimmedEmail)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"message": "If the email is valid, a login code has been sent.",
	})
}

// Verify handles POST /auth/verify.
func (c *AuthController) Verify(w http.ResponseWriter, r *http.Request) {
	var req VerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid request body"})
		return
	}

	trimmedEmail := strings.TrimSpace(req.Email)
	trimmedCode := strings.TrimSpace(req.Code)
	if trimmedEmail == "" || len(trimmedCode) != 6 {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "email and 6-digit code are required"})
		return
	}

	result, err := c.authService.VerifyCode(r.Context(), trimmedEmail, trimmedCode, r.UserAgent())
	if err != nil {
		if errors.Is(err, services.ErrTooManyAttempts) {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		if errors.Is(err, services.ErrInvalidCredentials) {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired verification code"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "verification failed"})
		return
	}

	if result.IsNewUser {
		respondJSON(w, http.StatusOK, VerifyResponse{
			NewUser:     true,
			SignupToken: result.SignupToken,
		})
		return
	}

	// Set refresh token cookie
	if result.RefreshToken != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     "refresh_token",
			Value:    result.RefreshToken,
			Path:     "/auth",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteStrictMode,
		})
	}

	respondJSON(w, http.StatusOK, VerifyResponse{
		NewUser:      false,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		User: &UserResponse{
			ID:          result.User.ID,
			Username:    result.User.Username,
			DisplayName: result.User.DisplayName,
			Role:        result.User.Role,
		},
	})
}

// Signup handles POST /auth/signup.
func (c *AuthController) Signup(w http.ResponseWriter, r *http.Request) {
	var req SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid request body"})
		return
	}

	signupToken := req.SignupToken
	if signupToken == "" {
		signupToken = req.SignupTokenCamel
	}
	if signupToken == "" {
		authHeader := r.Header.Get("Authorization")
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			signupToken = parts[1]
		}
	}

	if signupToken == "" {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "signup token is required"})
		return
	}

	displayName := req.DisplayName
	if displayName == "" {
		displayName = req.DisplayNameCamel
	}

	tokens, user, err := c.authService.Signup(r.Context(), signupToken, req.Username, displayName, req.Birthdate, r.UserAgent())
	if err != nil {
		if strings.Contains(err.Error(), "invalid signup token") {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		if errors.Is(err, services.ErrInvalidUsername) ||
			errors.Is(err, services.ErrInvalidDisplayName) ||
			errors.Is(err, services.ErrInvalidBirthdate) ||
			errors.Is(err, services.ErrUnderage) {
			respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		if errors.Is(err, models.ErrUsernameTaken) {
			respondJSON(w, http.StatusConflict, map[string]string{"error": "username is already taken"})
			return
		}
		if errors.Is(err, models.ErrEmailRegistered) {
			respondJSON(w, http.StatusConflict, map[string]string{"error": "email is already registered"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "signup failed"})
		return
	}

	if tokens.RefreshToken != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     "refresh_token",
			Value:    tokens.RefreshToken,
			Path:     "/auth",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteStrictMode,
		})
	}

	respondJSON(w, http.StatusCreated, TokensResponse{
		User: &UserResponse{
			ID:          user.ID,
			Username:    user.Username,
			DisplayName: user.DisplayName,
			Role:        user.Role,
		},
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	})
}

// Refresh handles POST /auth/refresh.
func (c *AuthController) Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	refreshToken := req.RefreshToken
	if refreshToken == "" {
		refreshToken = req.RefreshTokenCamel
	}
	if refreshToken == "" {
		if cookie, err := r.Cookie("refresh_token"); err == nil {
			refreshToken = cookie.Value
		}
	}

	if refreshToken == "" {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "refresh token is required"})
		return
	}

	tokens, err := c.authService.RefreshToken(r.Context(), refreshToken, r.UserAgent())
	if err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid, expired, or revoked refresh token"})
		return
	}

	if tokens.RefreshToken != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     "refresh_token",
			Value:    tokens.RefreshToken,
			Path:     "/auth",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteStrictMode,
		})
	}

	respondJSON(w, http.StatusOK, TokensResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	})
}

// Logout handles POST /auth/logout.
func (c *AuthController) Logout(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "authorization header required"})
		return
	}

	var req LogoutRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	refreshToken := req.RefreshToken
	if refreshToken == "" {
		refreshToken = req.RefreshTokenCamel
	}
	if refreshToken == "" {
		if cookie, err := r.Cookie("refresh_token"); err == nil {
			refreshToken = cookie.Value
		}
	}

	_ = c.authService.Logout(r.Context(), refreshToken)

	// Clear refresh token cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/auth",
		MaxAge:   -1,
		HttpOnly: true,
	})

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "successfully logged out",
	})
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
