DROP INDEX IF EXISTS sheets_claimable_idx;

CREATE INDEX sheets_claimable_idx ON sheets (created_at)
    WHERE status IN ('processing', 'failed');
