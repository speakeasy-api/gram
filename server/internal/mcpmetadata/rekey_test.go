package mcpmetadata_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environments_repo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func createHostedToolset(t *testing.T, ctx context.Context, ti *testInstance, slug string) toolsets_repo.Toolset {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsets_repo.New(ti.conn).CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Hosted Toolset",
		Slug:                   slug,
		Description:            conv.ToPGText("A hosted toolset"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(slug + "-mcp"),
		McpEnabled:             true,
	})
	require.NoError(t, err)
	return toolset
}

// createCanonicalWrapper inserts the mcp_servers row that shares the toolset's id.
func createCanonicalWrapper(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset) mcpservers_repo.McpServer {
	t.Helper()

	server, err := mcpservers_repo.New(ti.conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  toolset.ID,
		ProjectID:           toolset.ProjectID,
		Name:                conv.ToPGText(toolset.Name),
		Slug:                conv.ToPGText(toolset.Slug + "-" + uuid.NewString()[:8]),
		EnvironmentID:       uuid.NullUUID{},
		UserSessionIssuerID: uuid.NullUUID{},
		RemoteMcpServerID:   uuid.NullUUID{},
		TunneledMcpServerID: uuid.NullUUID{},
		ToolsetID:           uuid.NullUUID{UUID: toolset.ID, Valid: true},
		Visibility:          mcpservers.VisibilityPrivate,
		NetworkAccessMode:   networkaccess.Storage(networkaccess.ModePublicOnly),
	})
	require.NoError(t, err)
	return server
}

// rekeyToServer moves a toolset-keyed row onto its canonical wrapper, as the dependent move does.
func rekeyToServer(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset) {
	t.Helper()

	rows, err := testrepo.New(ti.conn).RekeyMcpMetadataToMcpServerFixture(ctx, testrepo.RekeyMcpMetadataToMcpServerFixtureParams{
		ProjectID: toolset.ProjectID,
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
}

// requireSingleMetadataRow asserts no toolset-keyed and server-keyed twin exist for id.
func requireSingleMetadataRow(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	count, err := testrepo.New(ti.conn).CountMcpMetadataForBackendIDFixture(ctx, testrepo.CountMcpMetadataForBackendIDFixtureParams{ProjectID: *authCtx.ProjectID, BackendID: id})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func userEnvConfigs(name string) []*types.McpEnvironmentConfigInput {
	return []*types.McpEnvironmentConfigInput{{
		VariableName:      name,
		HeaderDisplayName: new("API Key"),
		ProvidedBy:        "user",
	}}
}

func setBySlug(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, payload *gen.SetMcpMetadataPayload) *types.McpMetadata {
	t.Helper()

	payload.ToolsetSlug = new(types.Slug(toolset.Slug))
	row, err := ti.service.SetMcpMetadata(ctx, payload)
	require.NoError(t, err)
	return row
}

func TestSetMcpMetadata_NewRowsKeepTheRequestedKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	bySlug := createHostedToolset(t, ctx, ti, "hosted-new-slug")
	createCanonicalWrapper(t, ctx, ti, bySlug)
	byServer := createHostedToolset(t, ctx, ti, "hosted-new-server")
	createCanonicalWrapper(t, ctx, ti, byServer)

	slugRow := setBySlug(t, ctx, ti, bySlug, &gen.SetMcpMetadataPayload{Instructions: new("hello")})
	require.Equal(t, bySlug.ID.String(), *slugRow.ToolsetID)
	require.Nil(t, slugRow.McpServerID)

	serverRow, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{McpServerID: new(byServer.ID.String())})
	require.NoError(t, err)
	require.Equal(t, byServer.ID.String(), *serverRow.McpServerID)
	require.Nil(t, serverRow.ToolsetID)
}

func TestSetMcpMetadata_ToolsetKeyedRowUpdatesInPlace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createHostedToolset(t, ctx, ti, "hosted-toolset-row")
	createCanonicalWrapper(t, ctx, ti, toolset)
	env, err := environments_repo.New(ti.conn).CreateEnvironment(ctx, environments_repo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Toolset Row Env",
		Slug:           "toolset-row-env",
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	original := setBySlug(t, ctx, ti, toolset, &gen.SetMcpMetadataPayload{
		Instructions:         new("before"),
		DefaultEnvironmentID: new(env.ID.String()),
		EnvironmentConfigs:   userEnvConfigs("API_KEY"),
	})
	require.NotNil(t, original.ToolsetID)

	// Both references read and write the same toolset-keyed row.
	for _, payload := range []*gen.GetMcpMetadataPayload{
		{ToolsetSlug: new(types.Slug(toolset.Slug))},
		{McpServerID: new(toolset.ID.String())},
	} {
		got, err := ti.service.GetMcpMetadata(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, original.ID, got.Metadata.ID)
	}

	updated := setBySlug(t, ctx, ti, toolset, &gen.SetMcpMetadataPayload{
		Instructions:         new("after"),
		DefaultEnvironmentID: new(env.ID.String()),
	})
	require.Equal(t, original.ID, updated.ID)
	require.Equal(t, toolset.ID.String(), *updated.ToolsetID)
	require.Nil(t, updated.McpServerID)
	require.Len(t, updated.EnvironmentConfigs, 1)
	require.Equal(t, original.EnvironmentConfigs[0].ID, updated.EnvironmentConfigs[0].ID)

	viaServer, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		McpServerID:  new(toolset.ID.String()),
		Instructions: new("via server"),
	})
	require.NoError(t, err)
	require.Equal(t, original.ID, viaServer.ID)
	require.Equal(t, toolset.ID.String(), *viaServer.ToolsetID)
	require.Len(t, viaServer.EnvironmentConfigs, 1)
	requireSingleMetadataRow(t, ctx, ti, toolset.ID)

	for _, payload := range []*gen.ExportMcpMetadataPayload{
		{McpSlug: new(types.Slug(toolset.McpSlug.String))},
		{McpServerID: new(toolset.ID.String())},
	} {
		got, err := ti.service.ExportMcpMetadata(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, "via server", *got.Instructions)
	}
}

