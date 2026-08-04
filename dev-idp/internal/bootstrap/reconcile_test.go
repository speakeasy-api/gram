package bootstrap_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
)

// writeLegacyDB creates a database at path containing exactly the DDL given,
// standing in for one left behind by an older schema.
func writeLegacyDB(t *testing.T, path string, ddl string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(t.Context(), ddl)
	require.NoError(t, err, "seed legacy schema")
}

func columnNames(t *testing.T, ctx context.Context, db *sql.DB, tableName string) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, tableName)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		out = append(out, n)
	}
	require.NoError(t, rows.Err())
	return out
}

// A database carrying the retired `mode` column is the exact shape that
// stranded local checkouts: CREATE ... IF NOT EXISTS left it in place, and the
// first insert then failed its NOT NULL constraint on a column the code no
// longer sets.
func TestOpen_DropsRetiredColumn(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "devidp.db")
	writeLegacyDB(t, path, `
		CREATE TABLE auth_codes (
		  code TEXT NOT NULL PRIMARY KEY,
		  mode TEXT NOT NULL,
		  user_id TEXT NOT NULL,
		  client_id TEXT NOT NULL,
		  redirect_uri TEXT NOT NULL,
		  code_challenge TEXT,
		  code_challenge_method TEXT,
		  scope TEXT,
		  expires_at DATETIME NOT NULL,
		  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)

	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeFile, Path: path}, testLogger())
	require.NoError(t, err, "Open must upgrade a stale database rather than fail")
	t.Cleanup(func() { _ = db.Close() })

	require.NotContains(t, columnNames(t, t.Context(), db, "auth_codes"), "mode",
		"retired column should be gone")

	// The insert the new code actually issues -- omitting `mode` -- must work.
	_, err = db.ExecContext(t.Context(),
		`INSERT INTO users (id, email, display_name) VALUES ('u1', 'a@b', 'A')`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(),
		`INSERT INTO auth_codes (code, user_id, client_id, redirect_uri, expires_at)
		 VALUES ('c1', 'u1', 'client', 'http://localhost/cb', datetime('now', '+5 minutes'))`)
	require.NoError(t, err, "insert without the retired column must succeed")
}

// A retired index has to be dropped before its column can be, or SQLite
// refuses the ALTER.
func TestOpen_DropsRetiredIndexBlockingColumnDrop(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "devidp.db")
	writeLegacyDB(t, path, `
		CREATE TABLE oauth_clients (
		  client_id TEXT NOT NULL PRIMARY KEY,
		  mode TEXT NOT NULL,
		  client_secret TEXT NOT NULL,
		  redirect_uris TEXT NOT NULL DEFAULT '[]',
		  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX oauth_clients_mode_idx ON oauth_clients (mode);
	`)

	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeFile, Path: path}, testLogger())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cols := columnNames(t, t.Context(), db, "oauth_clients")
	require.NotContains(t, cols, "mode")
	require.Contains(t, cols, "rotate_refresh_tokens", "new column should have been added")

	var n int
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='oauth_clients_mode_idx'`).Scan(&n))
	require.Zero(t, n, "retired index should be gone")
}

// Curated rows (users, orgs, memberships people set up by hand) must survive
// an upgrade -- otherwise this is just a nuke with extra steps.
func TestOpen_PreservesRowsAcrossUpgrade(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "devidp.db")
	writeLegacyDB(t, path, `
		CREATE TABLE users (
		  id TEXT NOT NULL PRIMARY KEY,
		  email TEXT NOT NULL,
		  display_name TEXT NOT NULL,
		  photo_url TEXT,
		  github_handle TEXT,
		  admin INTEGER NOT NULL DEFAULT 0,
		  whitelisted INTEGER NOT NULL DEFAULT 1,
		  retired_extra TEXT,
		  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO users (id, email, display_name) VALUES ('u1', 'kept@example.com', 'Kept User');
	`)

	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeFile, Path: path}, testLogger())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var email string
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT email FROM users WHERE id = 'u1'`).Scan(&email))
	require.Equal(t, "kept@example.com", email, "existing rows must survive the upgrade")
	require.NotContains(t, columnNames(t, t.Context(), db, "users"), "retired_extra")
}

// A change SQLite cannot make in place should say so plainly and name the
// column, rather than surfacing later as a constraint violation.
func TestOpen_UnalterableDriftReportsActionably(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "devidp.db")
	writeLegacyDB(t, path, `
		CREATE TABLE users (
		  id TEXT NOT NULL PRIMARY KEY,
		  email TEXT NOT NULL,
		  display_name TEXT,
		  photo_url TEXT,
		  github_handle TEXT,
		  admin INTEGER NOT NULL DEFAULT 0,
		  whitelisted INTEGER NOT NULL DEFAULT 1,
		  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)

	_, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeFile, Path: path}, testLogger())
	require.Error(t, err, "in-place-impossible drift must fail loudly at boot")
	require.Contains(t, err.Error(), "users.display_name", "error should name the offending column")
	require.Contains(t, strings.ToLower(err.Error()), "local/devidp", "error should say how to recover")
}

// Reconciling must be idempotent: a database already at the current schema
// should come up unchanged, however many times it is opened.
func TestOpen_CurrentSchemaIsStable(t *testing.T) {
	t.Parallel()

	cfg := config.DB{Mode: config.DBModeFile, Path: filepath.Join(t.TempDir(), "devidp.db")}

	first, err := bootstrap.Open(t.Context(), cfg, testLogger())
	require.NoError(t, err)
	before := columnNames(t, t.Context(), first, "oauth_clients")
	require.NoError(t, first.Close())

	for range 2 {
		db, err := bootstrap.Open(t.Context(), cfg, testLogger())
		require.NoError(t, err)
		require.Equal(t, before, columnNames(t, t.Context(), db, "oauth_clients"))
		require.NoError(t, db.Close())
	}
}

// A retired column that SQLite refuses to drop -- because it is the primary
// key, or unique -- has to report the same actionable message as any other
// in-place-impossible change, not the driver's own wording. Renaming the key
// of a ledger table is a realistic change, and the raw error ("cannot drop
// PRIMARY KEY column") tells an operator nothing about what to do.
func TestOpen_UndroppableColumnReportsActionably(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "devidp.db")
	writeLegacyDB(t, path, `
		CREATE TABLE ema_resource_tokens (
		  token TEXT NOT NULL PRIMARY KEY,
		  resource_id TEXT NOT NULL,
		  user_id TEXT NOT NULL,
		  client_id TEXT NOT NULL,
		  audience TEXT NOT NULL,
		  scope TEXT NOT NULL DEFAULT '',
		  expires_at DATETIME NOT NULL,
		  revoked_at DATETIME,
		  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)

	_, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeFile, Path: path}, testLogger())
	require.Error(t, err, "an undroppable retired column must fail loudly at boot")
	require.Contains(t, err.Error(), "ema_resource_tokens.token", "error should name the offending column")
	require.Contains(t, strings.ToLower(err.Error()), "local/devidp", "error should say how to recover")
}
