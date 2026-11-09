-- name: ScheduledDepartures :many
-- Boardable departures at in-scope poles of one service day whose
-- departure_secs (seconds since the day's "noon minus 12 h") lie in
-- [from_secs, to_secs] (ARCHITECTURE §3.2). The caller turns service day +
-- seconds into absolute times and asks once per service day that can reach
-- the window. Left out: a trip's last stop (it only arrives there) and stop
-- times without pickup (pickup_type 1). Uses stop_times_departure_idx; run it
-- with plan_cache_mode = force_custom_plan (a generic plan cannot see how
-- narrow the range is).
SELECT st.trip_id,
       st.stop_id,
       st.stop_sequence,
       st.departure_secs::integer AS departure_secs,
       ti.route_id,
       coalesce(ti.direction_id, -1)::smallint AS direction_id,
       ti.headsign,
       coalesce(st.stop_headsign, '')::text AS stop_headsign
FROM gtfs.stop_times st
JOIN gtfs.trip_info ti ON ti.trip_id = st.trip_id
JOIN gtfs.service_dates sd ON sd.service_id = ti.service_id AND sd.service_date = sqlc.arg(service_date)::date
JOIN gtfs.stops s ON s.stop_id = st.stop_id
WHERE st.departure_secs BETWEEN sqlc.arg(from_secs)::integer AND sqlc.arg(to_secs)::integer
  AND s.in_scope
  AND coalesce(st.pickup_type, 0) <> 1
  AND st.stop_sequence < ti.last_stop_sequence;

-- name: LiveScheduleID :one
-- Identity of the live schedule: the OID of the "gtfs" schema changes with
-- every import swap (ADR-0004). No row (pgx.ErrNoRows) before the first
-- import.
SELECT oid::bigint AS id FROM pg_catalog.pg_namespace WHERE nspname = 'gtfs';

-- name: MaxDepartureSecs :one
-- Largest departure time of the live schedule in seconds (VBB: 31:17:00,
-- DATA_MODEL §7 #10): how many service days back can reach a window. Read
-- from stop_times_departure_idx.
SELECT coalesce(max(departure_secs), 0)::integer AS secs FROM gtfs.stop_times;
