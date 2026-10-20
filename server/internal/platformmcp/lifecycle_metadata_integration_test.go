package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const lifecycleMetadataTestKey = "platform-mcp-lifecycle-metadata-test-key"

// newLifecycleMetadataTestService composes the service the way the server
// does: renames through the mcpservers command, install pages through the
// mcpmetadata commands.
func newLifecycleMetadataTestService(t *testing.T, conn *pgxpool.Pool) *LifecycleMetadataService {
	t.Helper()
	auditLogger := audit.NewLogger()
	service, err := NewLifecycleMetadataService(conn, func(ctx context.Context, tx pgx.Tx, existing mcpserversrepo.McpServer, input LifecycleMetadataUpdate) (mcpserversrepo.McpServer, error) {
		name := input.Name
		return mcpservers.UpdateMCPServerLifecycleInTransaction(ctx, tx, auditLogger, existing, mcpservers.LifecycleUpdateInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, ActorUserID: input.ActorUserID, ActorEmail: nil, ServerID: input.ServerID,
			Name: &name, Visibility: existing.Visibility, EnvironmentID: existing.EnvironmentID, UserSessionIssuerID: existing.UserSessionIssuerID,
			RemoteMcpServerID: existing.RemoteMcpServerID, TunneledMcpServerID: existing.TunneledMcpServerID, ToolsetID: existing.ToolsetID,
			UnproxiedMcpServerID: existing.UnproxiedMcpServerID, ToolVariationsGroupID: existing.ToolVariationsGroupID,
		})
	}, LifecycleInstallPageStore{
		Lock: func(ctx context.Context, tx pgx.Tx, server mcpserversrepo.McpServer) (LifecycleInstallPage, error) {
			page, err := mcpmetadata.LockMCPServerInstallPageInTransaction(ctx, tx, server)
			return LifecycleInstallPageFromColumns(page.LogoAssetID, page.ExternalDocumentationURL, page.ExternalDocumentationText, page.Instructions), err
		},
		Patch: func(ctx context.Context, tx pgx.Tx, server mcpserversrepo.McpServer, patch LifecycleInstallPagePatch) (LifecycleInstallPage, error) {
			page, err := mcpmetadata.PatchMCPServerInstallPageInTransaction(ctx, tx, auditLogger, server, mcpmetadata.InstallPagePatch{
				OrganizationID: patch.OrganizationID, ProjectID: patch.ProjectID, ActorUserID: patch.ActorUserID, ActorEmail: nil,
				LogoAssetID: patch.LogoAssetID, ExternalDocumentationURL: patch.ExternalDocumentationURL,
				ExternalDocumentationText: patch.ExternalDocumentationText, Instructions: patch.Instructions,
			})
			if err != nil {
				switch {
				case errors.Is(err, mcpmetadata.ErrInstallPageLogoInvalid):
					return LifecycleInstallPage{}, ErrLifecycleMetadataLogoInvalid
				case errors.Is(err, mcpmetadata.ErrInstallPageInherited):
					return LifecycleInstallPage{}, ErrLifecycleMetadataInherited
				}
				return LifecycleInstallPage{}, fmt.Errorf("patch install page: %w", err)
			}
			return LifecycleInstallPageFromColumns(page.LogoAssetID, page.ExternalDocumentationURL, page.ExternalDocumentationText, page.Instructions), nil
		},
	}, lifecycleMetadataTestKey)
	require.NoError(t, err)
	return service
}

// getMCPDetail reads the server the way get_mcp does for an administrator, so
// the version under test is the one callers actually hold.
func getMCPDetail(t *testing.T, ctx context.Context, conn *pgxpool.Pool, principal Principal, project ResolvedProject, mcpID uuid.UUID) MCP {
	t.Helper()
	reader := NewPostgresReader(testenv.NewLogger(t), conn)
	reader.setInventoryCursorKey(lifecycleMetadataTestKey)
	mcp, err := reader.getMCPInventory(ctx, principal, project.ID, mcpID, true, "")
	require.NoError(t, err)
	require.NotEmpty(t, mcp.Version)
	require.NotNil(t, mcp.InstallPage)
	return mcp
}

