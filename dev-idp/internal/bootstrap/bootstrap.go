// Package bootstrap opens dev-idp's SQLite database and applies the
// embedded schema on every start. The schema is fully idempotent
// (CREATE TABLE / CREATE INDEX IF NOT EXISTS), so re-applying is a no-op
// once the tables exist. Columns added to a table after it first shipped are
// reconciled separately, because that idempotency also means the table is never
// revisited.
package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/dev-idp/internal/database"
)

// Open returns a *sql.DB ready for use. For in-memory mode, the caller
// must keep MaxOpenConns at 1 because sqlite ":memory:" is per-connection.
func Open(ctx context.Context, cfg config.DB) (*sql.DB, error) {
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

	if _, err := db.ExecContext(ctx, database.Schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	if err := addColumns(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

// addedColumns are columns added to the schema after it first shipped. The
// schema's CREATE TABLE IF NOT EXISTS is a no-op against a database that
// already has the table, so a developer's existing dev-idp file would never
// grow them. SQLite has no ADD COLUMN IF NOT EXISTS, so each is applied and a
// duplicate-column complaint is taken as "already there".
//
// Only nullable columns with no default belong here: SQLite can add those to a
// populated table without rewriting it.
var addedColumns = []string{
	"ALTER TABLE organizations ADD COLUMN external_id TEXT",
}

func addColumns(ctx context.Context, db *sql.DB) error {
	for _, stmt := range addedColumns {
		_, err := db.ExecContext(ctx, stmt)
		if err == nil || strings.Contains(err.Error(), "duplicate column name") {
			continue
		}
		return fmt.Errorf("apply %q: %w", stmt, err)
	}

	return nil
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
