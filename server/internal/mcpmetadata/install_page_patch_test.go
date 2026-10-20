package mcpmetadata_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func installPagePatch(ctx context.Context, t *testing.T) mcpmetadata.InstallPagePatch {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return mcpmetadata.InstallPagePatch{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ActorUserID:    authCtx.UserID,
		ActorEmail:     nil,
	}
}

func patchInstallPage(ctx context.Context, t *testing.T, ti *testInstance, server mcpservers_repo.McpServer, patch mcpmetadata.InstallPagePatch) (mcpmetadata.InstallPage, error) {
	t.Helper()
	tx := testenv.BeginTx(t, ctx, ti.conn)
	locked, err := mcpservers_repo.New(tx).LockMCPServerByIDAndProjectID(ctx, mcpservers_repo.LockMCPServerByIDAndProjectIDParams{ID: server.ID, ProjectID: server.ProjectID})
	require.NoError(t, err)
	page, err := mcpmetadata.PatchMCPServerInstallPageInTransaction(ctx, tx, audit.NewLogger(), locked, patch)
	if err != nil {
		// Release the locks now: the next patch in the same test takes them.
		require.NoError(t, tx.Rollback(ctx))
		return page, fmt.Errorf("patch install page: %w", err)
	}
	require.NoError(t, tx.Commit(ctx))
	return page, nil
}

func createRemoteMcpServer(t *testing.T, ctx context.Context, ti *testInstance) uuid.NullUUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	remote := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcp_repo.CreateServerParams{
		ProjectID:     *authCtx.ProjectID,
		TransportType: "streamable-http",
		Url:           "https://upstream.example.com/mcp",
	})
	return uuid.NullUUID{UUID: remote.ID, Valid: true}
}

func createOrganizationImage(ctx context.Context, t *testing.T, ti *testInstance, organizationID, name string) uuid.UUID {
	t.Helper()
	asset, err := assetsrepo.New(ti.conn).CreateOrganizationAsset(ctx, assetsrepo.CreateOrganizationAssetParams{
		Name: name, Url: "file:///" + name, OrganizationID: organizationID, Sha256: uuid.NewString(),
		Kind: "image", ContentType: "image/png", ContentLength: 4,
	})
	require.NoError(t, err)
	return asset.ID
}

func TestPatchMCPServerInstallPage_PatchSemantics(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)
	server, _ := createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{remoteMcpServerID: createRemoteMcpServer(t, ctx, ti)})
	base := installPagePatch(ctx, t)

	// A dashboard save left settings this path must never touch.
	seeded, err := repo.New(ti.conn).UpsertMetadataByMcpServerID(ctx, repo.UpsertMetadataByMcpServerIDParams{
		McpServerID: uuid.NullUUID{UUID: server.ID, Valid: true}, ProjectID: server.ProjectID,
		ExternalDocumentationUrl: conv.ToPGText("https://docs.example.test"), ExternalDocumentationText: conv.ToPGText("Docs"),
		LogoID: uuid.NullUUID{}, Instructions: conv.ToPGText("Original"),
		DefaultEnvironmentID: uuid.NullUUID{}, InstallationOverrideUrl: conv.ToPGText("https://install.example.test"),
	})
	require.NoError(t, err)
	_, err = repo.New(ti.conn).UpsertEnvironmentConfig(ctx, repo.UpsertEnvironmentConfigParams{
		ProjectID: server.ProjectID, McpMetadataID: seeded.ID, VariableName: "API_KEY", HeaderDisplayName: conv.ToPGText("Key"), ProvidedBy: "user",
	})
	require.NoError(t, err)

	logo := createOrganizationImage(ctx, t, ti, base.OrganizationID, "logo.png")
	patch := base
	patch.LogoAssetID = new(logo.String())
	page, err := patchInstallPage(ctx, t, ti, server, patch)
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: logo, Valid: true}, page.LogoAssetID)
	require.Equal(t, "https://docs.example.test", page.ExternalDocumentationURL.String, "an omitted field is left unchanged")
	require.Equal(t, "Original", page.Instructions.String)

	patch = base
	patch.ExternalDocumentationURL = new("")
	patch.Instructions = new("Updated")
	page, err = patchInstallPage(ctx, t, ti, server, patch)
	require.NoError(t, err)
	require.False(t, page.ExternalDocumentationURL.Valid, "an empty string clears the field")
	require.Equal(t, "Updated", page.Instructions.String)
	require.Equal(t, uuid.NullUUID{UUID: logo, Valid: true}, page.LogoAssetID)
	require.Equal(t, "Docs", page.ExternalDocumentationText.String)

	row, err := repo.New(ti.conn).GetMetadataByBackendID(ctx, repo.GetMetadataByBackendIDParams{ProjectID: server.ProjectID, BackendID: server.ID})
	require.NoError(t, err)
	require.Equal(t, seeded.ID, row.ID, "the existing row is patched in place")
	require.Equal(t, "https://install.example.test", row.InstallationOverrideUrl.String)
	configs, err := repo.New(ti.conn).ListEnvironmentConfigs(ctx, row.ID)
	require.NoError(t, err)
	require.Len(t, configs, 1, "environment configs are never touched")

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMCPMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, base.OrganizationID, record.OrganizationID)
}

