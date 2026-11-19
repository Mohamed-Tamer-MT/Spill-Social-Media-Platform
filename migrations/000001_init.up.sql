-- ---------------------------------------------------------------------
-- 0. Schemas and extensions
-- ---------------------------------------------------------------------
create schema if not exists extensions;
create extension if not exists citext schema extensions;
create extension if not exists "uuid-ossp" schema extensions;
create extension if not exists pgcrypto schema extensions;

-- ---------------------------------------------------------------------
-- 1. Users
-- ---------------------------------------------------------------------
create table public.users (
  id               uuid         primary key default gen_random_uuid(),
  username         extensions.citext not null unique,
  email            extensions.citext not null unique,
  display_name     text         not null,
  birthdate        date         not null,
  avatar_asset_id  uuid,                                  -- FK added after assets exists
  role             text         not null default 'user',
  follower_count   integer      not null default 0,      -- maintained by trigger
  following_count  integer      not null default 0,      -- maintained by trigger
  search_vector    tsvector     generated always as (to_tsvector('simple', display_name)) stored,
  created_at       timestamptz  not null default now(),

  constraint users_username_format  check (username::text ~ '^[a-z0-9_]{3,20}$'),
  constraint users_email_format     check (email::text ~ '^[^@\s]+@[^@\s]+\.[^@\s]+$'),
  constraint users_display_name_len check (char_length(display_name) between 1 and 50),
  constraint users_birthdate_range  check (birthdate > date '1900-01-01'),
  constraint users_role_check       check (role in ('user', 'admin')),
  constraint users_counts_nonneg    check (follower_count >= 0 and following_count >= 0)
);

-- ---------------------------------------------------------------------
-- 2. Assets (uploaded images, served from the Supabase Storage bucket)
-- ---------------------------------------------------------------------
create table public.assets (
  id                 uuid         primary key default gen_random_uuid(),
  owner_id           uuid         not null references public.users(id) on delete cascade,
  kind               text         not null,
  storage_bucket     text         not null default 'media',
  storage_key        text         not null unique,        -- u/{user_id}/{uuid}.{ext}
  original_filename  text,                                 -- stored for audit, never served
  mime_type          text         not null,
  size_bytes         bigint       not null,
  width_px           integer,
  height_px          integer,
  sha256             text,
  variants           jsonb        not null default '{}'::jsonb,   -- future thumbnails
  status             text         not null default 'pending',
  upload_expires_at  timestamptz  not null default now() + interval '1 hour',
  created_at         timestamptz  not null default now(),
  completed_at       timestamptz,

  constraint assets_kind_check       check (kind in ('post_image', 'avatar')),
  constraint assets_status_check     check (status in ('pending', 'ready')),
  constraint assets_mime_check       check (mime_type in ('image/jpeg', 'image/png', 'image/webp')),
  constraint assets_size_check       check (size_bytes between 1 and 5242880),
  constraint assets_dims_check       check ((width_px is null or width_px > 0)
                                        and (height_px is null or height_px > 0)),
  constraint assets_sha256_check     check (sha256 is null or sha256 ~ '^[0-9a-f]{64}$'),
  constraint assets_key_format       check (storage_key ~ '^u/[0-9a-f-]{36}/'),
  constraint assets_completed_match  check ((status = 'ready') = (completed_at is not null))
);

-- Users.avatar points at assets, and assets point back at users (circular), so add this FK now.
alter table public.users
  add constraint users_avatar_asset_fk
  foreign key (avatar_asset_id) references public.assets(id) on delete set null;

-- ---------------------------------------------------------------------
-- 3. Posts and attachments
-- ---------------------------------------------------------------------
create table public.posts (
  id              uuid         primary key default gen_random_uuid(),
  author_id       uuid         not null references public.users(id) on delete cascade,
  body            text         not null default '',
  like_count      integer      not null default 0,        -- maintained by trigger
  comment_count   integer      not null default 0,        -- maintained by trigger
  share_count     integer      not null default 0,        -- maintained by trigger
  search_vector   tsvector     generated always as (to_tsvector('english', body)) stored,
  created_at      timestamptz  not null default now(),
  edited_at       timestamptz,
  deleted_at      timestamptz,                            -- soft delete

  constraint posts_body_len        check (char_length(body) <= 2000),
  constraint posts_counts_nonneg   check (like_count >= 0 and comment_count >= 0 and share_count >= 0)
);