func cohortServerID(t *testing.T, ctx context.Context, conn *pgxpool.Pool, principal Principal, project ResolvedProject) uuid.UUID {
	t.Helper()
	rows, err := platformrepo.New(conn).ListPlatformMCPInventory(ctx, platformrepo.ListPlatformMCPInventoryParams{
		OrganizationID: principal.OrganizationID, SkipAuthorizationFilter: true,
		ConnectionID:         uuid.NullUUID{UUID: connectionIDFromPrincipal(t, principal), Valid: true},
		ConnectionGeneration: uuid.NullUUID{UUID: connectionIDFromPrincipalGeneration(t, principal), Valid: true},
		ProjectID:            uuid.NullUUID{UUID: project.ID, Valid: true}, QueryText: "Registration cohort server", LimitValue: 10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, uuid.Nil, rows[0].RegistrationID, "the cohort server was added from the dashboard, not by this platform")
	return rows[0].McpServerID
}

func organizationImage(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) string {
	t.Helper()
	asset, err := assetsrepo.New(conn).CreateOrganizationAsset(ctx, assetsrepo.CreateOrganizationAssetParams{
		Name: "logo.png", Url: "file:///logo-" + uuid.NewString() + ".png", OrganizationID: organizationID, Sha256: uuid.NewString(),
		Kind: "image", ContentType: "image/png", ContentLength: 4,
	})
	require.NoError(t, err)
	return asset.ID.String()
}

func TestUpdateMCPMetadataPatchesInstallPageOfAnyServer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_metadata_install_page")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := newLifecycleMetadataTestService(t, conn)
	mcpID := cohortServerID(t, ctx, conn, principal, project)
	logo := organizationImage(t, ctx, conn, principal.OrganizationID)
	before, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionMCPMetadataUpdate)
	require.NoError(t, err)

	detail := getMCPDetail(t, ctx, conn, principal, project, mcpID)
	require.Equal(t, MCPInstallPage{}, *detail.InstallPage)
	input := UpdateMCPMetadataInput{
		ProjectSlug: project.Slug, MCPID: mcpID.String(),
		LogoAssetID: &logo, ExternalDocumentationURL: new("https://docs.example.test/cohort"),
		ExternalDocumentationText: new("Cohort guide"), Instructions: new("Ask before writing."),
		ExpectedVersion: detail.Version, IdempotencyKey: "install-page-set",
	}
	result, err := service.Update(ctx, principal, input)
	require.NoError(t, err)
	require.Empty(t, result.RegistrationID, "an install-page patch is not bound to a registration")
	require.Equal(t, "Registration cohort server", result.Name)
	require.Equal(t, logo, *result.InstallPage.LogoAssetID)
	detail = getMCPDetail(t, ctx, conn, principal, project, mcpID)
	require.Equal(t, result.Version, detail.Version, "get_mcp hands back the version the update returned")
	require.Equal(t, MCPInstallPage{LogoAssetID: logo, ExternalDocumentationURL: "https://docs.example.test/cohort", ExternalDocumentationText: "Cohort guide", Instructions: "Ask before writing."}, *detail.InstallPage)

	// Idempotent replay returns the same receipt; the same key with another
	// input is refused.
	replay, err := service.Update(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replay.Receipt.Replayed)
	require.Equal(t, result.Receipt.ID, replay.Receipt.ID)
	require.Equal(t, result.Version, replay.Version)
	changed := input
	changed.Instructions = new("Something else.")
	_, err = service.Update(ctx, principal, changed)
	require.ErrorIs(t, err, ErrRegistrationConflict)

	// Omitted fields stay; an empty string clears.
	cleared, err := service.Update(ctx, principal, UpdateMCPMetadataInput{
		ProjectSlug: project.Slug, MCPID: mcpID.String(), ExternalDocumentationURL: new(""),
		ExpectedVersion: detail.Version, IdempotencyKey: "install-page-clear",
	})
	require.NoError(t, err)
	require.Nil(t, cleared.InstallPage.ExternalDocumentationURL)
	require.Equal(t, logo, *cleared.InstallPage.LogoAssetID)
	require.Equal(t, "Cohort guide", *cleared.InstallPage.ExternalDocumentationText)
	require.Equal(t, "Ask before writing.", *cleared.InstallPage.Instructions)
	require.NotEqual(t, detail.Version, cleared.Version)
	require.Equal(t, cleared.Version, getMCPDetail(t, ctx, conn, principal, project, mcpID).Version)

	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionMCPMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, before+2, after, "each applied patch is audited once; a replay is not")
}

