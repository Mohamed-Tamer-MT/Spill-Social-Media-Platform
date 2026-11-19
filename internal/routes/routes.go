package routes

import (
	"net/http"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/controllers"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/middleware"
	"github.com/gorilla/mux"
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

func SetupRouter(cfg RouterConfig) *mux.Router {
	r := mux.NewRouter()

	// Max request body size 1MB for all JSON payloads
	r.Use(middleware.MaxBytes(1 << 20))

	registerRoutes := func(router *mux.Router) {
		// Auth endpoints
		if cfg.AuthController != nil {
			authSub := router.PathPrefix("/auth").Subrouter()
			authSub.HandleFunc("/request-code", cfg.AuthController.RequestCode).Methods(http.MethodPost)
			authSub.HandleFunc("/verify", cfg.AuthController.Verify).Methods(http.MethodPost)
			authSub.HandleFunc("/signup", cfg.AuthController.Signup).Methods(http.MethodPost)
			authSub.HandleFunc("/refresh", cfg.AuthController.Refresh).Methods(http.MethodPost)
			authSub.HandleFunc("/logout", cfg.AuthController.Logout).Methods(http.MethodPost)
		}

		// Search (with OptionalAuth)
		if cfg.SearchController != nil {
			router.Handle("/search", middleware.OptionalAuth(cfg.JWTSecret)(http.HandlerFunc(cfg.SearchController.Search))).Methods(http.MethodGet)
		}

		// Public users endpoints
		if cfg.UserController != nil {
			router.HandleFunc("/users/{id}/followers", cfg.UserController.GetFollowers).Methods(http.MethodGet)
			router.HandleFunc("/users/{id}/following", cfg.UserController.GetFollowing).Methods(http.MethodGet)
			router.HandleFunc("/users/{username}", cfg.UserController.GetProfile).Methods(http.MethodGet)
		}

		// Posts read endpoints (with OptionalAuth)
		if cfg.PostController != nil {
			router.Handle("/users/{id}/posts", middleware.OptionalAuth(cfg.JWTSecret)(http.HandlerFunc(cfg.PostController.GetUserPosts))).Methods(http.MethodGet)
			router.Handle("/posts/{id}", middleware.OptionalAuth(cfg.JWTSecret)(http.HandlerFunc(cfg.PostController.GetPost))).Methods(http.MethodGet)
		}

		// Comments read endpoint
		if cfg.CommentController != nil {
			router.HandleFunc("/posts/{id}/comments", cfg.CommentController.GetComments).Methods(http.MethodGet)
		}

		// Protected endpoints (RequireAuth)
		protected := router.NewRoute().Subrouter()
		protected.Use(middleware.RequireAuth(cfg.JWTSecret))

		// Me
		if cfg.UserController != nil {
			protected.HandleFunc("/me", cfg.UserController.GetMe).Methods(http.MethodGet)
			protected.HandleFunc("/me", cfg.UserController.UpdateMe).Methods(http.MethodPatch)
			protected.HandleFunc("/users/{id}/follow", cfg.UserController.Follow).Methods(http.MethodPut)
			protected.HandleFunc("/users/{id}/follow", cfg.UserController.Unfollow).Methods(http.MethodDelete)
		}

		// Posts & Feed
		if cfg.PostController != nil {
			protected.HandleFunc("/feed", cfg.PostController.GetFeed).Methods(http.MethodGet)
			protected.HandleFunc("/posts", cfg.PostController.CreatePost).Methods(http.MethodPost)
			protected.HandleFunc("/posts/{id}", cfg.PostController.UpdatePost).Methods(http.MethodPatch)
			protected.HandleFunc("/posts/{id}", cfg.PostController.DeletePost).Methods(http.MethodDelete)
		}

		// Interactions
		if cfg.InteractionController != nil {
			protected.HandleFunc("/posts/{id}/like", cfg.InteractionController.Like).Methods(http.MethodPut)
			protected.HandleFunc("/posts/{id}/like", cfg.InteractionController.Unlike).Methods(http.MethodDelete)
			protected.HandleFunc("/posts/{id}/share", cfg.InteractionController.Share).Methods(http.MethodPost)
		}

		// Comments
		if cfg.CommentController != nil {
			protected.HandleFunc("/posts/{id}/comments", cfg.CommentController.CreateComment).Methods(http.MethodPost)
			protected.HandleFunc("/comments/{id}", cfg.CommentController.DeleteComment).Methods(http.MethodDelete)
		}

		// Assets
		if cfg.AssetController != nil {
			protected.HandleFunc("/assets", cfg.AssetController.CreateAsset).Methods(http.MethodPost)
			protected.HandleFunc("/assets/{id}/complete", cfg.AssetController.CompleteAsset).Methods(http.MethodPost)
		}
	}

	// Mount on /api/v1 first so subrouter matches before root
	v1Router := r.PathPrefix("/api/v1").Subrouter()
	registerRoutes(v1Router)
	registerRoutes(r)

	return r
}