create table public.post_assets (
  post_id     uuid        not null references public.posts(id) on delete cascade,
  asset_id    uuid        not null references public.assets(id),  -- no cascade: attached assets can't vanish
  position    smallint    not null,
  created_at  timestamptz not null default now(),

  primary key (post_id, position),
  constraint post_assets_asset_unique   unique (asset_id),         -- one asset, one post
  constraint post_assets_position_range check (position between 0 and 3)
);

-- ---------------------------------------------------------------------
-- 4. Interactions
-- ---------------------------------------------------------------------
create table public.comments (
  id             uuid         primary key default gen_random_uuid(),
  post_id        uuid         not null references public.posts(id) on delete cascade,
  author_id      uuid         not null references public.users(id) on delete cascade,
  body           text         not null,
  search_vector  tsvector     generated always as (to_tsvector('english', body)) stored,
  created_at     timestamptz  not null default now(),
  edited_at      timestamptz,
  deleted_at     timestamptz,                               -- soft delete

  constraint comments_body_len check (char_length(body) between 1 and 1000)
);

create table public.likes (
  user_id     uuid        not null references public.users(id) on delete cascade,
  post_id     uuid        not null references public.posts(id) on delete cascade,
  created_at  timestamptz not null default now(),
  primary key (user_id, post_id)                             -- idempotent like
);

create table public.shares (
  id          uuid         primary key default gen_random_uuid(),
  user_id     uuid         not null references public.users(id) on delete cascade,
  post_id     uuid         not null references public.posts(id) on delete cascade,
  quote       text,
  created_at  timestamptz  not null default now(),

  constraint shares_user_post_unique unique (user_id, post_id),   -- one share per user per post
  constraint shares_quote_len        check (char_length(quote) <= 500)
);

create table public.follows (
  follower_id  uuid        not null references public.users(id) on delete cascade,
  followee_id  uuid        not null references public.users(id) on delete cascade,
  created_at   timestamptz not null default now(),

  primary key (follower_id, followee_id),                    -- idempotent follow
  constraint no_self_follow check (follower_id <> followee_id)
);

-- ---------------------------------------------------------------------
-- 5. Authentication (passwordless)
-- ---------------------------------------------------------------------
-- email is not a foreign key: a code is requested before the user may exist.
create table public.login_codes (
  id           uuid         primary key default gen_random_uuid(),
  email        extensions.citext not null,
  code_hash    text         not null,                          -- HMAC-SHA256 hex, never the raw code
  attempts     smallint     not null default 0,
  expires_at   timestamptz  not null,
  consumed_at  timestamptz,
  created_at   timestamptz  not null default now(),

  constraint login_codes_email_format   check (email::text ~ '^[^@\s]+@[^@\s]+\.[^@\s]+$'),
  constraint login_codes_hash_format    check (code_hash ~ '^[0-9a-f]{64}$'),
  constraint login_codes_attempts_range check (attempts between 0 and 10),
  constraint login_codes_ttl            check (expires_at > created_at)
);

create table public.refresh_tokens (
  id            uuid         primary key default gen_random_uuid(),
  user_id       uuid         not null references public.users(id) on delete cascade,
  token_hash    text         not null unique,                  -- SHA-256 hex, never the raw token
  user_agent    text,
  expires_at    timestamptz  not null,
  revoked_at    timestamptz,
  last_used_at  timestamptz,
  created_at    timestamptz  not null default now(),

  constraint refresh_tokens_hash_format check (token_hash ~ '^[0-9a-f]{64}$'),
  constraint refresh_tokens_ua_len      check (char_length(user_agent) <= 512)
);

-- ---------------------------------------------------------------------
-- 6. Indexes
-- ---------------------------------------------------------------------
-- Username prefix search (citext is cast to text so text_pattern_ops applies)
create index idx_users_username_prefix   on public.users ((username::text) text_pattern_ops);
create index idx_users_search            on public.users using gin (search_vector);

-- Assets
create index idx_assets_owner            on public.assets (owner_id, created_at desc);
create index idx_assets_pending_expiry   on public.assets (upload_expires_at) where status = 'pending';

-- Posts: feed and profile timelines (partial index skips soft-deleted rows)
create index idx_posts_author_timeline   on public.posts (author_id, created_at desc, id desc)
  where deleted_at is null;
