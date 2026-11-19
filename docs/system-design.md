# Social Platform System Design (Go + Postgres, MVC), simplified

## 1. Requirements

```mermaid
mindmap
  root((Social Platform))
    Gateway
      Routing
      Request logging
      Panic recovery
    Users
      Username, email, display name
      Birthdate, profile image
    Auth
      Passwordless email code
      JWT access + refresh token
      Roles and ownership checks
    Posts
      Create, edit, delete
      Text plus images
    Interactions
      Like
      Comment
      Share
      Follow
    Feed
      Posts from followed users
    Assets
      Upload and serve images
    Search
      Users
      Posts
      Comments
```

**Non-functional:** stateless app server, no password storage, and indexed queries so reads stay fast without a cache.

---

## 2. High-Level Architecture

```mermaid
flowchart TB
    Client["Web Client"]

    subgraph GW["API Gateway Layer (middleware chain)"]
        direction LR
        L["Recover + Logger"] --> AU["Auth Middleware (JWT)"] --> R["Router"]
    end

    subgraph APP["Go Application (MVC)"]
        direction TB
        C["Controllers"]
        V["Views (JSON DTOs)"]
        S["Services (business rules)"]
        M["Models / Repositories"]
        C --> S --> M
        C --> V
    end

    PG[("PostgreSQL<br/>data + search indexes")]
    OBJ[("Object Storage<br/>MinIO / S3 / disk")]
    MAIL["Email Provider (SMTP)"]

    Client --> GW
    R --> C
    M --> PG
    S --> OBJ
    S --> MAIL
    Client -. "direct upload / download via presigned URL" .-> OBJ
```

---

## 3. MVC Layout in Go

In a JSON API, the **View** is the response DTO/serializer layer.

```
/cmd/server/main.go          # wiring: config, db, router
/internal
  /config                    # env loading
  /middleware                # logger, recover, auth
  /routes                    # route table -> controllers
  /controllers               # HTTP handlers (parse, validate, call service, respond)
  /services                  # business logic (auth, posts, feed, search...)
  /models                    # structs + repository (SQL queries via pgx)
  /views                     # response DTOs / mappers
  /storage                   # object storage client (presign, head, delete)
  /mailer                    # send OTP emails
/migrations                  # golang-migrate .sql files
```

**Suggested libs:** `gorilla/mux` (router), `pgx` (Postgres), `golang-jwt/jwt`, `golang-migrate`, `go-playground/validator`.

### Request lifecycle

```mermaid
sequenceDiagram
    participant C as Client
    participant MW as Middleware (recover, log, auth)
    participant CT as Controller
    participant SV as Service
    participant MD as Model (Repo)
    participant DB as Postgres
    participant VW as View (DTO)

    C->>MW: GET /posts/123
    MW->>MW: verify JWT
    MW->>CT: request + user context
    CT->>SV: GetPost(123)
    SV->>MD: FindPostByID(123)
    MD->>DB: SELECT ...
    DB-->>MD: row
    MD-->>SV: Post model
    SV-->>CT: Post
    CT->>VW: toPostResponse(Post)
    VW-->>CT: JSON DTO
    CT-->>C: 200 OK
```

---

## 4. Component Internals

### 4.1 API Gateway

A middleware chain: **Recover → Logger → Auth → Router**.

| Middleware | Job |
|---|---|
| Recover | Catches panics and returns 500 instead of crashing |
| Logger | Logs method, path, status, latency, and a request ID |
| Auth | Parses the JWT if present and puts `userID` and `role` in the request context |
| Router | Maps routes to controllers. Protected routes are wrapped with `RequireAuth` |

```mermaid
flowchart LR
    A[Incoming request] --> B[Recover]
    B --> C[Logger]
    C --> D{"Bearer token present?"}
    D -- No --> E["anonymous context"]
    D -- Yes --> F{"JWT valid?"}
    F -- No --> G["401 Unauthorized"]
    F -- Yes --> H["context: userID, role"]
    E --> I[Router]
    H --> I
    I --> J{"Route needs auth?"}
    J -- "Yes and anonymous" --> G
    J -- Otherwise --> K[Controller]
```

