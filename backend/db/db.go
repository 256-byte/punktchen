// Package db holds the SQL that defines and queries the database:
// migrations (embedded into the binary) and the sqlc query files under
// "queries". sqlc reads both (config: ../sqlc.yaml) and generates typed Go
// code into ../gen/queries; see generate.go.
package db

import "embed"

// Migrations contains the golang-migrate files (NNNN_title.up.sql /
// NNNN_title.down.sql) under the "migrations" directory.
//
// Each file is sent as one multi-statement query, which Postgres runs as a
// single implicit transaction: a failing migration leaves no partial changes.
// Consequence: statements that cannot run in a transaction (e.g.
// CREATE INDEX CONCURRENTLY) do not belong in these files.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// MigrationsDir is the directory inside Migrations that holds the files.
const MigrationsDir = "migrations"

// GTFSSchema creates the schema "gtfs" and its tables (DATA_MODEL §2). The
// importer runs it with "gtfs." rewritten to the staging schema; sqlc reads
// the file directly.
//
//go:embed gtfs/schema.sql
var GTFSSchema string

// GTFSIndexes adds keys and indexes to the GTFS tables after loading; same
// rewrite rule as GTFSSchema.
//
//go:embed gtfs/indexes.sql
var GTFSIndexes string

// GTFSDerived builds the derived tables (stations, service_dates,
// pole_directions) after GTFSIndexes; same rewrite rule.
//
//go:embed gtfs/derived.sql
var GTFSDerived string
