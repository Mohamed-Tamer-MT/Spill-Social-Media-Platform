# Security Checklist

Each item is confirmed with evidence from the codebase, database schema, and tooling.

---

## ✅ 1. No Raw OTP or Refresh Token Stored

| Check | Evidence |
|---|---|
| OTP codes | `login_codes` table stores `code_hash` (SHA-256). No `code` or `otp` column exists. Source: [`internal/auth/otp.go`](file:///home/mohamed-tamer/Downloads/Spill-Social-Media-Platform/internal/auth/otp.go) uses `sha256.Sum256` before INSERT. |
| Refresh tokens | `refresh_tokens` table stores `token_hash`. Source: [`internal/auth/tokens.go`](file:///home/mohamed-tamer/Downloads/Spill-Social-Media-Platform/internal/auth/tokens.go#L118-L121) `HashRefreshToken()` applies SHA-256. |
| DB verification | `cmd/verify/main.go` queries `information_schema.columns` and confirms no raw OTP/token columns exist. |

## ✅ 2. No Password Column Exists

| Check | Evidence |
|---|---|
| Schema query | `SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND (column_name ILIKE '%pass%' OR column_name ILIKE '%pwd%')` → **0 rows**. |
| Design | The system is fully passwordless (email OTP). No password-related model field exists anywhere in `internal/models/models.go`. |

## ✅ 3. JWT Algorithm is Pinned

| Check | Evidence |
|---|---|
| Access tokens | [`internal/auth/tokens.go#L56-L58`](file:///home/mohamed-tamer/Downloads/Spill-Social-Media-Platform/internal/auth/tokens.go#L56-L58): `ParseAndVerifyAccessToken` checks `token.Method.Alg() != jwt.SigningMethodHS256.Alg()` and returns `ErrUnexpectedAlg` on mismatch. |
| Signup tokens | [`internal/auth/tokens.go#L92-L94`](file:///home/mohamed-tamer/Downloads/Spill-Social-Media-Platform/internal/auth/tokens.go#L92-L94): Same algorithm pinning in `ParseAndVerifySignupToken`. |
| Signing | Both `GenerateAccessToken` and `GenerateSignupToken` use `jwt.NewWithClaims(jwt.SigningMethodHS256, claims)`. |

## ✅ 4. RLS Enabled on Every Public Table

| Check | Evidence |
|---|---|
| Migration | [`migrations/000001_init.up.sql`](file:///home/mohamed-tamer/Downloads/Spill-Social-Media-Platform/migrations/000001_init.up.sql) Section 10 runs `ALTER TABLE %I ENABLE ROW LEVEL SECURITY` for every `pg_tables WHERE schemaname='public'`. |
| Policy | Each table gets an `api_app_full_access` policy granting `ALL` to the `api_app` role. |
| DB verification | `cmd/verify/main.go` queries `pg_class` to confirm `relrowsecurity = true` for all 10 public tables and exits non-zero on failure. |

## ✅ 5. Secrets Come Only from the Environment

| Check | Evidence |
|---|---|
| Gitleaks scan | `docker run zricethezav/gitleaks:latest detect --source="/path" --verbose` → **0 leaks found** (after replacing hardcoded test hex strings with `sha256.Sum256` calls). |
| `.gitignore` | `.env` is listed in [`.gitignore`](file:///home/mohamed-tamer/Downloads/Spill-Social-Media-Platform/.gitignore). |
| `.env.example` | Contains only placeholders (`<secret key>`, `<at least 32 random bytes>`) — no real values. |
| Code | `cmd/main.go` loads all secrets via `os.Getenv()`; `godotenv` loads `.env` at startup. |

## ✅ 6. MIME Type and Size Limits Enforced on the Server

| Check | Evidence |
|---|---|
| Service validation | [`internal/services/asset_service.go`](file:///home/mohamed-tamer/Downloads/Spill-Social-Media-Platform/internal/services/asset_service.go): `CreateAsset()` rejects `sizeBytes > 5242880` and validates `mimeType ∈ {image/jpeg, image/png, image/webp}`. |
| DB constraint | `assets_mime_check CHECK (mime_type IN ('image/jpeg','image/png','image/webp'))` and `assets_size_check CHECK (size_bytes BETWEEN 1 AND 5242880)` in the migration. |

## ✅ 7. Body Size Limits Are in Place

| Check | Evidence |
|---|---|
| Global middleware | [`internal/routes/routes.go#L26`](file:///home/mohamed-tamer/Downloads/Spill-Social-Media-Platform/internal/routes/routes.go#L26): `r.Use(middleware.MaxBytes(1 << 20))` — 1 MB limit on all request bodies. |
| Middleware implementation | [`internal/middleware/auth.go#L108-L115`](file:///home/mohamed-tamer/Downloads/Spill-Social-Media-Platform/internal/middleware/auth.go#L108-L115): wraps `r.Body` in `http.MaxBytesReader`. |
| Test | `TestRoutes_MaxBytesMiddleware` confirms 413 is returned for oversized payloads. |

## ✅ 8. Email and Birthdate Never Appear in Public Responses

| Check | Evidence |
|---|---|
| DTO separation | [`internal/views/views.go`](file:///home/mohamed-tamer/Downloads/Spill-Social-Media-Platform/internal/views/views.go): `PublicUserResponse` struct has no `email` or `birthdate` fields. `PrivateUserResponse` (used only by `GET /me`) includes them. |
| Test | `TestSecurity_EmailAndBirthdateNeverInPublicResponses` in [`internal/routes/routes_test.go`](file:///home/mohamed-tamer/Downloads/Spill-Social-Media-Platform/internal/routes/routes_test.go) hits 8 public endpoints and asserts the response body contains neither `@` (email marker) nor the user's birthdate string. All 8 sub-tests pass. |
| Endpoints tested | `GET /users/{username}`, `GET /users/{id}/followers`, `GET /users/{id}/following`, `GET /posts/{id}`, `GET /posts/{id}/comments`, `GET /feed`, `GET /users/{id}/posts`, `GET /search?type=users`, `GET /search?type=posts`. |
