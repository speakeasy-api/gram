package repo

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
)

func TestTumMeasureExpr_BuiltFromComponentRegistry(t *testing.T) {
	t.Parallel()

	// The measure is derived from billing.TumComponents; this pins the
	// rendered SQL for the current registry so an accidental registry or
	// builder change is visible in review.
	require.Equal(t,
		"toInt64(sumIfMerge(total_input_tokens) + sumIfMerge(total_output_tokens) + sumIfMerge(cache_creation_input_tokens))",
		tumMeasureExpr)

	for _, c := range billing.TumComponents() {
		require.Contains(t, tumMeasureExpr, "sumIfMerge("+c.Column+")",
			"every registry component must be part of the TUM measure")
	}
}
