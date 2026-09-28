package schematest_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dataclassification"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

type column struct {
	table, name, comment string
}

func problems(columns []column, fix string) string {
	var out []string
	for _, c := range columns {
		if _, err := dataclassification.Parse(c.comment); err != nil {
			out = append(out, fmt.Sprintf("  %s.%s: %v", c.table, c.name, err))
		}
	}
	if len(out) == 0 {
		return ""
	}
	sort.Strings(out)
	return fmt.Sprintf("%d column(s) lack a valid data classification.\n%s\n\n%s\nValid classes: %v. See server/internal/dataclassification.",
		len(out), strings.Join(out, "\n"), fix, dataclassification.Classes)
}

// TestPostgresColumnsAreClassified checks the schema built from
// server/database/schema.sql, which CI keeps in sync with the migrations.
func TestPostgresColumnsAreClassified(t *testing.T) {
	t.Parallel()

	pool, err := infra.CloneTestDatabase(t, "dataclassification")
	require.NoError(t, err)

	rows, err := pool.Query(t.Context(), //nolint:glint // notestingrawsql: inspects the system catalog of the whole schema, which no application query should expose.
		`
SELECT c.relname, a.attname, coalesce(pg_catalog.col_description(c.oid, a.attnum), '')
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p', 'm', 'f')
ORDER BY c.relname, a.attnum`)
	require.NoError(t, err)
	defer rows.Close()

	var columns []column
	for rows.Next() {
		var c column
		require.NoError(t, rows.Scan(&c.table, &c.name, &c.comment))
		columns = append(columns, c)
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, columns)

	if msg := problems(columns, "Add a comment ending in an @access line in server/database/schema.sql, e.g.\n  COMMENT ON COLUMN my_table.my_column IS 'What it holds.\n  @access: confidential';\nthen run `mise run db:diff <name>`."); msg != "" {
		t.Fatal(msg)
	}
}

// TestClickHouseColumnsAreClassified checks every stored table built from
// server/clickhouse/schema.sql. Views and materialized views that write TO
// another table have no stored columns of their own.
func TestClickHouseColumnsAreClassified(t *testing.T) {
	t.Parallel()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	rows, err := conn.Query(t.Context(), `
SELECT table, name, comment
FROM system.columns
WHERE database = currentDatabase()
  AND table IN (
    SELECT name FROM system.tables
    WHERE database = currentDatabase() AND engine NOT IN ('View', 'MaterializedView')
  )
ORDER BY table, position`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var columns []column
	for rows.Next() {
		var c column
		require.NoError(t, rows.Scan(&c.table, &c.name, &c.comment))
		columns = append(columns, c)
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, columns)

	if msg := problems(columns, "Give the column a COMMENT ending in an @access line in server/clickhouse/schema.sql, e.g.\n  my_column String COMMENT 'What it holds.\\n@access: confidential',\nthen run `mise run clickhouse:diff <name>`. Do not use semicolons in comments."); msg != "" {
		t.Fatal(msg)
	}
}