func TestSetMcpMetadata_ServerKeyedRowUpdatesInPlace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createHostedToolset(t, ctx, ti, "hosted-moved")
	createCanonicalWrapper(t, ctx, ti, toolset)
	original := setBySlug(t, ctx, ti, toolset, &gen.SetMcpMetadataPayload{
		Instructions:       new("before"),
		EnvironmentConfigs: userEnvConfigs("API_KEY"),
	})
	rekeyToServer(t, ctx, ti, toolset)

	for _, payload := range []*gen.GetMcpMetadataPayload{
		{ToolsetSlug: new(types.Slug(toolset.Slug))},
		{McpServerID: new(toolset.ID.String())},
	} {
		got, err := ti.service.GetMcpMetadata(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, original.ID, got.Metadata.ID)
		require.Nil(t, got.Metadata.ToolsetID)
		require.Len(t, got.Metadata.EnvironmentConfigs, 1)
		require.Equal(t, original.EnvironmentConfigs[0].ID, got.Metadata.EnvironmentConfigs[0].ID)
	}

	updated := setBySlug(t, ctx, ti, toolset, &gen.SetMcpMetadataPayload{Instructions: new("after")})
	require.Equal(t, original.ID, updated.ID)
	require.Nil(t, updated.ToolsetID)
	require.Equal(t, toolset.ID.String(), *updated.McpServerID)
	require.Len(t, updated.EnvironmentConfigs, 1)
	requireSingleMetadataRow(t, ctx, ti, toolset.ID)

	// A dashboard read-modify-write echoes mcp_server_id alongside toolset_slug.
	echoed, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:  new(types.Slug(toolset.Slug)),
		McpServerID:  new(toolset.ID.String()),
		Instructions: new("echoed"),
	})
	require.NoError(t, err)
	require.Equal(t, original.ID, echoed.ID)
	requireSingleMetadataRow(t, ctx, ti, toolset.ID)

	_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug: new(types.Slug(toolset.Slug)),
		McpServerID: new(uuid.NewString()),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	for _, payload := range []*gen.ExportMcpMetadataPayload{
		{McpSlug: new(types.Slug(toolset.McpSlug.String))},
		{McpServerID: new(toolset.ID.String())},
	} {
		got, err := ti.service.ExportMcpMetadata(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, "echoed", *got.Instructions)
	}

	// Runtime readers keyed by toolset id find the server-keyed row.
	queries := repo.New(ti.conn)
	row, err := queries.GetMetadataByBackendID(ctx, repo.GetMetadataByBackendIDParams{ProjectID: *authCtx.ProjectID, BackendID: toolset.ID})
	require.NoError(t, err)
	require.Equal(t, original.ID, row.ID.String())
	_, err = queries.GetHeaderDisplayNames(ctx, repo.GetHeaderDisplayNamesParams{ProjectID: *authCtx.ProjectID, BackendID: toolset.ID})
	require.NoError(t, err)
	names, err := queries.GetHeaderDisplayNamesByToolsetIDs(ctx, repo.GetHeaderDisplayNamesByToolsetIDsParams{ProjectID: *authCtx.ProjectID, ToolsetIds: []uuid.UUID{toolset.ID}})
	require.NoError(t, err)
	require.Len(t, names, 1)
	require.Equal(t, toolset.ID, names[0].ToolsetID)
}

