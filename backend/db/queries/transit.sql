-- name: PoleDirections :many
-- The directions at the given poles (DATA_MODEL §3.2) with their route's
-- agency and colour, for building transit features. A NULL direction_id
-- becomes -1 (departures.NoDirection), the same key the departures use.
SELECT d.stop_id,
       d.route_id,
       coalesce(r.agency_id, '')::text AS agency_id,
       coalesce(d.line, '')::text AS line,
       d.mode,
       coalesce(d.direction_id, -1)::smallint AS direction_id,
       d.headsign,
       d.display_headsign,
       d.is_terminal,
       d.is_ring,
       d.trip_count,
       coalesce(r.route_color, '')::text AS route_color
FROM gtfs.pole_directions d
JOIN gtfs.routes r ON r.route_id = d.route_id
WHERE d.stop_id = ANY(sqlc.arg(stop_ids)::text[])
ORDER BY d.stop_id, d.route_id, d.direction_id, d.headsign;

-- name: StationPoles :many
-- One station with its served, in-scope poles (station sheet). No rows if
-- the station does not exist, is out of scope or has no served pole.
SELECT s.station_id,
       coalesce(s.name, '')::text AS station_name,
       ST_Y(s.geom::geometry)::float8 AS station_lat,
       ST_X(s.geom::geometry)::float8 AS station_lng,
       s.modes AS station_modes,
       p.stop_id,
       p.platform_code,
       p.stop_desc AS description,
       ST_Y(p.geom::geometry)::float8 AS lat,
       ST_X(p.geom::geometry)::float8 AS lng
FROM gtfs.stations s
JOIN gtfs.stops p ON p.station_id = s.station_id
WHERE s.station_id = sqlc.arg(station_id)
  AND p.in_scope
  AND coalesce(p.location_type, 0) = 0
  AND EXISTS (SELECT 1 FROM gtfs.pole_directions d WHERE d.stop_id = p.stop_id)
ORDER BY p.stop_id;