func TestPatchMCPServerInstallPage_NewRowsAreServerKeyed(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)
	server, _ := createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{remoteMcpServerID: createRemoteMcpServer(t, ctx, ti)})
	base := installPagePatch(ctx, t)

	patch := base
	patch.Instructions = new("")
	_, err := patchInstallPage(ctx, t, ti, server, patch)
	require.NoError(t, err)
	_, err = repo.New(ti.conn).GetMetadataByBackendID(ctx, repo.GetMetadataByBackendIDParams{ProjectID: server.ProjectID, BackendID: server.ID})
	require.ErrorIs(t, err, pgx.ErrNoRows, "clearing what was never set creates no row")

	patch = base
	patch.ExternalDocumentationText = new("Read the guide")
	page, err := patchInstallPage(ctx, t, ti, server, patch)
	require.NoError(t, err)
	require.Equal(t, "Read the guide", page.ExternalDocumentationText.String)
	row, err := repo.New(ti.conn).GetMetadataByBackendID(ctx, repo.GetMetadataByBackendIDParams{ProjectID: server.ProjectID, BackendID: server.ID})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: server.ID, Valid: true}, row.McpServerID)
	require.False(t, row.ToolsetID.Valid)
}

func TestPatchMCPServerInstallPage_PatchesTheToolsetKeyedRowReadsShow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)
	toolset := createHostedToolset(t, ctx, ti, "patch-hosted-"+uuid.NewString()[:8])
	wrapper := createCanonicalWrapper(t, ctx, ti, toolset)
	toolsetRow := seedToolsetKeyedRow(t, ctx, ti, toolset, "Toolset instructions", uuid.NullUUID{}, "API_KEY")
	twin := seedServerKeyedTwin(t, ctx, ti, toolset, "Twin instructions")

	patch := installPagePatch(ctx, t)
	patch.ExternalDocumentationURL = new("https://docs.example.test/hosted")
	page, err := patchInstallPage(ctx, t, ti, wrapper, patch)
	require.NoError(t, err)
	require.Equal(t, "Toolset instructions", page.Instructions.String, "the winning toolset-keyed row is the one patched")

	row, err := repo.New(ti.conn).GetMetadataByBackendID(ctx, repo.GetMetadataByBackendIDParams{ProjectID: toolset.ProjectID, BackendID: toolset.ID})
	require.NoError(t, err)
	require.Equal(t, toolsetRow.ID, row.ID, "a patch never rekeys")
	require.Equal(t, "https://docs.example.test/hosted", row.ExternalDocumentationUrl.String)
	rows, err := repo.New(ti.conn).LockMetadataByBackendID(ctx, repo.LockMetadataByBackendIDParams{ProjectID: toolset.ProjectID, BackendID: toolset.ID})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, candidate := range rows {
		if candidate.ID == twin.ID {
			require.False(t, candidate.ExternalDocumentationUrl.Valid, "the losing twin is left alone")
		}
	}
}

func TestPatchMCPServerInstallPage_RefusesInheritedToolsetSettings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)
	toolset := createTestToolset(t, ctx, ti, "patch-inherited-"+uuid.NewString()[:8])
	server, _ := createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{toolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}})

	patch := installPagePatch(ctx, t)
	patch.Instructions = new("Server instructions")
	_, err := patchInstallPage(ctx, t, ti, server, patch)
	require.NoError(t, err, "a toolset without settings leaves nothing to inherit")

	other := createTestToolset(t, ctx, ti, "patch-inherited-other-"+uuid.NewString()[:8])
	seedToolsetKeyedRow(t, ctx, ti, other, "Inherited", uuid.NullUUID{})
	inheriting, _ := createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{toolsetID: uuid.NullUUID{UUID: other.ID, Valid: true}})
	_, err = patchInstallPage(ctx, t, ti, inheriting, patch)
	require.ErrorIs(t, err, mcpmetadata.ErrInstallPageInherited)
}

func TestPatchMCPServerInstallPage_LogoMustBeAnImageOfTheOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)
	server, _ := createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{remoteMcpServerID: createRemoteMcpServer(t, ctx, ti)})
	base := installPagePatch(ctx, t)

	foreignOrganization := "org_" + uuid.NewString()
	_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: foreignOrganization, Name: "Other organization", Slug: "other-" + uuid.NewString()[:8], WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	foreign := createOrganizationImage(ctx, t, ti, foreignOrganization, "foreign.png")
	document, err := assetsrepo.New(ti.conn).CreateOrganizationAsset(ctx, assetsrepo.CreateOrganizationAssetParams{
		Name: "spec.yaml", Url: "file:///spec.yaml", OrganizationID: base.OrganizationID, Sha256: uuid.NewString(),
		Kind: "openapiv3", ContentType: "application/yaml", ContentLength: 4,
	})
	require.NoError(t, err)
	projectImage, err := assetsrepo.New(ti.conn).CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name: "project.png", Url: "file:///project.png", ProjectID: server.ProjectID, OrganizationID: base.OrganizationID,
		Sha256: uuid.NewString(), Kind: "image", ContentType: "image/png", ContentLength: 4,
	})
	require.NoError(t, err)

	for name, tc := range map[string]struct {
		logo string
		err  error
	}{
		"another organization's image": {foreign.String(), mcpmetadata.ErrInstallPageLogoInvalid},
		"an unknown asset":             {uuid.NewString(), mcpmetadata.ErrInstallPageLogoInvalid},
		"not an image":                 {document.ID.String(), mcpmetadata.ErrInstallPageLogoInvalid},
		"not a UUID":                   {"logo.png", mcpmetadata.ErrInstallPageLogoInvalid},
		"a project image":              {projectImage.ID.String(), nil},
	} {
		patch := base
		patch.LogoAssetID = new(tc.logo)
		_, err := patchInstallPage(ctx, t, ti, server, patch)
		if tc.err == nil {
			require.NoError(t, err, name)
			continue
		}
		require.ErrorIs(t, err, tc.err, name)
	}
}
