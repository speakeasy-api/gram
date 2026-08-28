package platformmcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestOperationBudgetToolResultMapsRegistrationInputErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		code string
	}{
		{
			name: "invalid registration input",
			err:  fmt.Errorf("begin catalog registration receipt: %w", ErrRegistrationInvalid),
			code: "invalid_request",
		},
		{
			name: "registration not found",
			err:  ErrReadinessRegistrationNotFound,
			code: "registration_not_found",
		},
		{
			name: "catalog unavailable",
			err:  ErrCatalogUnavailable,
			code: unavailableCode,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, ok := operationBudgetToolResult(test.err)

			require.True(t, ok)
			require.True(t, result.IsError)
			require.Len(t, result.Content, 1)
			text, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			var body operationBudgetResult
			require.NoError(t, json.Unmarshal([]byte(text.Text), &body))
			require.Equal(t, test.code, body.Code)
			if errors.Is(test.err, ErrCatalogUnavailable) {
				require.Equal(t, "catalog_unavailable", body.Reason)
				require.Contains(t, body.Message, "catalogue of MCP servers is temporarily unavailable")
			}
		})
	}
}
