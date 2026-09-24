package bootstrap_test

import (
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/plog"
)

func testLogger() *slog.Logger { return plog.NewLogger(io.Discard) }

func TestOpen_Memory(t *testing.T) {
	t.Parallel()
	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeMemory, Path: ""})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM users").Scan(&n); err != nil {
		t.Fatalf("query users: %v", err)
	}
	if n != 0 {
		t.Fatalf("fresh users count: got %d, want 0", n)
	}
}

func TestOpen_FileIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.DB{Mode: config.DBModeFile, Path: filepath.Join(dir, "devidp.db")}

	db1, err := bootstrap.Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open #1: %v", err)
	}
	if _, err := db1.ExecContext(t.Context(),
		"INSERT INTO users (id, email, display_name) VALUES (?, ?, ?)",
		"abc", "a@b", "A"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	_ = db1.Close()

	db2, err := bootstrap.Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open #2 (re-apply schema): %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })

	var n int
	if err := db2.QueryRowContext(t.Context(), "SELECT count(*) FROM users").Scan(&n); err != nil {
		t.Fatalf("query users: %v", err)
	}
	if n != 1 {
		t.Fatalf("re-opened users count: got %d, want 1 (schema apply must be idempotent)", n)
	}
}

func TestOpen_DoesNotEvolveExistingTables(t *testing.T) {
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
	`)

	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeFile, Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if slices.Contains(columnNames(t, t.Context(), db, "users"), "retired_extra") {
		return
	}
	t.Fatal("Open unexpectedly evolved the retired_extra column")
}

func TestOpen_CascadeForeignKeysHaveLeadingIndexes(t *testing.T) {
	t.Parallel()

	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeMemory, Path: ""})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	tableNames := func() []string {
		tables, err := db.QueryContext(t.Context(),
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
		require.NoError(t, err)
		defer func() { require.NoError(t, tables.Close()) }()

		var names []string
		for tables.Next() {
			var tableName string
			require.NoError(t, tables.Scan(&tableName))
			names = append(names, tableName)
		}
		require.NoError(t, tables.Err())
		return names
	}()

	for _, tableName := range tableNames {
		childColumns := func() []string {
			foreignKeys, err := db.QueryContext(t.Context(),
				`SELECT "from" FROM pragma_foreign_key_list(?) WHERE on_delete = 'CASCADE'`, tableName)
			require.NoError(t, err)
			defer func() { require.NoError(t, foreignKeys.Close()) }()

			var columns []string
			for foreignKeys.Next() {
				var childColumn string
				require.NoError(t, foreignKeys.Scan(&childColumn))
				columns = append(columns, childColumn)
			}
			require.NoError(t, foreignKeys.Err())
			return columns
		}()

		for _, childColumn := range childColumns {
			var found int
			err := db.QueryRowContext(t.Context(), `
				SELECT 1
				FROM pragma_index_list(?) AS indexes
				JOIN pragma_index_info(indexes.name) AS columns ON columns.seqno = 0
				WHERE columns.name = ?
				LIMIT 1
			`, tableName, childColumn).Scan(&found)
			require.NoErrorf(t, err, "%s.%s needs a leading index for ON DELETE CASCADE", tableName, childColumn)
		}
	}
}

func TestParseDB(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"", false},
		{"memory", false},
		{":memory:", false},
		{"file:./x.db", false},
		{"file:/abs/x.db", false},
		{"file:", true},
		{"sqlite://x", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			_, err := config.ParseDB(tt.in)
			gotErr := err != nil
			if gotErr != tt.wantErr {
				t.Fatalf("ParseDB(%q): err=%v, wantErr=%v", tt.in, err, tt.wantErr)
			}
		})
	}
}
