-- 001_create_sheets.sql
-- Creates the sheets table: the single source of truth for every uploaded scan.

CREATE TYPE sheet_status AS ENUM ('processing', 'ready', 'failed', 'deleted');

CREATE TABLE sheets (
    -- Identity
    id                  TEXT            PRIMARY KEY,

    -- Descriptive metadata
    title               TEXT            NOT NULL,
    institution         TEXT,
    source_url          TEXT,
    rights_note         TEXT,

    -- Lifecycle
    status              sheet_status    NOT NULL DEFAULT 'processing',
    error_message       TEXT,

    -- Original file — written at upload, never modified
    original_checksum   TEXT            NOT NULL,
    original_size       BIGINT          NOT NULL,
    original_path       TEXT            NOT NULL,

    -- Pyramid file — written by the worker; NULL until conversion succeeds
    pyramid_path        TEXT,

    -- Uploader identity — nullable until requirement is confirmed
    uploader_github_id  BIGINT,

    -- Timestamps
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);

-- Partial index used by the worker's ClaimProcessing query (SELECT … FOR UPDATE SKIP LOCKED).
-- Covers processing and failed rows only; ready and deleted rows are never claimed again.
CREATE INDEX sheets_claimable_idx ON sheets (created_at)
    WHERE status IN ('processing', 'failed');
