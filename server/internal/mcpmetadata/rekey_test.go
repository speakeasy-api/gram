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
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// newServerKeyMCPMetadataService saves with the server-key rollout on (phase B).
func newServerKeyMCPMetadataService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()
	ctx, ti := newTestMCPMetadataService(t)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagHostedMCPServerKeyWrites, feature.AnyDistinctID, true)
	ti.service.WithFeatureProvider(flags)
	return ctx, ti
}

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

func setBySlug(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, payload *gen.SetMcpMetadataPayload) *types.McpMetadata {
	t.Helper()

	payload.ToolsetSlug = new(types.Slug(toolset.Slug))
	row, err := ti.service.SetMcpMetadata(ctx, payload)
	require.NoError(t, err)
	return row
}

// seedToolsetKeyedRow writes the row shape phase A saves left behind.
func seedToolsetKeyedRow(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, instructions string, defaultEnv uuid.NullUUID, variables ...string) repo.McpMetadatum {
	t.Helper()

	queries := repo.New(ti.conn)
	row, err := queries.UpsertMetadata(ctx, repo.UpsertMetadataParams{
		ToolsetID:                 uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID:                 toolset.ProjectID,
		ExternalDocumentationUrl:  pgtype.Text{String: "", Valid: false},
		ExternalDocumentationText: pgtype.Text{String: "", Valid: false},
		LogoID:                    uuid.NullUUID{},
		Instructions:              conv.ToPGText(instructions),
		DefaultEnvironmentID:      defaultEnv,
		InstallationOverrideUrl:   pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	for _, variable := range variables {
		_, err := queries.UpsertEnvironmentConfig(ctx, repo.UpsertEnvironmentConfigParams{
			ProjectID:         toolset.ProjectID,
			McpMetadataID:     row.ID,
			VariableName:      variable,
			HeaderDisplayName: conv.ToPGText("API Key"),
			ProvidedBy:        "user",
		})
		require.NoError(t, err)
	}
	return row
}

// seedServerKeyedTwin writes a server-keyed row beside a toolset-keyed one, as a rolling deploy can.
func seedServerKeyedTwin(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, instructions string, variables ...string) repo.McpMetadatum {
	t.Helper()

	queries := repo.New(ti.conn)
	row, err := queries.UpsertMetadataByMcpServerID(ctx, repo.UpsertMetadataByMcpServerIDParams{
		McpServerID:               uuid.NullUUID{UUID: toolset.ID, Valid: true},
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
	return row
}

func envConfigKeys(configs []*types.McpEnvironmentConfig) []string {
	keys := make([]string, 0, len(configs))
	for _, c := range configs {
		keys = append(keys, c.ID+"|"+c.VariableName+"|"+conv.PtrValOr(c.HeaderDisplayName, "")+"|"+c.ProvidedBy)
	}
	return keys
}

func createEnvironment(t *testing.T, ctx context.Context, ti *testInstance, slug string) environments_repo.Environment {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	env, err := environments_repo.New(ti.conn).CreateEnvironment(ctx, environments_repo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           slug,
		Slug:           slug,
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return env
}

func TestSetMcpMetadata_NewRowsUseTheServerKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newServerKeyMCPMetadataService(t)
	bySlug := createHostedToolset(t, ctx, ti, "hosted-new-slug")
	createCanonicalWrapper(t, ctx, ti, bySlug)
	byServer := createHostedToolset(t, ctx, ti, "hosted-new-server")
	createCanonicalWrapper(t, ctx, ti, byServer)
	unwrapped := createHostedToolset(t, ctx, ti, "hosted-new-unwrapped")

	slugRow := setBySlug(t, ctx, ti, bySlug, &gen.SetMcpMetadataPayload{Instructions: new("hello")})
	requireHostedIDs(t, ctx, ti, slugRow, bySlug.ID, true)

	serverRow, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{McpServerID: new(byServer.ID.String())})
	require.NoError(t, err)
	requireHostedIDs(t, ctx, ti, serverRow, byServer.ID, true)

	// A toolset without a canonical wrapper keeps the toolset key.
	legacyRow := setBySlug(t, ctx, ti, unwrapped, &gen.SetMcpMetadataPayload{Instructions: new("hello")})
	require.NotNil(t, legacyRow.ToolsetID)
	require.Equal(t, unwrapped.ID.String(), *legacyRow.ToolsetID)
	require.Nil(t, legacyRow.McpServerID)
}

func TestSetMcpMetadata_RekeysToolsetRowInPlace(t *testing.T) {
	t.Parallel()

	ctx, ti := newServerKeyMCPMetadataService(t)
	env := createEnvironment(t, ctx, ti, "rekey-env")
	defaultEnv := uuid.NullUUID{UUID: env.ID, Valid: true}

	for name, viaServer := range map[string]bool{"slug": false, "server": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			toolset := createHostedToolset(t, ctx, ti, "hosted-rekey-"+name)
			createCanonicalWrapper(t, ctx, ti, toolset)
			legacy := seedToolsetKeyedRow(t, ctx, ti, toolset, "before", defaultEnv, "API_KEY")
			rows, err := testrepo.New(ti.conn).SetMcpMetadataHeaderDisplayNamesFixture(ctx, testrepo.SetMcpMetadataHeaderDisplayNamesFixtureParams{
				HeaderDisplayNames: []byte(`{"OTHER_KEY":"Alias-Key"}`),
				ID:                 legacy.ID,
				ProjectID:          toolset.ProjectID,
			})
			require.NoError(t, err)
			require.EqualValues(t, 1, rows)
			before, err := ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{ToolsetSlug: new(types.Slug(toolset.Slug))})
			require.NoError(t, err)

			payload := &gen.SetMcpMetadataPayload{Instructions: new("after")}
			if viaServer {
				payload.McpServerID = new(toolset.ID.String())
			} else {
				payload.ToolsetSlug = new(types.Slug(toolset.Slug))
				payload.DefaultEnvironmentID = new(env.ID.String())
			}
			saved, err := ti.service.SetMcpMetadata(ctx, payload)
			require.NoError(t, err)

			require.Equal(t, legacy.ID.String(), saved.ID, "same row, re-keyed in place")
			requireHostedIDs(t, ctx, ti, saved, toolset.ID, true)
			require.NotNil(t, saved.DefaultEnvironmentID)
			require.Equal(t, env.ID.String(), *saved.DefaultEnvironmentID)
			require.Equal(t, envConfigKeys(before.Metadata.EnvironmentConfigs), envConfigKeys(saved.EnvironmentConfigs), "env configs and header aliases kept")
			requireSingleMetadataRow(t, ctx, ti, toolset.ID)

			for _, get := range []*gen.GetMcpMetadataPayload{
				{ToolsetSlug: new(types.Slug(toolset.Slug))},
				{McpServerID: new(toolset.ID.String())},
			} {
				got, err := ti.service.GetMcpMetadata(ctx, get)
				require.NoError(t, err)
				require.Equal(t, saved.ID, got.Metadata.ID)
				require.Equal(t, "after", *got.Metadata.Instructions)
			}
			for _, export := range []*gen.ExportMcpMetadataPayload{
				{McpSlug: new(types.Slug(toolset.McpSlug.String))},
				{McpServerID: new(toolset.ID.String())},
			} {
				got, err := ti.service.ExportMcpMetadata(ctx, export)
				require.NoError(t, err)
				require.Equal(t, "after", *got.Instructions)
			}
		})
	}
}

