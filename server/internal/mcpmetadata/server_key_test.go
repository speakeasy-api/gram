package mcpmetadata_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	mcpendpoints_repo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	})
	require.NoError(t, err)
	testenv.SetLegacyAlias(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, slug+"-mcp", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	toolset, err = toolsets_repo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsets_repo.GetToolsetByIDAndProjectParams{ID: toolset.ID, ProjectID: toolset.ProjectID})
	require.NoError(t, err)
	return toolset
}

// createCanonicalWrapper inserts the mcp_servers row that shares the toolset's id.
func createCanonicalWrapper(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, visibility string) mcpservers_repo.McpServer {
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
		Visibility:          visibility,
		NetworkAccessMode:   networkaccess.Storage(networkaccess.ModePublicOnly),
	})
	require.NoError(t, err)
	return server
}

// requireSingleMetadataRow asserts id has exactly one metadata row.
func requireSingleMetadataRow(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	count, err := testrepo.New(ti.conn).CountMcpMetadataForBackendIDFixture(ctx, testrepo.CountMcpMetadataForBackendIDFixtureParams{ProjectID: *authCtx.ProjectID, BackendID: id})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func setBySlug(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, payload *gen.SetMcpMetadataPayload) *types.McpMetadata {
	t.Helper()

	payload.ToolsetSlug = new(types.Slug(toolset.Slug))
	row, err := ti.service.SetMcpMetadata(ctx, payload)
	require.NoError(t, err)
	return row
}

// seedServerKeyedRow writes a row under the toolset's canonical wrapper.
func seedServerKeyedRow(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, instructions string, variables ...string) uuid.UUID {
	t.Helper()

	queries := repo.New(ti.conn)
	row, err := queries.UpsertMetadataByMcpServerID(ctx, repo.UpsertMetadataByMcpServerIDParams{
		McpServerID:               toolset.ID,
		ProjectID:                 toolset.ProjectID,
		ExternalDocumentationUrl:  pgtype.Text{String: "", Valid: false},
		ExternalDocumentationText: pgtype.Text{String: "", Valid: false},
		LogoID:                    uuid.NullUUID{},
		Instructions:              conv.ToPGText(instructions),
		DefaultEnvironmentID:      uuid.NullUUID{},
		InstallationOverrideUrl:   pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	for _, variable := range variables {
		_, err := queries.UpsertEnvironmentConfig(ctx, repo.UpsertEnvironmentConfigParams{
			ProjectID:         toolset.ProjectID,
			McpMetadataID:     row.ID,
			VariableName:      variable,
			HeaderDisplayName: pgtype.Text{String: "", Valid: false},
			ProvidedBy:        "user",
		})
		require.NoError(t, err)
	}
	return row.ID
}

func TestSetMcpMetadata_NewRowsUseTheServerKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	bySlug := createHostedToolset(t, ctx, ti, "hosted-new-slug")
	createCanonicalWrapper(t, ctx, ti, bySlug, mcpservers.VisibilityPrivate)
	byServer := createHostedToolset(t, ctx, ti, "hosted-new-server")
	createCanonicalWrapper(t, ctx, ti, byServer, mcpservers.VisibilityPrivate)
	unwrapped := createHostedToolset(t, ctx, ti, "hosted-new-unwrapped")

	slugRow := setBySlug(t, ctx, ti, bySlug, &gen.SetMcpMetadataPayload{Instructions: new("hello")})
	requireHostedIDs(t, ctx, ti, slugRow, bySlug.ID)

	serverRow, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{McpServerID: new(byServer.ID.String())})
	require.NoError(t, err)
	requireHostedIDs(t, ctx, ti, serverRow, byServer.ID)

	// A toolset without a canonical wrapper has nothing to key the row on.
	_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{ToolsetSlug: new(types.Slug(unwrapped.Slug)), Instructions: new("hello")})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, hostedmcp.NoServerMessage)
}

