package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	tools_repo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

// Hosted input-schema validation refuses the call before the tool's server is
// contacted, so Code Mode reports it as a rejection the program can fix.
func TestCodeBridgeRejectsInvalidHostedArguments(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	hosted := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted", 0, "public", "list")
	addHTTPToolWithSchema(t, ctx, ti, hosted, *auth.ProjectID, auth.ActiveOrganizationID, "get_customer",
		`{"type":"object","properties":{"customer_id":{"type":"string"}},"required":["customer_id"],"additionalProperties":false}`)
	host := mcp.NewTestMetaCodeHost(t, ctx, ti.service.Service, gateway.ID)

	for _, arguments := range []string{`{}`, `{"customer_id":5}`} {
		result, err := host.Call(ctx, hosted.slug+"--get_customer", json.RawMessage(arguments))
		require.NoError(t, err)
		require.False(t, result.OK)
		require.Equal(t, "completed", result.Outcome, arguments)
		require.Equal(t, "tool_call_rejected", result.Error, arguments)
		require.Contains(t, string(result.Content), "The input to the tool is invalid", arguments)
	}
	// A body that does not decode is refused before dispatch as well.
	result, err := host.Call(ctx, hosted.slug+"--get_customer", json.RawMessage(`{"pathParameters":"c-1"}`))
	require.NoError(t, err)
	require.Equal(t, "tool_call_rejected", result.Error)
	require.Contains(t, string(result.Content), "invalid request body")

	// Valid arguments pass validation and reach dispatch; the fixture has no
	// server URL, so the outcome is unknown rather than rejected.
	result, err = host.Call(ctx, hosted.slug+"--get_customer", json.RawMessage(`{"customer_id":"c-1"}`))
	require.NoError(t, err)
	require.Equal(t, "tool_call_outcome_unknown", result.Error)
}

// addHTTPToolWithSchema publishes a new toolset version for the hosted member
// that adds one HTTP tool declaring the given input schema.
func addHTTPToolWithSchema(t *testing.T, ctx context.Context, ti *testInstance, hosted hostedMemberFixture, projectID uuid.UUID, orgID, name, schema string) {
	t.Helper()

	deploymentID, err := deployments_repo.New(ti.conn).InsertDeployment(ctx, deployments_repo.InsertDeploymentParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         "test-user",
		IdempotencyKey: uuid.New().String(),
	})
	require.NoError(t, err)
	require.NoError(t, deployments_repo.New(ti.conn).CreateDeploymentStatus(ctx, deployments_repo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	}))

	toolURN := urn.NewTool(urn.ToolKindHTTP, name, uuid.New().String()[:8])
	err = tools_repo.New(ti.conn).CreateHTTPToolDefinition(ctx, tools_repo.CreateHTTPToolDefinitionParams{
		ProjectID:       projectID,
		DeploymentID:    deploymentID,
		ToolUrn:         toolURN,
		Name:            name,
		UntruncatedName: pgtype.Text{},
		Summary:         name + " summary",
		Description:     name + " description",
		Tags:            []string{},
		HttpMethod:      "POST",
		Path:            "/test",
		SchemaVersion:   "3.0.0",
		Schema:          []byte(schema),
		ServerEnvVar:    "TEST_SERVER_URL",
		Security:        []byte(`[]`),
		HeaderSettings:  []byte(`{}`),
		QuerySettings:   []byte(`{}`),
		PathSettings:    []byte(`{}`),
		ReadOnlyHint:    pgtype.Bool{},
		DestructiveHint: pgtype.Bool{},
		IdempotentHint:  pgtype.Bool{},
		OpenWorldHint:   pgtype.Bool{},
	})
	require.NoError(t, err)

	latest, err := toolsets_repo.New(ti.conn).GetLatestToolsetVersion(ctx, hosted.toolsetID)
	require.NoError(t, err)
	_, err = toolsets_repo.New(ti.conn).CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     hosted.toolsetID,
		Version:       latest.Version + 1,
		ToolUrns:      append([]urn.Tool{toolURN}, latest.ToolUrns...),
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{UUID: latest.ID, Valid: true},
	})
	require.NoError(t, err)
}
