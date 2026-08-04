package risk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The model can only pick a valid category if the registry keys are enumerated
// into the schema — a guessed key returns an empty breakdown, which reads as
// "no findings" rather than as an error.
func TestGetRiskRuleBreakdownSchemaEnumeratesCategories(t *testing.T) {
	t.Parallel()

	schema := string(NewGetRiskRuleBreakdownTool(nil).Descriptor().InputSchema)

	for _, key := range []string{"secrets", "pii", "prompt_injection", "shadow_mcp"} {
		require.Contains(t, schema, `"`+key+`"`, "category %q missing from input schema", key)
	}
}
