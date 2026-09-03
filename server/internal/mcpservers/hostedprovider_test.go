package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	externalmcprepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type hostedProviderTool struct {
	slug          string
	kind          string
	remoteURL     string
	tokenEndpoint string
	requiresOAuth bool
}

// seedHostedProviderToolset deploys the given external MCP tools and selects
// them all in a new toolset's latest version.
func seedHostedProviderToolset(t *testing.T, ctx context.Context, ti *testInstance, tools ...hostedProviderTool) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	deploymentID, err := deploymentsrepo.New(ti.conn).InsertDeployment(ctx, deploymentsrepo.InsertDeploymentParams{
		ProjectID:      projectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         "test-user",
		IdempotencyKey: uuid.New().String(),
	})
	require.NoError(t, err)
	require.NoError(t, deploymentsrepo.New(ti.conn).CreateDeploymentStatus(ctx, deploymentsrepo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	}))

	extRepo := externalmcprepo.New(ti.conn)
	toolURNs := make([]urn.Tool, 0, len(tools))
	for _, tool := range tools {
		attachment, err := extRepo.CreateExternalMCPAttachment(ctx, externalmcprepo.CreateExternalMCPAttachmentParams{
			DeploymentID:            deploymentID,
			RegistryID:              uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Name:                    tool.slug,
			Slug:                    tool.slug,
			RegistryServerSpecifier: tool.slug,
		})
		require.NoError(t, err)
		toolURN := urn.NewTool(urn.ToolKindExternalMCP, tool.slug, "call")
		toolURNs = append(toolURNs, toolURN)
		_, err = extRepo.CreateExternalMCPToolDefinition(ctx, externalmcprepo.CreateExternalMCPToolDefinitionParams{
			ExternalMcpAttachmentID:    attachment.ID,
			ToolUrn:                    toolURN.String(),
			Type:                       tool.kind,
			Name:                       pgtype.Text{String: tool.slug, Valid: true},
			Description:                pgtype.Text{String: "tool", Valid: true},
			Schema:                     []byte(`{"type":"object"}`),
			RemoteUrl:                  tool.remoteURL,
			TransportType:              externalmcptypes.TransportTypeStreamableHTTP,
			RequiresOauth:              tool.requiresOAuth,
			OauthVersion:               "2.1",
			OauthAuthorizationEndpoint: pgtype.Text{},
			OauthTokenEndpoint:         conv.ToPGTextEmpty(tool.tokenEndpoint),
			OauthRegistrationEndpoint:  pgtype.Text{},
			OauthScopesSupported:       []string{},
			HeaderDefinitions:          nil,
			Title:                      pgtype.Text{},
			ReadOnlyHint:               pgtype.Bool{},
			DestructiveHint:            pgtype.Bool{},
			IdempotentHint:             pgtype.Bool{},
			OpenWorldHint:              pgtype.Bool{},
		})
		require.NoError(t, err)
	}

	slug := "hosted-provider-" + uuid.NewString()[:8]
	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           slug,
		Slug:           slug,
		McpSlug:        conv.ToPGText(slug),
		McpEnabled:     true,
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       1,
		ToolUrns:      toolURNs,
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	return toolset.ID
}

// Each case owns a database clone: the active deployment is per project, so
// cases seeding deployments cannot share one.
func newHostedProviderCase(t *testing.T) (context.Context, *testInstance, uuid.UUID) {
	t.Helper()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return ctx, ti, *authCtx.ProjectID
}

