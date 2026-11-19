package models

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound        = errors.New("record not found")
	ErrDuplicateKey    = errors.New("duplicate record already exists")
	ErrUsernameTaken   = errors.New("username is already taken")
	ErrEmailRegistered = errors.New("email is already registered")
	ErrConflict        = errors.New("conflict: resource was concurrently modified")
)

type User struct {
	ID             string
	Username       string
	Email          string
	DisplayName    string
	Birthdate      time.Time
	AvatarAssetID  *string
	Role           string
	FollowerCount  int
	FollowingCount int
	CreatedAt      time.Time
}

type LoginCode struct {
	ID         string
	Email      string
	CodeHash   string
	Attempts   int16
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}

type RefreshToken struct {
	ID         string
	UserID     string
	TokenHash  string
	UserAgent  *string
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// CheckLoginCodeCooldown returns true if an unconsumed login code was generated within the cooldown window.
func (r *Repository) CheckLoginCodeCooldown(ctx context.Context, email string, cooldown time.Duration) (bool, error) {
	query := `
SELECT EXISTS (
  SELECT 1 FROM public.login_codes
  WHERE email = $1
    AND consumed_at IS NULL
    AND created_at > now() - ($2 || ' seconds')::interval
);`
	var exists bool
	cooldownSec := fmt.Sprintf("%d", int(cooldown.Seconds()))
	err := r.db.QueryRow(ctx, query, email, cooldownSec).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check login code cooldown: %w", err)
	}
	return exists, nil
}

// CreateLoginCode stores a new HMAC-hashed OTP code.
func (r *Repository) CreateLoginCode(ctx context.Context, email, codeHash string, expiresAt time.Time) error {
	query := `
INSERT INTO public.login_codes (id, email, code_hash, attempts, expires_at, created_at)
VALUES (gen_random_uuid(), $1, $2, 0, $3, now());`
	_, err := r.db.Exec(ctx, query, email, codeHash, expiresAt)
	if err != nil {
		return fmt.Errorf("failed to insert login code: %w", err)
	}
	return nil
}

// GetLatestActiveCode finds the latest unconsumed, unexpired code for the email.
func (r *Repository) GetLatestActiveCode(ctx context.Context, email string) (*LoginCode, error) {
	query := `
SELECT id, email, code_hash, attempts, expires_at, consumed_at, created_at
FROM public.login_codes
WHERE email = $1
  AND consumed_at IS NULL
  AND expires_at > now()
ORDER BY created_at DESC
LIMIT 1;`

	var code LoginCode
	err := r.db.QueryRow(ctx, query, email).Scan(
		&code.ID,
		&code.Email,
		&code.CodeHash,
		&code.Attempts,
		&code.ExpiresAt,
		&code.ConsumedAt,
		&code.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to query active login code: %w", err)
	}
	return &code, nil
}

// IncrementAttempts increments the failed verification attempts counter on a login code,
// safely capped at 10 to satisfy the database check constraint.
func (r *Repository) IncrementAttempts(ctx context.Context, id string) error {
	query := `UPDATE public.login_codes SET attempts = LEAST(attempts + 1, 10) WHERE id = $1;`
	_, err := r.db.Exec(ctx, query, id)
	return err
}

// MarkCodeConsumed marks a login code as consumed upon successful verification.
// Uses an atomic conditional update to prevent race conditions / replay attacks.
func (r *Repository) MarkCodeConsumed(ctx context.Context, id string) error {
	query := `UPDATE public.login_codes SET consumed_at = now() WHERE id = $1 AND consumed_at IS NULL;`
	tag, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FindUserByEmail looks up an existing user by email.
func (r *Repository) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	query := `
SELECT id, username, email, display_name, birthdate, avatar_asset_id, role, follower_count, following_count, created_at
FROM public.users
WHERE email = $1;`

	var u User
	err := r.db.QueryRow(ctx, query, email).Scan(
		&u.ID,
		&u.Username,
		&u.Email,
		&u.DisplayName,
		&u.Birthdate,
		&u.AvatarAssetID,
		&u.Role,
		&u.FollowerCount,
		&u.FollowingCount,
		&u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to query user by email: %w", err)
	}
	return &u, nil
}

// FindUserByID looks up a user by UUID id.
func (r *Repository) FindUserByID(ctx context.Context, id string) (*User, error) {
	query := `
SELECT id, username, email, display_name, birthdate, avatar_asset_id, role, follower_count, following_count, created_at
FROM public.users
WHERE id = $1;`

	var u User
	err := r.db.QueryRow(ctx, query, id).Scan(
		&u.ID,
		&u.Username,
		&u.Email,
		&u.DisplayName,
		&u.Birthdate,
		&u.AvatarAssetID,
		&u.Role,
		&u.FollowerCount,
		&u.FollowingCount,
		&u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to query user by id: %w", err)
	}
	return &u, nil
}

