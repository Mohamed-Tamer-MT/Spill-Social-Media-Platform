package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	migURL := os.Getenv("MIGRATION_DATABASE_URL")
	if migURL == "" {
		log.Fatalf("MIGRATION_DATABASE_URL environment variable is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, migURL)
	if err != nil {
		log.Fatalf("failed to connect with MIGRATION_DATABASE_URL: %v", err)
	}
	defer conn.Close(ctx)

	var currentUser string
	if err := conn.QueryRow(ctx, "SELECT current_user").Scan(&currentUser); err != nil {
		log.Fatalf("failed to get current user: %v", err)
	}
	log.Printf("Connected for seeding as user: %s", currentUser)
	if currentUser == "api_app" {
		log.Fatalf("dev seed must run with MIGRATION_DATABASE_URL (e.g. postgres), not api_app")
	}

	seedSQL := `
INSERT INTO public.users (username, email, display_name, birthdate, role)
VALUES
  ('admin', 'admin@example.com', 'System Admin', '1990-01-01', 'admin'),
  ('alice', 'alice@example.com', 'Alice Smith', '1995-05-15', 'user'),
  ('bob', 'bob@example.com', 'Bob Jones', '1998-08-20', 'user')
ON CONFLICT (username) DO NOTHING;
`

	tag, err := conn.Exec(ctx, seedSQL)
	if err != nil {
		log.Fatalf("failed to execute seed script: %v", err)
	}
	log.Printf("Seed executed successfully: %s", tag.String())

	var totalUsers, adminCount, userCount int
	err = conn.QueryRow(ctx, `
SELECT 
  count(*),
  count(*) FILTER (WHERE role = 'admin'),
  count(*) FILTER (WHERE role = 'user')
FROM public.users
`).Scan(&totalUsers, &adminCount, &userCount)
	if err != nil {
		log.Fatalf("failed to query users count: %v", err)
	}

	log.Printf("Current users in database: %d total (%d admin, %d user)", totalUsers, adminCount, userCount)
	fmt.Printf("Seed completed. Total users: %d\n", totalUsers)
}
