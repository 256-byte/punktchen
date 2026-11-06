package platform

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// Migration bookkeeping lives in meta.schema_migrations (DATA_MODEL §3.3).
const (
	migrationsSchema = "meta"
	migrationsTable  = "schema_migrations"
)

// Migrate applies all pending migrations found in dir of fsys.
// Running it again without new files is a no-op, so it is safe on every start.
// golang-migrate holds a Postgres advisory lock while it runs, so concurrent
// starts do not apply the same migration twice.
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, dir string, log *slog.Logger) error {
	m, err := newMigrator(ctx, pool, fsys, dir, log)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()

	before, dirty, err := currentVersion(m)
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("database is dirty at migration %d: an earlier run failed halfway; "+
			"check the schema and fix the version in %s.%s by hand", before, migrationsSchema, migrationsTable)
	}

	// Stop between migrations (never inside one) when the process shuts down.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			m.GracefulStop <- true
		case <-done:
		}
	}()

	switch err := m.Up(); {
	case errors.Is(err, migrate.ErrNoChange):
		log.Info("database schema up to date", "version", before)
		return nil
	case err != nil:
		return fmt.Errorf("apply migrations: %w", err)
	}

	after, _, err := currentVersion(m)
	if err != nil {
		return err
	}
	log.Info("database schema migrated", "from_version", before, "to_version", after)
	return nil
}

// newMigrator builds a golang-migrate instance on top of the app's pool.
func newMigrator(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, dir string, log *slog.Logger) (*migrate.Migrate, error) {
	// The driver does not create the schema of its own version table.
	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+migrationsSchema); err != nil {
		return nil, fmt.Errorf("create schema %s: %w", migrationsSchema, err)
	}

	src, err := iofs.New(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	// Closing this *sql.DB does not close the pool.
	drv, err := pgxmigrate.WithInstance(stdlib.OpenDBFromPool(pool), &pgxmigrate.Config{
		SchemaName:      migrationsSchema,
		MigrationsTable: migrationsTable,
	})
	if err != nil {
		_ = src.Close()
		return nil, fmt.Errorf("init migration driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "pgx5", drv)
	if err != nil {
		_ = src.Close()
		_ = drv.Close()
		return nil, fmt.Errorf("init migrations: %w", err)
	}
	m.Log = migrateLogger{log}
	return m, nil
}

// currentVersion returns 0 when no migration has been applied yet.
func currentVersion(m *migrate.Migrate) (uint, bool, error) {
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read migration version: %w", err)
	}
	return v, dirty, nil
}

// migrateLogger adapts slog to golang-migrate's logger interface.
type migrateLogger struct{ log *slog.Logger }

func (l migrateLogger) Printf(format string, v ...any) {
	l.log.Info(strings.TrimSpace(fmt.Sprintf(format, v...)))
}

func (l migrateLogger) Verbose() bool { return false }