### 4.2 Authentication and Authorization

**Passwordless flow (email one-time code):**

```mermaid
sequenceDiagram
    participant U as User
    participant API as Auth Controller/Service
    participant DB as Postgres
    participant M as Mailer

    U->>API: POST /auth/request-code {email}
    API->>API: generate 6-digit code
    API->>DB: INSERT login_codes (email, hash(code), expires +10m)
    API->>M: send code
    API-->>U: 202 Accepted (always, avoids email enumeration)

    U->>API: POST /auth/verify {email, code}
    API->>DB: find latest unused, unexpired code, attempts < 5
    alt code valid
        API->>DB: mark consumed
        alt user exists
            API->>DB: INSERT refresh_tokens (hash)
            API-->>U: access JWT (15m) + refresh token (30d)
        else new user
            API-->>U: signup_token (JWT 10m, email claim)
            U->>API: POST /auth/signup {username, displayName, birthdate} + signup_token
            API->>DB: INSERT users
            API-->>U: access JWT + refresh token
        end
    else invalid
        API->>DB: attempts++
        API-->>U: 401
    end
```

**Details**
- Store **hashes** of codes and refresh tokens (SHA-256), never raw values.
- The `attempts < 5` check on each code is simple brute-force protection that lives in the DB. It is separate from the removed rate limiter.
- Access JWT claims: `sub` (user id), `role`, `exp`. Refresh tokens are rotated on use (`POST /auth/refresh`) and revoked on logout.
- Signup validation: username `^[a-z0-9_]{3,20}$` and unique; age ≥ 13 from `birthdate`.

**Authorization (simple RBAC + ownership):**
- Roles: `user`, `admin`.
- `RequireAuth` rejects missing or invalid JWTs.
- Ownership check in the service layer: only `post.author_id == currentUser` can edit or delete (admins can delete any).

### 4.3 Users and Profiles
- Fields: `username` (unique, case-insensitive via `citext`), `email` (unique, private), `display_name`, `birthdate` (private), `avatar_asset_id`.
- The public profile response excludes email and birthdate. The View layer enforces this by using a separate `PublicUserResponse` DTO.

### 4.4 Posts

```mermaid
sequenceDiagram
    participant C as Client
    participant API as Post Controller/Service
    participant DB as Postgres

    C->>API: POST /posts {body, assetIds[]}
    API->>DB: BEGIN
    API->>DB: INSERT posts
    API->>DB: INSERT post_assets (each asset must be owned + ready)
    API->>DB: COMMIT
    Note over DB: search_vector generated automatically
    API-->>C: 201 Created {post}
```

### 4.5 Relationships and Interactions

| Action | Endpoint | DB effect |
|---|---|---|
| Like / unlike | `PUT/DELETE /posts/{id}/like` | insert/delete `likes`, update `posts.like_count` (same transaction) |
| Comment | `POST /posts/{id}/comments` | insert `comments`, `comment_count++` |
| Share | `POST /posts/{id}/share` | insert `shares`, `share_count++` |
| Follow / unfollow | `PUT/DELETE /users/{id}/follow` | insert/delete `follows`, update the two counters |

Composite primary keys make likes and follows **idempotent** (`INSERT ... ON CONFLICT DO NOTHING`), and the counter only changes if a row was actually inserted.

```mermaid
sequenceDiagram
    participant C as Client
    participant API as Interaction Service
    participant DB as Postgres

    C->>API: PUT /posts/42/like
    API->>DB: BEGIN
    API->>DB: INSERT INTO likes (user_id, post_id) ON CONFLICT DO NOTHING
    alt row inserted
        API->>DB: UPDATE posts SET like_count = like_count + 1 WHERE id = 42
    end
    API->>DB: COMMIT
    API-->>C: 200 {liked: true}
```

