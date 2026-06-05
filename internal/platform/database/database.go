// Package database owns the pgx connection pool and the transaction helper that
// lets a write and its audit record commit atomically.
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Tx is the transaction handle passed to module stores. Aliased so modules depend
// on the platform layer, not on pgx directly.
type Tx = pgx.Tx

// DB wraps the pgx pool and exposes the transaction runner.
type DB struct {
	Pool *pgxpool.Pool
}

// NewPool opens a connection pool against url.
func NewPool(ctx context.Context, url string) (*DB, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// Close releases the pool.
func (d *DB) Close() { d.Pool.Close() }

// WithinTx runs fn inside a single transaction, committing on success and rolling
// back on error or panic.
func (d *DB) WithinTx(ctx context.Context, fn func(tx Tx) error) (err error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
