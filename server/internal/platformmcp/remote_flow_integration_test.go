package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TestRemoteURLGuidedFlowProbesRegistersAndDistributesEndToEnd walks the whole
// guided flow over the wire: the probe tool verifies an in-process fake MCP
// server and issues a receipt, the register tool redeems that receipt, the
// onboarding status tool reports dashboard setup as the next step, and the
// distribution tool refuses until the server reads freshly ready — then
// attaches it to the project's existing Default plugin.
func TestRemoteURLGuidedFlowProbesRegistersAndDistributesEndToEnd(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_remote_flow_happy_path")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	defaultPlugin, err := pluginsrepo.New(conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)

	// A real streamable-HTTP MCP server, reached over the wire through the
	// guardian policy that trusts its certificate.
	fixture := startProbeFixture(t, withWellKnownNotFound(probeFixtureMCPHandler("list_things", "get_thing")))
	prober, err := NewRemoteProbeService(testenv.NewLogger(t), probeFixturePolicy(t, fixture), remoteRegistrationTestKey, allowBudget())
	require.NoError(t, err)

	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 3})
	require.NoError(t, err)
	gate := &testRegistrationGate{enabled: true}
	readinessProber := &recordingCatalogReadinessProber{}
	service := newRegistrationService(testCatalog{}, gate, store).
		WithRemoteRegistration(remoteRegistrationTestKey, NewPostgresRemoteMCPApprovals(conn)).
		WithReadiness(NewReadinessService(store, gate, NewProviderAdapters(nil), allowOperationLimiter{}, testOperationBudget(), readinessProber)).
		WithDashboardURL(&url.URL{Scheme: "https", Host: "aicp.example.test"})
	onboarding := NewOnboardingService(conn)
	distributions := NewDistributionService(conn, nil, testExistingDefaultPluginAttacher(principal.OrganizationID), func(context.Context, uuid.UUID, string, string) error { return nil })
	_, registrar := newServer(nil, testCatalog{}, service, "", nil, nil, onboarding, distributions, nil, CatalogDescriptor{}, prober, testGate{enabled: true})

	probe := remoteToolDescriptor(t, registrar, "probe_remote_mcp")
	register := remoteToolDescriptor(t, registrar, "register_remote_mcp_for_project")
	handoff := remoteToolDescriptor(t, registrar, "get_setup_handoff")
	status := remoteToolDescriptor(t, registrar, "get_platform_mcp_onboarding_status")
	distribute := remoteToolDescriptor(t, registrar, "add_platform_mcp_to_default_plugin")

	probeArguments, err := json.Marshal(map[string]string{"remote_url": fixture.URL})
	require.NoError(t, err)
	raw, err := probe.Invoke(ContextWithPrincipal(ctx, principal), probeArguments)
	require.NoError(t, err)
	probed, ok := raw.(ProbeRemoteMCPToolOutput)
	require.True(t, ok)
	require.Equal(t, fixture.URL, probed.Evidence.NormalizedURL)
	require.Equal(t, "probe-fixture", probed.Evidence.ServerName)
	require.ElementsMatch(t, []string{"list_things", "get_thing"}, probed.Evidence.ToolNames)
	require.Equal(t, ProbeAuthPostureOpen, probed.Evidence.AuthPosture)
	require.Contains(t, probed.Evidence.Gaps, probeGapNoOAuthMetadata, "evidence gaps are disclosed for the user to confirm")
	require.NotEmpty(t, probed.ProbeReceipt)
	require.Equal(t, "confirm_evidence_with_user", probed.NextAction)

	raw, err = register.Invoke(ContextWithPrincipal(ctx, principal), registerRemoteToolArguments(t, project.Slug, probed.ProbeReceipt, "remote-flow-key"))
	require.NoError(t, err)
	registered, ok := raw.(RegisterRemoteMCPToolOutput)
	require.True(t, ok)
	require.Equal(t, fixture.URL, registered.RemoteURL)
	require.False(t, registered.Replayed)
	require.False(t, registered.BlockedPendingApproval)
	require.Equal(t, remoteURLCatalogProvider, registered.ProviderKey)
	require.Equal(t, fixture.URL, registered.CatalogRef)
	registrationID, err := uuid.Parse(registered.RegistrationID)
	require.NoError(t, err)

	// The setup handoff maps the remote registration to the existing Remote MCP
	// server Authentication settings dashboard surface — the only place headers
	// and authentication are ever configured.
	handoffArguments, err := json.Marshal(map[string]string{
		"project_slug":    project.Slug,
		"registration_id": registered.RegistrationID,
		"provider_key":    registered.ProviderKey,
		"catalog_ref":     registered.CatalogRef,
	})
	require.NoError(t, err)
	raw, err = handoff.Invoke(ContextWithPrincipal(ctx, principal), handoffArguments)
	require.NoError(t, err)
	handedOff, ok := raw.(GetSetupHandoffToolOutput)
	require.True(t, ok)
	require.Equal(t, "dashboard_source_settings", handedOff.Intent)
	require.True(t, strings.HasPrefix(handedOff.SetupURL, "https://aicp.example.test/"), "setup URL must be on the configured dashboard origin: %s", handedOff.SetupURL)
	require.Contains(t, handedOff.SetupURL, "/projects/"+project.Slug+"/mcp/x/")
	require.True(t, strings.HasSuffix(handedOff.SetupURL, "/settings#authentication"), "setup URL must land on Authentication settings: %s", handedOff.SetupURL)
	require.Empty(t, handedOff.Handoff, "a remote registration returns a server-owned settings URL, not a single-use handoff")

	// The registration persisted exactly the probed URL as a remote_url source.
	registration, err := platformrepo.New(conn).GetActivePlatformMCPCatalogRegistration(ctx, platformrepo.GetActivePlatformMCPCatalogRegistrationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		SourceKind:       remoteURLSourceKind,
		CatalogProvider:  remoteURLCatalogProvider,
		CatalogReference: fixture.URL,
	})
	require.NoError(t, err)
	require.Equal(t, registrationID, registration.ID)
	require.True(t, registrationComponentsComplete(registration))
	remote, err := remotemcprepo.New(conn).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{
		ID:        registration.RemoteMcpServerID.UUID,
		ProjectID: project.ID,
	})
	require.NoError(t, err)
	require.Equal(t, fixture.URL, remote.Url)

	// The first status check probes readiness through the generic
	// persisted-source path and reports dashboard setup as the next step.
	now := time.Now().UTC()
	readinessProber.result = ProviderReadinessProbeResult{
		AuthorizationIdentity: ProviderAuthorizationIdentity{
			OrganizationID: principal.OrganizationID,
			Subject:        urn.NewUserSubject(principal.UserID),
			RegistrationID: registrationID,
			Absence:        "anonymous",
		},
		State:        ReadinessNeedsConfiguration,
		EvidenceCode: "configuration_required",
		CheckedAt:    now,
		ExpiresAt:    now.Add(time.Hour),
	}
	statusArguments, err := json.Marshal(map[string]any{"project_slug": project.Slug})
	require.NoError(t, err)
	raw, err = status.Invoke(ContextWithPrincipal(ctx, principal), statusArguments)
	require.NoError(t, err)
	setupStatus, ok := raw.(GetOnboardingMCPStatusToolOutput)
	require.True(t, ok)
	require.True(t, setupStatus.Registered)
	require.Equal(t, registrationID.String(), setupStatus.RegistrationID)
	require.Equal(t, string(ReadinessNeedsConfiguration), setupStatus.Readiness)
	require.Equal(t, "continue_secure_setup", setupStatus.NextAction)
	require.Contains(t, setupStatus.Message, "dashboard setup")
	require.False(t, setupStatus.BlockedPendingApproval)
	require.Equal(t, 1, readinessProber.calls)

	// Distribution refuses while the server is not freshly ready, and nothing
	// lands in the Default plugin.
	distributeArguments, err := json.Marshal(map[string]any{"project_slug": project.Slug})
	require.NoError(t, err)
	_, err = distribute.Invoke(ContextWithPrincipal(ctx, principal), distributeArguments)
	refusal := decodeToolRefusal(t, err)
	require.Equal(t, "not_ready", refusal["code"])
	_, err = pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID:    defaultPlugin.ID,
		McpServerID: registration.McpServerID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "a not-ready refusal must leave the Default plugin untouched")

	// Dashboard setup completes upstream; a forced recheck reads fresh-ready
	// and hands the flow to distribution.
	now = time.Now().UTC()
	readinessProber.result.State = ReadinessReady
	readinessProber.result.EvidenceCode = "authenticated_initialize_tools_list"
	readinessProber.result.CheckedAt = now
	readinessProber.result.ExpiresAt = now.Add(time.Hour)
	forcedArguments, err := json.Marshal(map[string]any{"project_slug": project.Slug, "force": true})
	require.NoError(t, err)
	raw, err = status.Invoke(ContextWithPrincipal(ctx, principal), forcedArguments)
	require.NoError(t, err)
	readyStatus, ok := raw.(GetOnboardingMCPStatusToolOutput)
	require.True(t, ok)
	require.Equal(t, string(ReadinessReady), readyStatus.Readiness)
	require.Equal(t, "fresh", readyStatus.Freshness)
	require.Equal(t, "add_platform_mcp_to_default_plugin", readyStatus.NextAction)

	raw, err = distribute.Invoke(ContextWithPrincipal(ctx, principal), distributeArguments)
	require.NoError(t, err)
	distributed, ok := raw.(DistributeOnboardingMCPToolOutput)
	require.True(t, ok)
	require.True(t, distributed.Attached)
	require.Equal(t, publicationStateCurrent, distributed.PublicationState)
	live, err := pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID:    defaultPlugin.ID,
		McpServerID: registration.McpServerID,
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, live.ID)
}

