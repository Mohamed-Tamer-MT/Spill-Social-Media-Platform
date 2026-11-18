package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/config"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/controllers"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/db"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/mailer"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/models"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/routes"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/services"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	log.Println("Starting Spill backend...")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to database as api_app, set simple protocol, ping, verify role, log version
	appPool, err := db.NewAppPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	defer appPool.Close()

	// Initialize mailer
	emailMailer, err := mailer.NewMailer(cfg)
	if err != nil {
		log.Fatalf("Mailer initialization failed: %v", err)
	}
	log.Printf("[Mailer] Initialized (%s)", cfg.Mailer)

	// Initialize storage, repository and service layers
	storageClient := storage.NewClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey)
	repo := models.NewRepository(appPool.Pool)

	authService := services.NewAuthService(repo, emailMailer, cfg)
	userService := services.NewUserService(repo, cfg.StoragePublicBaseURL)
	postService := services.NewPostService(repo, cfg.StoragePublicBaseURL)
	commentService := services.NewCommentService(repo, cfg.StoragePublicBaseURL)
	assetService := services.NewAssetService(repo, storageClient, cfg.StorageBucket, cfg.StoragePublicBaseURL)
	searchService := services.NewSearchService(repo, cfg.StoragePublicBaseURL)

	// Scheduled background cleanup job for expired pending assets (Rule 7)
	assetService.StartCleanupJob(ctx, 5*time.Minute)

	// Initialize controllers
	authController := controllers.NewAuthController(authService)
	userController := controllers.NewUserController(userService)
	postController := controllers.NewPostController(postService)
	interactionController := controllers.NewInteractionController(postService)
	commentController := controllers.NewCommentController(commentService)
	assetController := controllers.NewAssetController(assetService)
	searchController := controllers.NewSearchController(searchService)

	// Build HTTP router
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"app":     "Spill Social Media Platform API",
			"status":  "ok",
			"env":     cfg.AppEnv,
			"db_user": appPool.CurrentUser,
		})
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, pingCancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer pingCancel()

		if err := appPool.Ping(pingCtx); err != nil {
			http.Error(w, `{"status":"error","message":"database unreachable"}`, http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":         "healthy",
			"current_user":   appPool.CurrentUser,
			"server_version": appPool.ServerVersion,
		})
	})

	// Mount API routes
	apiRouter := routes.SetupRouter(routes.RouterConfig{
		AuthController:        authController,
		UserController:        userController,
		PostController:        postController,
		InteractionController: interactionController,
		CommentController:     commentController,
		AssetController:       assetController,
		SearchController:      searchController,
		JWTSecret:             cfg.JWTSecret,
	})
	r.Mount("/", apiRouter)

	server := &http.Server{
		Addr:         ":" + cfg.HTTPPort,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("HTTP server listening on port %s", cfg.HTTPPort)
		serverErrors <- server.ListenAndServe()
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server error: %v", err)
		}
	case sig := <-shutdown:
		log.Printf("Received signal %v, initiating graceful shutdown...", sig)
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Graceful shutdown failed: %v, forcing close", err)
			_ = server.Close()
		}
		log.Println("Server gracefully stopped.")
	}
}
