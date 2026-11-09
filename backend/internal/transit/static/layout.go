package static

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/db"
)

// layoutRevision: increase it when Go code changes what an import writes
// without a change in db/gtfs/*.sql (e.g. a new column transform in
// tables.go). SQL changes are picked up by Layout on their own.
const layoutRevision = 1

// Layout identifies the table layout this binary writes and its queries
// expect: a hash of the gtfs SQL files and layoutRevision. Each import stamps
// it on the schema. A live schedule with another stamp (or none) is imported
// again, even if the feed did not change.
var Layout = layoutOf(layoutRevision, db.GTFSSchema, db.GTFSIndexes, db.GTFSDerived)

func layoutOf(revision int, sqlFiles ...string) string {
	h := sha256.New()
	h.Write([]byte(strconv.Itoa(revision)))
	for _, s := range sqlFiles {
		h.Write([]byte{0})
		h.Write([]byte(s))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// layoutComment is the schema comment that carries the stamp.
func layoutComment(layout string) string { return "livestop layout " + layout }

// stampLayout marks the staging schema with Layout (inside the load tx).
func stampLayout(ctx context.Context, tx pgx.Tx) error {
	// Layout is hex, safe to inline; COMMENT takes no parameters.
	_, err := tx.Exec(ctx, `COMMENT ON SCHEMA `+StagingSchema+` IS '`+layoutComment(Layout)+`'`)
	return err
}

// liveLayoutCurrent reports whether the live schema exists and was written
// with this binary's Layout.
func liveLayoutCurrent(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var comment *string
	err := pool.QueryRow(ctx, `SELECT obj_description(oid, 'pg_namespace') FROM pg_catalog.pg_namespace WHERE nspname = $1`,
		LiveSchema).Scan(&comment)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return comment != nil && *comment == layoutComment(Layout), nil
}
