# Authentication Flow & Architecture

This document describes the passwordless authentication architecture, state transitions, security guarantees, and API endpoints for the Spill Social Media Platform backend.

---

## 1. Sequence Diagram: Complete Authentication Lifecycle

```mermaid
sequenceDiagram
    autonumber
    actor User as Client
    participant API as Auth Controller / Service
    participant DB as Postgres (login_codes, users, refresh_tokens)
    participant Mailer as Mailer (SMTP / Mailpit)

    %% Request Code
    rect rgb(240, 248, 255)
    note right of User: Phase 1: Request Login Code
    User->>API: POST /auth/request-code { email }
    API->>API: Validate and normalize email
    API->>DB: Check cooldown (active unconsumed code < 60s)
    alt On cooldown or invalid email format
        API-->>User: 202 Accepted (constant response, avoids enumeration)
    else Cooldown clear
        API->>API: Generate 6-digit numeric OTP (crypto/rand)
        API->>API: Compute HMAC-SHA256(code, OTP_HMAC_KEY)
        API->>DB: INSERT login_codes (email, code_hash, expires_at +10m)
        API->>Mailer: Send email with OTP (NEVER log code)
        API-->>User: 202 Accepted
    end
    end

    %% Verify Code
    rect rgb(255, 250, 240)
    note right of User: Phase 2: Verify Code
    User->>API: POST /auth/verify { email, code }
    API->>DB: Find latest unconsumed, unexpired code for email
    alt No active code found or attempts >= 5
        API-->>User: 401 Unauthorized
    else Code found
        API->>API: Compare HMAC in constant time (subtle.ConstantTimeCompare)
        alt Code mismatch
            API->>DB: UPDATE login_codes SET attempts = attempts + 1
            API-->>User: 401 Unauthorized
        else Code valid
            API->>DB: UPDATE login_codes SET consumed_at = now()
            API->>DB: SELECT user WHERE email = email
            alt Existing User
                API->>API: Sign Access JWT (15m, sub=userID, role=role)
                API->>API: Generate opaque Refresh Token (32 bytes, crypto/rand)
                API->>DB: INSERT refresh_tokens (hash=SHA256(token), expires_at +30d)
                API-->>User: 200 OK { access_token, refresh_token, user }
            else New User
                API->>API: Sign Signup Token JWT (10m, email, purpose="signup")
                API-->>User: 200 OK { new_user: true, signup_token }
            end
        end
    end
    end

    %% Signup (if new user)
    rect rgb(245, 255, 245)
    note right of User: Phase 3: Registration (New Users Only)
    User->>API: POST /auth/signup { signup_token, username, display_name, birthdate }
    API->>API: Verify signup_token (pinned HS256, purpose="signup")
    API->>API: Validate username (^[a-z0-9_]{3,20}$)
    API->>API: Compute age from birthdate (require >= 13)
    API->>DB: INSERT users (id, username, email, display_name, birthdate, role='user')
    API->>API: Sign Access JWT (15m) & Refresh Token (30d)
    API->>DB: INSERT refresh_tokens (hash=SHA256(token))
    API-->>User: 201 Created { access_token, refresh_token, user }
    end
```

---

## 2. Sequence Diagram: Refresh Token Rotation & Reuse Attack Mitigation

Refresh tokens are single-use, 30-day opaque tokens. Every call to `POST /auth/refresh` revokes the submitted token and issues a new pair. If an already-revoked token is presented, an active reuse attack is inferred, and **all** refresh tokens for that user are revoked immediately.

