# Traceability Matrix

This document maps every requirement outlined in **Section 1 of `system-design.md`** to its corresponding HTTP endpoint, business service, database table/constraint, and automated test suite.

---

## 1. Traceability Table

| Requirement (Section 1) | HTTP Endpoint(s) | Business Service & Logic | Database Table(s) & Constraints | Automated Test Name(s) | Status | Gaps |
|---|---|---|---|---|---|---|
| **Gateway: Routing** | All routes mapped in `/internal/routes` | `chi.Mux` router wiring | N/A | `TestEveryOpenAPIEndpointStatusCodes` | Met | None |
| **Gateway: Request Logging** | All incoming HTTP requests | `middleware.Logger` | N/A | `TestMiddleware_Logger` | Met | None |
| **Gateway: Panic Recovery** | All incoming HTTP requests | `middleware.Recoverer` | N/A | `TestMiddleware_Recover` | Met | None |
| **Gateway: Max Body Size** | All incoming HTTP requests | `middleware.MaxBytes(1MB)` | N/A | `TestRoutes_MaxBytesMiddleware` | Met | None |
| **Users: User Profile** | `GET /users/{username}`, `GET /me`, `PATCH /me` | `UserService.GetProfile`, `GetMe`, `UpdateMe` | `public.users` (`username`, `display_name`, `email`) | `TestUserService_FullSuite`, `TestRepository_UserLifecycle`, `TestEveryOpenAPIEndpointStatusCodes` | Met | None |
| **Users: Age & Birthdate Validation** | `POST /auth/signup` | `AuthService.Signup`, `auth.CalculateAge` | `public.users` (`birthdate > '1900-01-01'`) | `TestAuthService_AgeValidation`, `TestAuthService_UnderageRejection` | Met | None |
| **Users: Profile Avatar Asset** | `PATCH /me` | `UserService.UpdateMe` | `public.users.avatar_asset_id` -> `assets(id)`, `trg_users_avatar_guard` | `TestUserService_FullSuite`, `TestDBRule_AttachingAvatarAssetAsPostImageFails` | Met | None |
| **Users: Privacy (No Email/Birthdate Leaks)** | `GET /users/{username}`, `GET /feed`, `GET /posts/{id}`, `/followers`, `/following`, `/search` | `views.PublicUserResponse` projection | `public.users` | `TestSecurity_EmailAndBirthdateNeverInPublicResponses` | Met | None |
| **Auth: Passwordless OTP Code** | `POST /auth/request-code`, `POST /auth/verify` | `AuthService.RequestCode`, `VerifyCode` | `public.login_codes` (`code_hash`, `expires_at`, `attempts`) | `TestAuthRoutes_AllStatusCodes`, `TestAuthService_OTPFlow`, `TestAuthService_OTPAttemptLimits` | Met | None |
| **Auth: Cooldown Window (60s)** | `POST /auth/request-code` | `AuthService.RequestCode` | `public.login_codes` (`created_at`) | `TestAuthService_RequestCooldown`, `TestAuthRoutes_AllStatusCodes` | Met | None |
| **Auth: JWT Access & Refresh Token** | `POST /auth/refresh`, `POST /auth/logout` | `AuthService.RefreshToken`, `Logout`, `auth.GenerateAccessToken` | `public.refresh_tokens` (`token_hash`, `revoked_at`, `expires_at`) | `TestAuthRoutes_AllStatusCodes`, `TestAccessTokenGenerationAndParsing`, `TestRefreshToken` | Met | None |
| **Auth: Refresh Token Reuse Detection** | `POST /auth/refresh` | `AuthService.RefreshToken` | `public.refresh_tokens` | `TestAuthService_RefreshTokenReuseDetection`, `TestAuthService_SignupAndRefreshFlow` | Met | None |
| **Auth: Role & Ownership Checks** | Protected routes (`RequireAuth`, `RequireRole`) | `middleware.RequireRole`, Services author checks | `public.users.role` (`user`, `admin`) | `TestService_OwnershipRules`, `TestRequireRole`, `TestEveryOpenAPIEndpointStatusCodes` | Met | None |
| **Posts: Create Post** | `POST /posts` | `PostService.CreatePost` | `public.posts` | `TestEveryOpenAPIEndpointStatusCodes`, `TestPostService_FullSuite`, `TestRepository_PostAndCommentLifecycle` | Met | None |
| **Posts: Edit Post (Ownership)** | `PATCH /posts/{id}` | `PostService.UpdatePost` | `public.posts` | `TestService_OwnershipRules`, `TestEveryOpenAPIEndpointStatusCodes` | Met | None |
| **Posts: Delete Post (Author/Admin)** | `DELETE /posts/{id}` | `PostService.DeletePost` | `public.posts.deleted_at` (soft-delete) | `TestService_OwnershipRules`, `TestEveryOpenAPIEndpointStatusCodes` | Met | None |
| **Posts: Attach Images (Max 4, Owned, Ready)** | `POST /posts` (`assetIds`) | `PostService.CreatePost` | `public.post_assets`, `trg_post_assets_guard` | `TestDBRule_AttachingFifthImageFails`, `TestDBRule_AttachingAnotherUsersAssetFails`, `TestAssetSecurityFifthImageAndWrongOwner` | Met | None |
| **Interactions: Like & Unlike** | `PUT /posts/{id}/like`, `DELETE /posts/{id}/like` | `PostService.LikePost`, `UnlikePost` | `public.likes`, `trg_likes_counter`, `posts.like_count` | `TestDBRule_DuplicateLikeDoesNotChangeLikeCount`, `TestDBRule_DeletingLikeDecrementsLikeCountNeverBelowZero`, `TestConcurrency_FiftyGoroutinesSameUserLike` | Met | None |
| **Interactions: Comment & Soft-Delete** | `POST /posts/{id}/comments`, `DELETE /comments/{id}` | `CommentService.CreateComment`, `DeleteComment` | `public.comments`, `trg_comments_counter_row`, `trg_comments_counter_softdelete` | `TestDBRule_SoftDeletingCommentDecrementsAndRestoringIncrements`, `TestCommentService_FullSuite` | Met | None |
| **Interactions: Share** | `POST /posts/{id}/share` | `PostService.SharePost` | `public.shares`, `shares_user_post_unique`, `trg_shares_counter` | `TestDBRule_ShareInsertedOnlyOncePerUserAndPost`, `TestEveryOpenAPIEndpointStatusCodes` | Met | None |
| **Interactions: Follow & Unfollow** | `PUT /users/{id}/follow`, `DELETE /users/{id}/follow` | `UserService.FollowUser`, `UnfollowUser` | `public.follows`, `no_self_follow`, `trg_follows_counter` | `TestDBRule_UserCannotFollowThemselves`, `TestConcurrency_FollowUnfollowCountersMatchSQLCount` | Met | None |
| **Feed: Followed Users Timeline** | `GET /feed` | `PostService.GetFeed` (keyset pagination) | `public.posts`, `public.follows`, `idx_posts_author_created` | `TestPagination_Seed45PostsInFeed`, `TestEveryOpenAPIEndpointStatusCodes` | Met | None |
| **Assets: Presigned Upload & Complete** | `POST /assets`, `POST /assets/{id}/complete` | `AssetService.CreateAsset`, `CompleteAsset` | `public.assets` (`mime_type`, `size_bytes`, `status`) | `TestEveryOpenAPIEndpointStatusCodes`, `TestRepository_AssetLifecycle` | Met | None |
| **Assets: Expired Pending Asset Cleanup** | Background ticker / job | `AssetService.StartCleanupJob`, `CleanupExpiredAssets` | `public.assets` (`upload_expires_at`) | `TestServices_TargetedBranchCoverage/AssetService_StartCleanupJob` | Met | None |
| **Search: Users** | `GET /search?q=&type=users` | `SearchService.Search` | `public.users.search_vector` (`simple`), `idx_users_search` | `TestSearchService_FullSuite`, `TestEveryOpenAPIEndpointStatusCodes` | Met | None |
| **Search: Posts (Stemmed & Active Only)** | `GET /search?q=&type=posts` | `SearchService.Search` | `public.posts.search_vector` (`english`), `idx_posts_search` | `TestSearch_StemmedAndExcludesSoftDeleted` | Met | None |
| **Search: Comments** | `GET /search?q=&type=comments` | `SearchService.Search` | `public.comments.search_vector` (`english`), `idx_comments_search` | `TestSearchService_FullSuite`, `TestEveryOpenAPIEndpointStatusCodes` | Met | None |

---

## 2. Gap Analysis Summary

- **Total Functional Requirements Evaluated:** 27
- **Requirements Satisfied:** 27 (100%)
- **Requirements with Open Gaps:** 0 (0%)
- **Conclusion:** The implementation completely fulfills the architectural, operational, and database integrity requirements specified in `system-design.md`.
