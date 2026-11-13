-- 002_add_retry_count.sql
-- Adds retry_count to track how many conversion attempts a sheet has had.
-- The worker uses this together with MAX_CONVERSION_RETRIES to decide whether
-- a failed attempt re-enqueues the sheet (status → processing) or marks it
-- terminal (status → failed).

ALTER TABLE sheets ADD COLUMN retry_count INTEGER NOT NULL DEFAULT 0;