// IsUsernameTaken checks case-insensitively whether a username already exists.
func (r *Repository) IsUsernameTaken(ctx context.Context, username string) (bool, error) {
	query := `SELECT EXISTS (SELECT 1 FROM public.users WHERE username = $1);`
	var taken bool
	err := r.db.QueryRow(ctx, query, username).Scan(&taken)
	return taken, err
}

// CreateUser inserts a newly registered user into the database.
func (r *Repository) CreateUser(ctx context.Context, username, email, displayName string, birthdate time.Time, role string) (*User, error) {
	query := `
INSERT INTO public.users (id, username, email, display_name, birthdate, role)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5)
RETURNING id, username, email, display_name, birthdate, avatar_asset_id, role, follower_count, following_count, created_at;`

	var u User
	err := r.db.QueryRow(ctx, query, username, email, displayName, birthdate, role).Scan(
		&u.ID,
		&u.Username,
		&u.Email,
		&u.DisplayName,
		&u.Birthdate,
		&u.AvatarAssetID,
		&u.Role,
		&u.FollowerCount,
		&u.FollowingCount,
		&u.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			if pgErr.ConstraintName == "users_username_key" {
				return nil, ErrUsernameTaken
			}
			if pgErr.ConstraintName == "users_email_key" {
				return nil, ErrEmailRegistered
			}
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	return &u, nil
}

// CreateRefreshToken persists a hashed refresh token.
func (r *Repository) CreateRefreshToken(ctx context.Context, userID, tokenHash string, userAgent *string, expiresAt time.Time) error {
	query := `
INSERT INTO public.refresh_tokens (id, user_id, token_hash, user_agent, expires_at, created_at)
VALUES (gen_random_uuid(), $1, $2, $3, $4, now());`
	_, err := r.db.Exec(ctx, query, userID, tokenHash, userAgent, expiresAt)
	if err != nil {
		return fmt.Errorf("failed to insert refresh token: %w", err)
	}
	return nil
}

// FindRefreshTokenByHash finds a refresh token by its SHA-256 hex digest.
func (r *Repository) FindRefreshTokenByHash(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	query := `
SELECT id, user_id, token_hash, user_agent, expires_at, revoked_at, last_used_at, created_at
FROM public.refresh_tokens
WHERE token_hash = $1;`

	var t RefreshToken
	err := r.db.QueryRow(ctx, query, tokenHash).Scan(
		&t.ID,
		&t.UserID,
		&t.TokenHash,
		&t.UserAgent,
		&t.ExpiresAt,
		&t.RevokedAt,
		&t.LastUsedAt,
		&t.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to query refresh token: %w", err)
	}
	return &t, nil
}

// RotateRefreshToken revokes the old refresh token and inserts the new one atomically.
func (r *Repository) RotateRefreshToken(ctx context.Context, oldTokenID string, userID string, newTokenHash string, userAgent *string, expiresAt time.Time) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Revoke old token ensuring it has not been concurrently revoked
	revokeQuery := `UPDATE public.refresh_tokens SET revoked_at = now(), last_used_at = now() WHERE id = $1 AND revoked_at IS NULL;`
	tag, err := tx.Exec(ctx, revokeQuery, oldTokenID)
	if err != nil {
		return fmt.Errorf("failed to revoke old refresh token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}

	// Insert new token
	insertQuery := `
INSERT INTO public.refresh_tokens (id, user_id, token_hash, user_agent, expires_at, created_at)
VALUES (gen_random_uuid(), $1, $2, $3, $4, now());`
	if _, err := tx.Exec(ctx, insertQuery, userID, newTokenHash, userAgent, expiresAt); err != nil {
		return fmt.Errorf("failed to insert rotated refresh token: %w", err)
	}

	return tx.Commit(ctx)
}

// RevokeAllUserRefreshTokens revokes every active refresh token for a user (triggered on token reuse detection).
func (r *Repository) RevokeAllUserRefreshTokens(ctx context.Context, userID string) error {
	query := `UPDATE public.refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;`
	_, err := r.db.Exec(ctx, query, userID)
	return err
}

// RevokeRefreshTokenByHash revokes a single refresh token by its hash (used on logout).
func (r *Repository) RevokeRefreshTokenByHash(ctx context.Context, tokenHash string) error {
	query := `UPDATE public.refresh_tokens SET revoked_at = now(), last_used_at = now() WHERE token_hash = $1 AND revoked_at IS NULL;`
	_, err := r.db.Exec(ctx, query, tokenHash)
	return err
}
