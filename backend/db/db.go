// Package db holds the SQL that defines and queries the database:
// migrations (embedded into the binary) and, later, sqlc queries.
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
