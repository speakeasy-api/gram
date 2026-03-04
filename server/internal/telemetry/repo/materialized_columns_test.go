package repo

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMaterializedColumnsMatchSchema guards against adding a MATERIALIZED
// toString(attributes.*) column to clickhouse/schema.sql without regenerating
// materialized_columns_gen.go. Run `mise run clickhouse:gen-materialized-cols`
// to fix failures.
func TestMaterializedColumnsMatchSchema(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile("../../../clickhouse/schema.sql")
	require.NoError(t, err)

	// Scope to the telemetry_logs CREATE TABLE block to match the generator.
	tableRe := regexp.MustCompile(`(?s)CREATE TABLE IF NOT EXISTS telemetry_logs\s*\((.+?)\)\s*ENGINE\s*=`)
	tableMatch := tableRe.FindSubmatch(schema)
	require.NotNil(t, tableMatch, "telemetry_logs table not found in clickhouse/schema.sql")
	block := tableMatch[1]

	// Matches: col_name <Type> MATERIALIZED toString(attributes.<path>)
	// Excludes resource_attributes.* — different namespace, not routable via AttributeFilter.
	re := regexp.MustCompile(`(\w+)\s+\w+\s+MATERIALIZED\s+toString\(attributes\.([^)]+)\)`)

	// schema → map: every materialized column in the schema must be in the map.
	schemaEntries := make(map[string]string)
	for _, m := range re.FindAllSubmatch(block, -1) {
		colName, path := string(m[1]), string(m[2])
		schemaEntries[path] = colName
		got, ok := materializedColumns[path]
		require.Truef(t, ok,
			"attributes.%s is materialized as column %q but is missing from materializedColumns; run `mise run clickhouse:gen-materialized-cols`",
			path, colName,
		)
		require.Equalf(t, colName, got,
			"materializedColumns[%q] = %q, want %q; run `mise run clickhouse:gen-materialized-cols`",
			path, got, colName,
		)
	}

	// map → schema: every entry in the map must still exist in the schema.
	for path := range materializedColumns {
		_, ok := schemaEntries[path]
		require.Truef(t, ok,
			"materializedColumns contains %q but it is not in schema.sql; run `mise run clickhouse:gen-materialized-cols`",
			path,
		)
	}
}