func TestSetMcpMetadata_ServerKeyedRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newServerKeyMCPMetadataService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createHostedToolset(t, ctx, ti, "hosted-moved")
	createCanonicalWrapper(t, ctx, ti, toolset)
	legacy := seedToolsetKeyedRow(t, ctx, ti, toolset, "before", uuid.NullUUID{}, "API_KEY")
	original, err := ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{ToolsetSlug: new(types.Slug(toolset.Slug))})
	require.NoError(t, err)
	require.Equal(t, legacy.ID.String(), original.Metadata.ID)
	rekeyToServer(t, ctx, ti, toolset)

	for _, payload := range []*gen.GetMcpMetadataPayload{
		{ToolsetSlug: new(types.Slug(toolset.Slug))},
		{McpServerID: new(toolset.ID.String())},
	} {
		got, err := ti.service.GetMcpMetadata(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, original.Metadata.ID, got.Metadata.ID)
		requireHostedIDs(t, ctx, ti, got.Metadata, toolset.ID, true)
		require.Len(t, got.Metadata.EnvironmentConfigs, 1)
		require.Equal(t, original.Metadata.EnvironmentConfigs[0].ID, got.Metadata.EnvironmentConfigs[0].ID)
	}

	// Runtime readers keyed by toolset id find the server-keyed row.
	queries := repo.New(ti.conn)
	row, err := queries.GetMetadataByBackendID(ctx, repo.GetMetadataByBackendIDParams{ProjectID: *authCtx.ProjectID, BackendID: toolset.ID})
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
	requireHostedIDs(t, ctx, ti, viaServer, toolset.ID, true)
	require.Len(t, viaServer.EnvironmentConfigs, 1)
	requireSingleMetadataRow(t, ctx, ti, toolset.ID)

	// A toolset_slug write updates the server-keyed row instead of adding a twin.
	viaSlug := setBySlug(t, ctx, ti, toolset, &gen.SetMcpMetadataPayload{Instructions: new("after")})
	require.Equal(t, original.Metadata.ID, viaSlug.ID)
	requireHostedIDs(t, ctx, ti, viaSlug, toolset.ID, true)
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

