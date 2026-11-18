-- Idempotent dev seed script (ON CONFLICT DO NOTHING)
-- Run with MIGRATION_DATABASE_URL (not with api_app)

INSERT INTO public.users (username, email, display_name, birthdate, role)
VALUES
  ('admin', 'admin@example.com', 'System Admin', '1990-01-01', 'admin'),
  ('alice', 'alice@example.com', 'Alice Smith', '1995-05-15', 'user'),
  ('bob', 'bob@example.com', 'Bob Jones', '1998-08-20', 'user')
ON CONFLICT (username) DO NOTHING;
