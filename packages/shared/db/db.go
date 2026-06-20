// Package db provides Postgres connection and migration helpers shared by services.
package db

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DSN builds a Postgres connection string for the given database name from the
// standard POSTGRES_* environment variables.
func DSN(database string) string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		env("POSTGRES_USER", "vid"),
		env("POSTGRES_PASSWORD", "vidpass"),
		env("POSTGRES_HOST", "localhost"),
		env("POSTGRES_PORT", "5432"),
		database,
	)
}

// Connect opens a pgx pool, retrying until Postgres is ready or the context is done.
func Connect(ctx context.Context, database string) (*pgxpool.Pool, error) {
	dsn := DSN(database)
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		pool, err := pgxpool.New(ctx, dsn)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return pool, nil
			}
			pool.Close()
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, fmt.Errorf("could not connect to %s: %w", database, lastErr)
}

// Migrate runs idempotent schema SQL (e.g. embedded schema.sql) against the pool.
func Migrate(ctx context.Context, pool *pgxpool.Pool, schemaSQL string) error {
	_, err := pool.Exec(ctx, schemaSQL)
	return err
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
