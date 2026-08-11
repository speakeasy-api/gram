package platformmcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestDistributionServiceAttachesAndRemovesOnlyWorkflowSelectedReadyMCP(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_distribution")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 5})
	require.NoError(t, err)
	request := registrationRequest(project, "distribution-fixture", "distribution-registration")
	receipt, err := store.BeginReceipt(ctx, principal, project, request, time.Now().UTC())
	require.NoError(t, err)
	receipt, err = store.ConvergeRegistration(ctx, principal, project, request, receipt)
	require.NoError(t, err)
	_, err = store.CompleteRegistrationWithRemoteURL(ctx, principal, project, request, receipt, "https://fixture.invalid/mcp")
	require.NoError(t, err)

	registration, err := platformrepo.New(conn).GetActivePlatformMCPCatalogRegistration(ctx, platformrepo.GetActivePlatformMCPCatalogRegistrationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		SourceKind:       request.SourceKind,
		CatalogProvider:  request.CatalogProvider,
		CatalogReference: request.CatalogReference,
	})
	require.NoError(t, err)
	require.True(t, registration.McpServerID.Valid)

	onboarding := NewOnboardingService(conn)
	_, err = onboarding.Start(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	_, err = onboarding.BindRegistration(ctx, principal.OrganizationID, principal.UserID, project.ID, registration.ID)
	require.NoError(t, err)

	defaultPlugin, err := pluginsrepo.New(conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)

	published := 0
	service := NewDistributionService(conn, nil, testExistingDefaultPluginAttacher(principal.OrganizationID), func(_ context.Context, _ uuid.UUID, _ string, _ string) error {
		published++
		return nil
	})
	_, err = service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: 0})
	require.ErrorIs(t, err, ErrDistributionNotReady)

	_, err = store.RecordReadiness(ctx, principal, ReadinessBinding{
		ProjectID:                        project.ID,
		RegistrationID:                   registration.ID,
		ProviderAuthorizationFingerprint: "fixture-readiness",
	}, ReadinessReady, "fixture", time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)

	attached, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: 0})
	require.NoError(t, err)
	require.Equal(t, distributionStateAttached, attached.State)
	require.EqualValues(t, 1, attached.Version)
	require.True(t, attached.AttachmentLive)
	require.Equal(t, publicationStateCurrent, attached.PublicationState)
	require.Equal(t, 1, published)

	live, err := pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID:    defaultPlugin.ID,
		McpServerID: registration.McpServerID,
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, live.ID)

	repeated, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: attached.Version})
	require.NoError(t, err)
	require.Equal(t, distributionStateAttached, repeated.State)
	require.EqualValues(t, 1, repeated.Version)
	require.True(t, repeated.AttachmentLive)

	removed, err := service.Remove(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: repeated.Version})
	require.NoError(t, err)
	require.Equal(t, distributionStateRemoved, removed.State)
	require.EqualValues(t, 2, removed.Version)
	require.False(t, removed.AttachmentLive)
	require.Equal(t, publicationStateCurrent, removed.PublicationState)
	require.Equal(t, 2, published)

	_, err = pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID:    defaultPlugin.ID,
		McpServerID: registration.McpServerID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	_, err = service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: attached.Version})
	require.ErrorIs(t, err, ErrDistributionConflict)

	redistributed, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: removed.Version})
	require.NoError(t, err)
	require.Equal(t, distributionStateAttached, redistributed.State)
	require.EqualValues(t, 3, redistributed.Version)
	require.Equal(t, publicationStateCurrent, redistributed.PublicationState)
	require.Equal(t, 3, published)
}

func TestDistributionServicePreservesAttachmentWhenPublicationFails(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_distribution_publication_failure")
	require.NoError(t, err)

	principal, project := seedReadyDistributionTarget(t, ctx, conn)
	publishErr := errors.New("local fixture publication failure")
	publish := func(context.Context, uuid.UUID, string, string) error { return publishErr }
	service := NewDistributionService(conn, nil, testExistingDefaultPluginAttacher(principal.OrganizationID), publish)

	distributed, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: 0})
	require.NoError(t, err)
	require.True(t, distributed.AttachmentLive)
	require.Equal(t, publicationStateRepairRequired, distributed.PublicationState)

	current, err := service.Current(ctx, principal, project.Slug)
	require.NoError(t, err)
	require.True(t, current.AttachmentLive, "publication must never roll back a committed attachment")
	require.Equal(t, publicationStateRepairRequired, current.PublicationState)

	publishErr = nil
	repaired, err := service.RepairPublication(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: distributed.Version})
	require.NoError(t, err)
	require.Equal(t, distributed.Version, repaired.Version, "publication repair must not mutate distribution concurrency")
	require.True(t, repaired.AttachmentLive, "publication repair must preserve the committed attachment")
	require.Equal(t, publicationStateCurrent, repaired.PublicationState)
}

func seedReadyDistributionTarget(t *testing.T, ctx context.Context, conn *pgxpool.Pool) (Principal, ResolvedProject) {
	t.Helper()

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 5})
	require.NoError(t, err)
	request := registrationRequest(project, "distribution-publisher-fixture", "distribution-publisher-registration")
	receipt, err := store.BeginReceipt(ctx, principal, project, request, time.Now().UTC())
	require.NoError(t, err)
	receipt, err = store.ConvergeRegistration(ctx, principal, project, request, receipt)
	require.NoError(t, err)
	_, err = store.CompleteRegistrationWithRemoteURL(ctx, principal, project, request, receipt, "https://fixture.invalid/mcp")
	require.NoError(t, err)
	registration, err := platformrepo.New(conn).GetActivePlatformMCPCatalogRegistration(ctx, platformrepo.GetActivePlatformMCPCatalogRegistrationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		SourceKind:       request.SourceKind,
		CatalogProvider:  request.CatalogProvider,
		CatalogReference: request.CatalogReference,
	})
	require.NoError(t, err)
	onboarding := NewOnboardingService(conn)
	_, err = onboarding.Start(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	_, err = onboarding.BindRegistration(ctx, principal.OrganizationID, principal.UserID, project.ID, registration.ID)
	require.NoError(t, err)
	_, err = pluginsrepo.New(conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID})
	require.NoError(t, err)
	_, err = store.RecordReadiness(ctx, principal, ReadinessBinding{ProjectID: project.ID, RegistrationID: registration.ID, ProviderAuthorizationFingerprint: "fixture-readiness"}, ReadinessReady, "fixture", time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)
	return principal, project
}

func testExistingDefaultPluginAttacher(organizationID string) ExistingDefaultPluginAttacher {
	return func(ctx context.Context, tx pgx.Tx, _ *contextvalues.AuthContext, _ string, projectID, mcpServerID uuid.UUID, displayName string) (uuid.UUID, bool, error) {
		plugin, err := pluginsrepo.New(tx).GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{
			OrganizationID: organizationID,
			ProjectID:      projectID,
		})
		if err != nil {
			return uuid.Nil, false, err
		}
		server, err := pluginsrepo.New(tx).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
			PluginID:    plugin.ID,
			McpServerID: uuid.NullUUID{UUID: mcpServerID, Valid: true},
			DisplayName: displayName,
			Policy:      "required",
			SortOrder:   0,
		})
		if err != nil {
			return uuid.Nil, false, err
		}
		return server.ID, true, nil
	}
}
