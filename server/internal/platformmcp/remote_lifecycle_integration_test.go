package platformmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TestRemoteRegistrationLifecycleReportsEnforcementInOnboardingStatus walks a
// remote URL registration through the same workflow-bound downstream lifecycle
// the catalogue flow uses: the register tool binds the onboarding workflow, and
// get_platform_mcp_onboarding_status reports enforcement honestly — unblocked
// with no policy, blocked_pending_approval with the dashboard approvals path
// under a block-by-default policy, and unblocked again once an admin approval
// reconciles into a bypass grant.
func TestRemoteRegistrationLifecycleReportsEnforcementInOnboardingStatus(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_remote_lifecycle_status")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 3})
	require.NoError(t, err)
	gate := &testRegistrationGate{enabled: true}
	service := newRegistrationService(testCatalog{}, gate, store).
		WithRemoteRegistration(remoteRegistrationTestKey, NewPostgresRemoteMCPApprovals(conn)).
		WithReadiness(NewReadinessService(store, gate, NewProviderAdapters(nil), nil, testOperationBudget()))
	onboarding := NewOnboardingService(conn)
	distributions := NewDistributionService(conn, nil, testExistingDefaultPluginAttacher(principal.OrganizationID), func(context.Context, uuid.UUID, string, string) error { return nil })
	_, registrar := newServer(nil, testCatalog{}, service, "", nil, nil, onboarding, distributions, nil, CatalogDescriptor{}, &fakeRemoteProber{}, testGate{enabled: true})
	register := remoteToolDescriptor(t, registrar, "register_remote_mcp_for_project")
	status := remoteToolDescriptor(t, registrar, "get_platform_mcp_onboarding_status")

	remoteURL := "https://lifecycle.example.test/mcp"
	receipt := mintProbeReceipt(t, remoteRegistrationTestKey, principal, remoteURL, time.Now().UTC())
	raw, err := register.Invoke(ContextWithPrincipal(ctx, principal), registerRemoteToolArguments(t, project.Slug, receipt, "lifecycle-registration-key"))
	require.NoError(t, err)
	registered, ok := raw.(RegisterRemoteMCPToolOutput)
	require.True(t, ok)
	require.False(t, registered.BlockedPendingApproval)
	registrationID, err := uuid.Parse(registered.RegistrationID)
	require.NoError(t, err)
	require.Contains(t, registered.Message, "get_platform_mcp_onboarding_status")

	// The register tool binds the registration into the caller's active
	// onboarding workflow and records the registration milestone, exactly as
	// the catalogue register tool does.
	projection, err := onboarding.Get(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	require.NotNil(t, projection.Workflow)
	require.Equal(t, registrationID, projection.Workflow.SelectedRegistrationID)
	require.NotNil(t, projection.SelectedProject)
	require.Equal(t, project.Slug, projection.SelectedProject.Slug)
	require.True(t, projection.RegistrationSucceeded)

	// Persist readiness evidence so the status tool reads state without a
	// provider probe.
	now := time.Now().UTC()
	_, err = store.RecordReadiness(ctx, principal, ReadinessBinding{
		ProjectID:                        project.ID,
		RegistrationID:                   registrationID,
		ProviderAuthorizationFingerprint: "remote-lifecycle-readiness",
	}, ReadinessUnauthorized, "upstream_authorization_rejected", now, now.Add(time.Hour))
	require.NoError(t, err)

	statusArguments := json.RawMessage(`{"project_slug":"` + project.Slug + `"}`)
	raw, err = status.Invoke(ContextWithPrincipal(ctx, principal), statusArguments)
	require.NoError(t, err)
	unblocked, ok := raw.(GetOnboardingMCPStatusToolOutput)
	require.True(t, ok)
	require.True(t, unblocked.Registered)
	require.Equal(t, registrationID.String(), unblocked.RegistrationID)
	require.False(t, unblocked.BlockedPendingApproval, "no blocking policy means no enforcement to report")
	require.Empty(t, unblocked.DashboardApprovalsURL)
	require.Equal(t, "continue_secure_setup", unblocked.NextAction)

	inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(remoteURL)
	require.True(t, ok)
	blockAllPolicy, err := riskrepo.New(conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
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

	raw, err = status.Invoke(ContextWithPrincipal(ctx, principal), statusArguments)
	require.NoError(t, err)
	blocked, ok := raw.(GetOnboardingMCPStatusToolOutput)
	require.True(t, ok)
	require.True(t, blocked.BlockedPendingApproval, "block-by-default enforcement blocks the unapproved server in status")
	require.Equal(t, "await_org_approval", blocked.NextAction)
	require.Equal(t, "/"+organization.Slug+"/projects/"+project.Slug+"/shadow-mcp", blocked.DashboardApprovalsURL)
	require.Contains(t, blocked.Message, "dashboard_approvals_url")
	require.Equal(t, string(ReadinessUnauthorized), blocked.Readiness, "readiness facts stay bounded while enforcement overrides the next step")

	// An admin approval reconciles into a bypass grant on the canonical URL;
	// the very next status check reads as unblocked.
	require.NoError(t, policybypass.ReplacePolicyURLAudience(ctx, conn, principal.OrganizationID, authz.ScopeRiskPolicyBypass, blockAllPolicy.ID.String(), inventoryURL.CanonicalURL, []urn.Principal{authz.AllUsersPrincipal()}))
	raw, err = status.Invoke(ContextWithPrincipal(ctx, principal), statusArguments)
	require.NoError(t, err)
	approved, ok := raw.(GetOnboardingMCPStatusToolOutput)
	require.True(t, ok)
	require.False(t, approved.BlockedPendingApproval, "a standing bypass grant is a recorded approval")
	require.Empty(t, approved.DashboardApprovalsURL)
	require.Equal(t, "continue_secure_setup", approved.NextAction)
}

// recordingCatalogReadinessProber records how the persisted Remote MCP source
// readiness path was consulted for a lifecycle-owned registration.
type recordingCatalogReadinessProber struct {
	calls               int
	remoteMCPServerID   uuid.UUID
	userSessionIssuerID uuid.UUID
	result              ProviderReadinessProbeResult
}

func (p *recordingCatalogReadinessProber) ProbeCatalogReadiness(_ context.Context, _ Principal, _, _, remoteMCPServerID, userSessionIssuerID, _, _ uuid.UUID) (ProviderReadinessProbeResult, error) {
	p.calls++
	p.remoteMCPServerID = remoteMCPServerID
	p.userSessionIssuerID = userSessionIssuerID
	return p.result, nil
}

// TestRemoteURLRegistrationRoutesReadinessThroughDiscoveryProber asserts the
// lifecycle non-regression: a remote_url registration's readiness probe runs
// through the generic persisted-source path — the same one browser-catalogue
// registrations use — and never consults a catalogue provider adapter.
func TestRemoteURLRegistrationRoutesReadinessThroughDiscoveryProber(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_remote_lifecycle_readiness")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 3})
	require.NoError(t, err)
	service := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, store).
		WithRemoteRegistration(remoteRegistrationTestKey, NewPostgresRemoteMCPApprovals(conn))

	remoteURL := "https://readiness.example.test/mcp"
	result, err := service.RegisterRemoteMCP(ctx, principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, remoteURL, time.Now().UTC()),
		IdempotencyKey: "readiness-registration-key",
	})
	require.NoError(t, err)
	registrationID, err := uuid.Parse(result.Registration)
	require.NoError(t, err)
	registration, err := platformrepo.New(conn).GetActivePlatformMCPCatalogRegistration(ctx, platformrepo.GetActivePlatformMCPCatalogRegistrationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		SourceKind:       remoteURLSourceKind,
		CatalogProvider:  remoteURLCatalogProvider,
		CatalogReference: remoteURL,
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	prober := &recordingCatalogReadinessProber{result: ProviderReadinessProbeResult{
		AuthorizationIdentity: ProviderAuthorizationIdentity{
			OrganizationID: principal.OrganizationID,
			Subject:        urn.NewUserSubject(principal.UserID),
			RegistrationID: registrationID,
			Absence:        "anonymous",
		},
		State:        ReadinessUnauthorized,
		EvidenceCode: "upstream_authorization_rejected",
		CheckedAt:    now,
		ExpiresAt:    now.Add(time.Minute),
	}}

	readiness, err := store.ProbeProviderReadiness(ctx, principal, project.ID, registrationID, NewProviderAdapters(nil), prober)

	require.NoError(t, err, "no catalogue provider adapter exists for remote-url, so success proves the generic path was taken")
	require.Equal(t, 1, prober.calls)
	require.Equal(t, registration.RemoteMcpServerID.UUID, prober.remoteMCPServerID, "the probe works off the persisted Remote MCP source component")
	require.Equal(t, registration.UserSessionIssuerID.UUID, prober.userSessionIssuerID)
	require.Equal(t, ReadinessUnauthorized, readiness.State)
}

