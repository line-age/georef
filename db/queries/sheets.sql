-- sheets.sql — sqlc query definitions for the sheets table.
-- Run `make generate` to regenerate internal/db/ after editing this file.

-- name: CreateSheet :one
INSERT INTO sheets (
    id, title, institution, source_url, rights_note,
    status, original_checksum, original_size, original_path,
    uploader_github_id
) VALUES (
    $1, $2, $3, $4, $5,
    'processing', $6, $7, $8,
    $9
)
RETURNING *;

-- name: GetSheetByID :one
SELECT * FROM sheets WHERE id = $1 LIMIT 1;

-- name: GetSheetByIDForUpdate :one
SELECT * FROM sheets WHERE id = $1 LIMIT 1 FOR UPDATE;

-- name: ListSheets :many
SELECT * FROM sheets
WHERE (sqlc.narg('status')::sheet_status IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: UpdateSheet :one
UPDATE sheets SET
    title               = $2,
    institution         = $3,
    source_url          = $4,
    rights_note         = $5,
    updated_at          = now()
WHERE id = $1
RETURNING *;

-- name: SetSheetReady :one
UPDATE sheets SET
    status          = 'ready',
    pyramid_path    = $2,
    error_message   = NULL,
    updated_at      = now()
WHERE id = $1
RETURNING *;

-- name: RecordConversionFailure :one
-- Increments retry_count and decides the new status atomically.
-- When retry_count + 1 reaches max_retries the sheet is marked failed
-- permanently; otherwise it goes back to processing for another attempt.
UPDATE sheets SET
    retry_count   = retry_count + 1,
    status        = CASE WHEN retry_count + 1 >= $3
                         THEN 'failed'::sheet_status
                         ELSE 'processing'::sheet_status END,
    error_message = $2,
    updated_at    = now()
WHERE id = $1
RETURNING *;

-- name: SoftDeleteSheet :one
UPDATE sheets SET
    status      = 'deleted',
    deleted_at  = now(),
    updated_at  = now()
WHERE id = $1 AND status != 'deleted'
RETURNING *;

-- name: ClaimNext :one
-- Atomically claims one sheet for the worker. Picks up processing rows and
-- failed rows that have not yet exhausted their retry budget (retry_count < $1).
-- Rows with retry_count >= max_retries are terminal and left untouched.
-- SKIP LOCKED means concurrent workers never block each other.
SELECT * FROM sheets
WHERE status IN ('processing', 'failed')
  AND retry_count < $1
ORDER BY created_at
LIMIT 1
FOR UPDATE SKIP LOCKED;