// TestRemoteURLGuidedFlowUnderEnforcementBlocksStatusAndWithholdsDistribution
// is the enforcement-on variant: with a block-by-default shadow MCP policy in
// place before the flow starts, the probe and registration still succeed — the
// registration stands — but both the register tool and every status check
// report blocked_pending_approval with the dashboard approvals path, and
// distribution refuses at its own enforcement chokepoint. Readiness is not the
// barrier: an open server reads fresh-ready through an anonymous probe while
// still blocked, and distribution must refuse it all the same.
func TestRemoteURLGuidedFlowUnderEnforcementBlocksStatusAndWithholdsDistribution(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_remote_flow_enforcement")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	defaultPlugin, err := pluginsrepo.New(conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	_, err = riskrepo.New(conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:                   uuid.New(),
		ProjectID:            project.ID,
		OrganizationID:       principal.OrganizationID,
		Name:                 "Block shadow MCP by default",
		Sources:              []string{"shadow_mcp"},
		Enabled:              true,
		Action:               "block",
		AudienceType:         "everyone",
		ShadowMcpDisposition: conv.ToPGTextEmpty(shadowmcp.DispositionBlockAll),
		AutoName:             false,
	})
	require.NoError(t, err)
	organization, err := organizationsrepo.New(conn).GetOrganizationMetadata(ctx, principal.OrganizationID)
	require.NoError(t, err)
	approvalsPath := "/" + organization.Slug + "/projects/" + project.Slug + "/shadow-mcp"

	fixture := startProbeFixture(t, withWellKnownNotFound(probeFixtureMCPHandler("guarded_tool")))
	prober, err := NewRemoteProbeService(testenv.NewLogger(t), probeFixturePolicy(t, fixture), remoteRegistrationTestKey, allowBudget())
	require.NoError(t, err)

	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 3})
	require.NoError(t, err)
	gate := &testRegistrationGate{enabled: true}
	readinessProber := &recordingCatalogReadinessProber{}
	service := newRegistrationService(testCatalog{}, gate, store).
		WithRemoteRegistration(remoteRegistrationTestKey, NewPostgresRemoteMCPApprovals(conn)).
		WithReadiness(NewReadinessService(store, gate, NewProviderAdapters(nil), allowOperationLimiter{}, testOperationBudget(), readinessProber))
	onboarding := NewOnboardingService(conn)
	distributions := NewDistributionService(conn, nil, testExistingDefaultPluginAttacher(principal.OrganizationID), func(context.Context, uuid.UUID, string, string) error { return nil })
	_, registrar := newServer(nil, testCatalog{}, service, "", nil, nil, onboarding, distributions, nil, CatalogDescriptor{}, prober, testGate{enabled: true})

	probe := remoteToolDescriptor(t, registrar, "probe_remote_mcp")
	register := remoteToolDescriptor(t, registrar, "register_remote_mcp_for_project")
	status := remoteToolDescriptor(t, registrar, "get_platform_mcp_onboarding_status")
	distribute := remoteToolDescriptor(t, registrar, "add_platform_mcp_to_default_plugin")

	// The probe is unaffected by enforcement: verification evidence and the
	// receipt are still issued.
	probeArguments, err := json.Marshal(map[string]string{"remote_url": fixture.URL})
	require.NoError(t, err)
	raw, err := probe.Invoke(ContextWithPrincipal(ctx, principal), probeArguments)
	require.NoError(t, err)
	probed, ok := raw.(ProbeRemoteMCPToolOutput)
	require.True(t, ok)
	require.NotEmpty(t, probed.ProbeReceipt)

	// Registration succeeds — enforcement is respected, not a refusal — and the
	// blocked state is reported honestly with the dashboard approvals path.
	raw, err = register.Invoke(ContextWithPrincipal(ctx, principal), registerRemoteToolArguments(t, project.Slug, probed.ProbeReceipt, "enforced-flow-key"))
	require.NoError(t, err)
	registered, ok := raw.(RegisterRemoteMCPToolOutput)
	require.True(t, ok)
	require.True(t, registered.BlockedPendingApproval)
	require.Equal(t, approvalsPath, registered.DashboardApprovalsURL)
	require.Equal(t, "await_org_approval", registered.NextAction)
	registrationID, err := uuid.Parse(registered.RegistrationID)
	require.NoError(t, err)
	registration, err := platformrepo.New(conn).GetActivePlatformMCPCatalogRegistration(ctx, platformrepo.GetActivePlatformMCPCatalogRegistrationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		SourceKind:       remoteURLSourceKind,
		CatalogProvider:  remoteURLCatalogProvider,
		CatalogReference: fixture.URL,
	})
	require.NoError(t, err)
	require.Equal(t, registrationID, registration.ID)
	require.True(t, registrationComponentsComplete(registration), "the registration stands complete while enforcement blocks the server")

	// Status keeps the bounded readiness facts but overrides the next step with
	// the enforcement state.
	now := time.Now().UTC()
	readinessProber.result = ProviderReadinessProbeResult{
		AuthorizationIdentity: ProviderAuthorizationIdentity{
			OrganizationID: principal.OrganizationID,
			Subject:        urn.NewUserSubject(principal.UserID),
			RegistrationID: registrationID,
			Absence:        "anonymous",
		},
		State:        ReadinessNeedsConfiguration,
		EvidenceCode: "configuration_required",
		CheckedAt:    now,
		ExpiresAt:    now.Add(time.Hour),
	}
	statusArguments, err := json.Marshal(map[string]any{"project_slug": project.Slug})
	require.NoError(t, err)
	raw, err = status.Invoke(ContextWithPrincipal(ctx, principal), statusArguments)
	require.NoError(t, err)
	blocked, ok := raw.(GetOnboardingMCPStatusToolOutput)
	require.True(t, ok)
	require.True(t, blocked.Registered)
	require.True(t, blocked.BlockedPendingApproval)
	require.Equal(t, approvalsPath, blocked.DashboardApprovalsURL)
	require.Equal(t, "await_org_approval", blocked.NextAction)
	require.Equal(t, string(ReadinessNeedsConfiguration), blocked.Readiness, "readiness facts stay bounded while enforcement overrides the next step")

	// Distribution refuses at its own enforcement chokepoint, before readiness
	// is even considered.
	distributeArguments, err := json.Marshal(map[string]any{"project_slug": project.Slug})
	require.NoError(t, err)
	_, err = distribute.Invoke(ContextWithPrincipal(ctx, principal), distributeArguments)
	refusal := decodeToolRefusal(t, err)
	require.Equal(t, "blocked_pending_approval", refusal["code"])
	_, err = pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID:    defaultPlugin.ID,
		McpServerID: registration.McpServerID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "an enforcement-blocked server must never land in the Default plugin")

	// The laundering scenario: an open server's anonymous probe reads
	// fresh-ready while enforcement still blocks it. Readiness facts stay
	// honest, the enforcement override holds, the readiness-verified milestone
	// is withheld — and distribution still refuses on enforcement rather than
	// admitting the freshly ready row.
	now = time.Now().UTC()
	readinessProber.result.State = ReadinessReady
	readinessProber.result.EvidenceCode = "tools_list_ok"
	readinessProber.result.CheckedAt = now
	readinessProber.result.ExpiresAt = now.Add(time.Hour)
	forcedArguments, err := json.Marshal(map[string]any{"project_slug": project.Slug, "force": true})
	require.NoError(t, err)
	raw, err = status.Invoke(ContextWithPrincipal(ctx, principal), forcedArguments)
	require.NoError(t, err)
	readyButBlocked, ok := raw.(GetOnboardingMCPStatusToolOutput)
	require.True(t, ok)
	require.Equal(t, string(ReadinessReady), readyButBlocked.Readiness)
	require.Equal(t, "fresh", readyButBlocked.Freshness)
	require.True(t, readyButBlocked.BlockedPendingApproval)
	require.Equal(t, "await_org_approval", readyButBlocked.NextAction)

	_, err = distribute.Invoke(ContextWithPrincipal(ctx, principal), distributeArguments)
	refusal = decodeToolRefusal(t, err)
	require.Equal(t, "blocked_pending_approval", refusal["code"])
	_, err = pluginsrepo.New(conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID:    defaultPlugin.ID,
		McpServerID: registration.McpServerID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "a fresh-ready but enforcement-blocked server must never land in the Default plugin")
}

