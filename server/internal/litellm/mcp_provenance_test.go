package litellm

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestMCPToolCallProvenanceFrom(t *testing.T) {
	t.Parallel()
	calls := []any{
		map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "mcp__GitHub__create_issue", "arguments": "{}"}},
		map[string]any{"id": "call_2", "type": "function", "function": map[string]any{"name": "bash"}},
		map[string]any{"type": "function", "function": map[string]any{"name": "mcp__github__x"}}, // no id
		map[string]any{"id": "call_3", "type": "function", "function": map[string]any{"name": "MCP:search"}},
		map[string]any{"id": "call_4", "function": map[string]any{"name": "mcp__linear__list"}}, // chunk form: no arguments
		"not-an-object",
	}
	got := mcpToolCallProvenanceFrom(calls)
	require.Equal(t, []mcpToolCallProvenance{
		{ToolCallID: "call_1", ToolName: "mcp__GitHub__create_issue", Server: "github", Identity: "mcp-tool://github"},
		{ToolCallID: "call_4", ToolName: "mcp__linear__list", Server: "linear", Identity: "mcp-tool://linear"},
	}, got)
}

func TestMCPToolCallProvenanceFromEmpty(t *testing.T) {
	t.Parallel()
	require.Nil(t, mcpToolCallProvenanceFrom(nil))
	require.Nil(t, mcpToolCallProvenanceFrom([]any{
		map[string]any{"id": "call_1", "function": map[string]any{"name": "bash"}},
	}))
}

func TestIngestResponseRecordsMCPToolCallProvenance(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	userID := "user_" + uuid.NewString()
	storedEmail := "Prov." + uuid.NewString() + "@Example.Test"
	_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       storedEmail,
		DisplayName: "Prov Member",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)
	_, err = organizationsrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, organizationsrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)

	callID := "prov-call-" + uuid.NewString()
	toolCallID := "toolu_" + uuid.NewString()
	payload := testPayload()
	payload.InputType = "response"
	payload.LitellmCallID = &callID
	payload.Texts = []string{"assistant reply"}
	payload.RequestData.UserAPIKeyUserEmail = new(storedEmail)
	payload.ToolCalls = []any{
		map[string]any{
			"id":   toolCallID,
			"type": "function",
			"function": map[string]any{
				"name":      "mcp__GitHub__create_issue",
				"arguments": "{}",
			},
		},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)

	traceID := hooks.HashToolCallIDToTraceID(toolCallID)
	projectID := authCtx.ProjectID.String()

	var serverURL, mcpMatch, toolSource, hookSource, userEmail, toolName string
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
		queryErr := ti.chConn.QueryRow(ctx,
			`SELECT toString(attributes.gram.mcp.server_url), toString(attributes.gram.mcp.match), tool_source, hook_source, user_email, tool_name
			 FROM telemetry_logs WHERE gram_project_id = ? AND trace_id = ?`,
			projectID, traceID,
		).Scan(&serverURL, &mcpMatch, &toolSource, &hookSource, &userEmail, &toolName)
		assert.NoError(collect, queryErr)
		assert.Equal(collect, "mcp-tool://github", serverURL)
	}, 10*time.Second, 50*time.Millisecond)
	require.Equal(t, "mcp-tool://github", mcpMatch)
	require.Equal(t, "github", toolSource)
	require.Equal(t, mcpProvenanceHookSource, hookSource)
	require.Equal(t, conv.NormalizeEmail(storedEmail), userEmail)
	require.Equal(t, "mcp__GitHub__create_issue", toolName)

	var summaryServerURL, summaryToolSource, summaryHookSource, summaryUserEmail string
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		queryErr := ti.chConn.QueryRow(ctx,
			`SELECT max(mcp_server_url), max(tool_source), max(hook_source), max(user_email)
			 FROM trace_summaries WHERE gram_project_id = ? AND trace_id = ?`,
			projectID, traceID,
		).Scan(&summaryServerURL, &summaryToolSource, &summaryHookSource, &summaryUserEmail)
		assert.NoError(collect, queryErr)
		assert.Equal(collect, "mcp-tool://github", summaryServerURL)
	}, 10*time.Second, 50*time.Millisecond)
	require.Equal(t, "github", summaryToolSource)
	require.Equal(t, mcpProvenanceHookSource, summaryHookSource)
	require.Equal(t, conv.NormalizeEmail(storedEmail), summaryUserEmail)
}
