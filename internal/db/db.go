package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Pool struct {
	*pgxpool.Pool
	ServerVersion string
	CurrentUser   string
}

// NewAppPool connects to the application database pool with role api_app
// and simple protocol for Supavisor transaction pooler compatibility.
func NewAppPool(ctx context.Context, databaseURL string) (*Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DATABASE_URL: %w", err)
	}

	// Supavisor transaction mode pooler (port 6543) requires simple protocol
	poolConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctxTimeout, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// On startup: ping database
	if err := pool.Ping(ctxTimeout); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// On startup: query server version and current user
	var currentUser, serverVersion string
	err = pool.QueryRow(ctxTimeout, "SELECT current_user, version()").Scan(&currentUser, &serverVersion)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to query database version and current_user: %w", err)
	}

	log.Printf("[DB] Connected to PostgreSQL as '%s'", currentUser)
	log.Printf("[DB] Server version: %s", serverVersion)

	// In production, enforce connecting as api_app; in non-production, log a warning
	if currentUser != "api_app" {
		if os.Getenv("APP_ENV") == "production" {
			pool.Close()
			return nil, fmt.Errorf("security violation: connected as '%s', expected 'api_app'", currentUser)
		}
		log.Printf("[DB] WARNING: connected as '%s' (production requires 'api_app')", currentUser)
	}

	return &Pool{
		Pool:          pool,
		ServerVersion: serverVersion,
		CurrentUser:   currentUser,
	}, nil
}
