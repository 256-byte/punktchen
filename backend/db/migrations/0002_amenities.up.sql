-- 0002: schema "amenities" for the toilet and drinking fountain layers
-- (ADR-0010, DATA_MODEL §7 #34). Unlike "gtfs" it is not swapped: an import
-- replaces one table's rows in a single transaction, so readers see either
-- the old or the new rows. Each import is logged in meta.feed_imports
-- (source 'berlin-toilets' / 'berlin-fountains').

CREATE SCHEMA IF NOT EXISTS amenities;

-- Public toilets from the Geoportal Berlin WFS "toiletten:toiletten".
CREATE TABLE amenities.toilets (
    -- The WFS feature id, e.g. 'toiletten.2'.
    id                text PRIMARY KEY,
    lat               double precision NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lng               double precision NOT NULL CHECK (lng BETWEEN -180 AND 180),
    -- Address as written by the source ("standort").
    address           text NOT NULL,
    district          text,
    operator          text,
    -- 'toilet' or 'urinal' (source "symbol": WC / Pissoir).
    kind              text NOT NULL CHECK (kind IN ('toilet', 'urinal')),
    -- Open only at events or funerals (DATA_MODEL §7 #34); shown with a note.
    event_only        boolean NOT NULL,
    -- Free text as the source writes it; NULL when unknown.
    opening_hours     text,
    open_24h          boolean NOT NULL,
    -- Euro; 0 = free; NULL when unknown.
    fee_eur           double precision CHECK (fee_eur >= 0),
    -- e.g. 'Karte/App' (no cash), 'Bar'; NULL when unknown.
    payment           text,
    -- "barrierefrei" (DIN 18040-1), "barrierearm", "wickeltisch"; NULL when unknown.
    accessible        boolean,
    mostly_accessible boolean,
    changing_table    boolean,
    geom              geography(Point, 4326)
        GENERATED ALWAYS AS (geography(ST_SetSRID(ST_MakePoint(lng, lat), 4326))) STORED
);
CREATE INDEX toilets_geom_idx ON amenities.toilets USING gist (geom);

-- Drinking fountains from the Geoportal Berlin WFS
-- "trinkwasserbrunnen:trinkwasserbrunnen".
CREATE TABLE amenities.fountains (
    -- The WFS feature id, e.g. 'trinkwasserbrunnen.1'.
    id                text PRIMARY KEY,
    lat               double precision NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lng               double precision NOT NULL CHECK (lng BETWEEN -180 AND 180),
    -- Location as written by the source ("standort").
    location          text NOT NULL,
    district          text,
    -- Fountain model ("trinkbrunnenart"), e.g. 'Kaiser'.
    model             text,
    built_year        smallint,
    -- Operating season, months 1–12 inclusive (all fountains of the Berliner Wasserbetriebe: May–October).
    season_from_month smallint NOT NULL CHECK (season_from_month BETWEEN 1 AND 12),
    season_to_month   smallint NOT NULL CHECK (season_to_month BETWEEN 1 AND 12),
    -- The source's restriction text when out of service, else NULL.
    out_of_service    text,
    geom              geography(Point, 4326)
        GENERATED ALWAYS AS (geography(ST_SetSRID(ST_MakePoint(lng, lat), 4326))) STORED
);
CREATE INDEX fountains_geom_idx ON amenities.fountains USING gist (geom);
