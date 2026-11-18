package testhelper

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTestContainersPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	superConn, appConn, anonConn, err := SetupTestDB(ctx)
	if err != nil {
		t.Fatalf("SetupTestDB failed: %v", err)
	}

	if superConn == "" || appConn == "" || anonConn == "" {
		t.Fatalf("expected non-empty connection strings")
	}

	// Verify appConn works
	pool, err := pgxpool.New(ctx, appConn)
	if err != nil {
		t.Fatalf("failed to connect as api_app: %v", err)
	}
	defer pool.Close()

	var one int
	err = pool.QueryRow(ctx, "SELECT 1").Scan(&one)
	if err != nil {
		t.Fatalf("failed to query as api_app: %v", err)
	}
	if one != 1 {
		t.Fatalf("expected 1, got %d", one)
	}
}
