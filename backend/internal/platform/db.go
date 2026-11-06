package platform

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// dbConnectTimeout bounds the initial connection check at startup.
const dbConnectTimeout = 10 * time.Second

// NewPool creates a pgx connection pool and verifies the database is
// reachable, so startup fails fast with a clear error.
// pgx redacts the password in its own parse errors.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	// Shows up in pg_stat_activity.
	cfg.ConnConfig.RuntimeParams["application_name"] = "livestop"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, dbConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database %s:%d/%s: %w",
			cfg.ConnConfig.Host, cfg.ConnConfig.Port, cfg.ConnConfig.Database, err)
	}
	return pool, nil
}
