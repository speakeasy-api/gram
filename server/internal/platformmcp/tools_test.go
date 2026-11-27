package platformmcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestOperationBudgetToolResultAddsOnlyTypedSetupDiagnostics(t *testing.T) {
	t.Parallel()

	categorized, ok := operationBudgetToolResult(setupFailure(SetupCategoryUnsafeTargetOrRedirect, ErrDirectRemoteRejected))
	require.True(t, ok)
	categorizedText, ok := categorized.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var categorizedBody operationBudgetResult
	require.NoError(t, json.Unmarshal([]byte(categorizedText.Text), &categorizedBody))
	require.Equal(t, SetupCategoryUnsafeTargetOrRedirect, categorizedBody.SetupCategory)
	require.Equal(t, []RepairAction{{Kind: "review_remote_url", Label: "Use a public HTTPS MCP endpoint without credentials in the URL"}}, categorizedBody.Actions)
	require.NotContains(t, categorizedText.Text, "blocked IP")

	legacy, ok := operationBudgetToolResult(ErrDirectRemoteUnavailable)
	require.True(t, ok)
	legacyText, ok := legacy.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.JSONEq(t, `{"code":"feature_unavailable","reason":"remote_inspection_unavailable","message":"That MCP server could not be checked safely right now. Try again shortly."}`, legacyText.Text)
}

func TestOperationBudgetToolResultMapsIneligibleProject(t *testing.T) {
	t.Parallel()

	result, ok := operationBudgetToolResult(ErrTargetIneligible)
	require.True(t, ok)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.JSONEq(t, `{"code":"ineligible_project","message":"That project is not available for MCP setup. Check the project slug and try again."}`, text.Text)
}

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

func TestOperationBudgetToolResultMapsDiagnosticsInputErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		code     string
		contains string
	}{
		{
			name:     "gateway id passed as mcp_id",
			err:      fmt.Errorf("resolve drilldown mcp: parse mcp id: %w: %w", ErrMCPIDInvalid, errors.New("invalid UUID length: 44")),
			code:     "invalid_request",
			contains: "A gateway is not an MCP server",
		},
		{
			name:     "window longer than the tool allows",
			err:      fmt.Errorf("%w: 7d allows at most 24h", ErrDiagnosticWindowTooLong),
			code:     "invalid_request",
			contains: "query_mcp_metrics at most 7d",
		},
		{
			name:     "unknown window",
			err:      ErrDiagnosticWindowInvalid,
			code:     "invalid_request",
			contains: "window field",
		},
		{
			name:     "mcp not in project",
			err:      ErrDiagnosticsTargetNotFound,
			code:     "mcp_not_found",
			contains: "find_mcp",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, ok := operationBudgetToolResult(test.err)
			require.True(t, ok)
			require.True(t, result.IsError)
			text, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			var body operationBudgetResult
			require.NoError(t, json.Unmarshal([]byte(text.Text), &body))
			require.Equal(t, test.code, body.Code)
			require.Contains(t, body.Message, test.contains)
			require.NotContains(t, text.Text, "UUID length")
		})
	}
}