```mermaid
sequenceDiagram
    autonumber
    actor Client
    participant API as Auth Service
    participant DB as Postgres (refresh_tokens)

    Client->>API: POST /auth/refresh { refresh_token }
    API->>API: token_hash = SHA256(refresh_token)
    API->>DB: SELECT * FROM refresh_tokens WHERE token_hash = token_hash

    alt Token not found
        API-->>Client: 401 Unauthorized
    else Token already revoked (revoked_at IS NOT NULL)
        note over API,DB: REUSE ATTACK DETECTED
        API->>DB: UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = token.user_id
        API-->>Client: 401 Unauthorized ("refresh token has been revoked")
    else Token expired (expires_at < now())
        API-->>Client: 401 Unauthorized ("refresh token has expired")
    else Token valid (unrevoked & unexpired)
        API->>DB: BEGIN TRANSACTION
        API->>DB: UPDATE refresh_tokens SET revoked_at = now(), last_used_at = now() WHERE id = token.id
        API->>API: Generate new Access JWT (15m)
        API->>API: Generate new Refresh Token (32-byte opaque)
        API->>DB: INSERT refresh_tokens (user_id, token_hash=SHA256(newToken), expires_at +30d)
        API->>DB: COMMIT TRANSACTION
        API-->>Client: 200 OK { access_token, refresh_token }
    end
```

---

## 3. State Diagram: OTP Code Lifecycle (`login_codes`)

```mermaid
stateDiagram-v2
    [*] --> Generated: POST /auth/request-code (cooldown >= 60s)
    
    state Generated {
        [*] --> Active
        Active --> VerificationFailed: Wrong code (attempts < 5)
        VerificationFailed --> Active: attempts++
    }

    Active --> Consumed: POST /auth/verify (code matches in constant time)
    Active --> MaxAttemptsExceeded: attempts >= 5
    Active --> Expired: now() > expires_at (10 min TTL)

    Consumed --> [*]
    MaxAttemptsExceeded --> [*]
    Expired --> [*]
```

---

## 4. State Diagram: Refresh Token Lifecycle (`refresh_tokens`)

```mermaid
stateDiagram-v2
    [*] --> Active: Issued upon Verify / Signup / Refresh
    
    Active --> Rotated: POST /auth/refresh (exchanged for new token pair)
    Active --> RevokedByUser: POST /auth/logout
    Active --> Expired: now() > expires_at (30-day TTL)
    
    Rotated --> ReuseCompromised: Submitted again to POST /auth/refresh
    Active --> ReuseCompromised: Revoked on sibling token reuse detection
    
    Rotated --> [*]
    RevokedByUser --> [*]
    Expired --> [*]
    ReuseCompromised --> [*]
```

---

## 5. Security Invariants & Implementation Details

1. **HMAC-SHA256 for OTP Codes**:
   - 6-digit numeric codes span only $10^6$ values. Storing plain SHA-256 digests would allow trivial offline rainbow table / precomputed lookups.
   - Spill computes `HMAC-SHA256(code, OTP_HMAC_KEY)` where `OTP_HMAC_KEY` is a minimum 32-byte server secret.
   - The resulting 64-character hex digest is verified using `crypto/subtle.ConstantTimeCompare`.

2. **Never Log Codes Rule**:
   - OTP codes and raw refresh tokens are never written to server logs, stdout, or diagnostics, even in development mode.
   - Mailer logs only recipient and subject.

3. **Enumeration Defenses**:
   - `POST /auth/request-code` always responds with `202 Accepted` regardless of whether the email is registered or on cooldown.

4. **Database-Enforced Cooldown & Brute Force Guard**:
   - Cooldown (`OTP_RESEND_COOLDOWN = 60s`): checked in PostgreSQL before inserting.
   - Brute-force limit (`attempts < 5`): enforced directly on the `login_codes` row.

5. **Algorithm Pinning**:
   - JWT verification explicitly requires `token.Method.Alg() == "HS256"`. Tokens specifying `"none"`, asymmetric keys (`RS256`), or unsupported algorithms are rejected immediately.

6. **Age Requirement**:
   - `POST /auth/signup` calculates the applicant's exact age on the server as of the current date and enforces age $\ge 13$.

7. **Mailer Environments**:
   - `MAILER=mailpit`: Delivers unauthenticated SMTP messages to `localhost:1025` for inspection at `http://localhost:8025`.
   - `MAILER=smtp`: Connects using TLS/STARTTLS credentials via standard `net/smtp`.
