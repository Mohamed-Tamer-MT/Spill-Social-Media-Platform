-- ---------------------------------------------------------------------
-- 11. Revert scheduled cleanup (pg_cron)
-- ---------------------------------------------------------------------
do $body$
begin
  if exists (select 1 from pg_extension where extname = 'pg_cron') then
    perform cron.unschedule('purge-refresh-tokens');
    perform cron.unschedule('purge-login-codes');
  end if;
exception when others then
  null;
end $body$;

-- ---------------------------------------------------------------------
-- 10. Revert roles, grants, and row-level security
-- ---------------------------------------------------------------------
do $$
declare
  r record;
begin
  for r in select tablename from pg_tables where schemaname = 'public' loop
    execute format('drop policy if exists api_app_full_access on public.%I', r.tablename);
    execute format('alter table public.%I disable row level security', r.tablename);
  end loop;
end $$;

-- ---------------------------------------------------------------------
-- 9. Revert storage bucket
-- ---------------------------------------------------------------------
delete from storage.buckets where id = 'media';

-- ---------------------------------------------------------------------
-- 8. Revert triggers: asset integrity
-- ---------------------------------------------------------------------
drop trigger if exists trg_users_avatar_guard on public.users;
drop function if exists public.fn_users_avatar_guard();

drop trigger if exists trg_post_assets_guard on public.post_assets;
drop function if exists public.fn_post_assets_guard();

-- ---------------------------------------------------------------------
-- 7. Revert triggers: counters
-- ---------------------------------------------------------------------
drop trigger if exists trg_follows_counter on public.follows;
drop function if exists public.fn_follows_counter();

drop trigger if exists trg_comments_counter_softdelete on public.comments;
drop trigger if exists trg_comments_counter_row on public.comments;
drop function if exists public.fn_comments_counter();

drop trigger if exists trg_shares_counter on public.shares;
drop function if exists public.fn_shares_counter();

drop trigger if exists trg_likes_counter on public.likes;
drop function if exists public.fn_likes_counter();

-- ---------------------------------------------------------------------
-- 6. Revert circular FK between users and assets
-- ---------------------------------------------------------------------
alter table if exists public.users drop constraint if exists users_avatar_asset_fk;

-- ---------------------------------------------------------------------
-- 5-1. Revert tables in reverse dependency order
-- ---------------------------------------------------------------------
drop table if exists public.refresh_tokens cascade;
drop table if exists public.login_codes cascade;
drop table if exists public.follows cascade;
drop table if exists public.shares cascade;
drop table if exists public.likes cascade;
drop table if exists public.comments cascade;
drop table if exists public.post_assets cascade;
drop table if exists public.posts cascade;
drop table if exists public.assets cascade;
drop table if exists public.users cascade;
