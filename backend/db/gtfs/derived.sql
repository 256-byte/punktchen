-- Derived tables (DATA_MODEL §3), built in the import transaction after
-- indexes.sql. Same "gtfs." rewrite rule as schema.sql. Deterministic: the
-- result depends only on the feed, not on the import date.

-- 1. Station of every pole (§3.4): parent_station, else the DHID prefix
--    de:<AGS>:<station>, else the pole itself.
UPDATE gtfs.stops s
SET station_id = CASE
        WHEN s.parent_station IS NOT NULL THEN s.parent_station
        WHEN s.stop_id LIKE 'de:%:%' THEN split_part(s.stop_id, ':', 1) || ':' || split_part(s.stop_id, ':', 2) || ':' || split_part(s.stop_id, ':', 3)
        ELSE s.stop_id
    END
WHERE coalesce(s.location_type, 0) = 0;

-- 2. Service dates over the whole feed: calendar weekdays in its date range,
--    plus added dates, minus removed dates (§3.1).
INSERT INTO gtfs.service_dates (service_date, service_id)
SELECT d::date, c.service_id
FROM gtfs.calendar c
CROSS JOIN LATERAL generate_series(c.start_date, c.end_date, interval '1 day') AS d
WHERE CASE extract(isodow FROM d)
          WHEN 1 THEN c.monday    WHEN 2 THEN c.tuesday WHEN 3 THEN c.wednesday
          WHEN 4 THEN c.thursday  WHEN 5 THEN c.friday  WHEN 6 THEN c.saturday
          ELSE c.sunday
      END
UNION
SELECT date, service_id FROM gtfs.calendar_dates WHERE exception_type = 1
EXCEPT
SELECT date, service_id FROM gtfs.calendar_dates WHERE exception_type = 2;

ALTER TABLE gtfs.service_dates ADD PRIMARY KEY (service_date, service_id);

-- 3. Per trip: first/last stop_sequence, loop flag (starts and ends at the
--    same station, §7 #11) and number of instances.
CREATE TEMP TABLE trip_info ON COMMIT DROP AS
WITH ends AS (
    SELECT trip_id, min(stop_sequence) AS first_seq, max(stop_sequence) AS last_seq
    FROM gtfs.stop_times
    GROUP BY trip_id
), instances AS (
    SELECT service_id, count(*) AS n
    FROM gtfs.service_dates
    GROUP BY service_id
)
-- 32 VBB trips have no trip_headsign (§7 #8): use the last stop's name, as the
-- GTFS reference suggests.
SELECT t.trip_id, t.route_id, t.direction_id, coalesce(t.trip_headsign, ls.stop_name, '') AS headsign,
       e.first_seq, e.last_seq,
       fs.station_id = ls.station_id AS is_loop,
       coalesce(i.n, 0) AS instances
FROM gtfs.trips t
JOIN ends e ON e.trip_id = t.trip_id
JOIN gtfs.stop_times f ON f.trip_id = e.trip_id AND f.stop_sequence = e.first_seq
JOIN gtfs.stop_times l ON l.trip_id = e.trip_id AND l.stop_sequence = e.last_seq
JOIN gtfs.stops fs ON fs.stop_id = f.stop_id
JOIN gtfs.stops ls ON ls.stop_id = l.stop_id
LEFT JOIN instances i ON i.service_id = t.service_id;

-- 4. Directions per pole (§3.2). A pole is terminal (origin) for a direction
--    when it is the last (first) stop of >= 95 % of the direction's non-loop
--    trips there; loop trips never make a pole terminal.
INSERT INTO gtfs.pole_directions (stop_id, station_id, route_id, line, mode, direction_id, headsign,
                                  display_headsign, is_terminal, is_origin, is_ring, trip_count)
SELECT st.stop_id, s.station_id, ti.route_id, r.route_short_name,
       CASE
           WHEN r.route_type = 109 THEN 'sbahn'
           WHEN r.route_type IN (400, 1) THEN 'ubahn'
           WHEN r.route_type IN (900, 0) THEN 'tram'
           WHEN r.route_type IN (700, 3) THEN 'bus'
           WHEN r.route_type IN (1000, 4) THEN 'ferry'
           WHEN r.route_type IN (100, 106, 2) THEN 'regional'
           ELSE 'other'
       END,
       ti.direction_id, ti.headsign,
       mode() WITHIN GROUP (ORDER BY coalesce(st.stop_headsign, ti.headsign)),
       coalesce(count(*) FILTER (WHERE NOT ti.is_loop AND st.stop_sequence = ti.last_seq)
                    >= 0.95 * nullif(count(*) FILTER (WHERE NOT ti.is_loop), 0), false),
       coalesce(count(*) FILTER (WHERE NOT ti.is_loop AND st.stop_sequence = ti.first_seq)
                    >= 0.95 * nullif(count(*) FILTER (WHERE NOT ti.is_loop), 0), false),
       (r.route_type = 109 AND r.route_short_name IN ('S41', 'S42')) OR ti.headsign LIKE 'Ringbahn%',
       sum(ti.instances)
FROM gtfs.stop_times st
JOIN trip_info ti ON ti.trip_id = st.trip_id
JOIN gtfs.routes r ON r.route_id = ti.route_id
JOIN gtfs.stops s ON s.stop_id = st.stop_id
WHERE s.station_id IS NOT NULL
GROUP BY st.stop_id, s.station_id, ti.route_id, r.route_short_name, r.route_type, ti.direction_id, ti.headsign;

CREATE INDEX pole_directions_stop_idx    ON gtfs.pole_directions (stop_id);
CREATE INDEX pole_directions_station_idx ON gtfs.pole_directions (station_id);

-- 5. Stations (§3.4): name and location of the location_type=1 row if it
--    exists, else the most common pole name and the poles' centroid.
INSERT INTO gtfs.stations (station_id, name, geom, modes, in_scope, pole_count)
SELECT p.station_id,
       coalesce(min(st.stop_name), mode() WITHIN GROUP (ORDER BY p.stop_name)),
       coalesce((array_agg(st.geom))[1], ST_Centroid(ST_Collect(p.geom::geometry))::geography),
       coalesce(m.modes, '{}'),
       bool_or(p.in_scope),
       count(*)
FROM gtfs.stops p
LEFT JOIN gtfs.stops st ON st.stop_id = p.station_id AND st.location_type = 1
LEFT JOIN (
    SELECT station_id, array_agg(DISTINCT mode ORDER BY mode) AS modes
    FROM gtfs.pole_directions
    GROUP BY station_id
) m ON m.station_id = p.station_id
WHERE coalesce(p.location_type, 0) = 0
GROUP BY p.station_id, m.modes;

ALTER TABLE gtfs.stations ADD PRIMARY KEY (station_id);
CREATE INDEX stations_geom_idx ON gtfs.stations USING gist (geom);
CREATE INDEX stops_station_idx ON gtfs.stops (station_id);
