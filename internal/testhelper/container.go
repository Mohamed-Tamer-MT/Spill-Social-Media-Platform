package testhelper

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	sharedContainer   *tcpostgres.PostgresContainer
	sharedConnStr     string
	sharedAppConnStr  string
	sharedAnonConnStr string
	containerOnce     sync.Once
	containerErr      error
)

// SetupTestDB starts a PostgreSQL 16 container using testcontainers-go,
// applies pre-requisites and migrations from migrations/000001_init.up.sql,
// and returns the connection string for api_app, anon, and superuser.
func SetupTestDB(ctx context.Context) (superConn, appConn, anonConn string, err error) {
	containerOnce.Do(func() {
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		pgContainer, err := tcpostgres.Run(ctxTimeout,
			"postgres:16",
			tcpostgres.WithDatabase("spill_test"),
			tcpostgres.WithUsername("postgres"),
			tcpostgres.WithPassword("postgres_pass"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(30*time.Second),
			),
		)
		if err != nil {
			containerErr = fmt.Errorf("failed to start postgres container: %w", err)
			return
		}
		sharedContainer = pgContainer

		connStr, err := pgContainer.ConnectionString(ctxTimeout, "sslmode=disable")
		if err != nil {
			containerErr = fmt.Errorf("failed to get connection string: %w", err)
			return
		}
		sharedConnStr = connStr

		// Connect as superuser to apply migrations
		pool, err := pgxpool.New(ctxTimeout, connStr)
		if err != nil {
			containerErr = fmt.Errorf("failed to connect to postgres container: %w", err)
			return
		}
		defer pool.Close()

		// 1. Create storage schema and table required by migration
		initStorageSQL := `
			CREATE SCHEMA IF NOT EXISTS storage;
			CREATE TABLE IF NOT EXISTS storage.buckets (
				id text primary key,
				name text not null,
				public boolean not null default false,
				file_size_limit bigint,
				allowed_mime_types text[]
			);
		`
		if _, err := pool.Exec(ctxTimeout, initStorageSQL); err != nil {
			containerErr = fmt.Errorf("failed to create storage schema: %w", err)
			return
		}

		// 2. Find migration file
		migrationPath := findMigrationFile()
		if migrationPath == "" {
			containerErr = fmt.Errorf("could not find 000001_init.up.sql")
			return
		}

		migrationSQL, err := os.ReadFile(migrationPath)
		if err != nil {
			containerErr = fmt.Errorf("failed to read migration file: %w", err)
			return
		}

		if _, err := pool.Exec(ctxTimeout, string(migrationSQL)); err != nil {
			containerErr = fmt.Errorf("failed to apply migration: %w", err)
			return
		}

		// 3. Configure api_app password and anon role for testing
		rolesSQL := `
			ALTER ROLE api_app WITH PASSWORD 'api_app_pass';
			GRANT ALL ON SCHEMA public TO api_app;
			GRANT ALL ON SCHEMA extensions TO api_app;

			DO $$
			BEGIN
				IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
					CREATE ROLE anon LOGIN PASSWORD 'anon_pass' NOINHERIT;
				END IF;
			END $$;
			GRANT USAGE ON SCHEMA public TO anon;
			GRANT SELECT ON public.users TO anon;
		`
		if _, err := pool.Exec(ctxTimeout, rolesSQL); err != nil {
			containerErr = fmt.Errorf("failed to configure roles: %w", err)
			return
		}

		// Build connection strings for api_app and anon
		// Replace postgres://postgres:postgres_pass@... with postgres://api_app:api_app_pass@...
		sharedAppConnStr = strings.Replace(connStr, "postgres:postgres_pass", "api_app:api_app_pass", 1)
		sharedAnonConnStr = strings.Replace(connStr, "postgres:postgres_pass", "anon:anon_pass", 1)
	})

	if containerErr != nil {
		return "", "", "", containerErr
	}
	return sharedConnStr, sharedAppConnStr, sharedAnonConnStr, nil
}

func findMigrationFile() string {
	candidates := []string{
		"migrations/000001_init.up.sql",
		"../migrations/000001_init.up.sql",
		"../../migrations/000001_init.up.sql",
		"../../../migrations/000001_init.up.sql",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	return ""
}
