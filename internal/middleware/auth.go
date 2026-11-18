package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/auth"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/views"
)

type contextKey string

const (
	UserIDKey contextKey = "userID"
	RoleKey   contextKey = "role"
)

// GetUserID retrieves the authenticated user ID from context.
func GetUserID(ctx context.Context) string {
	val, _ := ctx.Value(UserIDKey).(string)
	return val
}

// GetRole retrieves the authenticated user role from context.
func GetRole(ctx context.Context) string {
	val, _ := ctx.Value(RoleKey).(string)
	return val
}

// RequireAuth enforces a valid Bearer JWT access token on protected routes.
func RequireAuth(jwtSecret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				views.WriteProblem(w, r, http.StatusUnauthorized, "authorization header required")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				views.WriteProblem(w, r, http.StatusUnauthorized, "invalid authorization header format, expected Bearer <token>")
				return
			}

			tokenStr := strings.TrimSpace(parts[1])
			claims, err := auth.ParseAndVerifyAccessToken(tokenStr, jwtSecret)
			if err != nil {
				views.WriteProblem(w, r, http.StatusUnauthorized, "invalid or expired access token")
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, claims.Subject)
			ctx = context.WithValue(ctx, RoleKey, claims.Role)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalAuth parses a Bearer JWT if present and sets user context. If absent or invalid, it continues as anonymous.
func OptionalAuth(jwtSecret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
					tokenStr := strings.TrimSpace(parts[1])
					if claims, err := auth.ParseAndVerifyAccessToken(tokenStr, jwtSecret); err == nil {
						ctx := context.WithValue(r.Context(), UserIDKey, claims.Subject)
						ctx = context.WithValue(ctx, RoleKey, claims.Role)
						r = r.WithContext(ctx)
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole ensures that the authenticated user possesses one of the allowed roles.
func RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedRoles))
	for _, role := range allowedRoles {
		allowed[role] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := GetRole(r.Context())
			if role == "" {
				views.WriteProblem(w, r, http.StatusUnauthorized, "unauthorized")
				return
			}

			if _, ok := allowed[role]; !ok {
				views.WriteProblem(w, r, http.StatusForbidden, "forbidden: insufficient permissions")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// MaxBytes wraps the request body in an http.MaxBytesReader.
func MaxBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}