**Home feed** (fan-out on read, the simplest approach):
```sql
SELECT p.* FROM posts p
JOIN follows f ON f.followee_id = p.author_id
WHERE f.follower_id = $1
  AND p.deleted_at IS NULL
  AND (p.created_at, p.id) < ($2, $3)
ORDER BY p.created_at DESC, p.id DESC
LIMIT 20;
```
Use **keyset pagination** (cursor = `created_at, id`) with the index `posts(author_id, created_at DESC)`.

### 4.6 Assets

Use a **presigned upload** so large files never pass through the Go server.

```mermaid
sequenceDiagram
    participant C as Client
    participant API as Asset Controller/Service
    participant DB as Postgres
    participant S as Object Storage

    C->>API: POST /assets {mimeType, sizeBytes}
    API->>API: validate (jpeg/png/webp, max 5 MB)
    API->>DB: INSERT assets (status = pending, storage_key)
    API-->>C: {assetId, presignedPutUrl}
    C->>S: PUT file (presigned URL)
    C->>API: POST /assets/{id}/complete
    API->>S: HEAD object (verify exists + size)
    API->>DB: UPDATE assets SET status = ready
    API-->>C: {assetId, url}
```

- Public read URL: `https://<storage-host>/{storage_key}`. Keys look like `u/{userId}/{uuid}.webp`.
- A small cleanup job (a goroutine with a ticker) deletes `pending` assets older than 1 hour.
- Simplest alternative for a course project: store files on local disk and serve them with `http.FileServer`.

### 4.7 Search (inverted index)

Postgres full-text search stores a **`tsvector`** (the tokens) and uses a **GIN index**, which is an inverted index mapping `token → list of rows`. You don't have to build one yourself.

```mermaid
flowchart LR
    I["INSERT / UPDATE row"] --> TV["Generated column<br/>search_vector = to_tsvector(...)"]
    TV --> GIN[("GIN inverted index<br/>token → row ids")]
    Q["GET /search?q=foo&type=posts"] --> PQ["websearch_to_tsquery(q)"]
    PQ --> GIN
    GIN --> RANK["ts_rank + ORDER BY"]
    RANK --> RES[Results]
```

| Target | Strategy |
|---|---|
| Usernames | prefix search: `username ILIKE 'q%'` (with `text_pattern_ops` index) |
| Display names | `tsvector` with `'simple'` config (no stemming for names) + GIN |
| Posts | `tsvector` on `body` with `'english'` config + GIN |
| Comments | same as posts |

```sql
SELECT id, body, ts_rank(search_vector, q) AS rank
FROM posts, websearch_to_tsquery('english', $1) q
WHERE search_vector @@ q AND deleted_at IS NULL
ORDER BY rank DESC, created_at DESC
LIMIT 20 OFFSET $2;
```

---

## 5. Database Design

```mermaid
erDiagram
    USERS ||--o{ POSTS : writes
    USERS ||--o{ COMMENTS : writes
    USERS ||--o{ LIKES : gives
    USERS ||--o{ SHARES : makes
    USERS ||--o{ FOLLOWS : "is follower"
    USERS ||--o{ FOLLOWS : "is followed"
    USERS ||--o{ ASSETS : owns
    USERS ||--o{ REFRESH_TOKENS : has
    ASSETS |o--o| USERS : "avatar of"
    POSTS ||--o{ COMMENTS : has
    POSTS ||--o{ LIKES : receives
    POSTS ||--o{ SHARES : "shared in"
    POSTS ||--o{ POST_ASSETS : includes
    ASSETS ||--o{ POST_ASSETS : "attached via"

    USERS {
        uuid id PK
        citext username UK
        citext email UK
        text display_name
        date birthdate
        uuid avatar_asset_id FK
        text role "user | admin"
        int follower_count
        int following_count
        tsvector search_vector "generated"
        timestamptz created_at
        timestamptz updated_at
    }
    LOGIN_CODES {
        uuid id PK
        citext email
        text code_hash
        int attempts
        timestamptz expires_at
        timestamptz consumed_at
    }
    REFRESH_TOKENS {
        uuid id PK
        uuid user_id FK
        text token_hash UK
        timestamptz expires_at
        timestamptz revoked_at
        timestamptz created_at
    }
    ASSETS {
        uuid id PK
        uuid owner_id FK
        text storage_key UK
        text mime_type
        bigint size_bytes
        text status "pending | ready"
        timestamptz created_at
    }
    POSTS {
        uuid id PK
        uuid author_id FK
        text body
        int like_count
        int comment_count
        int share_count
        tsvector search_vector "generated"
        timestamptz created_at
        timestamptz deleted_at "soft delete"
    }
    POST_ASSETS {
        uuid post_id PK, FK
        uuid asset_id PK, FK
        int position
    }
    COMMENTS {
        uuid id PK
        uuid post_id FK
        uuid author_id FK
        text body
        tsvector search_vector "generated"
        timestamptz created_at
        timestamptz deleted_at
    }
    LIKES {
        uuid user_id PK, FK
        uuid post_id PK, FK
        timestamptz created_at
    }
    SHARES {
        uuid id PK
        uuid user_id FK
        uuid post_id FK
        text quote "optional"
        timestamptz created_at
    }
    FOLLOWS {
        uuid follower_id PK, FK
        uuid followee_id PK, FK
        timestamptz created_at
    }
```

