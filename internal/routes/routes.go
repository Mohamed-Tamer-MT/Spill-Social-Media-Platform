package routes

import (
	"net/http"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/controllers"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/middleware"
	"github.com/go-chi/chi/v5"
)

type RouterConfig struct {
	AuthController        *controllers.AuthController
	UserController        *controllers.UserController
	PostController        *controllers.PostController
	InteractionController *controllers.InteractionController
	CommentController     *controllers.CommentController
	AssetController       *controllers.AssetController
	SearchController      *controllers.SearchController
	JWTSecret             []byte
}

func SetupRouter(cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

	// Max request body size 1MB for all JSON payloads
	r.Use(middleware.MaxBytes(1 << 20))

	registerRoutes := func(router chi.Router) {
		// Auth endpoints
		if cfg.AuthController != nil {
			router.Route("/auth", func(r chi.Router) {
				r.Post("/request-code", cfg.AuthController.RequestCode)
				r.Post("/verify", cfg.AuthController.Verify)
				r.Post("/signup", cfg.AuthController.Signup)
				r.Post("/refresh", cfg.AuthController.Refresh)
				r.Post("/logout", cfg.AuthController.Logout)
			})
		}

		// Search
		if cfg.SearchController != nil {
			router.With(middleware.OptionalAuth(cfg.JWTSecret)).Get("/search", cfg.SearchController.Search)
		}

		// Public users endpoints
		if cfg.UserController != nil {
			router.Get("/users/{username}", cfg.UserController.GetProfile)
			router.Get("/users/{id}/followers", cfg.UserController.GetFollowers)
			router.Get("/users/{id}/following", cfg.UserController.GetFollowing)
		}

		// Posts read endpoints
		if cfg.PostController != nil {
			router.With(middleware.OptionalAuth(cfg.JWTSecret)).Get("/posts/{id}", cfg.PostController.GetPost)
			router.With(middleware.OptionalAuth(cfg.JWTSecret)).Get("/users/{id}/posts", cfg.PostController.GetUserPosts)
		}

		// Comments read endpoint
		if cfg.CommentController != nil {
			router.Get("/posts/{id}/comments", cfg.CommentController.GetComments)
		}

		// Protected endpoints (RequireAuth)
		router.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))

			// Me
			if cfg.UserController != nil {
				r.Get("/me", cfg.UserController.GetMe)
				r.Patch("/me", cfg.UserController.UpdateMe)
				r.Put("/users/{id}/follow", cfg.UserController.Follow)
				r.Delete("/users/{id}/follow", cfg.UserController.Unfollow)
			}

			// Posts & Feed
			if cfg.PostController != nil {
				r.Post("/posts", cfg.PostController.CreatePost)
				r.Patch("/posts/{id}", cfg.PostController.UpdatePost)
				r.Delete("/posts/{id}", cfg.PostController.DeletePost)
				r.Get("/feed", cfg.PostController.GetFeed)
			}

			// Interactions
			if cfg.InteractionController != nil {
				r.Put("/posts/{id}/like", cfg.InteractionController.Like)
				r.Delete("/posts/{id}/like", cfg.InteractionController.Unlike)
				r.Post("/posts/{id}/share", cfg.InteractionController.Share)
			}

			// Comments
			if cfg.CommentController != nil {
				r.Post("/posts/{id}/comments", cfg.CommentController.CreateComment)
				r.Delete("/comments/{id}", cfg.CommentController.DeleteComment)
			}

			// Assets
			if cfg.AssetController != nil {
				r.Post("/assets", cfg.AssetController.CreateAsset)
				r.Post("/assets/{id}/complete", cfg.AssetController.CompleteAsset)
			}
		})
	}

	// Mount on root "/" and on "/api/v1"
	registerRoutes(r)
	r.Route("/api/v1", func(sub chi.Router) {
		registerRoutes(sub)
	})

	return r
}
