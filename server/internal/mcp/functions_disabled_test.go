package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/functions"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// seedDisabledFunctionsServer publishes a public server whose toolset holds
// one function tool and one function resource, served by the disabled
// Functions orchestrator. It returns the server slug, tool name and resource
// URI.
func seedDisabledFunctionsServer(t *testing.T) (context.Context, *testInstance, string, string, string) {
	t.Helper()

	ctx, ti, _ := newTestMCPServiceWithScanSpans(t, functions.Disabled{})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	slug := "functions-disabled-" + uuid.NewString()[:8]
	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, slug)
	exposeCanonicalWrapper(t, ctx, ti, toolset.ProjectID, toolset.ID, slug, "public")

	deployments := deploymentsrepo.New(ti.conn)
	deploymentID, err := deployments.InsertDeployment(ctx, deploymentsrepo.InsertDeploymentParams{
		ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID,
		UserID: authCtx.UserID, IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)
	require.NoError(t, deployments.CreateDeploymentStatus(ctx, deploymentsrepo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID, Status: "completed",
	}))
	asset, err := assetsrepo.New(ti.conn).CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name: "functions-disabled.zip", Url: "file://functions-disabled.zip", ProjectID: projectID,
		OrganizationID: authCtx.ActiveOrganizationID, Sha256: "functions-disabled-asset",
		Kind: "functions", ContentType: "application/zip", ContentLength: 1,
	})
	require.NoError(t, err)
	function, err := deployments.UpsertDeploymentFunctionsAsset(ctx, deploymentsrepo.UpsertDeploymentFunctionsAssetParams{
		DeploymentID: deploymentID, AssetID: asset.ID, Name: "functions-disabled", Slug: "functions-disabled",
		Runtime: string(functions.RuntimeNodeJS22), MemoryMib: pgtype.Int4{Int32: 128, Valid: true},
		Scale: pgtype.Int4{Int32: 1, Valid: true},
	})
	require.NoError(t, err)
	_, err = deployments.CreateDeploymentFunctionsAccess(ctx, deploymentsrepo.CreateDeploymentFunctionsAccessParams{
		ProjectID: projectID, DeploymentID: deploymentID, FunctionID: function.ID,
		EncryptionKey: conv.NewSecret([]byte("unused-runner-key")), BearerFormat: conv.ToPGText("v1"),
	})
	require.NoError(t, err)

	toolName := "functions_disabled_tool"
	toolURN := urn.NewTool(urn.ToolKindFunction, "functions-disabled", toolName)
	_, err = deployments.CreateFunctionsTool(ctx, deploymentsrepo.CreateFunctionsToolParams{
		DeploymentID: deploymentID, FunctionID: function.ID, ToolUrn: toolURN, ProjectID: projectID,
		Runtime: string(functions.RuntimeNodeJS22), Name: toolName, Description: "A function tool",
		InputSchema: []byte(`{"type":"object"}`), Variables: []byte(`{}`), AuthInput: nil,
		Tags: []string{}, Meta: []byte(`{}`),
		ReadOnlyHint: pgtype.Bool{Bool: false, Valid: false}, DestructiveHint: pgtype.Bool{Bool: false, Valid: false},
		IdempotentHint: pgtype.Bool{Bool: false, Valid: false}, OpenWorldHint: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	resourceURI := "gram://functions-disabled/resource"
	resourceURN := urn.NewResource(urn.ResourceKindFunction, "functions-disabled", resourceURI)
	_, err = deployments.CreateFunctionsResource(ctx, deploymentsrepo.CreateFunctionsResourceParams{
		DeploymentID: deploymentID, FunctionID: function.ID, ResourceUrn: resourceURN,
		ProjectID: projectID, Runtime: string(functions.RuntimeNodeJS22),
		Name: "functions-disabled", Description: "A function resource", Uri: resourceURI,
		Title: conv.ToPGText("Function resource"), MimeType: conv.ToPGText("text/plain"),
		Variables: []byte(`{}`), Meta: []byte(`{}`),
	})
	require.NoError(t, err)

	_, err = toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID: toolset.ID, Version: 1, ToolUrns: []urn.Tool{toolURN}, ResourceUrns: []urn.Resource{resourceURN},
		PredecessorID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	return ctx, ti, slug, toolName, resourceURI
}

// A function tool on a deployment without Functions answers with a failed
// tool result naming the reason, so the agent can tell the user, rather than
// a protocol error that reads as tools/call being unsupported.
func TestFunctionToolCallOnDisabledFunctionsReturnsToolError(t *testing.T) {
	t.Parallel()
	_, ti, slug, toolName, _ := seedDisabledFunctionsServer(t)

	body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": toolName, "arguments": map[string]any{}})
	response, err := servePublicHTTP(t, t.Context(), ti, slug, body, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	rpc := decodeRPCResponse(t, response)
	require.NotContains(t, rpc, "error")

	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(rpc["result"], &result))
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	require.Equal(t, functions.ErrFunctionsDisabled.Error(), result.Content[0].Text)
}

// Resource reads have no error result, so a function resource on a
// deployment without Functions answers as a missing resource that names the
// reason.
func TestFunctionResourceReadOnDisabledFunctionsNamesReason(t *testing.T) {
	t.Parallel()
	_, ti, slug, _, resourceURI := seedDisabledFunctionsServer(t)

	body := makeMetaRPCBody(t, "resources/read", map[string]any{"uri": resourceURI})
	response, err := servePublicHTTP(t, t.Context(), ti, slug, body, "", nil)
	require.NoError(t, err)
	rpc := decodeRPCResponse(t, response)
	require.Contains(t, rpc, "error")

	var rpcErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rpc["error"], &rpcErr))
	require.NotEqual(t, -32601, rpcErr.Code, "a disabled deployment must not read as an unsupported method")
	require.Contains(t, rpcErr.Message, functions.ErrFunctionsDisabled.Error())
}