create index idx_posts_search            on public.posts using gin (search_vector);

-- Comments
create index idx_comments_post_timeline  on public.comments (post_id, created_at, id)
  where deleted_at is null;
create index idx_comments_author         on public.comments (author_id);
create index idx_comments_search         on public.comments using gin (search_vector);

-- Interactions
create index idx_likes_post              on public.likes (post_id);
create index idx_shares_post             on public.shares (post_id);
create index idx_follows_followee        on public.follows (followee_id);
create index idx_follows_followee_keyset on public.follows (followee_id, created_at desc, follower_id desc);
create index idx_follows_follower_keyset on public.follows (follower_id, created_at desc, followee_id desc);

-- Auth
create index idx_login_codes_email_recent on public.login_codes (email, created_at desc);
create index idx_login_codes_active       on public.login_codes (email) where consumed_at is null;
create index idx_refresh_tokens_user      on public.refresh_tokens (user_id);

-- ---------------------------------------------------------------------
-- 7. Triggers: counters
--    Each trigger fires only when a row is really inserted or removed,
--    so ON CONFLICT DO NOTHING never double-counts.
-- ---------------------------------------------------------------------
create or replace function public.fn_likes_counter() returns trigger
language plpgsql set search_path = '' as $$
begin
  if tg_op = 'INSERT' then
    update public.posts set like_count = like_count + 1 where id = new.post_id;
  else
    update public.posts set like_count = greatest(like_count - 1, 0) where id = old.post_id;
  end if;
  return null;
end; $$;

create trigger trg_likes_counter
  after insert or delete on public.likes
  for each row execute function public.fn_likes_counter();

create or replace function public.fn_shares_counter() returns trigger
language plpgsql set search_path = '' as $$
begin
  if tg_op = 'INSERT' then
    update public.posts set share_count = share_count + 1 where id = new.post_id;
  else
    update public.posts set share_count = greatest(share_count - 1, 0) where id = old.post_id;
  end if;
  return null;
end; $$;

create trigger trg_shares_counter
  after insert or delete on public.shares
  for each row execute function public.fn_shares_counter();

-- Comments count only non-deleted rows, so soft delete and restore adjust the counter.
create or replace function public.fn_comments_counter() returns trigger
language plpgsql set search_path = '' as $$
begin
  if tg_op = 'INSERT' then
    if new.deleted_at is null then
      update public.posts set comment_count = comment_count + 1 where id = new.post_id;
    end if;
  elsif tg_op = 'DELETE' then
    if old.deleted_at is null then
      update public.posts set comment_count = greatest(comment_count - 1, 0) where id = old.post_id;
    end if;
  elsif tg_op = 'UPDATE' then
    if old.deleted_at is null and new.deleted_at is not null then
      update public.posts set comment_count = greatest(comment_count - 1, 0) where id = new.post_id;
    elsif old.deleted_at is not null and new.deleted_at is null then
      update public.posts set comment_count = comment_count + 1 where id = new.post_id;
    end if;
  end if;
  return null;
end; $$;

create trigger trg_comments_counter_row
  after insert or delete on public.comments
  for each row execute function public.fn_comments_counter();

create trigger trg_comments_counter_softdelete
  after update of deleted_at on public.comments
  for each row when (old.deleted_at is distinct from new.deleted_at)
  execute function public.fn_comments_counter();

create or replace function public.fn_follows_counter() returns trigger
language plpgsql set search_path = '' as $$
begin
  if tg_op = 'INSERT' then
    update public.users set follower_count  = follower_count + 1  where id = new.followee_id;
    update public.users set following_count = following_count + 1 where id = new.follower_id;
  else
    update public.users set follower_count  = greatest(follower_count - 1, 0)  where id = old.followee_id;
    update public.users set following_count = greatest(following_count - 1, 0) where id = old.follower_id;
  end if;
  return null;
end; $$;

create trigger trg_follows_counter
  after insert or delete on public.follows
  for each row execute function public.fn_follows_counter();

-- ---------------------------------------------------------------------
-- 8. Triggers: asset integrity
-- ---------------------------------------------------------------------
-- A post image must be owned by the post's author, be ready, be a post_image,
-- and the post may hold at most 4. Locking the post row serialises concurrent attaches.
create or replace function public.fn_post_assets_guard() returns trigger
language plpgsql set search_path = '' as $$
declare
  v_author  uuid;
  v_owner   uuid;
  v_kind    text;
  v_status  text;
  v_count   integer;
