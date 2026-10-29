//nolint:wrapcheck // Integration assertions intentionally return test setup errors directly.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
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
	store, err := NewRegistrationStore(conn)
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
		PluginID: defaultPlugin.ID, ProjectID: defaultPlugin.ProjectID, OrganizationID: defaultPlugin.OrganizationID,
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
		PluginID: defaultPlugin.ID, ProjectID: defaultPlugin.ProjectID, OrganizationID: defaultPlugin.OrganizationID,
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
		PluginID: marketing.ID, ProjectID: marketing.ProjectID, OrganizationID: marketing.OrganizationID,
		McpServerID: target.McpServerID,
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, live.ID)

	// The default plugin is untouched: naming a plugin distributes there and
	// nowhere else.
	_, err = pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID: defaultPlugin.ID, ProjectID: defaultPlugin.ProjectID, OrganizationID: defaultPlugin.OrganizationID,
		McpServerID: target.McpServerID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	removed, err := service.Remove(ctx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: "Marketing Tools", ExpectedVersion: distributed.Version})
	require.NoError(t, err)
	require.False(t, removed.AttachmentLive)

	_, err = pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID: marketing.ID, ProjectID: marketing.ProjectID, OrganizationID: marketing.OrganizationID,
		McpServerID: target.McpServerID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

// The publish a distribution runs is an automatic one, which keeps a plugin
// whose auto sync is off as it was last published. Sharing a server into such
// a plugin, or taking it back out, reports that the change waits for a manual
// sync even though that publish succeeded.
func TestDistributionServiceReportsAPluginWhoseAutoSyncIsOff(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_distribution_paused_plugin")
	require.NoError(t, err)

	principal, project := seedReadyDistributionTarget(t, ctx, conn)
	seedPluginWithAutoSync(t, ctx, conn, principal.OrganizationID, project.ID, "Held Tools", "held", pgtype.Bool{Bool: false, Valid: true})
	seedPluginWithAutoSync(t, ctx, conn, principal.OrganizationID, project.ID, "Live Tools", "live", pgtype.Bool{})
	service := NewDistributionService(conn, nil, testExistingPluginAttacher(), func(context.Context, uuid.UUID, string, string) error { return nil }, testPluginTargets(conn))

	held, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: "held", ExpectedVersion: 0})
	require.NoError(t, err)
	require.True(t, held.AttachmentLive)
	require.Equal(t, publicationStateCurrent, held.PublicationState, "the automatic publish itself succeeded")
	require.True(t, held.AwaitingManualSync)
	require.Contains(t, distributionOutcomeMessage(held, false), "get it only after its project is synced by hand")

	removed, err := service.Remove(ctx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: "held", ExpectedVersion: held.Version})
	require.NoError(t, err)
	require.True(t, removed.AwaitingManualSync)

	live, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: "live", ExpectedVersion: 0})
	require.NoError(t, err)
	require.False(t, live.AwaitingManualSync)
	require.Contains(t, distributionOutcomeMessage(live, false), "the people it is shared with will get it")
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

	created, err := pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{PluginID: plugin.ID, ProjectID: plugin.ProjectID, OrganizationID: plugin.OrganizationID, McpServerID: target.McpServerID})
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

	live, err := pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{PluginID: plugin.ID, ProjectID: plugin.ProjectID, OrganizationID: plugin.OrganizationID, McpServerID: target.McpServerID})
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

	live, err := pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{PluginID: plugin.ID, ProjectID: plugin.ProjectID, OrganizationID: plugin.OrganizationID, McpServerID: target.McpServerID})
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

	current, err := service.Current(ctx, principal, project.Slug, uuid.Nil, "")
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
	store, err := NewRegistrationStore(conn)
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
// so every registration surface must run the same post-commit bind. The
// registration here goes through the register_catalog_mcp handler rather than
// the store, because the defect this guards was the handler never binding at
// all: a catalogue registration succeeded and then could never be distributed.
func TestCatalogRegistrationToolBindsOnboardingSoTheMCPCanBeDistributed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_catalog_registration_bind")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	onboarding := NewOnboardingService(conn)
	registrations := newRegistrationService(testCatalog{details: CatalogDetails{
		CatalogCandidate: CatalogCandidate{Name: "Reviewed MCP", ProviderKey: "provider", CatalogRef: "reviewed/mcp", SetupIntent: "authorize"},
		Transport:        "streamable-http",
		remoteURL:        "https://reviewed.example.test/mcp",
	}}, &testRegistrationGate{enabled: true}, store)

	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
	registerCatalogRegistrationTool(reg, registrations, onboarding)
	register := descriptorByName(t, reg, "register_catalog_mcp")

	output, err := register.Invoke(ContextWithPrincipal(ctx, principal), json.RawMessage(`{"project_slug":"`+project.Slug+`","provider_key":"provider","catalog_ref":"reviewed/mcp","idempotency_key":"catalog-bind-key"}`))
	require.NoError(t, err)
	registered, ok := output.(RegisterCatalogMCPToolOutput)
	require.True(t, ok)
	registrationID, err := uuid.Parse(registered.RegistrationID)
	require.NoError(t, err)

	_, err = pluginsrepo.New(conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	_, err = store.RecordReadiness(ctx, principal, ReadinessBinding{
		ProjectID:                        project.ID,
		RegistrationID:                   registrationID,
		ProviderAuthorizationFingerprint: "fixture-readiness",
	}, ReadinessReady, "fixture", time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)

	service := NewDistributionService(conn, nil, testExistingPluginAttacher(), func(_ context.Context, _ uuid.UUID, _ string, _ string) error {
		return nil
	}, testPluginTargets(conn))
	attached, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, ExpectedVersion: 0})
	require.NoError(t, err, "a registration made through the catalog tool must be distributable")
	require.True(t, attached.AttachmentLive)
}