func TestUpdateMCPMetadataRefusals(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_metadata_refusals")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := newLifecycleMetadataTestService(t, conn)
	mcpID := cohortServerID(t, ctx, conn, principal, project)
	version := getMCPDetail(t, ctx, conn, principal, project, mcpID).Version

	foreignOrganization := "org_" + uuid.NewString()
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID: foreignOrganization, Name: "Another organization", Slug: "another-" + uuid.NewString()[:8], WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	foreignLogo := organizationImage(t, ctx, conn, foreignOrganization)

	for name, tc := range map[string]struct {
		input UpdateMCPMetadataInput
		err   error
	}{
		"rename without a registration": {UpdateMCPMetadataInput{Name: new("Renamed")}, ErrLifecycleMetadataRenameUnsupported},
		"rename of a dashboard server":   {UpdateMCPMetadataInput{Name: new("Renamed"), RegistrationID: uuid.NewString()}, ErrLifecycleMetadataRenameUnsupported},
		"another organization's logo":    {UpdateMCPMetadataInput{LogoAssetID: &foreignLogo}, ErrLifecycleMetadataLogoInvalid},
		"an unknown logo":                {UpdateMCPMetadataInput{LogoAssetID: new(uuid.NewString())}, ErrLifecycleMetadataLogoInvalid},
		"nothing to change":              {UpdateMCPMetadataInput{}, ErrLifecycleMetadataInvalid},
	} {
		tc.input.ProjectSlug, tc.input.MCPID, tc.input.ExpectedVersion, tc.input.IdempotencyKey = project.Slug, mcpID.String(), version, "refusal-"+uuid.NewString()
		_, err := service.Update(ctx, principal, tc.input)
		require.ErrorIs(t, err, tc.err, name)
	}
	require.Equal(t, version, getMCPDetail(t, ctx, conn, principal, project, mcpID).Version, "no refusal changed the server")

	// A logo changed elsewhere after the caller read the server makes its
	// version stale.
	tx := testenv.BeginTx(t, ctx, conn)
	server, err := mcpserversrepo.New(tx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{ID: mcpID, ProjectID: project.ID})
	require.NoError(t, err)
	_, err = mcpmetadata.PatchMCPServerInstallPageInTransaction(ctx, tx, audit.NewLogger(), server, mcpmetadata.InstallPagePatch{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, ActorUserID: principal.UserID, ActorEmail: nil,
		LogoAssetID: new(organizationImage(t, ctx, conn, principal.OrganizationID)),
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	_, err = service.Update(ctx, principal, UpdateMCPMetadataInput{
		ProjectSlug: project.Slug, MCPID: mcpID.String(), Instructions: new("Stale"),
		ExpectedVersion: version, IdempotencyKey: "stale-version",
	})
	require.ErrorIs(t, err, ErrRegistrationConflict)
}

func TestUpdateMCPMetadataRenamesPlatformRegistration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_metadata_rename")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	request := registrationRequest(project, "metadata-rename", "metadata-rename-key")
	receipt, err := store.BeginReceipt(ctx, principal, project, request, time.Now().UTC())
	require.NoError(t, err)
	receipt, err = store.ConvergeRegistration(ctx, principal, project, request, receipt)
	require.NoError(t, err)
	receipt, err = store.CompleteRegistrationWithRemoteURL(ctx, principal, project, request, receipt, "https://reviewed.example.test/metadata-rename")
	require.NoError(t, err)
	registration, err := lifecycleRegistration(ctx, platformrepo.New(conn), principal, project.ID, receipt.RegistrationID.UUID)
	require.NoError(t, err)
	mcpID := registration.McpServerID.UUID
	service := newLifecycleMetadataTestService(t, conn)
	logo := organizationImage(t, ctx, conn, principal.OrganizationID)

	result, err := service.Update(ctx, principal, UpdateMCPMetadataInput{
		ProjectSlug: project.Slug, RegistrationID: registration.ID.String(), MCPID: mcpID.String(),
		Name: new("  Renamed server "), LogoAssetID: &logo,
		ExpectedVersion: getMCPDetail(t, ctx, conn, principal, project, mcpID).Version, IdempotencyKey: "rename-and-logo",
	})
	require.NoError(t, err)
	require.Equal(t, "Renamed server", result.Name)
	require.Equal(t, registration.ID.String(), result.RegistrationID)
	require.Equal(t, logo, *result.InstallPage.LogoAssetID)
	detail := getMCPDetail(t, ctx, conn, principal, project, mcpID)
	require.Equal(t, "Renamed server", detail.Name)
	require.Equal(t, result.Version, detail.Version)
}