begin
  select author_id into v_author from public.posts where id = new.post_id for update;
  if not found then
    raise exception 'post % does not exist', new.post_id;
  end if;

  select owner_id, kind, status into v_owner, v_kind, v_status
    from public.assets where id = new.asset_id;
  if not found then
    raise exception 'asset % does not exist', new.asset_id;
  end if;

  if v_owner is distinct from v_author then
    raise exception 'asset % is not owned by the post author', new.asset_id;
  end if;
  if v_kind <> 'post_image' or v_status <> 'ready' then
    raise exception 'asset % must be a ready post_image', new.asset_id;
  end if;

  select count(*) into v_count from public.post_assets where post_id = new.post_id;
  if v_count >= 4 then
    raise exception 'post % already has 4 images', new.post_id;
  end if;

  return new;
end; $$;

create trigger trg_post_assets_guard
  before insert on public.post_assets
  for each row execute function public.fn_post_assets_guard();

-- An avatar must be a ready avatar asset owned by the user.
create or replace function public.fn_users_avatar_guard() returns trigger
language plpgsql set search_path = '' as $$
begin
  if new.avatar_asset_id is null then
    return new;
  end if;
  if not exists (
    select 1 from public.assets a
    where a.id = new.avatar_asset_id
      and a.owner_id = new.id
      and a.kind = 'avatar'
      and a.status = 'ready'
  ) then
    raise exception 'avatar % is not a ready avatar owned by this user', new.avatar_asset_id;
  end if;
  return new;
end; $$;

create trigger trg_users_avatar_guard
  before insert or update of avatar_asset_id on public.users
  for each row execute function public.fn_users_avatar_guard();

-- ---------------------------------------------------------------------
-- 9. Storage bucket (images are public by URL; writes go through the backend only)
-- ---------------------------------------------------------------------
create schema if not exists storage;
create table if not exists storage.buckets (
  id text primary key,
  name text not null,
  public boolean not null default false,
  file_size_limit bigint,
  allowed_mime_types text[]
);

insert into storage.buckets (id, name, public, file_size_limit, allowed_mime_types)
values ('media', 'media', true, 5242880, array['image/jpeg', 'image/png', 'image/webp'])
on conflict (id) do update
  set public             = excluded.public,
      file_size_limit    = excluded.file_size_limit,
      allowed_mime_types = excluded.allowed_mime_types;

-- ---------------------------------------------------------------------
-- 10. Roles, grants, and row-level security
--     The Go API connects as api_app. Set its password in Step 2, not here.
-- ---------------------------------------------------------------------
do $$
begin
  if not exists (select 1 from pg_roles where rolname = 'api_app') then
    create role api_app login noinherit;
  end if;
end $$;

grant usage on schema public to api_app;
grant usage on schema extensions to api_app;
grant select, insert, update, delete on all tables in schema public to api_app;
alter default privileges in schema public grant select, insert, update, delete on tables to api_app;

-- RLS on every public table. No policies for anon or authenticated means no access.
-- api_app gets an explicit full-access policy because the Go service enforces ownership.
do $$
declare
  r record;
begin
  for r in select tablename from pg_tables where schemaname = 'public' loop
    execute format('alter table public.%I enable row level security', r.tablename);
    execute format('drop policy if exists api_app_full_access on public.%I', r.tablename);
    execute format(
      'create policy api_app_full_access on public.%I for all to api_app using (true) with check (true)',
      r.tablename);
  end loop;
end $$;

-- ---------------------------------------------------------------------
-- 11. Scheduled cleanup (pg_cron, if available on the project)
--     Asset cleanup stays in the Go app because it must also delete storage objects.
-- ---------------------------------------------------------------------
do $body$
begin
  create extension if not exists pg_cron;
  perform cron.schedule(
    'purge-login-codes', '17 * * * *',
    $$delete from public.login_codes where expires_at < now() - interval '1 day'$$);
  perform cron.schedule(
    'purge-refresh-tokens', '23 3 * * *',
    $$delete from public.refresh_tokens where expires_at < now()$$);
exception when others then
  raise notice 'pg_cron unavailable (%); purge login_codes and refresh_tokens from the app instead.', sqlerrm;
end $body$;