// A save collapses a twin the way the dependent move does: the toolset-keyed row, which every reader shows, wins.
func TestSetMcpMetadata_CollapsesTwinKeepingToolsetRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newServerKeyMCPMetadataService(t)
	for name, viaServer := range map[string]bool{"slug": false, "server": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			toolset := createHostedToolset(t, ctx, ti, "hosted-twin-"+name)
			createCanonicalWrapper(t, ctx, ti, toolset)
			toolsetRow := seedToolsetKeyedRow(t, ctx, ti, toolset, "toolset", uuid.NullUUID{}, "TOOLSET_KEY")
			seedServerKeyedTwin(t, ctx, ti, toolset, "server", "TWIN_KEY")

			for _, payload := range []*gen.GetMcpMetadataPayload{
				{ToolsetSlug: new(types.Slug(toolset.Slug))},
				{McpServerID: new(toolset.ID.String())},
			} {
				got, err := ti.service.GetMcpMetadata(ctx, payload)
				require.NoError(t, err)
				require.Equal(t, toolsetRow.ID.String(), got.Metadata.ID)
			}

			payload := &gen.SetMcpMetadataPayload{Instructions: new("updated")}
			if viaServer {
				payload.McpServerID = new(toolset.ID.String())
			} else {
				payload.ToolsetSlug = new(types.Slug(toolset.Slug))
			}
			updated, err := ti.service.SetMcpMetadata(ctx, payload)
			require.NoError(t, err)
			require.Equal(t, toolsetRow.ID.String(), updated.ID)
			requireHostedIDs(t, ctx, ti, updated, toolset.ID, true)
			require.Len(t, updated.EnvironmentConfigs, 1)
			require.Equal(t, "TOOLSET_KEY", updated.EnvironmentConfigs[0].VariableName)
			requireSingleMetadataRow(t, ctx, ti, toolset.ID)
		})
	}
}

func TestExportMcpMetadata_ByMcpServerID(t *testing.T) {
	t.Parallel()

	ctx, ti := newServerKeyMCPMetadataService(t)
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

func TestHeaderDisplayNames_PreferToolsetKeyedTwin(t *testing.T) {
	t.Parallel()

	ctx, ti := newServerKeyMCPMetadataService(t)
	toolset := createHostedToolset(t, ctx, ti, "hosted-twin-names")
	createCanonicalWrapper(t, ctx, ti, toolset)
	toolsetRow := seedToolsetKeyedRow(t, ctx, ti, toolset, "toolset", uuid.NullUUID{})

	queries := repo.New(ti.conn)
	serverRow, err := queries.UpsertMetadataByMcpServerID(ctx, repo.UpsertMetadataByMcpServerIDParams{
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

	fixtures := testrepo.New(ti.conn)
	for id, names := range map[string]string{
		toolsetRow.ID.String(): `{"API_KEY":"X-Toolset"}`,
		serverRow.ID.String():  `{"API_KEY":"X-Server"}`,
	} {
		rows, err := fixtures.SetMcpMetadataHeaderDisplayNamesFixture(ctx, testrepo.SetMcpMetadataHeaderDisplayNamesFixtureParams{
			HeaderDisplayNames: []byte(names),
			ID:                 uuid.MustParse(id),
			ProjectID:          toolset.ProjectID,
		})
		require.NoError(t, err)
		require.EqualValues(t, 1, rows)
	}

	single, err := queries.GetHeaderDisplayNames(ctx, repo.GetHeaderDisplayNamesParams{ProjectID: toolset.ProjectID, BackendID: toolset.ID})
	require.NoError(t, err)
	require.JSONEq(t, `{"API_KEY":"X-Toolset"}`, string(single))

	rows, err := queries.GetHeaderDisplayNamesByToolsetIDs(ctx, repo.GetHeaderDisplayNamesByToolsetIDsParams{ProjectID: toolset.ProjectID, ToolsetIds: []uuid.UUID{toolset.ID}})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	last := rows[len(rows)-1]
	require.Equal(t, toolset.ID, last.ToolsetID)
	require.JSONEq(t, `{"API_KEY":"X-Toolset"}`, string(last.HeaderDisplayNames))
}

func TestSetMcpMetadata_ServerKeyedSaveKeepsHeaderAliases(t *testing.T) {
	t.Parallel()

	ctx, ti := newServerKeyMCPMetadataService(t)
	toolset := createHostedToolset(t, ctx, ti, "hosted-save-aliases")
	createCanonicalWrapper(t, ctx, ti, toolset)
	original := seedToolsetKeyedRow(t, ctx, ti, toolset, "before", uuid.NullUUID{})

	rows, err := testrepo.New(ti.conn).SetMcpMetadataHeaderDisplayNamesFixture(ctx, testrepo.SetMcpMetadataHeaderDisplayNamesFixtureParams{
		HeaderDisplayNames: []byte(`{"API_KEY":"Alias-Key"}`),
		ID:                 original.ID,
		ProjectID:          toolset.ProjectID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	rekeyToServer(t, ctx, ti, toolset)

	viaSlug := setBySlug(t, ctx, ti, toolset, &gen.SetMcpMetadataPayload{Instructions: new("after")})
	require.Equal(t, original.ID.String(), viaSlug.ID)
	requireHostedIDs(t, ctx, ti, viaSlug, toolset.ID, true)

	queries := repo.New(ti.conn)
	after, err := queries.GetHeaderDisplayNames(ctx, repo.GetHeaderDisplayNamesParams{ProjectID: toolset.ProjectID, BackendID: toolset.ID})
	require.NoError(t, err)
	require.JSONEq(t, `{"API_KEY":"Alias-Key"}`, string(after))
	requireSingleMetadataRow(t, ctx, ti, toolset.ID)
}