// TestRemoteURLIdentityProviderAttachmentTakesDiscoveryPath registers a remote
// URL source whose server publishes no OAuth metadata at the RFC 9728/8414
// well-knowns, then asserts that confirmed attachment runs live discovery
// against the persisted server URL — the catalogue-identity gate admits the
// registration — and reports the bounded "not discovered" fact rather than the
// catalogue "unsupported contract" refusal.
func TestRemoteURLIdentityProviderAttachmentTakesDiscoveryPath(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_remote_lifecycle_idp")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 3})
	require.NoError(t, err)
	service := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, store).
		WithRemoteRegistration(remoteRegistrationTestKey, NewPostgresRemoteMCPApprovals(conn))

	// A live server whose well-known endpoints all answer 404: reachable, but
	// publishing no OAuth protected-resource metadata.
	fixture := startProbeFixture(t, withWellKnownNotFound(http.NotFoundHandler()))
	result, err := service.RegisterRemoteMCP(ctx, principal, RegisterRemoteMCPInput{
		ProjectSlug:    project.Slug,
		ProbeReceipt:   mintProbeReceipt(t, remoteRegistrationTestKey, principal, fixture.URL, time.Now().UTC()),
		IdempotencyKey: "idp-registration-key",
	})
	require.NoError(t, err)
	registrationID, err := uuid.Parse(result.Registration)
	require.NoError(t, err)

	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	attachment := NewCatalogIdentityProviderAttachmentService(conn, enc, probeFixturePolicy(t, fixture), audit.NewLogger(), &url.URL{Scheme: "https", Host: "aicp.example.test"})

	_, err = attachment.Attach(ctx, principal, project, registrationID)

	require.ErrorIs(t, err, ErrIdentityProviderNotDiscovered, "no discoverable metadata on a remote URL source is a bounded fact; setup falls through to the dashboard")
	require.NotErrorIs(t, err, ErrIdentityProviderAttachmentUnsupported, "the catalogue-identity refusal must not fire for a remote_url registration")
}
