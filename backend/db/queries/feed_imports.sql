-- name: LastSuccessfulFeedImport :one
-- The newest successful import of a feed. Its ETag, Last-Modified and
-- SHA-256 are what the next download is compared against (ARCHITECTURE §3.1).
SELECT *
FROM meta.feed_imports
WHERE source = $1
  AND status = 'succeeded'
ORDER BY started_at DESC
LIMIT 1;