`LOGIN_CODES` is keyed by email rather than a foreign key, because a code is requested before the user may exist.

### Key indexes and constraints

```sql
CREATE EXTENSION IF NOT EXISTS citext;

-- feed + profile timelines
CREATE INDEX idx_posts_author_created ON posts (author_id, created_at DESC);
CREATE INDEX idx_comments_post_created ON comments (post_id, created_at);
CREATE INDEX idx_follows_followee ON follows (followee_id);   -- "who follows me"
CREATE INDEX idx_login_codes_email ON login_codes (email, expires_at DESC);

-- search (inverted indexes)
ALTER TABLE posts ADD COLUMN search_vector tsvector
  GENERATED ALWAYS AS (to_tsvector('english', body)) STORED;
CREATE INDEX idx_posts_search ON posts USING GIN (search_vector);
-- same pattern for comments (english) and users (to_tsvector('simple', display_name))
CREATE INDEX idx_users_username_prefix ON users (username text_pattern_ops);

-- sanity
ALTER TABLE follows ADD CONSTRAINT no_self_follow CHECK (follower_id <> followee_id);
```

---

## 6. API Surface

| Area | Endpoints |
|---|---|
| Auth | `POST /auth/request-code`, `/auth/verify`, `/auth/signup`, `/auth/refresh`, `/auth/logout` |
| Users | `GET /users/{username}`, `GET/PATCH /me`, `PUT/DELETE /users/{id}/follow`, `GET /users/{id}/followers`, `/following` |
| Posts | `POST /posts`, `GET/PATCH/DELETE /posts/{id}`, `GET /feed`, `GET /users/{id}/posts` |
| Interactions | `PUT/DELETE /posts/{id}/like`, `POST /posts/{id}/share`, `GET/POST /posts/{id}/comments`, `DELETE /comments/{id}` |
| Assets | `POST /assets`, `POST /assets/{id}/complete` |
| Search | `GET /search?q=&type=users\|posts\|comments&page=` |

---

## 7. Data Interaction Overview

```mermaid
flowchart LR
    subgraph Write Path
        W1[Auth] --> PG1[(users, login_codes,<br/>refresh_tokens)]
        W2[Posts / Comments] --> PG2[(posts, comments,<br/>post_assets)]
        W3[Likes / Shares / Follows] --> PG3[(likes, shares, follows<br/>+ counters)]
        W4[Assets] --> PG4[(assets)]
        W4 --> OBJ[(Object Storage)]
    end

    subgraph Read Path
        R1[Profile / Post / Feed reads] --> PGR[(Postgres + B-tree indexes)]
        R3[Search] --> GIN[(GIN indexes in Postgres)]
    end

    PG2 -. "generated tsvector" .-> GIN
```
