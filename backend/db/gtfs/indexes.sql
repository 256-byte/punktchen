-- Keys and indexes of the imported GTFS tables (DATA_MODEL §2), created after
-- loading, in the same transaction. Same "gtfs." rewrite rule as schema.sql.
--
-- Primary keys double as checks: a duplicate trip_id or (trip_id,
-- stop_sequence) fails the import instead of producing wrong departures.
-- No foreign keys: dangling references are reported by the sanity checks
-- (task 1.6) instead of aborting on the first one.

ALTER TABLE gtfs.routes         ADD PRIMARY KEY (route_id);
ALTER TABLE gtfs.trips          ADD PRIMARY KEY (trip_id);
ALTER TABLE gtfs.stops          ADD PRIMARY KEY (stop_id);
ALTER TABLE gtfs.calendar       ADD PRIMARY KEY (service_id);
ALTER TABLE gtfs.calendar_dates ADD PRIMARY KEY (service_id, date);
ALTER TABLE gtfs.stop_times     ADD PRIMARY KEY (trip_id, stop_sequence);

-- Departures of a pole in a time window (schedule window, ARCHITECTURE §3.2).
CREATE INDEX stop_times_stop_departure_idx ON gtfs.stop_times (stop_id, departure_secs);
CREATE INDEX trips_service_idx             ON gtfs.trips (service_id);
CREATE INDEX trips_route_idx               ON gtfs.trips (route_id);
CREATE INDEX stops_parent_idx              ON gtfs.stops (parent_station);
-- Poles near a point (task 1.8).
CREATE INDEX stops_geom_idx                ON gtfs.stops USING gist (geom);
