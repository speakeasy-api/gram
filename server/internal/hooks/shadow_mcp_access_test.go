package hooks

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	tsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestClaudeShadowMCPEvidence_DerivesServerIdentityOnly(t *testing.T) {
	t.Parallel()

	evidence := claudeShadowMCPEvidence("mcp__claude_ai_Calendly__authenticate")

	require.Empty(t, evidence.FullURL)
	require.Empty(t, evidence.URLHost)
	require.Equal(t, "claude_ai_Calendly", evidence.ServerIdentity)
}

func TestCursorShadowMCPEvidence_DerivesURLAndServerIdentity(t *testing.T) {
	t.Parallel()

	serverURL := "https://mcp.calendly.com/sse"
	toolName := "MCP:authenticate"
	evidence := cursorShadowMCPEvidence(&gen.CursorPayload{
		ToolName: &toolName,
		URL:      &serverURL,
	})

	require.Equal(t, serverURL, evidence.FullURL)
	require.Empty(t, evidence.URLHost)
	require.Equal(t, "mcp.calendly.com", evidence.ServerIdentity)
}

func TestEnforceShadowMCPToolAccess_DenyRuleOverridesValidToolsetCall(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolsetID := createHookToolsetWithHTTPTool(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "do_thing")
	_, err := accessrepo.New(ti.conn).CreateAccessRule(ctx, accessrepo.CreateAccessRuleParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       uuid.NullUUID{},
		AccessScope:     "organization",
		ResourceType:    "shadow_mcp",
		Disposition:     "denied",
		MatchKind:       shadowmcp.MatchBreadthServerIdentity,
		MatchValue:      "blocked-server",
		DisplayName:     "Blocked server",
		ObservedSummary: []byte("{}"),
		SourceRequestID: uuid.NullUUID{},
		CreatedBy:       pgtype.Text{String: "", Valid: false},
		UpdatedBy:       pgtype.Text{String: "", Valid: false},
		Reason:          pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	detail, denied := ti.service.enforceShadowMCPToolAccess(
		ctx,
		authCtx.ActiveOrganizationID,
		authCtx.ProjectID.String(),
		authCtx.UserID,
		map[string]any{shadowmcp.XGramToolsetIDField: toolsetID.String()},
		"do_thing",
		shadowmcp.AccessEvidence{FullURL: "", URLHost: "", ServerIdentity: "blocked-server"},
	)

	require.True(t, denied)
	require.Contains(t, detail, `matched denied Shadow MCP Access Rule "Blocked server"`)
}

func createHookToolsetWithHTTPTool(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, toolName string) uuid.UUID {
	t.Helper()

	toolsetSlug := "ts-" + uuid.NewString()[:8]
	toolset, err := tsrepo.New(conn).CreateToolset(ctx, tsrepo.CreateToolsetParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Name:           toolsetSlug,
		Slug:           toolsetSlug,
	})
	require.NoError(t, err)

	deploymentID := createHookCompletedDeployment(t, ctx, conn, organizationID, projectID)
	toolURN := urn.NewTool(urn.ToolKindHTTP, "test-api", uuid.NewString()[:8])
	_, err = deploymentsrepo.New(conn).CreateOpenAPIv3ToolDefinition(ctx, deploymentsrepo.CreateOpenAPIv3ToolDefinitionParams{
		ProjectID:           projectID,
		DeploymentID:        deploymentID,
		Openapiv3DocumentID: uuid.NullUUID{},
		ToolUrn:             toolURN,
		Name:                toolName,
		UntruncatedName:     pgtype.Text{String: "", Valid: true},
		Openapiv3Operation:  pgtype.Text{},
		Summary:             "Test tool",
		Description:         "A test tool",
		Tags:                []string{},
		Confirm:             pgtype.Text{},
		ConfirmPrompt:       pgtype.Text{},
		XGram:               pgtype.Bool{},
		OriginalName:        pgtype.Text{},
		OriginalSummary:     pgtype.Text{},
		OriginalDescription: pgtype.Text{},
		Security:            []byte("[]"),
		HttpMethod:          "POST",
		Path:                "/test",
		SchemaVersion:       "3.0.0",
		Schema:              []byte("{}"),
		HeaderSettings:      []byte("{}"),
		QuerySettings:       []byte("{}"),
		PathSettings:        []byte("{}"),
		ServerEnvVar:        "TEST_SERVER_URL",
		DefaultServerUrl:    pgtype.Text{},
		RequestContentType:  pgtype.Text{},
		ResponseFilter:      nil,
		ReadOnlyHint:        pgtype.Bool{},
		DestructiveHint:     pgtype.Bool{},
		IdempotentHint:      pgtype.Bool{},
		OpenWorldHint:       pgtype.Bool{},
	})
	require.NoError(t, err)

	_, err = tsrepo.New(conn).CreateToolsetVersion(ctx, tsrepo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       1,
		ToolUrns:      []urn.Tool{toolURN},
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	return toolset.ID
}

func createHookCompletedDeployment(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	deployments := deploymentsrepo.New(conn)
	idempotencyKey := "test-" + uuid.NewString()
	_, err := deployments.CreateDeployment(ctx, deploymentsrepo.CreateDeploymentParams{
		IdempotencyKey: idempotencyKey,
		UserID:         "test-user",
		OrganizationID: organizationID,
		ProjectID:      projectID,
		GithubRepo:     pgtype.Text{},
		GithubPr:       pgtype.Text{},
		GithubSha:      pgtype.Text{},
		ExternalID:     pgtype.Text{},
		ExternalUrl:    pgtype.Text{},
	})
	require.NoError(t, err)

	deployment, err := deployments.GetDeploymentByIdempotencyKey(ctx, deploymentsrepo.GetDeploymentByIdempotencyKeyParams{
		IdempotencyKey: idempotencyKey,
		ProjectID:      projectID,
	})
	require.NoError(t, err)

	for _, status := range []string{"created", "pending", "completed"} {
		_, err = deployments.TransitionDeployment(ctx, deploymentsrepo.TransitionDeploymentParams{
			DeploymentID: deployment.Deployment.ID,
			Status:       status,
			ProjectID:    projectID,
			Event:        "test",
			Message:      "test deployment status",
		})
		require.NoError(t, err)
	}

	return deployment.Deployment.ID
}
