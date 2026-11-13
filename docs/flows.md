# Upload and worker flows

This document explains what happens when a scan is uploaded and how the worker
converts it into a tile-ready pyramidal TIFF. It covers the happy path and what
happens when conversion fails.

## Upload flow

When a client sends `POST /sheets`, the server processes the request in three
steps: validate the file, store it, then write the database record.

```mermaid
flowchart TD
    A([client sends POST /sheets]) --> B[parse multipart form\nup to MAX_UPLOAD_SIZE bytes]
    B --> C{size limit hit?}
    C -->|yes| D[413 Request Entity Too Large]
    C -->|no| E{MIME type\nimage/tiff or image/jpeg?}
    E -->|no| F[400 Bad Request]
    E -->|yes| G[generate ULID for new sheet ID]
    G --> H[stream file to storage\ncompute SHA-256 and byte count]
    H --> I{storage write OK?}
    I -->|no| J[500 Internal Server Error]
    I -->|yes| K[insert sheet row\nstatus = 'processing']
    K --> L{DB insert OK?}
    L -->|no| M[500 Internal Server Error]
    L -->|yes| N[202 Accepted\nreturn ID, sheet URL, IIIF URL, manifest URL]
```

### Key points

- **The ID is a ULID**, not an auto-increment integer. It is stable forever and
  safe to embed in IIIF annotations.
- **The file is streamed**, not buffered — the server reads the upload once,
  writing bytes to storage and feeding the SHA-256 hash in a single pass.
- **Status starts as `processing`** immediately. The client can poll
  `GET /sheets/{id}` or wait for a `503 Retry-After` on IIIF requests until
  the worker finishes.
- **IIIF URLs are stable from this moment.** The sheet URL, IIIF base URL and
  manifest URL returned in the `202` response never change, even before
  conversion completes.

---

## Worker flow

The worker runs as a separate process. It polls Postgres for sheets to convert
and processes them one at a time.

```mermaid
flowchart TD
    A([worker starts]) --> B[wait for poll interval\nor immediately if previous job was found]
    B --> C[BEGIN transaction]
    C --> D["SELECT … FOR UPDATE SKIP LOCKED\nWHERE status IN ('processing', 'failed')\nORDER BY created_at LIMIT 1"]
    D --> E{row found?}
    E -->|no| F[ROLLBACK\nsleep poll interval]
    F --> B
    E -->|yes| G[call convertSheet\nrow is locked for the whole conversion]
    G --> H[resolve original scan path\nfrom storage]
    H --> I[create temp file\nin OS temp directory]
    I --> J["run vips tiffsave\n--tile --pyramid --compression jpeg\n--tile-width N --tile-height N"]
    J --> K{vips succeeded?}
    K -->|no| L[delete temp file\nreturn error to ProcessNext]
    K -->|yes| M[open temp file\nstream to storage as pyramid.tif\ndelete temp file]
    M --> N{storage write OK?}
    N -->|no| O[return error to ProcessNext]
    N -->|yes| P[return pyramid path to ProcessNext]
    L --> Q["RecordConversionFailure\nretry_count++\nretry_count < MAX → status = 'processing'\nretry_count >= MAX → status = 'failed'"]
    O --> Q
    P --> R["SetSheetReady\nstatus = 'ready'\npyramid path saved"]
    Q --> S[COMMIT transaction]
    R --> S
    S --> B
```

### How the queue works

The worker does not need a separate job queue. The `sheets` table itself is the
queue. `ProcessNext` opens a database transaction and runs:

```sql
SELECT * FROM sheets
WHERE status IN ('processing', 'failed')
  AND retry_count < $1   -- $1 = MAX_CONVERSION_RETRIES; skips terminal sheets
ORDER BY created_at
LIMIT 1
FOR UPDATE SKIP LOCKED;
```

`FOR UPDATE` locks the row so no other worker can claim the same sheet.
`SKIP LOCKED` means other workers do not wait — they move on to the next
available sheet immediately. This lets multiple worker instances run safely
without coordinating with each other.

The lock is held for the entire conversion. When `convertSheet` returns, the
worker writes the final status (`ready`, `processing` for retry, or `failed`
when the retry budget is exhausted) and commits — releasing the lock in the
same step.

### Atomic file writes

vips writes to a temporary file, not directly to storage. Once vips finishes
successfully, the worker streams that temp file into the storage layer:

- **Local filesystem:** writes to a temp path, then renames it to the final
  path in one `os.Rename` call. Rename is atomic on POSIX — a reader never
  sees a half-written file.
- **S3:** uploads as a single PUT request. S3 does not expose partial objects
  until the PUT completes.

The temp file is always deleted, whether conversion succeeded or not.

---

## Retry flow

Retries are bounded. Each failed attempt increments `retry_count` on the
sheet. `ClaimNext` only picks up rows where `retry_count < MAX_CONVERSION_RETRIES`
(default 3), so a permanently broken sheet is not retried indefinitely.

```mermaid
flowchart TD
    A([conversion fails]) --> B["RecordConversionFailure\nretry_count = retry_count + 1"]
    B --> C{retry_count >= MAX_RETRIES?}
    C -->|no| D["status = 'processing'\nsheet re-enters the queue"]
    C -->|yes| E["status = 'failed'\nterminal — ClaimNext skips it"]
    D --> F[worker polls on next interval]
    F --> G[ClaimNext picks it up\nbecause retry_count < MAX_RETRIES]
    G --> H[convertSheet runs again]
    H --> I{succeeds?}
    I -->|yes| J["status = 'ready'"]
    I -->|no| B
```

### What this means in practice

- **Transient failures** (a vips crash, a full disk that gets cleared, a
  network blip to S3) resolve on their own within `MAX_CONVERSION_RETRIES`
  attempts.
- **Permanent failures** (a corrupt scan that vips cannot parse) are retried
  up to `MAX_CONVERSION_RETRIES` times, then the sheet is marked `failed`
  and left untouched. To retry again, delete the sheet and re-upload.
- **Worker crash mid-conversion:** if the worker dies while `convertSheet` is
  running, the transaction is never committed. Postgres rolls it back
  automatically, returning the sheet to its previous status without
  incrementing `retry_count`. The next worker that polls picks it up.

---

## Sheet status lifecycle

```mermaid
stateDiagram-v2
    [*] --> processing: POST /sheets\n(upload succeeds)
    processing --> ready: worker converts successfully
    processing --> failed: worker exhausts retry budget\n(retry_count >= MAX_CONVERSION_RETRIES)
    processing --> processing: worker attempt fails\nbut retry budget remains\n(retry_count < MAX)
    failed --> processing: manual re-upload after delete
    processing --> ready: worker converts successfully
    ready --> deleted: DELETE /sheets/{id}
    processing --> deleted: DELETE /sheets/{id}
    failed --> deleted: DELETE /sheets/{id}
    deleted --> [*]: ID permanently returns 410
```