func TestSetMcpMetadata_ServerKeyedRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createHostedToolset(t, ctx, ti, "hosted-moved")
	createCanonicalWrapper(t, ctx, ti, toolset, mcpservers.VisibilityPrivate)
	seeded := seedServerKeyedRow(t, ctx, ti, toolset, "before", "API_KEY")
	original, err := ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{ToolsetSlug: new(types.Slug(toolset.Slug))})
	require.NoError(t, err)
	require.Equal(t, seeded.String(), original.Metadata.ID)

	for _, payload := range []*gen.GetMcpMetadataPayload{
		{ToolsetSlug: new(types.Slug(toolset.Slug))},
		{McpServerID: new(toolset.ID.String())},
	} {
		got, err := ti.service.GetMcpMetadata(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, original.Metadata.ID, got.Metadata.ID)
		requireHostedIDs(t, ctx, ti, got.Metadata, toolset.ID)
		require.Len(t, got.Metadata.EnvironmentConfigs, 1)
		require.Equal(t, original.Metadata.EnvironmentConfigs[0].ID, got.Metadata.EnvironmentConfigs[0].ID)
	}

	// Runtime readers keyed by toolset id find the server-keyed row.
	queries := repo.New(ti.conn)
	row, err := queries.GetMetadataByBackendID(ctx, repo.GetMetadataByBackendIDParams{ProjectID: *authCtx.ProjectID, BackendID: toolset.ID, ToolsetBackend: true})
	require.NoError(t, err)
	require.Equal(t, original.Metadata.ID, row.ID.String())
	_, err = queries.GetHeaderDisplayNames(ctx, repo.GetHeaderDisplayNamesParams{ProjectID: *authCtx.ProjectID, BackendID: toolset.ID})
	require.NoError(t, err)
	names, err := queries.GetHeaderDisplayNamesByToolsetIDs(ctx, repo.GetHeaderDisplayNamesByToolsetIDsParams{ProjectID: *authCtx.ProjectID, ToolsetIds: []uuid.UUID{toolset.ID}})
	require.NoError(t, err)
	require.Len(t, names, 1)
	require.Equal(t, toolset.ID, names[0].ToolsetID)

	viaServer, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		McpServerID:  new(toolset.ID.String()),
		Instructions: new("via server"),
	})
	require.NoError(t, err)
	require.Equal(t, original.Metadata.ID, viaServer.ID)
	requireHostedIDs(t, ctx, ti, viaServer, toolset.ID)
	require.Len(t, viaServer.EnvironmentConfigs, 1)
	requireSingleMetadataRow(t, ctx, ti, toolset.ID)

	// A toolset_slug write updates the server-keyed row instead of adding a twin.
	viaSlug := setBySlug(t, ctx, ti, toolset, &gen.SetMcpMetadataPayload{Instructions: new("after")})
	require.Equal(t, original.Metadata.ID, viaSlug.ID)
	requireHostedIDs(t, ctx, ti, viaSlug, toolset.ID)
	require.Len(t, viaSlug.EnvironmentConfigs, 1)
	require.Equal(t, original.Metadata.EnvironmentConfigs[0].ID, viaSlug.EnvironmentConfigs[0].ID)
	requireSingleMetadataRow(t, ctx, ti, toolset.ID)

	// A dashboard read-modify-write echoes mcp_server_id alongside toolset_slug.
	echoed, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:  new(types.Slug(toolset.Slug)),
		McpServerID:  new(toolset.ID.String()),
		Instructions: new("echoed"),
	})
	require.NoError(t, err)
	require.Equal(t, viaSlug.ID, echoed.ID)
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
}

