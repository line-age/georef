-- 004_add_public_to_sheets.sql
-- Adds per-sheet visibility. All existing sheets default to public.

ALTER TABLE sheets ADD COLUMN public BOOLEAN NOT NULL DEFAULT TRUE;
