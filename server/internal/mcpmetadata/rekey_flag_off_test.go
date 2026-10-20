package mcpmetadata_test

// Phase A expectations from main: with the server-key rollout off, saves keep the row key they always wrote.

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	environments_repo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// serverKeyFlagsOff turns the hosted MCP server-key rollout explicitly off (phase A).
func serverKeyFlagsOff() feature.Provider {
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagHostedMCPServerKeyWrites, feature.AnyDistinctID, false)
	return flags
}

func userEnvConfigs(name string) []*types.McpEnvironmentConfigInput {
	return []*types.McpEnvironmentConfigInput{{
		VariableName:      name,
		HeaderDisplayName: new("API Key"),
		ProvidedBy:        "user",
	}}
}

func TestSetMcpMetadata_NewRowsKeepTheRequestedKeyFlagOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	ti.service.WithFeatureProvider(serverKeyFlagsOff())
	bySlug := createHostedToolset(t, ctx, ti, "off-hosted-new-slug")
	createCanonicalWrapper(t, ctx, ti, bySlug)
	byServer := createHostedToolset(t, ctx, ti, "off-hosted-new-server")
	createCanonicalWrapper(t, ctx, ti, byServer)

	slugRow := setBySlug(t, ctx, ti, bySlug, &gen.SetMcpMetadataPayload{Instructions: new("hello")})
	requireHostedIDs(t, ctx, ti, slugRow, bySlug.ID, false)

	serverRow, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{McpServerID: new(byServer.ID.String())})
	require.NoError(t, err)
	requireHostedIDs(t, ctx, ti, serverRow, byServer.ID, true)
}

func TestSetMcpMetadata_ToolsetKeyedRowUpdatesInPlaceFlagOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	ti.service.WithFeatureProvider(serverKeyFlagsOff())
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createHostedToolset(t, ctx, ti, "off-hosted-toolset-row")
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
	requireHostedIDs(t, ctx, ti, updated, toolset.ID, false)
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
	require.NotNil(t, viaServer.DefaultEnvironmentID)
	require.Equal(t, env.ID.String(), *viaServer.DefaultEnvironmentID)
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

func TestSetMcpMetadata_ServerKeyedRowFlagOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	ti.service.WithFeatureProvider(serverKeyFlagsOff())
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createHostedToolset(t, ctx, ti, "off-hosted-moved")
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
		requireHostedIDs(t, ctx, ti, got.Metadata, toolset.ID, true)
		require.Len(t, got.Metadata.EnvironmentConfigs, 1)
		require.Equal(t, original.EnvironmentConfigs[0].ID, got.Metadata.EnvironmentConfigs[0].ID)
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

	viaServer, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		McpServerID:  new(toolset.ID.String()),
		Instructions: new("via server"),
	})
	require.NoError(t, err)
	require.Equal(t, original.ID, viaServer.ID)
	requireHostedIDs(t, ctx, ti, viaServer, toolset.ID, true)
	require.Len(t, viaServer.EnvironmentConfigs, 1)
	requireSingleMetadataRow(t, ctx, ti, toolset.ID)

	// A toolset_slug write stays toolset-keyed so pods that read only toolset_id still see it.
	viaSlug := setBySlug(t, ctx, ti, toolset, &gen.SetMcpMetadataPayload{Instructions: new("after")})
	require.NotEqual(t, original.ID, viaSlug.ID)
	requireHostedIDs(t, ctx, ti, viaSlug, toolset.ID, false)
	require.Len(t, viaSlug.EnvironmentConfigs, 1)
	require.Equal(t, "API_KEY", viaSlug.EnvironmentConfigs[0].VariableName)
	require.Equal(t, "user", viaSlug.EnvironmentConfigs[0].ProvidedBy)

	// A dashboard read-modify-write echoes mcp_server_id alongside toolset_slug.
	echoed, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:  new(types.Slug(toolset.Slug)),
		McpServerID:  new(toolset.ID.String()),
		Instructions: new("echoed"),
	})
	require.NoError(t, err)
	require.Equal(t, viaSlug.ID, echoed.ID)

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