func TestExportMcpMetadata_ByMcpServerID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolset := createHostedToolset(t, ctx, ti, "hosted-export")
	createCanonicalWrapper(t, ctx, ti, toolset, mcpservers.VisibilityPrivate)

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

	freshID, err := testrepo.New(ti.conn).CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{
		ID:         uuid.New(),
		ProjectID:  toolset.ProjectID,
		ToolsetID:  uuid.NullUUID{UUID: toolset.ID, Valid: true},
		Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
	_, err = ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{McpServerID: new(freshID.String())})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSetMcpMetadata_ServerKeyedSaveKeepsHeaderAliases(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolset := createHostedToolset(t, ctx, ti, "hosted-save-aliases")
	createCanonicalWrapper(t, ctx, ti, toolset, mcpservers.VisibilityPrivate)
	original := seedServerKeyedRow(t, ctx, ti, toolset, "before")

	rows, err := testrepo.New(ti.conn).SetMcpMetadataHeaderDisplayNamesFixture(ctx, testrepo.SetMcpMetadataHeaderDisplayNamesFixtureParams{
		HeaderDisplayNames: []byte(`{"API_KEY":"Alias-Key"}`),
		ID:                 original,
		ProjectID:          toolset.ProjectID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	viaSlug := setBySlug(t, ctx, ti, toolset, &gen.SetMcpMetadataPayload{Instructions: new("after")})
	require.Equal(t, original.String(), viaSlug.ID)
	requireHostedIDs(t, ctx, ti, viaSlug, toolset.ID)

	queries := repo.New(ti.conn)
	after, err := queries.GetHeaderDisplayNames(ctx, repo.GetHeaderDisplayNamesParams{ProjectID: toolset.ProjectID, BackendID: toolset.ID})
	require.NoError(t, err)
	require.JSONEq(t, `{"API_KEY":"Alias-Key"}`, string(after))
	requireSingleMetadataRow(t, ctx, ti, toolset.ID)
}

// A toolset's id held by a server that is not its canonical wrapper (remote-backed after an in-place switch, or a
// fresh-id wrapper) is that server's metadata, never the toolset's; the server still reads its own row.
func TestMcpMetadata_ToolsetIgnoresNonCanonicalServerHoldingItsID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	remote := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcp_repo.CreateServerParams{
		ProjectID:     *authCtx.ProjectID,
		TransportType: "streamable-http",
		Url:           "https://upstream.example.com/mcp",
	})
	other := createHostedToolset(t, ctx, ti, "held-id-other")
	for name, backend := range map[string]mcpservers_repo.CreateMCPServerParams{
		"remote-backed": {RemoteMcpServerID: uuid.NullUUID{UUID: remote.ID, Valid: true}},
		"other toolset": {ToolsetID: uuid.NullUUID{UUID: other.ID, Valid: true}},
	} {
		toolset := createHostedToolset(t, ctx, ti, "held-id-"+uuid.NewString()[:8])
		_, err := mcpservers_repo.New(ti.conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
			ID:                  toolset.ID,
			ProjectID:           toolset.ProjectID,
			Name:                conv.ToPGText("Held id"),
			Slug:                conv.ToPGText("held-id-" + uuid.NewString()[:8]),
			EnvironmentID:       uuid.NullUUID{},
			UserSessionIssuerID: uuid.NullUUID{},
			RemoteMcpServerID:   backend.RemoteMcpServerID,
			TunneledMcpServerID: uuid.NullUUID{},
			ToolsetID:           backend.ToolsetID,
			Visibility:          mcpservers.VisibilityPrivate,
			NetworkAccessMode:   networkaccess.Storage(networkaccess.ModePublicOnly),
		})
		require.NoError(t, err, name)
		saved, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{McpServerID: new(toolset.ID.String()), Instructions: new("server")})
		require.NoError(t, err, name)
		rows, err := testrepo.New(ti.conn).SetMcpMetadataHeaderDisplayNamesFixture(ctx, testrepo.SetMcpMetadataHeaderDisplayNamesFixtureParams{
			HeaderDisplayNames: []byte(`{"API_KEY":"X-Server"}`), ID: uuid.MustParse(saved.ID), ProjectID: toolset.ProjectID,
		})
		require.NoError(t, err, name)
		require.EqualValues(t, 1, rows, name)

		queries := repo.New(ti.conn)
		_, err = queries.GetMetadataByBackendID(ctx, repo.GetMetadataByBackendIDParams{ProjectID: toolset.ProjectID, BackendID: toolset.ID, ToolsetBackend: true})
		require.ErrorIs(t, err, pgx.ErrNoRows, name)
		_, err = queries.GetHeaderDisplayNames(ctx, repo.GetHeaderDisplayNamesParams{ProjectID: toolset.ProjectID, BackendID: toolset.ID})
		require.ErrorIs(t, err, pgx.ErrNoRows, name)
		names, err := queries.GetHeaderDisplayNamesByToolsetIDs(ctx, repo.GetHeaderDisplayNamesByToolsetIDsParams{ProjectID: toolset.ProjectID, ToolsetIds: []uuid.UUID{toolset.ID}})
		require.NoError(t, err, name)
		require.Empty(t, names, name)
		_, err = ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{ToolsetSlug: new(types.Slug(toolset.Slug))})
		requireOopsCode(t, err, oops.CodeNotFound)
		_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{ToolsetSlug: new(types.Slug(toolset.Slug)), Instructions: new("toolset")})
		requireOopsCode(t, err, oops.CodeBadRequest)

		got, err := ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{McpServerID: new(toolset.ID.String())})
		require.NoError(t, err, name)
		require.Equal(t, saved.ID, got.Metadata.ID, name)
		require.Equal(t, "server", *got.Metadata.Instructions, name)
	}
}

func TestExportMcpMetadata_BySlugKeepsFrozenAliasIdentity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)

	legacy := createHostedToolset(t, ctx, ti, "alias-owner")
	createCanonicalWrapper(t, ctx, ti, legacy, mcpservers.VisibilityPrivate)
	other := createHostedToolset(t, ctx, ti, "endpoint-owner")
	otherServer := createCanonicalWrapper(t, ctx, ti, other, mcpservers.VisibilityPrivate)
	_, err := mcpendpoints_repo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpoints_repo.CreateMCPEndpointParams{
		ProjectID: other.ProjectID, McpServerID: uuid.NullUUID{UUID: otherServer.ID, Valid: true}, Slug: legacy.McpSlug.String,
	})
	require.NoError(t, err)

	result, err := ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{McpSlug: new(types.Slug(legacy.McpSlug.String))})
	require.NoError(t, err)
	require.Equal(t, legacy.Slug, result.Slug, "the frozen alias keeps naming its toolset")

	// A slug no toolset's mcp_slug holds names its endpoint's server.
	_, err = mcpendpoints_repo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpoints_repo.CreateMCPEndpointParams{
		ProjectID: other.ProjectID, McpServerID: uuid.NullUUID{UUID: otherServer.ID, Valid: true}, Slug: "endpoint-owner-second",
	})
	require.NoError(t, err)
	result, err = ti.service.ExportMcpMetadata(ctx, &gen.ExportMcpMetadataPayload{McpSlug: new(types.Slug("endpoint-owner-second"))})
	require.NoError(t, err)
	require.Equal(t, other.Slug, result.Slug)
}
