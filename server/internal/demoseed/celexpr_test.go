package demoseed

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
)

// CEL predicates in the seed (custom rule detection_expr, policy scope_*) are
// opaque strings the seed inserts past the API's validation, so a typo yields a
// policy that silently never matches. Auto-discovers SQL literals that open
// with a celenv root variable followed by a dot or a space; CEL string literals
// are double-quoted, so no expression contains an apostrophe.
var seedCELExpr = regexp.MustCompile(`'((?:tool_calls|content|prompt|assistant|tool_result|kind)[.\s][^']*)'`)

func TestSeedCELCompiles(t *testing.T) {
	t.Parallel()

	eng, err := celenv.New()
	require.NoError(t, err)

	matches := seedCELExpr.FindAllStringSubmatch(postgresSQL, -1)
	require.GreaterOrEqual(t, len(matches), 4, "postgres.sql lost its CEL literals; update seedCELExpr")

	for _, m := range matches {
		_, err := eng.Compile(m[1])
		require.NoErrorf(t, err, "seed CEL %q", m[1])
	}
}
