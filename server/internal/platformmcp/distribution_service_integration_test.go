//nolint:wrapcheck // Integration assertions intentionally return test setup errors directly.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
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
	service := NewDistributionService(conn, nil, testExistingPluginAttacher(), func(_ context.Context, _ uuid.UUID, _ string, _ string) error {
		published++
		return nil
	}, testPluginTargets(conn))
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

	connectionID := connectionIDFromPrincipal(t, principal)
	newGeneration := uuid.New()
	_, err = platformrepo.New(conn).RotatePlatformMCPConnectionGeneration(ctx, platformrepo.RotatePlatformMCPConnectionGenerationParams{
		ActiveGeneration:       newGeneration,
		ReauthorizedAt:         timestamp(time.Now().UTC()),
		AuthorizationExpiresAt: timestamp(time.Now().UTC().Add(90 * 24 * time.Hour)),
		ConnectionID:           connectionID,
		OrganizationID:         principal.OrganizationID,
	})
	require.NoError(t, err)
	principal.Generation = newGeneration.String()
	_, err = store.RecordReadiness(ctx, principal, ReadinessBinding{
		ProjectID:                        project.ID,
		RegistrationID:                   registration.ID,
		ProviderAuthorizationFingerprint: "fixture-readiness-reauthorized",
	}, ReadinessReady, "fixture", time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)

	reauthorized, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: repeated.Version})
	require.NoError(t, err)
	require.EqualValues(t, 2, reauthorized.Version, "a reauthorized connection must own current distribution state")
	require.True(t, reauthorized.AttachmentLive)

	removed, err := service.Remove(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: reauthorized.Version})
	require.NoError(t, err)
	require.Equal(t, distributionStateRemoved, removed.State)
	require.EqualValues(t, 3, removed.Version)
	require.False(t, removed.AttachmentLive)
	require.Equal(t, publicationStateCurrent, removed.PublicationState)
	require.Equal(t, 3, published)

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
	require.EqualValues(t, 4, redistributed.Version)
	require.Equal(t, publicationStateCurrent, redistributed.PublicationState)
	require.Equal(t, 4, published)
}

func TestDistributionServiceTargetsTheNamedPluginAndRefusesAnUnmatchedOne(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_distribution_named_plugin")
	require.NoError(t, err)

	principal, project := seedReadyDistributionTarget(t, ctx, conn)
	target, err := platformrepo.New(conn).GetPlatformMCPOnboardingDistributionTarget(ctx, platformrepo.GetPlatformMCPOnboardingDistributionTargetParams{
		OrganizationID:       principal.OrganizationID,
		InitiatingSubjectUrn: userSubjectURN(principal.UserID),
	})
	require.NoError(t, err)
	require.True(t, target.McpServerID.Valid)

	defaultPlugin, err := pluginsrepo.New(conn).GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID})
	require.NoError(t, err)
	marketing, err := pluginsrepo.New(conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
		Name:           "Marketing Tools",
		Slug:           "marketing",
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)

	service := NewDistributionService(conn, nil, testExistingPluginAttacher(), func(context.Context, uuid.UUID, string, string) error { return nil }, testPluginTargets(conn))

	// A plugin nobody has is refused rather than redirected to the default,
	// which is the whole point of naming a target.
	_, err = service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: "sales", ExpectedVersion: 0})
	require.ErrorIs(t, err, ErrPluginNotFound)

	distributed, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: "marketing", ExpectedVersion: 0})
	require.NoError(t, err)
	require.True(t, distributed.AttachmentLive)
	require.Equal(t, "Marketing Tools", distributed.Plugin)

	live, err := pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID:    marketing.ID,
		McpServerID: target.McpServerID,
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, live.ID)

	// The default plugin is untouched: naming a plugin distributes there and
	// nowhere else.
	_, err = pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID:    defaultPlugin.ID,
		McpServerID: target.McpServerID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	removed, err := service.Remove(ctx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: "Marketing Tools", ExpectedVersion: distributed.Version})
	require.NoError(t, err)
	require.False(t, removed.AttachmentLive)

	_, err = pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID:    marketing.ID,
		McpServerID: target.McpServerID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestDistributionServicePreservesAdminReplacementOnRemoval(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_distribution_replacement")
	require.NoError(t, err)

	principal, project := seedReadyDistributionTarget(t, ctx, conn)
	target, err := platformrepo.New(conn).GetPlatformMCPOnboardingDistributionTarget(ctx, platformrepo.GetPlatformMCPOnboardingDistributionTargetParams{
		OrganizationID:       principal.OrganizationID,
		InitiatingSubjectUrn: userSubjectURN(principal.UserID),
	})
	require.NoError(t, err)
	require.True(t, target.McpServerID.Valid)

	plugin, err := pluginsrepo.New(conn).GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID})
	require.NoError(t, err)
	service := NewDistributionService(conn, nil, testExistingPluginAttacher(), func(context.Context, uuid.UUID, string, string) error { return nil }, testPluginTargets(conn))
	distributed, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: 0})
	require.NoError(t, err)

	created, err := pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{PluginID: plugin.ID, McpServerID: target.McpServerID})
	require.NoError(t, err)
	_, err = pluginsrepo.New(conn).RemovePluginServer(ctx, pluginsrepo.RemovePluginServerParams{ID: created.ID, PluginID: plugin.ID})
	require.NoError(t, err)
	replacement, err := pluginsrepo.New(conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID:    plugin.ID,
		McpServerID: target.McpServerID,
		DisplayName: "Admin-managed replacement",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	redistributed, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: distributed.Version})
	require.NoError(t, err)
	require.Equal(t, distributionStateAttached, redistributed.State)

	removed, err := service.Remove(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: redistributed.Version})
	require.NoError(t, err)
	require.Equal(t, distributionStateRemoved, removed.State)
	require.True(t, removed.AttachmentLive, "removing onboarding state must not delete an administrator replacement")

	live, err := pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{PluginID: plugin.ID, McpServerID: target.McpServerID})
	require.NoError(t, err)
	require.Equal(t, replacement.ID, live.ID)
}

