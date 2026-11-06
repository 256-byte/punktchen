-- 0001: PostGIS + the non-swapped "meta" schema (DATA_MODEL §3.3, ADR-0004).
-- The "gtfs" schema is not created here: the importer builds it through
-- gtfs_staging + atomic rename.

-- Needs superuser (or a role allowed to create this extension) on first run.
CREATE EXTENSION IF NOT EXISTS postgis;

-- Normally already created by the migrate runner, because
-- meta.schema_migrations lives here. Kept for a self-contained migration.
CREATE SCHEMA IF NOT EXISTS meta;

-- One row per import attempt of an upstream feed.
CREATE TABLE meta.feed_imports (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Logical feed name, e.g. 'vbb-gtfs'.
    source        text        NOT NULL,
    url           text        NOT NULL,
    -- HTTP validators exactly as received, sent back in conditional GETs.
    etag          text,
    last_modified text,
    -- Hex SHA-256 of the downloaded file; NULL when nothing was downloaded
    -- (e.g. 304 Not Modified).
    sha256        text        CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    started_at    timestamptz NOT NULL DEFAULT now(),
    finished_at   timestamptz,
    status        text        NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'skipped')),
    -- Rows per imported table, e.g. {"stops": 41000, "stop_times": 5600000}.
    row_counts    jsonb,
    error         text,

    -- A finished attempt has an end time; a running one does not.
    CONSTRAINT feed_imports_finished_consistent
        CHECK ((status = 'running') = (finished_at IS NULL)),
    CONSTRAINT feed_imports_error_only_on_failure
        CHECK (error IS NULL OR status = 'failed')
);

-- "Last attempt / last success of feed X".
CREATE INDEX feed_imports_source_started_idx
    ON meta.feed_imports (source, started_at DESC);
