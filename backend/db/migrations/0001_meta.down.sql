-- Schema "meta" and the postgis extension stay: meta.schema_migrations lives
-- in "meta", and imported GTFS data may depend on postgis.
DROP TABLE IF EXISTS meta.feed_imports;
