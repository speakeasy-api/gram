package celenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The dashboard's insertable CEL example snippets must compile against the
// real engine; a celenv field rename silently broke them once.
func TestDashboardExamplesCompile(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "..", "client", "dashboard", "src", "pages", "security", "cel-examples.json")
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "dashboard cel-examples.json moved? update this test's path")

	var examples struct {
		ScopeInclude []struct {
			Label string `json:"label"`
			Expr  string `json:"expr"`
		} `json:"scope_include"`
		ScopeExempt []struct {
			Label string `json:"label"`
			Expr  string `json:"expr"`
		} `json:"scope_exempt"`
		Detection []struct {
			Label string `json:"label"`
			Expr  string `json:"expr"`
		} `json:"detection"`
	}
	require.NoError(t, json.Unmarshal(raw, &examples))
	require.NotEmpty(t, examples.ScopeInclude)
	require.NotEmpty(t, examples.ScopeExempt)
	require.NotEmpty(t, examples.Detection)

	eng, err := New()
	require.NoError(t, err)

	type entry struct{ group, label, expr string }
	var all []entry
	for _, e := range examples.ScopeInclude {
		all = append(all, entry{"scope_include", e.Label, e.Expr})
	}
	for _, e := range examples.ScopeExempt {
		all = append(all, entry{"scope_exempt", e.Label, e.Expr})
	}
	for _, e := range examples.Detection {
		all = append(all, entry{"detection", e.Label, e.Expr})
	}

	for _, e := range all {
		_, err := eng.Compile(e.expr)
		require.NoErrorf(t, err, "%s example %q", e.group, e.label)
	}
}
