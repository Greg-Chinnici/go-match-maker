-- Applied automatically by docker compose on a fresh volume. For a hosted
-- database, run it once: psql "$DATABASE_URL" -f db/schema.sql
CREATE TABLE IF NOT EXISTS public.glickoplayers (
  id uuid PRIMARY KEY,
  rating double precision NOT NULL,
  rd double precision NOT NULL,
  volatility double precision NOT NULL
);
