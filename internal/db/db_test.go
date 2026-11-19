package db_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/db"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/testhelper"
)

func TestNewAppPool(t *testing.T) {
	ctx := context.Background()

	t.Run("Invalid connection string returns error", func(t *testing.T) {
		_, err := db.NewAppPool(ctx, "not-a-valid-postgres-url")
		if err == nil {
			t.Fatal("expected error on malformed URL, got nil")
		}
	})

	t.Run("Unreachable database returns error", func(t *testing.T) {
		ctxTimeout, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		_, err := db.NewAppPool(ctxTimeout, "postgres://user:pass@127.0.0.1:54329/spill?sslmode=disable")
		if err == nil {
			t.Fatal("expected error connecting to unreachable database, got nil")
		}
	})

	t.Run("Valid database connection succeeds", func(t *testing.T) {
		_, appConn, _, err := testhelper.SetupTestDB(ctx)
		if err != nil {
			t.Fatalf("SetupTestDB failed: %v", err)
		}

		pool, err := db.NewAppPool(ctx, appConn)
		if err != nil {
			t.Fatalf("unexpected error connecting to test db: %v", err)
		}
		defer pool.Close()

		if pool.CurrentUser != "api_app" {
			t.Errorf("expected CurrentUser to be 'api_app', got '%s'", pool.CurrentUser)
		}
		if pool.ServerVersion == "" {
			t.Error("expected non-empty ServerVersion")
		}
	})

	t.Run("Enforces api_app in production", func(t *testing.T) {
		superConn, _, _, err := testhelper.SetupTestDB(ctx)
		if err != nil {
			t.Fatalf("SetupTestDB failed: %v", err)
		}

		origEnv := os.Getenv("APP_ENV")
		defer os.Setenv("APP_ENV", origEnv)
		os.Setenv("APP_ENV", "production")

		// superConn connects as postgres/superuser, not api_app
		_, err = db.NewAppPool(ctx, superConn)
		if err == nil {
			t.Fatal("expected security violation error in production when connected as non-api_app, got nil")
		}
	})
}
