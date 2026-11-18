-- Amenity layers (ADR-0010): replace a dataset, find features near a point.
-- An import runs DeleteX + InsertX in one transaction.

-- name: DeleteToilets :exec
DELETE FROM amenities.toilets;

-- name: InsertToilets :copyfrom
INSERT INTO amenities.toilets
    (id, lat, lng, address, district, operator, kind, event_only, opening_hours,
     open_24h, fee_eur, payment, accessible, mostly_accessible, changing_table)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15);

-- name: CountToilets :one
SELECT count(*)::integer AS n FROM amenities.toilets;

-- name: NearbyToilets :many
-- Sparse-layer rule (ADR-0010): every toilet within radius_m, plus the
-- nearest ones within max_m until there are min_results. Nearest first.
-- Both parts use the GiST index (ST_DWithin, KNN "<->").
WITH center AS (
    SELECT ST_SetSRID(ST_MakePoint(sqlc.arg(lng)::float8, sqlc.arg(lat)::float8), 4326)::geography AS g
), picked AS (
    SELECT t.id
    FROM amenities.toilets t, center c
    WHERE ST_DWithin(t.geom, c.g, sqlc.arg(radius_m)::float8)
    UNION
    (SELECT t.id
     FROM amenities.toilets t, center c
     WHERE ST_DWithin(t.geom, c.g, sqlc.arg(max_m)::float8)
     ORDER BY t.geom <-> c.g
     LIMIT sqlc.arg(min_results)::integer)
)
SELECT t.id, t.lat, t.lng, t.address, t.district, t.operator, t.kind, t.event_only,
       t.opening_hours, t.open_24h, t.fee_eur, t.payment, t.accessible,
       t.mostly_accessible, t.changing_table,
       ST_Distance(t.geom, c.g)::float8 AS distance_m
FROM amenities.toilets t
JOIN picked p ON p.id = t.id
CROSS JOIN center c
ORDER BY distance_m, t.id;

-- name: DeleteFountains :exec
DELETE FROM amenities.fountains;

-- name: InsertFountains :copyfrom
INSERT INTO amenities.fountains
    (id, lat, lng, location, district, model, built_year, season_from_month,
     season_to_month, out_of_service)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: CountFountains :one
SELECT count(*)::integer AS n FROM amenities.fountains;

-- name: NearbyFountains :many
-- Same rule as NearbyToilets.
WITH center AS (
    SELECT ST_SetSRID(ST_MakePoint(sqlc.arg(lng)::float8, sqlc.arg(lat)::float8), 4326)::geography AS g
), picked AS (
    SELECT f.id
    FROM amenities.fountains f, center c
    WHERE ST_DWithin(f.geom, c.g, sqlc.arg(radius_m)::float8)
    UNION
    (SELECT f.id
     FROM amenities.fountains f, center c
     WHERE ST_DWithin(f.geom, c.g, sqlc.arg(max_m)::float8)
     ORDER BY f.geom <-> c.g
     LIMIT sqlc.arg(min_results)::integer)
)
SELECT f.id, f.lat, f.lng, f.location, f.district, f.model, f.built_year,
       f.season_from_month, f.season_to_month, f.out_of_service,
       ST_Distance(f.geom, c.g)::float8 AS distance_m
FROM amenities.fountains f
JOIN picked p ON p.id = f.id
CROSS JOIN center c
ORDER BY distance_m, f.id;
