-- Schema of the imported GTFS tables (DATA_MODEL §2).
--
-- This file is not a migration. The importer runs it for every import with
-- the prefix "gtfs." rewritten to "gtfs_staging." (ADR-0004), then swaps the
-- schemas. sqlc reads it as is, so queries are written against "gtfs.".
-- Rules: qualify every table as gtfs.<name>; no other occurrence of "gtfs.".
--
-- Columns follow the GTFS reference; all columns of the files we import are
-- kept, also those we do not use yet. Empty CSV values become NULL.
-- No keys or indexes here: they are added after loading (task 1.4).

CREATE SCHEMA gtfs;

CREATE TABLE gtfs.agency (
    agency_id       text,
    agency_name     text NOT NULL,
    agency_url      text,
    agency_timezone text,
    agency_lang     text,
    agency_phone    text,
    agency_fare_url text,
    agency_email    text
);

CREATE TABLE gtfs.routes (
    route_id         text NOT NULL,
    agency_id        text,
    route_short_name text,
    route_long_name  text,
    route_desc       text,
    -- Extended route types, e.g. 109 S-Bahn, 400 U-Bahn, 700 bus (DATA_MODEL §6).
    route_type       integer NOT NULL,
    route_url        text,
    route_color      text,
    route_text_color text,
    route_sort_order integer
);

CREATE TABLE gtfs.trips (
    trip_id               text NOT NULL,
    route_id              text NOT NULL,
    service_id            text NOT NULL,
    trip_headsign         text,
    trip_short_name       text,
    direction_id          smallint,
    block_id              text,
    shape_id              text,
    wheelchair_accessible smallint,
    bikes_allowed         smallint
);

CREATE TABLE gtfs.stop_times (
    trip_id             text NOT NULL,
    stop_sequence       integer NOT NULL,
    stop_id             text NOT NULL,
    -- *_time: raw text as in the feed, may exceed 24:00:00 (DATA_MODEL §7 #10).
    -- *_secs: seconds since the start of the service day ("noon minus 12 h").
    arrival_time        text,
    arrival_secs        integer,
    departure_time      text,
    departure_secs      integer,
    stop_headsign       text,
    pickup_type         smallint,
    drop_off_type       smallint,
    shape_dist_traveled double precision,
    timepoint           smallint
);

CREATE TABLE gtfs.stops (
    stop_id             text NOT NULL,
    stop_code           text,
    stop_name           text,
    stop_desc           text,
    stop_lat            double precision,
    stop_lon            double precision,
    zone_id             text,
    stop_url            text,
    -- 0/NULL pole, 1 station, 2 entrance, 3 generic node, 4 boarding area.
    location_type       smallint,
    parent_station      text,
    stop_timezone       text,
    wheelchair_boarding smallint,
    level_id            text,
    platform_code       text,
    geom                geography(Point, 4326)
        GENERATED ALWAYS AS (geography(ST_SetSRID(ST_MakePoint(stop_lon, stop_lat), 4326))) STORED
);

CREATE TABLE gtfs.calendar (
    service_id text NOT NULL,
    monday     boolean NOT NULL,
    tuesday    boolean NOT NULL,
    wednesday  boolean NOT NULL,
    thursday   boolean NOT NULL,
    friday     boolean NOT NULL,
    saturday   boolean NOT NULL,
    sunday     boolean NOT NULL,
    start_date date NOT NULL,
    end_date   date NOT NULL
);

CREATE TABLE gtfs.calendar_dates (
    service_id     text NOT NULL,
    date           date NOT NULL,
    -- 1 = service added, 2 = service removed.
    exception_type smallint NOT NULL
);