func TestResolveHostedOAuthProvider(t *testing.T) {
	t.Parallel()

	const notes = "https://notes.example.test/mcp"
	const notesToken = "https://notes.example.test/oauth/token"

	t.Run("single provider", func(t *testing.T) {
		t.Parallel()
		ctx, ti, projectID := newHostedProviderCase(t)
		toolsetID := seedHostedProviderToolset(t, ctx, ti,
			hostedProviderTool{slug: "notes-a", kind: "direct", remoteURL: notes, tokenEndpoint: notesToken, requiresOAuth: true},
			hostedProviderTool{slug: "notes-b", kind: "direct", remoteURL: notes + "/", tokenEndpoint: notesToken + "/", requiresOAuth: true},
			hostedProviderTool{slug: "open", kind: "direct", remoteURL: "https://open.example.test/mcp", tokenEndpoint: "", requiresOAuth: false},
		)
		provider, err := mcpservers.ResolveHostedOAuthProvider(ctx, ti.conn, projectID, toolsetID)
		require.NoError(t, err)
		require.Equal(t, &mcpservers.HostedOAuthProvider{Name: "notes-a", ResourceURL: notes, TokenEndpoint: notesToken}, provider)
	})

	t.Run("no oauth tools", func(t *testing.T) {
		t.Parallel()
		ctx, ti, projectID := newHostedProviderCase(t)
		toolsetID := seedHostedProviderToolset(t, ctx, ti,
			hostedProviderTool{slug: "open", kind: "direct", remoteURL: "https://open.example.test/mcp", tokenEndpoint: "", requiresOAuth: false},
		)
		provider, err := mcpservers.ResolveHostedOAuthProvider(ctx, ti.conn, projectID, toolsetID)
		require.NoError(t, err)
		require.Nil(t, provider)
	})

	t.Run("passthrough tools are not dispatched by the gateway", func(t *testing.T) {
		t.Parallel()
		ctx, ti, projectID := newHostedProviderCase(t)
		toolsetID := seedHostedProviderToolset(t, ctx, ti,
			hostedProviderTool{slug: "proxy", kind: "proxy", remoteURL: notes, tokenEndpoint: notesToken, requiresOAuth: true},
		)
		provider, err := mcpservers.ResolveHostedOAuthProvider(ctx, ti.conn, projectID, toolsetID)
		require.NoError(t, err)
		require.Nil(t, provider)
	})

	t.Run("several providers", func(t *testing.T) {
		t.Parallel()
		ctx, ti, projectID := newHostedProviderCase(t)
		toolsetID := seedHostedProviderToolset(t, ctx, ti,
			hostedProviderTool{slug: "notes", kind: "direct", remoteURL: notes, tokenEndpoint: notesToken, requiresOAuth: true},
			hostedProviderTool{slug: "tasks", kind: "direct", remoteURL: "https://tasks.example.test/mcp", tokenEndpoint: "https://tasks.example.test/oauth/token", requiresOAuth: true},
		)
		_, err := mcpservers.ResolveHostedOAuthProvider(ctx, ti.conn, projectID, toolsetID)
		var cfg *mcpservers.HostedProviderError
		require.ErrorAs(t, err, &cfg)
		require.Contains(t, cfg.Reason, "several OAuth upstreams")
	})

	t.Run("same resource, different authorization server", func(t *testing.T) {
		t.Parallel()
		ctx, ti, projectID := newHostedProviderCase(t)
		toolsetID := seedHostedProviderToolset(t, ctx, ti,
			hostedProviderTool{slug: "notes-a", kind: "direct", remoteURL: notes, tokenEndpoint: notesToken, requiresOAuth: true},
			hostedProviderTool{slug: "notes-b", kind: "direct", remoteURL: notes, tokenEndpoint: "https://other-as.example.test/token", requiresOAuth: true},
		)
		_, err := mcpservers.ResolveHostedOAuthProvider(ctx, ti.conn, projectID, toolsetID)
		var cfg *mcpservers.HostedProviderError
		require.ErrorAs(t, err, &cfg)
	})

	t.Run("cleartext upstream", func(t *testing.T) {
		t.Parallel()
		ctx, ti, projectID := newHostedProviderCase(t)
		toolsetID := seedHostedProviderToolset(t, ctx, ti,
			hostedProviderTool{slug: "plain", kind: "direct", remoteURL: "http://notes.example.test/mcp", tokenEndpoint: notesToken, requiresOAuth: true},
		)
		_, err := mcpservers.ResolveHostedOAuthProvider(ctx, ti.conn, projectID, toolsetID)
		var cfg *mcpservers.HostedProviderError
		require.ErrorAs(t, err, &cfg)
		require.Contains(t, cfg.Reason, "must use https")
	})

	t.Run("missing token endpoint", func(t *testing.T) {
		t.Parallel()
		ctx, ti, projectID := newHostedProviderCase(t)
		toolsetID := seedHostedProviderToolset(t, ctx, ti,
			hostedProviderTool{slug: "bare", kind: "direct", remoteURL: notes, tokenEndpoint: "", requiresOAuth: true},
		)
		_, err := mcpservers.ResolveHostedOAuthProvider(ctx, ti.conn, projectID, toolsetID)
		var cfg *mcpservers.HostedProviderError
		require.ErrorAs(t, err, &cfg)
		require.Contains(t, cfg.Reason, "no OAuth token endpoint")
	})
}