func descriptorByName(t *testing.T, reg *Registrar, name string) Descriptor {
	t.Helper()

	for _, descriptor := range reg.Descriptors() {
		if descriptor.Name == name {
			return descriptor
		}
	}
	t.Fatalf("tool %q was not registered", name)
	return Descriptor{}
}

func TestDistributionToolErrorClassifiesAnInvalidTarget(t *testing.T) {
	t.Parallel()

	result, ok := distributionToolError(ErrDistributionInvalid, "")
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

// seedRegisteredMCP adds one MCP server to the project through setup and
// returns its registration, without touching the onboarding workflow.
func seedRegisteredMCP(t *testing.T, ctx context.Context, conn *pgxpool.Pool, principal Principal, project ResolvedProject, reference string) platformrepo.PlatformMcpCatalogRegistration {
	t.Helper()

	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	request := registrationRequest(project, reference, reference+"-key")
	receipt, err := store.BeginReceipt(ctx, principal, project, request, time.Now().UTC())
	require.NoError(t, err)
	receipt, err = store.ConvergeRegistration(ctx, principal, project, request, receipt)
	require.NoError(t, err)
	_, err = store.CompleteRegistrationWithRemoteURL(ctx, principal, project, request, receipt, "https://"+reference+".invalid/mcp")
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
	return registration
}

// A chat that adds two servers leaves the workflow pointing at the last one.
// Naming the first must share the first, and a refusal must say which server
// it is about, rather than acting on whatever the session last selected.
func TestDistributeToolSharesTheNamedServerNotTheSessionSelection(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_distribution_named_server")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	ready := seedRegisteredMCP(t, ctx, conn, principal, project, "ready-fixture")
	unchecked := seedRegisteredMCP(t, ctx, conn, principal, project, "unchecked-fixture")

	onboarding := NewOnboardingService(conn)
	_, err = onboarding.Start(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	_, err = onboarding.BindRegistration(ctx, principal.OrganizationID, principal.UserID, project.ID, unchecked.ID)
	require.NoError(t, err)
	defaultPlugin, err := pluginsrepo.New(conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID})
	require.NoError(t, err)
	_, err = store.RecordReadiness(ctx, principal, ReadinessBinding{ProjectID: project.ID, RegistrationID: ready.ID, ProviderAuthorizationFingerprint: "fixture-readiness"}, ReadinessReady, "fixture", time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)

	service := NewDistributionService(conn, nil, testExistingPluginAttacher(), func(context.Context, uuid.UUID, string, string) error { return nil }, testPluginTargets(conn))
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
	registerDistributionTools(reg, onboarding, service, nil)
	distribute := descriptorByName(t, reg, "distribute_mcp_to_plugin")
	callCtx := ContextWithPrincipal(ctx, principal)
	call := func(mcpID string) json.RawMessage {
		return json.RawMessage(`{"project_slug":"` + project.Slug + `","mcp_id":"` + mcpID + `","plugin":"` + defaultPlugin.ID.String() + `"}`)
	}

	output, err := distribute.Invoke(callCtx, call(ready.McpServerID.UUID.String()))
	require.NoError(t, err)
	shared, ok := output.(DistributeMCPToolOutput)
	require.True(t, ok, "expected a distribution, got %T", output)
	require.True(t, shared.Attached)
	require.Equal(t, ready.McpServerID.UUID.String(), shared.MCPID)
	require.NotEmpty(t, shared.MCPServer)
	require.Contains(t, shared.Message, shared.MCPServer)

	_, err = pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID: defaultPlugin.ID, ProjectID: project.ID, OrganizationID: principal.OrganizationID, McpServerID: ready.McpServerID,
	})
	require.NoError(t, err, "the named server must be the one shared")
	_, err = pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID: defaultPlugin.ID, ProjectID: project.ID, OrganizationID: principal.OrganizationID, McpServerID: unchecked.McpServerID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "the session's selection must not be shared")

	_, err = distribute.Invoke(callCtx, call(unchecked.McpServerID.UUID.String()))
	notReady := decodeDistributionRefusal(t, err)
	require.Equal(t, "not_ready", notReady.Code)
	require.NotEmpty(t, notReady.MCPServer)
	require.Contains(t, notReady.Message, notReady.MCPServer)

	for _, mcpID := range []string{uuid.NewString(), "", "not-a-uuid"} {
		_, err = distribute.Invoke(callCtx, call(mcpID))
		require.Equal(t, "mcp_not_found", decodeDistributionRefusal(t, err).Code, "mcp_id %q", mcpID)
	}

	remove := descriptorByName(t, reg, "remove_mcp_from_plugin")
	output, err = remove.Invoke(callCtx, call(ready.McpServerID.UUID.String()))
	require.NoError(t, err)
	removed, ok := output.(DistributeMCPToolOutput)
	require.True(t, ok, "expected a removal, got %T", output)
	require.False(t, removed.Attached)
	require.Equal(t, shared.MCPServer, removed.MCPServer)
}