func TestDistributionServicePreservesPreexistingAttachmentOnRemoval(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_distribution_preexisting_attachment")
	require.NoError(t, err)

	principal, project := seedReadyDistributionTarget(t, ctx, conn)
	target, err := platformrepo.New(conn).GetPlatformMCPOnboardingDistributionTarget(ctx, platformrepo.GetPlatformMCPOnboardingDistributionTargetParams{
		OrganizationID:       principal.OrganizationID,
		InitiatingSubjectUrn: userSubjectURN(principal.UserID),
	})
	require.NoError(t, err)
	require.True(t, target.McpServerID.Valid)

	plugin, err := pluginsrepo.New(conn).GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID})
	require.NoError(t, err)
	preexisting, err := pluginsrepo.New(conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID:    plugin.ID,
		McpServerID: target.McpServerID,
		DisplayName: "Admin-managed MCP",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	service := NewDistributionService(conn, nil, testExistingPluginAttacher(), func(context.Context, uuid.UUID, string, string) error { return nil }, testPluginTargets(conn))
	distributed, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: 0})
	require.NoError(t, err)
	require.True(t, distributed.AttachmentLive)

	removed, err := service.Remove(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: distributed.Version})
	require.NoError(t, err)
	require.Equal(t, distributionStateRemoved, removed.State)
	require.True(t, removed.AttachmentLive, "removing onboarding state must not delete a pre-existing plugin attachment")

	live, err := pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{PluginID: plugin.ID, McpServerID: target.McpServerID})
	require.NoError(t, err)
	require.Equal(t, preexisting.ID, live.ID)
}

func TestDistributionServicePreservesAttachmentWhenPublicationFails(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_distribution_publication_failure")
	require.NoError(t, err)

	principal, project := seedReadyDistributionTarget(t, ctx, conn)
	publishErr := errors.New("local fixture publication failure")
	publish := func(context.Context, uuid.UUID, string, string) error { return publishErr }
	service := NewDistributionService(conn, nil, testExistingPluginAttacher(), publish, testPluginTargets(conn))

	distributed, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: 0})
	require.NoError(t, err)
	require.True(t, distributed.AttachmentLive)
	require.Equal(t, publicationStateRepairRequired, distributed.PublicationState)

	current, err := service.Current(ctx, principal, project.Slug, "")
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

func testExistingPluginAttacher() ExistingPluginAttacher {
	return func(ctx context.Context, tx pgx.Tx, _ *contextvalues.AuthContext, _ string, _, pluginID, mcpServerID uuid.UUID, displayName string) (uuid.UUID, bool, error) {
		server, err := pluginsrepo.New(tx).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
			PluginID:    pluginID,
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

// testPluginTargets resolves named plugin targets against the same inventory
// the plugin tools read.
func testPluginTargets(conn *pgxpool.Pool) *PluginsService {
	limiter := func() Limiter { return &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}} }
	return NewPluginsService(conn, OperationBudget{Connection: limiter(), Organization: limiter()}, "test-cursor-key")
}

// Distribution resolves its target only from the caller's onboarding workflow,
// so every registration surface must run the same post-commit bind. This guards
// the catalogue path, which once registered without ever binding and left every
// later distribute_mcp_to_plugin call refused as an invalid target.
func TestRegistrationOnboardingBindMakesTheRegistrationDistributable(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_registration_onboarding_bind")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 5})
	require.NoError(t, err)
	request := registrationRequest(project, "catalog-bind-fixture", "catalog-bind-registration")
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

	_, err = pluginsrepo.New(conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	_, err = store.RecordReadiness(ctx, principal, ReadinessBinding{
		ProjectID:                        project.ID,
		RegistrationID:                   registration.ID,
		ProviderAuthorizationFingerprint: "fixture-readiness",
	}, ReadinessReady, "fixture", time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)

	service := NewDistributionService(conn, nil, testExistingPluginAttacher(), func(_ context.Context, _ uuid.UUID, _ string, _ string) error {
		return nil
	}, testPluginTargets(conn))
	_, err = service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: 0})
	require.ErrorIs(t, err, ErrDistributionInvalid, "an unbound registration has no distribution target")

	onboarding := NewOnboardingService(conn)
	require.NoError(t, recordRegistrationOnboarding(ctx, onboarding, principal, project.ID, registration.ID.String()))

	attached, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: 0})
	require.NoError(t, err)
	require.True(t, attached.AttachmentLive)
}

func TestDistributionToolErrorClassifiesAnInvalidTarget(t *testing.T) {
	t.Parallel()

	result, ok := distributionToolError(ErrDistributionInvalid)
	require.True(t, ok, "an invalid distribution target must not escape as a bare error string")
	require.NotNil(t, result)
	require.True(t, result.IsError)
	text, isText := result.Content[0].(*mcp.TextContent)
	require.True(t, isText)

	var decoded distributionErrorResult
	require.NoError(t, json.Unmarshal([]byte(text.Text), &decoded))
	require.Equal(t, "no_distribution_target", decoded.Code)
	require.NotEmpty(t, decoded.Message)
}
