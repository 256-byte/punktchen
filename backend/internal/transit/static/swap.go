package static

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// LiveSchema is what queries read (sqlc queries use "gtfs.*").
	LiveSchema = "gtfs"
	oldSchema  = "gtfs_old"
	// swapLockTimeout keeps the swap from queueing behind a stuck query.
	swapLockTimeout = "10s"
	// dropLockTimeout bounds waiting for readers of the old schema; if it
	// expires, the next swap drops the leftover.
	dropLockTimeout = "60s"
)

// swap makes the staging schema live (ADR-0004): in one transaction rename
// gtfs → gtfs_old and gtfs_staging → gtfs, then drop gtfs_old.
//
// Readers never see a missing or half-filled schema: a statement resolves
// "gtfs.<table>" either before the commit (old tables) or after it (new
// tables). Statements still running on the old tables keep their locks, so
// the DROP waits for them instead of breaking them.
func swap(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	start := time.Now()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var hasStaging, hasLive bool
	if err := tx.QueryRow(ctx, `SELECT
			EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1),
			EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $2)`, StagingSchema, LiveSchema).Scan(&hasStaging, &hasLive); err != nil {
		return err
	}
	if !hasStaging {
		return fmt.Errorf("swap: schema %s does not exist", StagingSchema)
	}
	stmts := []string{
		`SET LOCAL lock_timeout = '` + swapLockTimeout + `'`,
		`DROP SCHEMA IF EXISTS ` + oldSchema + ` CASCADE`, // leftover of an earlier swap
	}
	if hasLive {
		stmts = append(stmts, `ALTER SCHEMA `+LiveSchema+` RENAME TO `+oldSchema)
	}
	stmts = append(stmts, `ALTER SCHEMA `+StagingSchema+` RENAME TO `+LiveSchema)
	for _, s := range stmts {
		if _, err := tx.Exec(ctx, s); err != nil {
			return fmt.Errorf("swap: %s: %w", s, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("swap: commit: %w", err)
	}
	log.Info("gtfs schema swapped", "duration", time.Since(start).Round(time.Millisecond))

	if hasLive {
		dropOld(ctx, pool, log)
	}
	return nil
}

// dropOld removes the previous schedule. Failure is not an error of the
// import: the new data is already live, and the next swap retries.
func dropOld(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
	start := time.Now()
	tx, err := pool.Begin(ctx)
	if err == nil {
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout = '`+dropLockTimeout+`'`); err == nil {
			if _, err = tx.Exec(ctx, `DROP SCHEMA IF EXISTS `+oldSchema+` CASCADE`); err == nil {
				err = tx.Commit(ctx)
			}
		}
	}
	if err != nil {
		log.Warn("drop old gtfs schema failed; next import retries", "error", err.Error())
		return
	}
	log.Info("old gtfs schema dropped", "duration", time.Since(start).Round(time.Millisecond))
}
