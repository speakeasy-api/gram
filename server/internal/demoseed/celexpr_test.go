package demoseed

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
)

// CEL predicates in the seed (custom rule detection_expr, per-category
// detection scope include/exempt) are
// opaque strings the seed inserts past the API's validation, so a typo yields a
// policy that silently never matches. Auto-discovers SQL literals that open
// with a celenv root variable followed by a dot or a space, in Go raw strings
// and SQL literals; CEL string literals are double-quoted, so no expression
// contains a backquote or an apostrophe.
var seedCELExpr = regexp.MustCompile("[`']((?:tool_calls|content|prompt|assistant|tool_result|kind)[.\\s][^`']*)[`']")

func TestSeedCELCompiles(t *testing.T) {
	t.Parallel()

	eng, err := celenv.New()
	require.NoError(t, err)

	var matches [][]string
	for _, path := range seedSourceFiles(t) {
		matches = append(matches, seedCELExpr.FindAllStringSubmatch(readFile(t, path), -1)...)
	}
	require.Len(t, matches, 11, "CEL literal count changed; add the new expression here or fix seedCELExpr so it is discovered")

	for _, m := range matches {
		_, err := eng.Compile(m[1])
		require.NoErrorf(t, err, "seed CEL %q", m[1])
	}
}