// trapCatalogProviderAdapter is a catalogue provider adapter that must never be
// consulted. It registers under the remote-url sentinel key so that, if any
// lifecycle path resolved a remote_url registration through the catalogue
// adapter registry, the adapter would be found and the call recorded.
type trapCatalogProviderAdapter struct {
	key   string
	calls int
}

func (a *trapCatalogProviderAdapter) ProviderKey() string {
	return a.key
}

func (a *trapCatalogProviderAdapter) PreflightSetup(context.Context, ProviderSetupRequest) error {
	a.calls++
	return errors.New("catalogue provider adapter must not be consulted for a remote_url registration")
}

func (a *trapCatalogProviderAdapter) BeginSetup(context.Context, ProviderSetupRequest) (ProviderSetupResult, error) {
	a.calls++
	return ProviderSetupResult{}, errors.New("catalogue provider adapter must not be consulted for a remote_url registration")
}

func (a *trapCatalogProviderAdapter) ProbeReadiness(context.Context, ProviderReadinessProbeRequest) (ProviderReadinessProbeResult, error) {
	a.calls++
	return ProviderReadinessProbeResult{}, errors.New("catalogue provider adapter must not be consulted for a remote_url registration")
}

// TestRemoteURLLifecycleBypassesCatalogueIdentityPaths is the lifecycle
// non-regression: a remote_url registration's readiness check routes through
// the generic discovery prober while a trap adapter registered under the
// remote-url sentinel key goes unconsulted, and confirmed identity-provider
// attachment runs live RFC 9728/8414 discovery against the persisted server
// URL — reporting the bounded "not discovered" fact, never the
// catalogue-identity "unsupported" refusal.
func TestRemoteURLLifecycleBypassesCatalogueIdentityPaths(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_remote_flow_lifecycle_paths")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	// A live server whose well-known endpoints all answer 404: reachable, but
	// publishing no OAuth protected-resource metadata.
	fixture := startProbeFixture(t, withWellKnownNotFound(http.NotFoundHandler()))
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 3})
	require.NoError(t, err)
	gate := &testRegistrationGate{enabled: true}
	trap := &trapCatalogProviderAdapter{key: remoteURLCatalogProvider}
	readinessProber := &recordingCatalogReadinessProber{}
	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	attachment := NewCatalogIdentityProviderAttachmentService(conn, enc, probeFixturePolicy(t, fixture), audit.NewLogger(), &url.URL{Scheme: "https", Host: "aicp.example.test"})
	service := newRegistrationService(testCatalog{}, gate, store).
		WithRemoteRegistration(remoteRegistrationTestKey, NewPostgresRemoteMCPApprovals(conn)).
		WithReadiness(NewReadinessService(store, gate, NewProviderAdapters([]ProviderAdapter{trap}), allowOperationLimiter{}, testOperationBudget(), readinessProber)).
		WithIdentityProviderAttachment(attachment)
	onboarding := NewOnboardingService(conn)
	distributions := NewDistributionService(conn, nil, testExistingDefaultPluginAttacher(principal.OrganizationID), func(context.Context, uuid.UUID, string, string) error { return nil })
	_, registrar := newServer(nil, testCatalog{}, service, "", nil, nil, onboarding, distributions, nil, CatalogDescriptor{}, &fakeRemoteProber{}, testGate{enabled: true})

	register := remoteToolDescriptor(t, registrar, "register_remote_mcp_for_project")
	status := remoteToolDescriptor(t, registrar, "get_platform_mcp_onboarding_status")
	attach := remoteToolDescriptor(t, registrar, "attach_platform_mcp_identity_provider")

	receipt := mintProbeReceipt(t, remoteRegistrationTestKey, principal, fixture.URL, time.Now().UTC())
	raw, err := register.Invoke(ContextWithPrincipal(ctx, principal), registerRemoteToolArguments(t, project.Slug, receipt, "lifecycle-paths-key"))
	require.NoError(t, err)
	registered, ok := raw.(RegisterRemoteMCPToolOutput)
	require.True(t, ok)
	registrationID, err := uuid.Parse(registered.RegistrationID)
	require.NoError(t, err)

	// The status tool's forced readiness probe routes through the generic
	// discovery prober; the trap adapter, registered under the exact key the
	// catalogue path would resolve, is never consulted.
	now := time.Now().UTC()
	readinessProber.result = ProviderReadinessProbeResult{
		AuthorizationIdentity: ProviderAuthorizationIdentity{
			OrganizationID: principal.OrganizationID,
			Subject:        urn.NewUserSubject(principal.UserID),
			RegistrationID: registrationID,
			Absence:        "anonymous",
		},
		State:        ReadinessUnauthorized,
		EvidenceCode: "upstream_authorization_rejected",
		CheckedAt:    now,
		ExpiresAt:    now.Add(time.Hour),
	}
	statusArguments, err := json.Marshal(map[string]any{"project_slug": project.Slug})
	require.NoError(t, err)
	raw, err = status.Invoke(ContextWithPrincipal(ctx, principal), statusArguments)
	require.NoError(t, err)
	statusOutput, ok := raw.(GetOnboardingMCPStatusToolOutput)
	require.True(t, ok)
	require.Equal(t, string(ReadinessUnauthorized), statusOutput.Readiness)
	require.Equal(t, 1, readinessProber.calls, "readiness must run through the generic persisted-source prober")
	require.Zero(t, trap.calls, "the catalogue provider adapter path must not be consulted")

	// Confirmed identity-provider attachment takes the live-discovery path
	// against the persisted server URL: no metadata is a bounded fact routed to
	// the dashboard, not the catalogue-identity "unsupported" refusal.
	attachArguments, err := json.Marshal(map[string]any{"project_slug": project.Slug, "confirmed": true})
	require.NoError(t, err)
	_, err = attach.Invoke(ContextWithPrincipal(ctx, principal), attachArguments)
	refusal := decodeToolRefusal(t, err)
	require.Equal(t, "no_identity_provider_discovered", refusal["code"])
	require.Contains(t, refusal["message"], "Authentication settings")
	require.Zero(t, trap.calls, "identity-provider attachment must not consult the catalogue adapter path either")
}