func TestGetMcpMetadata_PrefersToolsetKeyedTwin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolset := createHostedToolset(t, ctx, ti, "hosted-twin")
	createCanonicalWrapper(t, ctx, ti, toolset)
	toolsetRow := setBySlug(t, ctx, ti, toolset, &gen.SetMcpMetadataPayload{Instructions: new("toolset")})

	// Older code could add a server-keyed twin; readers keep serving the toolset-keyed row.
	_, err := repo.New(ti.conn).UpsertMetadataByMcpServerID(ctx, repo.UpsertMetadataByMcpServerIDParams{
		McpServerID:               uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID:                 toolset.ProjectID,
		ExternalDocumentationUrl:  pgtype.Text{String: "", Valid: false},
		ExternalDocumentationText: pgtype.Text{String: "", Valid: false},
		LogoID:                    uuid.NullUUID{},
		Instructions:              conv.ToPGText("server"),
		DefaultEnvironmentID:      uuid.NullUUID{},
		InstallationOverrideUrl:   pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	for _, payload := range []*gen.GetMcpMetadataPayload{
		{ToolsetSlug: new(types.Slug(toolset.Slug))},
		{McpServerID: new(toolset.ID.String())},
	} {
		got, err := ti.service.GetMcpMetadata(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, toolsetRow.ID, got.Metadata.ID)
	}
	updated := setBySlug(t, ctx, ti, toolset, &gen.SetMcpMetadataPayload{Instructions: new("updated")})
	require.Equal(t, toolsetRow.ID, updated.ID)
}

func TestExportMcpMetadata_ByMcpServerID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolset := createHostedToolset(t, ctx, ti, "hosted-export")
	createCanonicalWrapper(t, ctx, ti, toolset)

	result, err := ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{McpServerID: new(toolset.ID.String())})
	require.NoError(t, err)
	require.Equal(t, toolset.Slug, result.Slug)
	require.Contains(t, result.ServerURL, toolset.McpSlug.String)

	_, err = ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{
		McpSlug:     new(types.Slug(toolset.McpSlug.String)),
		McpServerID: new(toolset.ID.String()),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{McpServerID: new(uuid.NewString())})
	requireOopsCode(t, err, oops.CodeNotFound)
}