// decodeDistributionRefusal reads the refusal JSON that Invoke surfaces as the
// error of a refused tool call.
func decodeDistributionRefusal(t *testing.T, err error) distributionErrorResult {
	t.Helper()
	require.Error(t, err)
	var decoded distributionErrorResult
	require.NoError(t, json.Unmarshal([]byte(err.Error()), &decoded))
	return decoded
}

// readinessByRegistrationAdapter is the deterministic fixture adapter, except
// that it reports the notReady registration as needing configuration and counts
// every probe.
type readinessByRegistrationAdapter struct {
	deterministicProviderAdapter
	notReady uuid.UUID
	probes   *atomic.Int32
}

func (a readinessByRegistrationAdapter) ProbeReadiness(ctx context.Context, request ProviderReadinessProbeRequest) (ProviderReadinessProbeResult, error) {
	a.probes.Add(1)
	result, err := a.deterministicProviderAdapter.ProbeReadiness(ctx, request)
	if err == nil && request.RegistrationID == a.notReady {
		result.State = ReadinessNeedsConfiguration
		result.EvidenceCode = "upstream_identity_provider_not_configured"
	}
	return result, err
}

// A server with no stored readiness check is checked when it is shared, so a
// working server is shared without a separate readiness call and one that is
// not working is refused by name.
func TestDistributeToolChecksTheNamedServerWhenNoCheckIsStored(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_distribution_call_time_probe")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	working := seedRegisteredMCP(t, ctx, conn, principal, project, "working-fixture")
	broken := seedRegisteredMCP(t, ctx, conn, principal, project, "broken-fixture")
	plugins := pluginsrepo.New(conn)
	defaultPlugin, err := plugins.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID})
	require.NoError(t, err)
	attachment := func(mcpServerID uuid.NullUUID) error {
		_, err := plugins.GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
			PluginID: defaultPlugin.ID, ProjectID: project.ID, OrganizationID: principal.OrganizationID, McpServerID: mcpServerID,
		})
		return err
	}

	probes := &atomic.Int32{}
	adapter := readinessByRegistrationAdapter{deterministicProviderAdapter: deterministicProviderAdapter{providerKey: "fixture"}, notReady: broken.ID, probes: probes}
	gate := &testRegistrationGate{enabled: true}
	readiness := NewReadinessService(store, gate, NewProviderAdapters([]ProviderAdapter{adapter}), allowOperationLimiter{}, allowBudget())
	registrations := NewRegistrationService(nil, gate, store).WithReadiness(readiness)

	service := NewDistributionService(conn, nil, testExistingPluginAttacher(), func(context.Context, uuid.UUID, string, string) error { return nil }, testPluginTargets(conn))
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
	registerDistributionTools(reg, NewOnboardingService(conn), service, registrations)
	distribute := descriptorByName(t, reg, "distribute_mcp_to_plugin")
	callCtx := ContextWithPrincipal(ctx, principal)
	call := func(mcpID string) json.RawMessage {
		return json.RawMessage(`{"project_slug":"` + project.Slug + `","mcp_id":"` + mcpID + `","plugin":"` + defaultPlugin.ID.String() + `"}`)
	}

	output, err := distribute.Invoke(callCtx, call(working.McpServerID.UUID.String()))
	require.NoError(t, err)
	shared, ok := output.(DistributeMCPToolOutput)
	require.True(t, ok, "expected a distribution, got %T", output)
	require.True(t, shared.Attached)
	require.Equal(t, int32(1), probes.Load(), "a server with no stored check must be checked once")
	require.NoError(t, attachment(working.McpServerID), "the checked server must be shared")

	_, err = distribute.Invoke(callCtx, call(broken.McpServerID.UUID.String()))
	refusal := decodeDistributionRefusal(t, err)
	require.Equal(t, "not_ready", refusal.Code)
	require.NotEmpty(t, refusal.MCPServer)
	require.Equal(t, int32(2), probes.Load())
	require.ErrorIs(t, attachment(broken.McpServerID), pgx.ErrNoRows, "a server that is not working must not be shared")
}
