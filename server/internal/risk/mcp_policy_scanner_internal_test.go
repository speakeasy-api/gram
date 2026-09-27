package risk

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestArgumentsWithToolsetID_NullPayload(t *testing.T) {
	t.Parallel()

	result, err := argumentsWithToolsetID("null", "toolset-test")
	require.NoError(t, err)
	require.JSONEq(t, `{"`+shadowmcp.XGramToolsetIDField+`":"toolset-test"}`, result)
}
