-- Drop the old partial index that included failed rows. Because a sheet only
-- reaches status='failed' when retry_count >= MAX_CONVERSION_RETRIES, failed
-- rows are permanently terminal and ClaimNext never claims them. Including them
-- in the index adds dead weight that grows with every exhausted conversion.
DROP INDEX IF EXISTS sheets_claimable_idx;

CREATE INDEX sheets_claimable_idx ON sheets (created_at)
    WHERE status = 'processing';
