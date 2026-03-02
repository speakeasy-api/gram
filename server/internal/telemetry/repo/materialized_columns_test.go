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
	schema, err := os.ReadFile("../../../clickhouse/schema.sql")
	require.NoError(t, err)

	// Matches: col_name <Type> MATERIALIZED toString(attributes.<path>)
	// Excludes resource_attributes.* — different namespace, not routable via AttributeFilter.
	re := regexp.MustCompile(`(\w+)\s+\w+\s+MATERIALIZED\s+toString\(attributes\.([^)]+)\)`)

	for _, m := range re.FindAllSubmatch(schema, -1) {
		colName, path := string(m[1]), string(m[2])
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
}
