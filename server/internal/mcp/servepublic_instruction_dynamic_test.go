package mcp_test

import (
	"encoding/json"
	"net/http"
	"testing"

	pgvector_go "github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	rag_repo "github.com/speakeasy-api/gram/server/internal/rag/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// makeExecuteToolCallBody builds a dynamic-mode execute_tool call that wraps
// a call to innerToolName, mirroring what an agent sends after resolving a
// tool via search_tools/describe_tools.
func makeExecuteToolCallBody(innerToolName string) []byte {
	bs, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "execute_tool",
			"arguments": map[string]any{
				"name":      innerToolName,
				"arguments": map[string]any{},
			},
		},
	})
	return bs
}

// TestServePublic_InstructionGateAppliesToDynamicExecuteToolCall verifies the
// read-before-use gate applies to the unwrapped inner tool name when a call
// arrives through the dynamic-mode execute_tool wrapper, and that a direct
// call to the instructions tool itself is never gated.
func TestServePublic_InstructionGateAppliesToDynamicExecuteToolCall(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	instructions := "Read the server usage guide before doing anything else."
	mcpSlug := createInstructionToolset(t, ctx, ti, "instr-gate-dynamic", instructions, "required")

	// A fresh session's first call is execute_tool wrapping a real tool name.
	// If the gate only matched on the literal RPC method name "execute_tool"
	// (rather than the unwrapped inner tool), this would incorrectly skip
	// the gate and this assertion would fail.
	wrapped := makeExecuteToolCallBody("some_other_tool")
	w, err := servePublicHTTP(t, ctx, ti, mcpSlug, wrapped, "", map[string]string{
		"Gram-Mode":      "dynamic",
		"Mcp-Session-Id": "dyn-gate-session-1",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	require.Contains(t, w.Body.String(), instructions, "gated call must carry the instructions text")
	require.Contains(t, w.Body.String(), "retry your original call", "gated call must not have executed the wrapped tool")

	// A direct call to "instructions" (not routed through execute_tool) is
	// its own special case and is never gated, even in a brand-new dynamic
	// session that has not read anything yet.
	direct := makeToolsCallBody("instructions")
	w2, err := servePublicHTTP(t, ctx, ti, mcpSlug, direct, "", map[string]string{
		"Gram-Mode":      "dynamic",
		"Mcp-Session-Id": "dyn-gate-session-2",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w2.Code, "body: %s", w2.Body.String())
	require.Contains(t, w2.Body.String(), instructions)
	require.NotContains(t, w2.Body.String(), "retry your original call")
}

// TestServePublic_InstructionToolListedInDynamicMode verifies the synthetic
// instructions tool is listed alongside search_tools/describe_tools/execute_tool
// when a client requests dynamic mode. The toolset's tools are marked as
// already indexed by writing a tool-embeddings row directly, bypassing the
// real Temporal + embeddings-API indexing pipeline that this test suite does
// not stand up a worker for.
func TestServePublic_InstructionToolListedInDynamicMode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := createInstructionToolset(t, ctx, ti, "instr-list-dynamic", "Read this first.", "required")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsets_repo.New(ti.conn).GetToolset(ctx, toolsets_repo.GetToolsetParams{
		Slug:      mcpSlug,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	// A toolset with no explicit toolset_version row (as created here)
	// resolves to version 0 in mv.DescribeToolset, so the seeded embedding
	// row must match that to be picked up by ToolsetToolsAreIndexed.
	_, err = rag_repo.New(ti.conn).InsertToolsetEmbedding(ctx, rag_repo.InsertToolsetEmbeddingParams{
		ProjectID:      *authCtx.ProjectID,
		ToolsetID:      toolset.ID,
		ToolsetVersion: 0,
		EntryKey:       "tools:seed",
		EmbeddingModel: "test-embedding-model",
		Embedding1536:  pgvector_go.NewVector(make([]float32, 1536)),
		Payload:        []byte(`{}`),
		Tags:           []string{},
	})
	require.NoError(t, err)

	w, err := servePublicHTTP(t, ctx, ti, mcpSlug, makeToolsListBody(), "", map[string]string{
		"Gram-Mode": "dynamic",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var response struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response), "response body: %s", w.Body.String())

	names := make([]string, 0, len(response.Result.Tools))
	for _, tool := range response.Result.Tools {
		names = append(names, tool.Name)
	}
	require.Equal(t, []string{"instructions", "search_tools", "describe_tools", "execute_tool"}, names)
}

// TestServePublic_InstructionsToolCallNotFoundWhenDisabled verifies that in
// "disabled" mode, calling the instructions tool by name resolves as any
// other unknown tool would: a not-found error, never the instructions text.
func TestServePublic_InstructionsToolCallNotFoundWhenDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	instructions := "Some instructions that must never be returned."
	mcpSlug := createInstructionToolset(t, ctx, ti, "instr-call-disabled", instructions, "disabled")

	body := callTool(t, ctx, ti, mcpSlug, "instructions", "")
	require.NotContains(t, body, instructions)

	var resp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &resp), "body: %s", body)
	require.NotNil(t, resp.Error, "expected a JSON-RPC error, body: %s", body)
	require.Contains(t, resp.Error.Message, "not found")
}
