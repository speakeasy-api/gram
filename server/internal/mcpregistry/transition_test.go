package mcpregistry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// TestExpandMigrationTransition replays the generated registry expand migration, not
// a relaxed final schema. The existing test environment owns and destroys this
// database clone; a private schema keeps unrelated final-schema objects intact.
func TestExpandMigrationTransition(t *testing.T) {
	t.Parallel()
	ctx, _, db := newTestService(t)
	conn, err := db.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := conn.Exec(ctx, sql, args...) //nolint:glint // notestingrawsql: actual migration replay and transition-only fixtures in an isolated disposable schema.
		require.NoError(t, err)
	}
	query := func(sql string, args ...any) pgx.Row {
		t.Helper()
		return conn.QueryRow(ctx, sql, args...) //nolint:glint // notestingrawsql: inspect migration invariants and historical rows in the disposable transition schema.
	}
	migration := func(name string) {
		t.Helper()
		sql, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		require.NoError(t, err)
		// These specific generated DDL files contain no procedural bodies or quoted
		// semicolons. Execute separately: CONCURRENTLY cannot run in a transaction.
		for statement := range strings.SplitSeq(string(sql), ";") {
			if strings.TrimSpace(statement) != "" {
				exec(statement)
			}
		}
	}
	exec("CREATE SCHEMA registry_transition")
	exec("SET search_path TO registry_transition, public")
	migration("20260924212827_mcp_registry_entries.sql")
	migration("20260929165337_registry_publication_metadata.sql")
	exec(`INSERT INTO mcp_registry_entries (id, data, published, published_at, created_at, updated_at) VALUES
 ('00000000-0000-0000-0000-000000000001', '{"server":{"name":"example.test/invalid","version":"historical-invalid"},"extra":"preserve"}', true, '2025-01-01', '2024-01-01', '2025-01-01'),
 ('00000000-0000-0000-0000-000000000002', '{"server":{"name":"example.test/null","version":null}}', false, null, '2024-01-01', '2025-01-01'),
 ('00000000-0000-0000-0000-000000000003', '{"server":{"name":"example.test/missing"}}', true, '2025-01-01', '2024-01-01', '2025-01-01'),
 ('00000000-0000-0000-0000-000000000004', '{"server":{"name":"example.test/existing","version":"1.0.0"}}', true, '2025-01-01', '2024-01-01', '2025-01-01')`)
	var before, after string
	require.NoError(t, query(`SELECT jsonb_agg(to_jsonb(e) ORDER BY id)::text FROM mcp_registry_entries e`).Scan(&before))
	migration("20261116173332_registry_server_identities.sql")
	// An operator-created parent must not acquire a default or become visible.
	exec(`INSERT INTO mcp_registry_servers(id, name, visible) VALUES ('00000000-0000-0000-0000-000000000005', 'example.test/existing', false)`)
	exec(registryBackfillSQL)
	require.NoError(t, query(`SELECT jsonb_agg(to_jsonb(e) - 'server_id' ORDER BY id)::text FROM mcp_registry_entries e`).Scan(&after))
	require.JSONEq(t, before, after)
	var count int
	require.NoError(t, query(`SELECT count(*) FROM mcp_registry_servers`).Scan(&count))
	require.Equal(t, 4, count)
	require.NoError(t, query(`SELECT count(*) FROM mcp_registry_servers s JOIN mcp_registry_entries e ON e.id = s.id WHERE e.server_id = s.id AND s.visible AND s.default_entry_id IS NOT DISTINCT FROM CASE WHEN e.published THEN e.id END`).Scan(&count))
	require.Equal(t, 3, count)
	var visible, noDefault bool
	require.NoError(t, query(`SELECT visible, default_entry_id IS NULL FROM mcp_registry_servers WHERE name = 'example.test/existing'`).Scan(&visible, &noDefault))
	require.False(t, visible)
	require.True(t, noDefault)
	var snapshot string
	require.NoError(t, query(`SELECT jsonb_agg(to_jsonb(s) ORDER BY id)::text FROM mcp_registry_servers s`).Scan(&snapshot))
	exec(registryBackfillSQL)
	require.NoError(t, query(`SELECT jsonb_agg(to_jsonb(s) ORDER BY id)::text FROM mcp_registry_servers s`).Scan(&after))
	require.JSONEq(t, snapshot, after)
	require.NoError(t, query(`SELECT count(*) FROM mcp_registry_entries e LEFT JOIN mcp_registry_servers s ON e.server_id = s.id WHERE s.id IS NULL OR s.name <> e.data #>> '{server,name}'`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, query(`SELECT count(*) FROM mcp_registry_servers s LEFT JOIN mcp_registry_entries e ON e.id = s.default_entry_id WHERE s.default_entry_id IS NOT NULL AND (e.id IS NULL OR e.server_id <> s.id OR NOT e.published)`).Scan(&count))
	require.Zero(t, count)
	exec("SET search_path TO public")
}
