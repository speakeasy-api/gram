// Package bootstrap opens dev-idp's SQLite database and applies the embedded
// schema on every start.
//
// The schema is idempotent (CREATE TABLE / CREATE INDEX IF NOT EXISTS), which
// creates anything missing but never alters an existing table. Destructive
// drift repair is an explicit operation performed by git:worksync.
package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/dev-idp/internal/database"
)

// schemaSQL is indirected so reconcile.go can derive the expected shape.
func schemaSQL() string { return database.Schema }

// Open returns a *sql.DB ready for use. For in-memory mode, the caller
// must keep MaxOpenConns at 1 because sqlite ":memory:" is per-connection.
func Open(ctx context.Context, cfg config.DB) (*sql.DB, error) {
	db, err := openSQLite(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if _, err := db.ExecContext(ctx, database.Schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return db, nil
}

func openSQLite(ctx context.Context, cfg config.DB) (*sql.DB, error) {
	dsn, err := buildDSN(cfg)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite (%s): %w", dsn, err)
	}

	if cfg.Mode == config.DBModeMemory {
		db.SetMaxOpenConns(1)
	}

	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	}
	if cfg.Mode == config.DBModeFile {
		pragmas = append(pragmas, "PRAGMA journal_mode = WAL")
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("apply %s: %w", p, err)
		}
	}

	return db, nil
}

func buildDSN(cfg config.DB) (string, error) {
	switch cfg.Mode {
	case config.DBModeMemory:
		return ":memory:", nil
	case config.DBModeFile:
		if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o755); err != nil {
			return "", fmt.Errorf("create db parent dir: %w", err)
		}
		return cfg.Path, nil
	default:
		return "", fmt.Errorf("unknown db mode %v", cfg.Mode)
	}
}
