package platformmcp

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	audittestrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

var platformMCPInfra *testenv.Environment

func TestMain(m *testing.M) {
	infra, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	platformMCPInfra = infra

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

func TestRegistrationStoreCompleteRegistrationConvergesPrivateComponents(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_registration")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store := NewRegistrationStore(conn)
	request := CatalogRegistrationRequest{
		ProjectSlug:      project.Slug,
		SourceKind:       "catalog",
		CatalogProvider:  "fixture",
		CatalogReference: "reviewed-reference",
		IdempotencyKey:   "registration-key",
		InputHash:        catalogRegistrationInputHash(project.Slug, "catalog", "fixture", "reviewed-reference"),
	}

	receipt, err := store.BeginReceipt(ctx, principal, project, request, time.Now().UTC())
	require.NoError(t, err)
	receipt, err = store.ConvergeRegistration(ctx, principal, project, request, receipt)
	require.NoError(t, err)

	const remoteURL = "https://reviewed.example.test/mcp"
	completed, err := store.CompleteRegistration(ctx, principal, project, request, receipt, remoteURL)
	require.NoError(t, err)
	require.Equal(t, receiptStatusSucceeded, completed.Status)
	require.True(t, completed.RegistrationID.Valid)

	registration, err := platformrepo.New(conn).GetActivePlatformMCPCatalogRegistration(ctx, platformrepo.GetActivePlatformMCPCatalogRegistrationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		SourceKind:       request.SourceKind,
		CatalogProvider:  request.CatalogProvider,
		CatalogReference: request.CatalogReference,
	})
	require.NoError(t, err)
	require.Equal(t, registrationStatusRegistered, registration.Status)
	require.True(t, registration.RemoteMcpServerID.Valid)
	require.True(t, registration.RemoteMcpServerOwned)
	require.True(t, registration.UserSessionIssuerID.Valid)
	require.True(t, registration.UserSessionIssuerOwned)
	require.True(t, registration.McpServerID.Valid)
	require.True(t, registration.McpServerOwned)
	require.True(t, registration.McpEndpointID.Valid)
	require.True(t, registration.McpEndpointOwned)

	remote, err := remotemcprepo.New(conn).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{ID: registration.RemoteMcpServerID.UUID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, "streamable-http", remote.TransportType)
	require.Equal(t, remoteURL, remote.Url)

	issuer, err := usersessionsrepo.New(conn).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{ID: registration.UserSessionIssuerID.UUID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, "interactive", issuer.AuthnChallengeMode)

	server, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: registration.McpServerID.UUID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, "private", server.Visibility)
	require.Equal(t, registration.RemoteMcpServerID.UUID, server.RemoteMcpServerID.UUID)
	require.Equal(t, registration.UserSessionIssuerID.UUID, server.UserSessionIssuerID.UUID)

	endpoint, err := mcpendpointsrepo.New(conn).GetMCPEndpointByID(ctx, mcpendpointsrepo.GetMCPEndpointByIDParams{ID: registration.McpEndpointID.UUID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, registration.McpServerID.UUID, endpoint.McpServerID)
	require.True(t, strings.HasPrefix(endpoint.Slug, "org-"), "endpoint slug must be organization-prefixed")

	storedReceipt, err := platformrepo.New(conn).GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
		ProjectID:      project.ID,
		Operation:      operationRegisterCatalogMCP,
		IdempotencyKey: request.IdempotencyKey,
	})
	require.NoError(t, err)
	require.Equal(t, receiptStatusSucceeded, storedReceipt.Status)
	require.Equal(t, registration.ID, storedReceipt.RegistrationID.UUID)

	handoffBinding := SetupHandoffBinding{
		ProjectID:        project.ID,
		RegistrationID:   registration.ID,
		ProviderKey:      request.CatalogProvider,
		CatalogReference: request.CatalogReference,
		Intent:           "provider_setup",
	}
	issuedHandoff, err := store.IssueSetupHandoff(ctx, principal, handoffBinding, time.Now().UTC())
	require.NoError(t, err)
	require.NotEmpty(t, issuedHandoff.Value)
	persistedHandoffHash, err := testrepo.New(conn).GetPlatformMCPSetupHandoffHashFixture(ctx, issuedHandoff.ID)
	require.NoError(t, err)
	require.Equal(t, setupHandoffHash(issuedHandoff.Value), persistedHandoffHash)
	require.NotEqual(t, issuedHandoff.Value, persistedHandoffHash)
	adapters := NewProviderAdapters([]ProviderAdapter{deterministicProviderAdapter{providerKey: request.CatalogProvider}})
	_, err = store.BeginProviderSetup(ctx, principal, handoffBinding, issuedHandoff.Value, NewProviderAdapters(nil))
	require.ErrorIs(t, err, ErrProviderAdapterUnavailable, "an unavailable adapter must not consume the handoff")
	_, err = store.BeginProviderSetup(ctx, principal, handoffBinding, issuedHandoff.Value, adapters)
	require.NoError(t, err)
	_, err = store.BeginProviderSetup(ctx, principal, handoffBinding, issuedHandoff.Value, adapters)
	require.ErrorIs(t, err, ErrSetupHandoffInvalid)
	for _, action := range []audit.Action{audit.ActionPlatformMcpRegistrationHandoffIssue, audit.ActionPlatformMcpRegistrationHandoffRedeem} {
		handoffAudit, err := audittest.LatestAuditLogByAction(ctx, conn, action)
		require.NoError(t, err)
		handoffMetadata, err := audittest.DecodeAuditData(handoffAudit.Metadata)
		require.NoError(t, err)
		require.Equal(t, issuedHandoff.ID.String(), handoffMetadata["handoff_id"])
		require.Equal(t, handoffBinding.Intent, handoffMetadata["intent"])
		require.NotContains(t, string(handoffAudit.Metadata), issuedHandoff.Value)
		require.NotContains(t, string(handoffAudit.Metadata), setupHandoffHash(issuedHandoff.Value))
	}

	preflightHandoff, err := store.IssueSetupHandoff(ctx, principal, handoffBinding, time.Now().UTC())
	require.NoError(t, err)
	preflightErr := errors.New("provider preflight unavailable")
	_, err = store.BeginProviderSetup(ctx, principal, handoffBinding, preflightHandoff.Value, NewProviderAdapters([]ProviderAdapter{deterministicProviderAdapter{providerKey: request.CatalogProvider, preflightErr: preflightErr}}))
	require.ErrorIs(t, err, preflightErr)
	_, err = store.BeginProviderSetup(ctx, principal, handoffBinding, preflightHandoff.Value, adapters)
	require.NoError(t, err, "preflight failure must leave the handoff redeemable")

	failedSetupHandoff, err := store.IssueSetupHandoff(ctx, principal, handoffBinding, time.Now().UTC())
	require.NoError(t, err)
	beginErr := errors.New("provider setup unavailable")
	_, err = store.BeginProviderSetup(ctx, principal, handoffBinding, failedSetupHandoff.Value, NewProviderAdapters([]ProviderAdapter{deterministicProviderAdapter{providerKey: request.CatalogProvider, beginErr: beginErr}}))
	require.ErrorIs(t, err, ErrSetupHandoffReissueRequired)
	require.ErrorIs(t, err, beginErr)
	_, err = store.BeginProviderSetup(ctx, principal, handoffBinding, failedSetupHandoff.Value, adapters)
	require.ErrorIs(t, err, ErrSetupHandoffInvalid, "post-consume setup failures require a new handoff")

	expiredHandoffBinding := handoffBinding
	expiredHandoffBinding.Intent = "expired_provider_setup"
	expiredDashboardHandoff, err := store.IssueSetupHandoff(ctx, principal, expiredHandoffBinding, time.Now().UTC())
	require.NoError(t, err)
	err = testrepo.New(conn).ExpirePlatformMCPSetupHandoffFixture(ctx, expiredDashboardHandoff.ID)
	require.NoError(t, err)
	_, err = platformrepo.New(conn).GetPlatformMCPSetupHandoffForDashboardStart(ctx, platformrepo.GetPlatformMCPSetupHandoffForDashboardStartParams{
		HandoffHash:    setupHandoffHash(expiredDashboardHandoff.Value),
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "dashboard setup rejects an expired handoff")

	invalidatedHandoffBinding := handoffBinding
	invalidatedHandoffBinding.Intent = "invalidated_provider_setup"
	invalidatedDashboardHandoff, err := store.IssueSetupHandoff(ctx, principal, invalidatedHandoffBinding, time.Now().UTC())
	require.NoError(t, err)
	_, err = store.IssueSetupHandoff(ctx, principal, invalidatedHandoffBinding, time.Now().UTC())
	require.NoError(t, err)
	dashboardHandoff, err := store.IssueSetupHandoff(ctx, principal, handoffBinding, time.Now().UTC())
	require.NoError(t, err)
	_, err = platformrepo.New(conn).GetPlatformMCPSetupHandoffForDashboardStart(ctx, platformrepo.GetPlatformMCPSetupHandoffForDashboardStartParams{
		HandoffHash:    setupHandoffHash(invalidatedDashboardHandoff.Value),
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "dashboard setup rejects an invalidated handoff")
	dashboardStart, err := platformrepo.New(conn).GetPlatformMCPSetupHandoffForDashboardStart(ctx, platformrepo.GetPlatformMCPSetupHandoffForDashboardStartParams{
		HandoffHash:    setupHandoffHash(dashboardHandoff.Value),
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
	})
	require.NoError(t, err)
	require.Equal(t, registration.ID, dashboardStart.RegistrationID)
	require.Equal(t, project.ID, dashboardStart.ProjectID)
	require.Equal(t, project.Slug, dashboardStart.ProjectSlug)
	require.Equal(t, handoffBinding.ProviderKey, dashboardStart.ProviderKey)
	require.Equal(t, handoffBinding.CatalogReference, dashboardStart.CatalogReference)

	_, err = platformrepo.New(conn).GetPlatformMCPSetupHandoffForDashboardStart(ctx, platformrepo.GetPlatformMCPSetupHandoffForDashboardStartParams{
		HandoffHash:    setupHandoffHash(dashboardHandoff.Value),
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN("another-user"),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "a dashboard session for another subject cannot start setup")
	_, err = platformrepo.New(conn).GetPlatformMCPSetupHandoffForDashboardStart(ctx, platformrepo.GetPlatformMCPSetupHandoffForDashboardStartParams{
		HandoffHash:    setupHandoffHash(dashboardHandoff.Value),
		OrganizationID: "another-organization",
		SubjectUrn:     userSubjectURN(principal.UserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "a dashboard session for another organization cannot start setup")

	dashboardGate := &testRegistrationGate{enabled: true}
	dashboardAuthorizer := &testAuthorizer{}
	dashboardSetup := NewDashboardSetupService(store, dashboardGate, dashboardAuthorizer, adapters)
	providerSetup, err := dashboardSetup.StartDashboardSetup(ctx, principal.UserID, principal.OrganizationID, dashboardHandoff.Value)
	require.NoError(t, err)
	require.Equal(t, "https://provider.test/authorize", providerSetup.AuthorizationURL)
	require.Equal(t, principal.OrganizationID, dashboardGate.organizationID)
	require.Equal(t, project.Slug, dashboardGate.projectSlug)
	require.Equal(t, 1, dashboardGate.calls, "dashboard setup must evaluate the registration gate")
	require.Equal(t, 1, dashboardAuthorizer.calls, "dashboard setup must reauthorize the active organization")
	_, err = dashboardSetup.StartDashboardSetup(ctx, principal.UserID, principal.OrganizationID, dashboardHandoff.Value)
	require.ErrorIs(t, err, ErrSetupHandoffInvalid, "dashboard setup must consume the handoff exactly once")

	gateDisabledHandoff, err := store.IssueSetupHandoff(ctx, principal, handoffBinding, time.Now().UTC())
	require.NoError(t, err)
	dashboardGate.enabled = false
	_, err = dashboardSetup.StartDashboardSetup(ctx, principal.UserID, principal.OrganizationID, gateDisabledHandoff.Value)
	require.ErrorIs(t, err, ErrRegistrationUnavailable)
	dashboardGate.enabled = true
	_, err = dashboardSetup.StartDashboardSetup(ctx, principal.UserID, principal.OrganizationID, gateDisabledHandoff.Value)
	require.NoError(t, err, "a disabled gate must not consume the handoff")
	dashboardAuthorizer.err = ErrForbidden
	authorizationDeniedHandoff, err := store.IssueSetupHandoff(ctx, principal, handoffBinding, time.Now().UTC())
	require.NoError(t, err)
	_, err = dashboardSetup.StartDashboardSetup(ctx, principal.UserID, principal.OrganizationID, authorizationDeniedHandoff.Value)
	require.ErrorIs(t, err, ErrForbidden)
	dashboardAuthorizer.err = nil
	_, err = dashboardSetup.StartDashboardSetup(ctx, principal.UserID, principal.OrganizationID, authorizationDeniedHandoff.Value)
	require.NoError(t, err, "an authorization denial must not consume the handoff")

	rotatedGenerationHandoff, err := store.IssueSetupHandoff(ctx, principal, handoffBinding, time.Now().UTC())
	require.NoError(t, err)
	connectionID := connectionIDFromPrincipal(t, principal)
	oldGeneration := connectionIDFromPrincipalGeneration(t, principal)
	newGeneration := uuid.New()
	_, err = platformrepo.New(conn).RotatePlatformMCPConnectionGeneration(ctx, platformrepo.RotatePlatformMCPConnectionGenerationParams{
		ActiveGeneration: newGeneration,
		ReauthorizedAt:   timestamp(time.Now().UTC()),
		ConnectionID:     connectionID,
		OrganizationID:   principal.OrganizationID,
	})
	require.NoError(t, err)
	_, err = platformrepo.New(conn).GetPlatformMCPSetupHandoffForDashboardStart(ctx, platformrepo.GetPlatformMCPSetupHandoffForDashboardStartParams{
		HandoffHash:    setupHandoffHash(rotatedGenerationHandoff.Value),
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "dashboard setup rejects a handoff after connection generation rotation")
	_, err = platformrepo.New(conn).ConsumePlatformMCPSetupHandoff(ctx, platformrepo.ConsumePlatformMCPSetupHandoffParams{
		HandoffHash:          setupHandoffHash(rotatedGenerationHandoff.Value),
		OrganizationID:       principal.OrganizationID,
		ProjectID:            handoffBinding.ProjectID,
		RegistrationID:       handoffBinding.RegistrationID,
		ConnectionID:         connectionID,
		ConnectionGeneration: oldGeneration,
		ProviderKey:          handoffBinding.ProviderKey,
		Intent:               handoffBinding.Intent,
		SubjectUrn:           userSubjectURN(principal.UserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "a rotated Platform connection cannot redeem an issued handoff")
	principal.Generation = newGeneration.String()

	readiness, err := store.ProbeProviderReadiness(ctx, principal, project.ID, registration.ID, adapters)
	require.NoError(t, err)
	require.Equal(t, ReadinessReady, readiness.State)
	require.Equal(t, "authenticated_initialize_tools_list", readiness.EvidenceCode)

	secondClient, err := platformrepo.New(conn).CreatePlatformMCPOAuthClient(ctx, platformrepo.CreatePlatformMCPOAuthClientParams{
		ClientID:              "client-" + uuid.NewString(),
		ClientSecretHash:      pgtype.Text{},
		ClientName:            "Platform MCP second test client",
		RedirectUris:          []string{"https://client.example.test/callback"},
		ClientSecretExpiresAt: pgtype.Timestamptz{},
	})
	require.NoError(t, err)
	secondConnectionID := uuid.New()
	secondGeneration := uuid.New()
	_, err = platformrepo.New(conn).CreatePlatformMCPConnection(ctx, platformrepo.CreatePlatformMCPConnectionParams{
		ID:               secondConnectionID,
		OrganizationID:   principal.OrganizationID,
		SubjectUrn:       userSubjectURN(principal.UserID),
		OauthClientID:    secondClient.ID,
		ActiveGeneration: secondGeneration,
	})
	require.NoError(t, err)
	secondPrincipal := principal
	secondPrincipal.ConnectionID = secondConnectionID.String()
	secondPrincipal.Generation = secondGeneration.String()
	_, err = store.ProbeProviderReadiness(ctx, secondPrincipal, project.ID, registration.ID, adapters)
	require.NoError(t, err, "a second active connection for the creating user may manage the registration")

	revokedConnectionHandoff, err := store.IssueSetupHandoff(ctx, secondPrincipal, handoffBinding, time.Now().UTC())
	require.NoError(t, err)
	_, err = platformrepo.New(conn).RevokePlatformMCPConnection(ctx, platformrepo.RevokePlatformMCPConnectionParams{
		RevokedAt:      timestamp(time.Now().UTC()),
		ID:             secondConnectionID,
		OrganizationID: principal.OrganizationID,
	})
	require.NoError(t, err)
	_, err = platformrepo.New(conn).GetPlatformMCPSetupHandoffForDashboardStart(ctx, platformrepo.GetPlatformMCPSetupHandoffForDashboardStartParams{
		HandoffHash:    setupHandoffHash(revokedConnectionHandoff.Value),
		OrganizationID: secondPrincipal.OrganizationID,
		SubjectUrn:     userSubjectURN(secondPrincipal.UserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "dashboard setup rejects a handoff after connection revocation")
	_, err = platformrepo.New(conn).ConsumePlatformMCPSetupHandoff(ctx, platformrepo.ConsumePlatformMCPSetupHandoffParams{
		HandoffHash:          setupHandoffHash(revokedConnectionHandoff.Value),
		OrganizationID:       secondPrincipal.OrganizationID,
		ProjectID:            handoffBinding.ProjectID,
		RegistrationID:       handoffBinding.RegistrationID,
		ConnectionID:         secondConnectionID,
		ConnectionGeneration: secondGeneration,
		ProviderKey:          handoffBinding.ProviderKey,
		Intent:               handoffBinding.Intent,
		SubjectUrn:           userSubjectURN(secondPrincipal.UserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "a revoked Platform connection cannot redeem an issued handoff")

	foreignClient, err := platformrepo.New(conn).CreatePlatformMCPOAuthClient(ctx, platformrepo.CreatePlatformMCPOAuthClientParams{
		ClientID:              "client-" + uuid.NewString(),
		ClientSecretHash:      pgtype.Text{},
		ClientName:            "Platform MCP foreign test client",
		RedirectUris:          []string{"https://client.example.test/callback"},
		ClientSecretExpiresAt: pgtype.Timestamptz{},
	})
	require.NoError(t, err)
	foreignConnectionID := uuid.New()
	foreignGeneration := uuid.New()
	foreignUserID := "user_" + uuid.NewString()
	_, err = platformrepo.New(conn).CreatePlatformMCPConnection(ctx, platformrepo.CreatePlatformMCPConnectionParams{
		ID:               foreignConnectionID,
		OrganizationID:   principal.OrganizationID,
		SubjectUrn:       userSubjectURN(foreignUserID),
		OauthClientID:    foreignClient.ID,
		ActiveGeneration: foreignGeneration,
	})
	require.NoError(t, err)
	foreignPrincipal := Principal{
		UserID:         foreignUserID,
		OrganizationID: principal.OrganizationID,
		ConnectionID:   foreignConnectionID.String(),
		Generation:     foreignGeneration.String(),
	}
	_, err = store.ProbeProviderReadiness(ctx, foreignPrincipal, project.ID, registration.ID, adapters)
	require.ErrorIs(t, err, ErrReadinessInvalid, "another user cannot manage the registration through a same-organization connection")

	foreignRequest := request
	foreignRequest.IdempotencyKey = "foreign-registration-key"
	foreignReceipt, err := store.BeginReceipt(ctx, foreignPrincipal, project, foreignRequest, time.Now().UTC())
	require.NoError(t, err)
	_, err = store.ConvergeRegistration(ctx, foreignPrincipal, project, foreignRequest, foreignReceipt)
	require.ErrorIs(t, err, ErrRegistrationInvalid, "another user cannot converge an existing registration")

	auditRecord, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionPlatformMcpRegistrationCreate)
	require.NoError(t, err)
	require.Equal(t, project.ID, auditRecord.ProjectID.UUID)
	require.Equal(t, "platform_mcp_registration", auditRecord.SubjectType)
	metadata, err := audittest.DecodeAuditData(auditRecord.Metadata)
	require.NoError(t, err)
	require.Equal(t, request.CatalogProvider, metadata["catalog_provider"])
	require.Equal(t, request.CatalogReference, metadata["catalog_reference"])
	require.Equal(t, registration.RemoteMcpServerID.UUID.String(), metadata["remote_mcp_server_id"])
	require.Equal(t, registration.UserSessionIssuerID.UUID.String(), metadata["user_session_issuer_id"])
	require.Equal(t, registration.McpServerID.UUID.String(), metadata["mcp_server_id"])
	require.Equal(t, registration.McpEndpointID.UUID.String(), metadata["mcp_endpoint_id"])
	require.NotContains(t, string(auditRecord.Metadata), remoteURL)

	outboxPayload, err := audittestrepo.New(conn).GetLatestOutboxPayloadByOrg(ctx, audittestrepo.GetLatestOutboxPayloadByOrgParams{
		OrganizationID: principal.OrganizationID,
		EventType:      "audit_log.platform_mcp_registration_event_v1",
	})
	require.NoError(t, err)
	require.NotContains(t, string(outboxPayload), remoteURL)

	auditCount, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionPlatformMcpRegistrationCreate)
	require.NoError(t, err)
	replayedRequest := request
	replayedRequest.IdempotencyKey = "registration-key-replay"
	replayedReceipt, err := store.BeginReceipt(ctx, principal, project, replayedRequest, time.Now().UTC())
	require.NoError(t, err)
	replayedReceipt, err = store.ConvergeRegistration(ctx, principal, project, replayedRequest, replayedReceipt)
	require.NoError(t, err)
	_, err = store.CompleteRegistration(ctx, principal, project, replayedRequest, replayedReceipt, remoteURL)
	require.NoError(t, err)
	auditCountAfterReplay, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionPlatformMcpRegistrationCreate)
	require.NoError(t, err)
	require.Equal(t, auditCount, auditCountAfterReplay)

	sameKeyReceipt, err := store.BeginReceipt(ctx, principal, project, request, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, sameKeyReceipt.Replayed)
	require.Equal(t, completed.ID, sameKeyReceipt.ID)
	require.Equal(t, receiptStatusSucceeded, sameKeyReceipt.Status)
	sameKeyReceipt, err = store.ConvergeRegistration(ctx, principal, project, request, sameKeyReceipt)
	require.NoError(t, err)
	require.Equal(t, completed.ID, sameKeyReceipt.ID)
	require.Equal(t, receiptStatusSucceeded, sameKeyReceipt.Status)

	plugins, err := pluginsrepo.New(conn).ListPlugins(ctx, pluginsrepo.ListPluginsParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Empty(t, plugins)
}

func connectionIDFromPrincipal(t *testing.T, principal Principal) uuid.UUID {
	t.Helper()

	connectionID, err := uuid.Parse(principal.ConnectionID)
	require.NoError(t, err)
	return connectionID
}

func connectionIDFromPrincipalGeneration(t *testing.T, principal Principal) uuid.UUID {
	t.Helper()

	generation, err := uuid.Parse(principal.Generation)
	require.NoError(t, err)
	return generation
}

func seedRegistrationLifecycle(t *testing.T, ctx context.Context, conn *pgxpool.Pool) (Principal, ResolvedProject) {
	t.Helper()

	organizationID := "org_" + uuid.NewString()
	organizationSlug := "org-" + uuid.NewString()[:8]
	_, err := organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Platform MCP test organization",
		Slug:        organizationSlug,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	projectRow, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Platform MCP test project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	oauthClient, err := platformrepo.New(conn).CreatePlatformMCPOAuthClient(ctx, platformrepo.CreatePlatformMCPOAuthClientParams{
		ClientID:              "client-" + uuid.NewString(),
		ClientSecretHash:      pgtype.Text{},
		ClientName:            "Platform MCP test client",
		RedirectUris:          []string{"https://client.example.test/callback"},
		ClientSecretExpiresAt: pgtype.Timestamptz{},
	})
	require.NoError(t, err)

	connectionID := uuid.New()
	generation := uuid.New()
	userID := "user_" + uuid.NewString()
	_, err = platformrepo.New(conn).CreatePlatformMCPConnection(ctx, platformrepo.CreatePlatformMCPConnectionParams{
		ID:               connectionID,
		OrganizationID:   organizationID,
		SubjectUrn:       userSubjectURN(userID),
		OauthClientID:    oauthClient.ID,
		ActiveGeneration: generation,
	})
	require.NoError(t, err)

	return Principal{
			UserID:         userID,
			OrganizationID: organizationID,
			ConnectionID:   connectionID.String(),
			Generation:     generation.String(),
		}, ResolvedProject{
			ID:   projectRow.ID,
			Name: projectRow.Name,
			Slug: projectRow.Slug,
		}
}
