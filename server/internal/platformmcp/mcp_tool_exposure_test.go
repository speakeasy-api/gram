package platformmcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestToolInventoryCursorBindsContinuationToPrincipalProjectAndFilters(t *testing.T) {
	t.Parallel()

	codec, err := newToolInventoryCursorCodec("test-cursor-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", ConnectionID: "connection", Generation: "generation"}
	projectID := uuid.New()
	value, err := codec.Encode(toolInventoryCursor{
		OrganizationID: principal.OrganizationID,
		Binding:        principalCursorBinding(principal),
		ProjectID:      projectID.String(),
		Query:          "deploy",
		SourceKind:     "function",
		AfterToolURN:   "tools:function:orders:create_order",
	})
	require.NoError(t, err)

	after, err := codec.Decode(value, principal, projectID, "deploy", "function")
	require.NoError(t, err)
	require.Equal(t, "tools:function:orders:create_order", after)

	_, err = codec.Decode(value, Principal{OrganizationID: principal.OrganizationID, ConnectionID: "connection", Generation: "other"}, projectID, "deploy", "function")
	require.ErrorIs(t, err, ErrToolInventoryCursor)
	_, err = codec.Decode(value, principal, uuid.New(), "deploy", "function")
	require.ErrorIs(t, err, ErrToolInventoryCursor)
	_, err = codec.Decode(value, principal, projectID, "other", "function")
	require.ErrorIs(t, err, ErrToolInventoryCursor)
	_, err = codec.Decode(value, principal, projectID, "deploy", "openapi")
	require.ErrorIs(t, err, ErrToolInventoryCursor)
}

// The version token is what stands between a stale read and a silent
// overwrite, so it must move with the list and with the target, and must not
// move with the order the list happens to come back in.
func TestToolExposureVersionTracksListAndTarget(t *testing.T) {
	t.Parallel()

	projectID, mcpID, toolsetID := uuid.New(), uuid.New(), uuid.New()
	urns := []string{"tools:function:orders:create_order", "tools:http:billing:get_invoice"}
	base := toolExposureVersion(projectID, mcpID, toolsetID, 4, urns)
	require.Len(t, base, 64)

	require.Equal(t, base, toolExposureVersion(projectID, mcpID, toolsetID, 4, []string{urns[1], urns[0]}),
		"the same committed list must yield the same token whatever order it is read in")
	require.NotEqual(t, base, toolExposureVersion(projectID, mcpID, toolsetID, 5, urns))
	require.NotEqual(t, base, toolExposureVersion(projectID, mcpID, toolsetID, 4, append([]string{"tools:function:orders:cancel_order"}, urns...)))
	require.NotEqual(t, base, toolExposureVersion(projectID, mcpID, uuid.New(), 4, urns))
	require.NotEqual(t, base, toolExposureVersion(projectID, uuid.New(), toolsetID, 4, urns))
	require.NotEqual(t, base, toolExposureVersion(uuid.New(), mcpID, toolsetID, 4, urns))
}

func TestToolExposureMutationRefusesUnsafeRequestsBeforeTouchingTheDatabase(t *testing.T) {
	t.Parallel()

	service := &MCPToolExposureService{}
	version := strings.Repeat("a", 64)
	valid := ChangeMCPToolsInput{
		ProjectID: uuid.NewString(), MCPID: uuid.NewString(),
		ToolURNs:        []string{"tools:function:orders:create_order"},
		ExpectedVersion: version, IdempotencyKey: "key", Confirmed: true,
	}

	for name, mutate := range map[string]func(ChangeMCPToolsInput) ChangeMCPToolsInput{
		"no idempotency key": func(in ChangeMCPToolsInput) ChangeMCPToolsInput { in.IdempotencyKey = " "; return in },
		"short version":      func(in ChangeMCPToolsInput) ChangeMCPToolsInput { in.ExpectedVersion = "abc"; return in },
		"bad project":        func(in ChangeMCPToolsInput) ChangeMCPToolsInput { in.ProjectID = "not-a-uuid"; return in },
		"bad mcp":            func(in ChangeMCPToolsInput) ChangeMCPToolsInput { in.MCPID = "not-a-uuid"; return in },
		"no tools":           func(in ChangeMCPToolsInput) ChangeMCPToolsInput { in.ToolURNs = nil; return in },
		"too many tools": func(in ChangeMCPToolsInput) ChangeMCPToolsInput {
			in.ToolURNs = make([]string, maxToolExposureBatch+1)
			for i := range in.ToolURNs {
				in.ToolURNs[i] = "tools:function:orders:create_order"
			}
			return in
		},
	} {
		_, _, _, err := service.validate(mutate(valid))
		require.Error(t, err, "%s must be refused", name)
		var refusal *MCPToolExposureError
		require.ErrorAs(t, err, &refusal)
		require.Equal(t, "invalid_request", refusal.Code, "%s", name)
	}

	// A tool the caller cannot have obtained from the listing is named back
	// rather than dropped from the batch.
	malformed := valid
	malformed.ToolURNs = []string{"tools:function:orders:create_order", "create_order"}
	_, _, _, err := service.validate(malformed)
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, []string{"create_order"}, refusal.UnknownTools)

	// Duplicates collapse instead of being applied twice.
	duplicated := valid
	duplicated.ToolURNs = []string{"tools:function:orders:create_order", " tools:function:orders:create_order "}
	_, _, requested, err := service.validate(duplicated)
	require.NoError(t, err)
	require.Len(t, requested, 1)
}

func TestToolExposureMutationRequiresExplicitConfirmation(t *testing.T) {
	t.Parallel()

	service := &MCPToolExposureService{}
	unconfirmed := ChangeMCPToolsInput{
		ProjectID: uuid.NewString(), MCPID: uuid.NewString(),
		ToolURNs:        []string{"tools:function:orders:create_order"},
		ExpectedVersion: strings.Repeat("a", 64), IdempotencyKey: "key", Confirmed: false,
	}
	_, _, _, err := service.validate(unconfirmed)
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "invalid_request", refusal.Code)
	require.Contains(t, refusal.Message, "Confirm")
}

func TestToolExposureRefusalNamesTheToolsItRefused(t *testing.T) {
	t.Parallel()

	result, ok := toolExposureToolResult(&MCPToolExposureError{
		Code: "invalid_request", Message: "nothing was changed", UnknownTools: []string{"tools:function:orders:create_order"},
	})
	require.True(t, ok)
	require.True(t, result.IsError)
	text, isText := result.Content[0].(*mcp.TextContent)
	require.True(t, isText)
	var refusal toolExposureRefusal
	require.NoError(t, json.Unmarshal([]byte(text.Text), &refusal))
	require.Equal(t, "invalid_request", refusal.Code)
	require.Equal(t, toolExposureFeature, refusal.Feature)
	require.Equal(t, []string{"tools:function:orders:create_order"}, refusal.UnknownTools)

	conflict, ok := toolExposureToolResult(toolExposureConflict())
	require.True(t, ok)
	require.True(t, conflict.IsError)

	missing, ok := toolExposureToolResult(toolExposureMissing())
	require.True(t, ok)
	missingText, isText := missing.Content[0].(*mcp.TextContent)
	require.True(t, isText)
	require.Contains(t, missingText.Text, "dashboard", "a server outside this workflow points the caller somewhere that can do it")
}

// The tool list belongs to the server detail, not to the list of servers: an
// empty list on a find_mcp row would read as "exposes nothing".
func TestFindMCPResultsCarryNoToolExposure(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(MCP{ID: uuid.NewString(), ProjectID: uuid.NewString()})
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "tool_exposure")
}
