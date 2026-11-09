-- name: NearbyPoles :many
-- Served, in-scope poles within radius_m meters of a point, with their
-- station (DATA_MODEL §3.4). Grouped by station: rows come station by
-- station, nearest station first; a station's distance is that of its
-- nearest pole in the circle. Poles without any departures are left out
-- (DATA_MODEL §7 #12). Uses the GiST index on stops.geom.
WITH center AS (
    SELECT ST_SetSRID(ST_MakePoint(sqlc.arg(lng)::float8, sqlc.arg(lat)::float8), 4326)::geography AS g
), poles AS (
    SELECT p.stop_id, p.station_id, p.platform_code,
           p.stop_desc AS description,
           ST_Y(p.geom::geometry)::float8 AS lat,
           ST_X(p.geom::geometry)::float8 AS lng,
           ST_Distance(p.geom, c.g)::float8 AS distance_m
    FROM gtfs.stops p, center c
    WHERE ST_DWithin(p.geom, c.g, sqlc.arg(radius_m)::float8)
      AND p.in_scope
      AND coalesce(p.location_type, 0) = 0
      AND EXISTS (SELECT 1 FROM gtfs.pole_directions d WHERE d.stop_id = p.stop_id)
), ranked AS (
    SELECT poles.*, min(distance_m) OVER (PARTITION BY station_id) AS station_distance_m
    FROM poles
)
SELECT s.station_id,
       s.name AS station_name,
       ST_Y(s.geom::geometry)::float8 AS station_lat,
       ST_X(s.geom::geometry)::float8 AS station_lng,
       s.modes AS station_modes,
       r.station_distance_m::float8 AS station_distance_m,
       r.stop_id,
       r.platform_code,
       r.description,
       r.lat,
       r.lng,
       r.distance_m
FROM ranked r
JOIN gtfs.stations s ON s.station_id = r.station_id
ORDER BY r.station_distance_m, r.station_id, r.distance_m, r.stop_id;
