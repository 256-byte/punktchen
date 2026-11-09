-- name: LastSuccessfulFeedImport :one
-- The newest successful import of a feed. Its ETag, Last-Modified and
-- SHA-256 are what the next download is compared against (ARCHITECTURE §3.1).
SELECT *
FROM meta.feed_imports
WHERE source = $1
  AND status = 'succeeded'
ORDER BY started_at DESC
LIMIT 1;

-- name: StartFeedImport :one
-- Opens an attempt before anything is downloaded, so failed downloads are
-- logged too.
INSERT INTO meta.feed_imports (source, url, status)
VALUES ($1, $2, 'running')
RETURNING id;

-- name: RecordFeedDownload :execrows
-- Stores the validators as received and the hash of the downloaded file
-- (hash NULL after 304 Not Modified: nothing was downloaded).
UPDATE meta.feed_imports
SET etag          = sqlc.narg(etag),
    last_modified = sqlc.narg(last_modified),
    sha256        = sqlc.narg(sha256)
WHERE id = sqlc.arg(id)
  AND status = 'running';

-- name: FinishFeedImport :execrows
-- Closes a running attempt. Affects 0 rows if it was already finished.
UPDATE meta.feed_imports
SET status      = sqlc.arg(status),
    finished_at = now(),
    row_counts  = sqlc.narg(row_counts),
    error       = sqlc.narg(error)
WHERE id = sqlc.arg(id)
  AND status = 'running';
