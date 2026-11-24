// serve_meta_read_only_execute_test.go verifies execute_read_only_tool: it
// runs member tools whose effective annotations classify them read-only and
// refuses every other tool before dispatch, on hosted and proxied members.
package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	variations_repo "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

func TestServePublic_MetaEndpoint_ExecuteReadOnlyTool_HostedReadOnlyToolDispatches(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")

	variationsRepo := variations_repo.New(ti.conn)
	group, err := variationsRepo.InitGlobalToolVariationsGroup(ctx, variations_repo.InitGlobalToolVariationsGroupParams{
		ProjectID:   *authCtx.ProjectID,
		Name:        "default-group",
		Description: conv.ToPGText("default group"),
	})
	require.NoError(t, err)
	_, err = variationsRepo.UpsertToolVariation(ctx, variations_repo.UpsertToolVariationParams{
		GroupID:      group,
		SrcToolUrn:   member.toolURNs["alpha_tool"],
		SrcToolName:  "alpha_tool",
		ReadOnlyHint: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)

	envelope := callMetaTool(t, ctx, ti, slug, "execute_read_only_tool", map[string]any{
		"name":      member.slug + "--alpha_tool",
		"arguments": map[string]any{},
	})

	// The fixture tool has no server URL, so reaching that failure proves the
	// read-only check let the call through to execution.
	text, isError := metaToolResultText(t, envelope)
	require.True(t, isError)
	require.Contains(t, text, "no server URL")
	require.NotContains(t, text, "not annotated read-only")
}

func TestServePublic_MetaEndpoint_ExecuteReadOnlyTool_HostedUnannotatedToolRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")

	envelope := callMetaTool(t, ctx, ti, slug, "execute_read_only_tool", map[string]any{
		"name":      member.slug + "--alpha_tool",
		"arguments": map[string]any{},
	})

	text, isError := metaToolResultText(t, envelope)
	require.True(t, isError, "a refused tool is a member-scoped failure the agent can correct")
	require.Contains(t, text, `"alpha_tool" is not annotated read-only`)
	require.Contains(t, text, "execute_tool")
	require.NotContains(t, text, "no server URL", "the refusal must precede execution")
}

func TestServePublic_MetaEndpoint_ExecuteReadOnlyTool_ProxiedReadOnlyToolRuns(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	metaSlug, serverID, upstream, bearer := seedReadOnlyProxiedGateway(t, ctx, ti)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := mcpserversrepo.New(ti.conn).AddMCPServerToolMetadata(ctx, mcpserversrepo.AddMCPServerToolMetadataParams{
		ProjectID:   *authCtx.ProjectID,
		McpServerID: serverID,
		Tools:       []byte(`[{"tool_name":"ping","read_only_hint":true}]`),
	})
	require.NoError(t, err)

	text, isError := metaToolResultText(t, executeReadOnlyMetaTool(t, ti, metaSlug, bearer, "member-a--ping"))
	require.False(t, isError, "a read-only member tool must run: %s", text)
	require.Contains(t, text, "pong from ping")
	upstream.requireToolCallAuth(t, "Bearer token-member-a", "the call reaches the member's upstream")
}

func TestServePublic_MetaEndpoint_ExecuteReadOnlyTool_ProxiedWriteToolRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	metaSlug, serverID, upstream, bearer := seedReadOnlyProxiedGateway(t, ctx, ti)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := mcpserversrepo.New(ti.conn).AddMCPServerToolMetadata(ctx, mcpserversrepo.AddMCPServerToolMetadataParams{
		ProjectID:   *authCtx.ProjectID,
		McpServerID: serverID,
		Tools:       []byte(`[{"tool_name":"ping","read_only_hint":false,"destructive_hint":true}]`),
	})
	require.NoError(t, err)

	text, isError := metaToolResultText(t, executeReadOnlyMetaTool(t, ti, metaSlug, bearer, "member-a--ping"))
	require.True(t, isError)
	require.Contains(t, text, `"ping" is not annotated read-only`)
	require.Zero(t, upstream.requestCount(), "a refused call must not reach the member's upstream")
}

// A tool the catalog has never recorded has no annotations to trust.
func TestServePublic_MetaEndpoint_ExecuteReadOnlyTool_ProxiedUnrecordedToolRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	metaSlug, _, upstream, bearer := seedReadOnlyProxiedGateway(t, ctx, ti)

	text, isError := metaToolResultText(t, executeReadOnlyMetaTool(t, ti, metaSlug, bearer, "member-a--ping"))
	require.True(t, isError)
	require.Contains(t, text, `"ping" is not annotated read-only`)
	require.Zero(t, upstream.requestCount(), "a refused call must not reach the member's upstream")
}

// execute_tool keeps running a tool execute_read_only_tool refuses.
func TestServePublic_MetaEndpoint_ExecuteTool_RunsToolReadOnlyVariantRefuses(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	metaSlug, _, _, bearer := seedReadOnlyProxiedGateway(t, ctx, ti)

	text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member-a--ping"))
	require.False(t, isError, "execute_tool must run any tool the caller may call: %s", text)
	require.Contains(t, text, "pong from ping")
}

// seedReadOnlyProxiedGateway builds an issuer-gated gateway with one proxied
// member, member-a, serving a ping tool, and returns the gateway slug, the
// member's mcp_servers id, its upstream, and a bearer for a signed-in user.
func seedReadOnlyProxiedGateway(t *testing.T, ctx context.Context, ti *testInstance) (string, uuid.UUID, *recordingUpstream, string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-read-only-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)

	upstream := newRecordingUpstream(t, "ping")
	serverID := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Member A", "member-a", 0, upstream.url)

	client := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-read-only-a", "", []uuid.UUID{issuerID})
	subject := createTestUser(t, ctx, ti, "meta-read-only-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, client, subject, "token-member-a", upstream.url)

	return metaSlug, serverID, upstream, mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)
}

// executeReadOnlyMetaTool drives execute_read_only_tool through the public
// HTTP surface and returns the decoded RPC response.
func executeReadOnlyMetaTool(t *testing.T, ti *testInstance, metaSlug, bearer, qualified string) map[string]json.RawMessage {
	t.Helper()

	body := makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "execute_read_only_tool",
		"arguments": map[string]any{"name": qualified, "arguments": map[string]any{}},
	})
	resp, err := servePublicHTTP(t, t.Context(), ti, metaSlug, body, bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, "execute_read_only_tool response: %s", resp.Body.String())
	return decodeRPCResponse(t, resp)
}
