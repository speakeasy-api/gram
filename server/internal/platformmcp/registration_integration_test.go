package platformmcp

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
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
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
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
	infra, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
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

func TestPostgresOAuthStoreRefreshReplayRecordsTerminalTransitionOnce(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_refresh_replay_terminal_metric")
	require.NoError(t, err)

	organizationID := "org_" + uuid.NewString()
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Platform MCP OAuth test organization",
		Slug:        "org-" + uuid.NewString()[:8],
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	telemetry := &testOAuthTelemetry{}
	store := NewPostgresOAuthStore(conn).WithTelemetry(telemetry)
	client := platformoauth.Client{ID: "client-" + uuid.NewString(), Name: "Platform MCP OAuth test client", RedirectURIs: []string{"https://client.example.test/callback"}}
	require.NoError(t, store.RegisterClient(ctx, client))
	connection := platformoauth.Connection{ID: uuid.NewString(), ClientID: client.ID, Subject: userSubjectURN("user_" + uuid.NewString()), OrganizationID: organizationID, Generation: uuid.NewString(), AuthorizationExpiresAt: now.Add(platformoauth.AuthorizationLifetime)}
	require.NoError(t, store.RegisterConnection(ctx, connection))
	parent := platformoauth.Session{ID: uuid.NewString(), ClientID: client.ID, Connection: connection, JTI: "jti-" + uuid.NewString(), RefreshHash: "refresh-" + uuid.NewString(), ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(30 * 24 * time.Hour)}
	require.NoError(t, store.CreateSession(ctx, parent))
	replacement := parent
	replacement.ID = uuid.NewString()
	replacement.JTI = "jti-" + uuid.NewString()
	replacement.RefreshHash = "refresh-" + uuid.NewString()
	_, err = store.RotateSession(ctx, platformoauth.RotateSessionInput{OrganizationID: organizationID, RefreshHash: parent.RefreshHash, ClientID: client.ID, Generation: connection.Generation, Now: now.Add(time.Minute), Replacement: replacement})
	require.NoError(t, err)

	_, err = store.PrepareRefresh(ctx, platformoauth.PrepareRefreshInput{OrganizationID: organizationID, RefreshHash: parent.RefreshHash, ClientID: client.ID, Now: now.Add(2 * time.Minute)})
	require.ErrorIs(t, err, platformoauth.ErrAlreadyUsed)
	_, err = store.RotateSession(ctx, platformoauth.RotateSessionInput{OrganizationID: organizationID, RefreshHash: parent.RefreshHash, ClientID: client.ID, Generation: connection.Generation, Now: now.Add(3 * time.Minute), Replacement: platformoauth.Session{ID: uuid.NewString(), ClientID: client.ID, Connection: connection, JTI: "jti-" + uuid.NewString(), RefreshHash: "refresh-" + uuid.NewString(), ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(30 * 24 * time.Hour)}})
	require.ErrorIs(t, err, platformoauth.ErrAlreadyUsed)
	require.Equal(t, []platformoauth.ReauthorizationReason{platformoauth.ReauthorizationReasonRefreshReuse}, telemetry.terminalTransitionReasons)
}

func TestPostgresOAuthStoreRefreshReplayAfterConnectionRevocationIsTerminal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_refresh_replay_revoked_connection")
	require.NoError(t, err)

	organizationID := "org_" + uuid.NewString()
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Platform MCP OAuth test organization",
		Slug:        "org-" + uuid.NewString()[:8],
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	store := NewPostgresOAuthStore(conn)
	client := platformoauth.Client{ID: "client-" + uuid.NewString(), Name: "Platform MCP OAuth test client", RedirectURIs: []string{"https://client.example.test/callback"}}
	require.NoError(t, store.RegisterClient(ctx, client))
	connection := platformoauth.Connection{
		ID:                     uuid.NewString(),
		ClientID:               client.ID,
		Subject:                userSubjectURN("user_" + uuid.NewString()),
		OrganizationID:         organizationID,
		Generation:             uuid.NewString(),
		AuthorizationExpiresAt: now.Add(platformoauth.AuthorizationLifetime),
	}
	require.NoError(t, store.RegisterConnection(ctx, connection))
	parent := platformoauth.Session{
		ID:               uuid.NewString(),
		ClientID:         client.ID,
		Connection:       connection,
		JTI:              "jti-" + uuid.NewString(),
		RefreshHash:      "refresh-" + uuid.NewString(),
		ExpiresAt:        now.Add(time.Hour),
		RefreshExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	require.NoError(t, store.CreateSession(ctx, parent))
	replacement := parent
	replacement.ID = uuid.NewString()
	replacement.JTI = "jti-" + uuid.NewString()
	replacement.RefreshHash = "refresh-" + uuid.NewString()
	_, err = store.RotateSession(ctx, platformoauth.RotateSessionInput{
		OrganizationID: organizationID,
		RefreshHash:    parent.RefreshHash,
		ClientID:       client.ID,
		Generation:     connection.Generation,
		Now:            now.Add(time.Minute),
		Replacement:    replacement,
	})
	require.NoError(t, err)
	require.NoError(t, store.RevokeConnection(ctx, organizationID, connection.ID, now.Add(2*time.Minute)))

	_, err = store.PrepareRefresh(ctx, platformoauth.PrepareRefreshInput{
		OrganizationID: organizationID,
		RefreshHash:    parent.RefreshHash,
		ClientID:       client.ID,
		Now:            now.Add(3 * time.Minute),
	})
	require.ErrorIs(t, err, platformoauth.ErrAlreadyUsed)

	_, err = store.PrepareRefresh(ctx, platformoauth.PrepareRefreshInput{
		OrganizationID: organizationID,
		RefreshHash:    replacement.RefreshHash,
		ClientID:       client.ID,
		Now:            now.Add(3 * time.Minute),
	})
	require.ErrorIs(t, err, platformoauth.ErrRevoked)
}

func TestPostgresOAuthStoreGenerationRotationRevokesGenerationCommittedWhileWaiting(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_generation_rotation_lock")
	require.NoError(t, err)

	organizationID := "org_" + uuid.NewString()
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Platform MCP rotation test organization",
		Slug:        "org-" + uuid.NewString()[:8],
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	store := NewPostgresOAuthStore(conn)
	client := platformoauth.Client{ID: "client-" + uuid.NewString(), Name: "Platform MCP rotation test client", RedirectURIs: []string{"https://client.example.test/callback"}}
	require.NoError(t, store.RegisterClient(ctx, client))
	connection := platformoauth.Connection{ID: uuid.NewString(), ClientID: client.ID, Subject: userSubjectURN("user_" + uuid.NewString()), OrganizationID: organizationID, Generation: uuid.NewString(), AuthorizationExpiresAt: now.Add(platformoauth.AuthorizationLifetime)}
	require.NoError(t, store.RegisterConnection(ctx, connection))

	connectionID := uuid.MustParse(connection.ID)
	intermediateGeneration := uuid.New()
	blockingTx := testenv.BeginTx(t, ctx, conn)
	blockingQueries := platformrepo.New(blockingTx)
	locked, err := blockingQueries.GetPlatformMCPConnectionForUpdate(ctx, platformrepo.GetPlatformMCPConnectionForUpdateParams{ID: connectionID, OrganizationID: organizationID})
	require.NoError(t, err)
	_, err = blockingQueries.RotatePlatformMCPConnectionGeneration(ctx, platformrepo.RotatePlatformMCPConnectionGenerationParams{ConnectionID: connectionID, OrganizationID: organizationID, ActiveGeneration: intermediateGeneration, ReauthorizedAt: timestamp(now.Add(time.Minute)), AuthorizationExpiresAt: timestamp(now.Add(time.Minute).Add(platformoauth.AuthorizationLifetime))})
	require.NoError(t, err)
	intermediateRefreshHash := "refresh-" + uuid.NewString()
	_, err = blockingQueries.CreatePlatformMCPSession(ctx, platformrepo.CreatePlatformMCPSessionParams{ID: uuid.New(), OrganizationID: organizationID, ConnectionID: connectionID, OauthClientID: locked.OauthClientID, ConnectionGeneration: intermediateGeneration, Jti: "jti-" + uuid.NewString(), RefreshTokenHash: intermediateRefreshHash, ExpiresAt: timestamp(now.Add(time.Hour)), RefreshExpiresAt: timestamp(now.Add(24 * time.Hour))})
	require.NoError(t, err)

	finalGeneration := uuid.New()
	rotationResult := make(chan error, 1)
	go func() {
		_, rotateErr := store.RotateConnectionGeneration(ctx, organizationID, connection.ID, finalGeneration.String(), now.Add(2*time.Minute))
		rotationResult <- rotateErr
	}()
	select {
	case rotateErr := <-rotationResult:
		require.FailNow(t, "generation rotation did not wait for the connection lock", "error: %v", rotateErr)
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, blockingTx.Commit(ctx))
	require.NoError(t, <-rotationResult)
	intermediateSession, err := platformrepo.New(conn).GetPlatformMCPSessionForRefreshForUpdate(ctx, platformrepo.GetPlatformMCPSessionForRefreshForUpdateParams{OrganizationID: organizationID, RefreshTokenHash: intermediateRefreshHash})
	require.NoError(t, err)
	require.True(t, intermediateSession.RevokedAt.Valid, "the rotation must revoke the generation that was current after acquiring the lock")
}

func TestPostgresOAuthStoreTerminalConnectionCannotBeRotatedWithoutAuthorization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_terminal_generation_rotation")
	require.NoError(t, err)

	organizationID := "org_" + uuid.NewString()
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Platform MCP terminal rotation test organization",
		Slug:        "org-" + uuid.NewString()[:8],
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	store := NewPostgresOAuthStore(conn)
	client := platformoauth.Client{ID: "client-" + uuid.NewString(), Name: "Platform MCP terminal rotation test client", RedirectURIs: []string{"https://client.example.test/callback"}}
	require.NoError(t, store.RegisterClient(ctx, client))
	connection := platformoauth.Connection{ID: uuid.NewString(), ClientID: client.ID, Subject: userSubjectURN("user_" + uuid.NewString()), OrganizationID: organizationID, Generation: uuid.NewString(), AuthorizationExpiresAt: now.Add(platformoauth.AuthorizationLifetime)}
	require.NoError(t, store.RegisterConnection(ctx, connection))
	require.NoError(t, store.MarkAuthorizationLost(ctx, organizationID, connection.ID, connection.Generation, now.Add(time.Minute)))

	_, err = store.RotateConnectionGeneration(ctx, organizationID, connection.ID, uuid.NewString(), now.Add(2*time.Minute))
	require.ErrorIs(t, err, platformoauth.ErrRevoked)

	row, err := platformrepo.New(conn).GetPlatformMCPConnectionForUpdate(ctx, platformrepo.GetPlatformMCPConnectionForUpdateParams{ID: uuid.MustParse(connection.ID), OrganizationID: organizationID})
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse(connection.Generation), row.ActiveGeneration)
	require.True(t, row.ReauthorizationRequiredAt.Valid)
	require.Equal(t, string(platformoauth.ReauthorizationReasonAuthorizationLost), row.ReauthorizationReason.String)
}

func TestRegistrationStoreAllowsFreshOrganizationTarget(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_registration_fresh_organization")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 1})
	require.NoError(t, err)

	eligible, err := store.EligibleCatalogRegistrationTarget(ctx, principal.OrganizationID, project)
	require.NoError(t, err)
	require.True(t, eligible)
}

func TestRegistrationStoreEnforcesActiveRegistrationCap(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_registration_cap")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 1})
	require.NoError(t, err)

	registeredRequest := registrationRequest(project, "registered", "registered-key")
	registeredReceipt, err := store.BeginReceipt(ctx, principal, project, registeredRequest, time.Now().UTC())
	require.NoError(t, err)
	registeredReceipt, err = store.ConvergeRegistration(ctx, principal, project, registeredRequest, registeredReceipt)
	require.NoError(t, err)
	registeredReceipt, err = store.CompleteRegistrationWithRemoteURL(ctx, principal, project, registeredRequest, registeredReceipt, "https://reviewed.example.test/registered")
	require.NoError(t, err)

	reusedRequest := registeredRequest
	reusedRequest.IdempotencyKey = "reused-key"
	reusedReceipt, err := store.BeginReceipt(ctx, principal, project, reusedRequest, time.Now().UTC())
	require.NoError(t, err)
	reusedReceipt, err = store.ConvergeRegistration(ctx, principal, project, reusedRequest, reusedReceipt)
	require.NoError(t, err)
	require.Equal(t, registeredReceipt.RegistrationID, reusedReceipt.RegistrationID)

	deniedRequest := registrationRequest(project, "denied", "denied-key")
	deniedReceipt, err := store.BeginReceipt(ctx, principal, project, deniedRequest, time.Now().UTC())
	require.NoError(t, err)
	deniedReceipt, err = store.ConvergeRegistration(ctx, principal, project, deniedRequest, deniedReceipt)
	require.ErrorIs(t, err, ErrRegistrationCap)
	require.Equal(t, receiptStatusSucceeded, deniedReceipt.Status)
	require.Equal(t, receiptResultActiveCap, deniedReceipt.ResultCode)
	require.False(t, deniedReceipt.RegistrationID.Valid)

	storedDeniedReceipt, err := platformrepo.New(conn).GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID,
		UserID:         conv.ToPGText(principal.UserID),
		SubjectUrn:     userSubjectURN(principal.UserID),
		ProjectID:      project.ID,
		Operation:      operationRegisterCatalogMCP,
		IdempotencyKey: deniedRequest.IdempotencyKey,
	})
	require.NoError(t, err)
	require.Equal(t, receiptStatusSucceeded, storedDeniedReceipt.Status)
	require.Equal(t, receiptResultActiveCap, storedDeniedReceipt.ResultCode.String)
	require.False(t, storedDeniedReceipt.RegistrationID.Valid)

	registrations, err := platformrepo.New(conn).CountActiveRegisteredPlatformMCPCatalogRegistrations(ctx, platformrepo.CountActiveRegisteredPlatformMCPCatalogRegistrationsParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, registrations)
	_, err = platformrepo.New(conn).GetActivePlatformMCPCatalogRegistration(ctx, platformrepo.GetActivePlatformMCPCatalogRegistrationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		SourceKind:       deniedRequest.SourceKind,
		CatalogProvider:  deniedRequest.CatalogProvider,
		CatalogReference: deniedRequest.CatalogReference,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	auditCount, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionPlatformMcpRegistrationCreate)
	require.NoError(t, err)
	require.EqualValues(t, 1, auditCount)

	replayedReceipt, err := store.BeginReceipt(ctx, principal, project, deniedRequest, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, replayedReceipt.Replayed)
	require.Equal(t, deniedReceipt.ID, replayedReceipt.ID)
	replayedReceipt, err = store.ConvergeRegistration(ctx, principal, project, deniedRequest, replayedReceipt)
	require.ErrorIs(t, err, ErrRegistrationCap)
	require.True(t, replayedReceipt.Replayed)
	require.Equal(t, receiptResultActiveCap, replayedReceipt.ResultCode)
}

func TestRegistrationStoreSerializesCapRejectionsForDistinctCandidates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_registration_cap_concurrent")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 1})
	require.NoError(t, err)

	registeredRequest := registrationRequest(project, "registered", "registered-key")
	registeredReceipt, err := store.BeginReceipt(ctx, principal, project, registeredRequest, time.Now().UTC())
	require.NoError(t, err)
	registeredReceipt, err = store.ConvergeRegistration(ctx, principal, project, registeredRequest, registeredReceipt)
	require.NoError(t, err)
	_, err = store.CompleteRegistrationWithRemoteURL(ctx, principal, project, registeredRequest, registeredReceipt, "https://reviewed.example.test/registered")
	require.NoError(t, err)

	requests := []CatalogRegistrationRequest{
		registrationRequest(project, "first", "first-key"),
		registrationRequest(project, "second", "second-key"),
	}
	errorsByRequest := make(chan error, len(requests))
	var start sync.WaitGroup
	start.Add(1)
	for _, request := range requests {
		go func(request CatalogRegistrationRequest) {
			start.Wait()
			receipt, err := store.BeginReceipt(ctx, principal, project, request, time.Now().UTC())
			if err == nil {
				_, err = store.ConvergeRegistration(ctx, principal, project, request, receipt)
			}
			errorsByRequest <- err
		}(request)
	}
	start.Done()
	for range requests {
		require.ErrorIs(t, <-errorsByRequest, ErrRegistrationCap)
	}

	registrations, err := platformrepo.New(conn).CountActiveRegisteredPlatformMCPCatalogRegistrations(ctx, platformrepo.CountActiveRegisteredPlatformMCPCatalogRegistrationsParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, registrations)
}

func TestRegistrationStoreDoesNotCountPendingRegistrationsTowardActiveCap(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_registration_pending_cap")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 1})
	require.NoError(t, err)

	pendingRequest := registrationRequest(project, "pending", "pending-key")
	pendingReceipt, err := store.BeginReceipt(ctx, principal, project, pendingRequest, time.Now().UTC())
	require.NoError(t, err)
	pendingReceipt, err = store.ConvergeRegistration(ctx, principal, project, pendingRequest, pendingReceipt)
	require.NoError(t, err)
	require.True(t, pendingReceipt.RegistrationID.Valid)

	secondRequest := registrationRequest(project, "second", "second-key")
	secondReceipt, err := store.BeginReceipt(ctx, principal, project, secondRequest, time.Now().UTC())
	require.NoError(t, err)
	secondReceipt, err = store.ConvergeRegistration(ctx, principal, project, secondRequest, secondReceipt)
	require.NoError(t, err)
	require.True(t, secondReceipt.RegistrationID.Valid)

	completeRequests := []struct {
		request CatalogRegistrationRequest
		receipt OperationReceipt
		remote  string
	}{
		{request: pendingRequest, receipt: pendingReceipt, remote: "https://reviewed.example.test/pending"},
		{request: secondRequest, receipt: secondReceipt, remote: "https://reviewed.example.test/second"},
	}
	completeErrors := make(chan error, len(completeRequests))
	var start sync.WaitGroup
	start.Add(1)
	for _, complete := range completeRequests {
		go func(complete struct {
			request CatalogRegistrationRequest
			receipt OperationReceipt
			remote  string
		}) {
			start.Wait()
			_, err := store.CompleteRegistrationWithRemoteURL(ctx, principal, project, complete.request, complete.receipt, complete.remote)
			completeErrors <- err
		}(complete)
	}
	start.Done()

	var succeeded, capped int
	for range completeRequests {
		err := <-completeErrors
		if err == nil {
			succeeded++
			continue
		}
		require.ErrorIs(t, err, ErrRegistrationCap)
		capped++
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, capped)

	registrations, err := platformrepo.New(conn).CountActiveRegisteredPlatformMCPCatalogRegistrations(ctx, platformrepo.CountActiveRegisteredPlatformMCPCatalogRegistrationsParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, registrations)
}

func TestRegistrationStoreCompleteRegistrationConvergesPrivateComponents(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_registration")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 5})
	require.NoError(t, err)
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
	completed, err := store.CompleteRegistration(ctx, principal, project, request, receipt, resolvedCatalogConfiguration{
		remoteURL:   remoteURL,
		displayName: "Reviewed MCP",
		headers: []resolvedCatalogHeader{{
			name:     "X-API-Key",
			required: true,
			secret:   true,
		}},
	})
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
	require.Equal(t, "Reviewed MCP source", remote.Name.String)
	require.Equal(t, remoteURL, remote.Url)
	headers, err := remotemcprepo.New(conn).ListServerHeaders(ctx, remotemcprepo.ListServerHeadersParams{RemoteMcpServerID: remote.ID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Len(t, headers, 1)
	require.Equal(t, "X-API-Key", headers[0].Name)
	require.True(t, headers[0].IsSecret)
	require.True(t, headers[0].Value.Valid)
	require.Empty(t, headers[0].Value.String)
	require.False(t, headers[0].ValueFromRequestHeader.Valid)
	declaredSecret := []CatalogConfigurationField{{Key: "header:x-api-key", Kind: "header", Name: "X-API-Key", Required: true, Secret: true}}
	pending, err := store.ResolveRegistrationPendingSecretFields(ctx, principal, project, registration.ID, declaredSecret)
	require.NoError(t, err)
	require.Equal(t, declaredSecret, pending)

	issuer, err := usersessionsrepo.New(conn).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{ID: registration.UserSessionIssuerID.UUID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, "interactive", issuer.AuthnChallengeMode)

	server, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: registration.McpServerID.UUID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, "private", server.Visibility)
	require.Equal(t, "Reviewed MCP", server.Name.String)
	require.Equal(t, registration.RemoteMcpServerID.UUID, server.RemoteMcpServerID.UUID)
	require.Equal(t, registration.UserSessionIssuerID.UUID, server.UserSessionIssuerID.UUID)

	endpoint, err := mcpendpointsrepo.New(conn).GetMCPEndpointByID(ctx, mcpendpointsrepo.GetMCPEndpointByIDParams{ID: registration.McpEndpointID.UUID, ProjectID: project.ID})
	require.NoError(t, err)
	require.True(t, endpoint.McpServerID.Valid)
	require.Equal(t, registration.McpServerID.UUID, endpoint.McpServerID.UUID)
	require.True(t, strings.HasPrefix(endpoint.Slug, "org-"), "endpoint slug must be organization-prefixed")

	storedReceipt, err := platformrepo.New(conn).GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID,
		UserID:         conv.ToPGText(principal.UserID),
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
	assertSetupMilestone(t, ctx, conn, principal.OrganizationID, project.ID, request.CatalogProvider+":"+request.CatalogReference, issuedHandoff.ID, "provider_setup_started")
	ready, err := store.ProbeProviderReadiness(ctx, principal, project.ID, registration.ID, adapters)
	require.NoError(t, err)
	require.Equal(t, ReadinessReady, ready.State)
	assertSetupMilestone(t, ctx, conn, principal.OrganizationID, project.ID, request.CatalogProvider+":"+request.CatalogReference, issuedHandoff.ID, "provider_setup_succeeded")
	assertSetupMilestone(t, ctx, conn, principal.OrganizationID, project.ID, request.CatalogProvider+":"+request.CatalogReference, issuedHandoff.ID, "platform_flow_ready")
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
		UserID:         conv.ToPGText(principal.UserID),
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
		UserID:         conv.ToPGText(principal.UserID),
		SubjectUrn:     userSubjectURN(principal.UserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "dashboard setup rejects an invalidated handoff")
	dashboardStart, err := platformrepo.New(conn).GetPlatformMCPSetupHandoffForDashboardStart(ctx, platformrepo.GetPlatformMCPSetupHandoffForDashboardStartParams{
		HandoffHash:    setupHandoffHash(dashboardHandoff.Value),
		OrganizationID: principal.OrganizationID,
		UserID:         conv.ToPGText(principal.UserID),
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
	dashboardSetup := NewDashboardSetupService(store, dashboardGate, dashboardAuthorizer, adapters, testOperationBudget())
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
		ActiveGeneration:       newGeneration,
		ReauthorizedAt:         timestamp(time.Now().UTC()),
		AuthorizationExpiresAt: timestamp(time.Now().UTC().Add(90 * 24 * time.Hour)),
		ConnectionID:           connectionID,
		OrganizationID:         principal.OrganizationID,
	})
	require.NoError(t, err)
	_, err = platformrepo.New(conn).GetPlatformMCPSetupHandoffForDashboardStart(ctx, platformrepo.GetPlatformMCPSetupHandoffForDashboardStartParams{
		HandoffHash:    setupHandoffHash(rotatedGenerationHandoff.Value),
		OrganizationID: principal.OrganizationID,
		UserID:         conv.ToPGText(principal.UserID),
		SubjectUrn:     userSubjectURN(principal.UserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "dashboard setup rejects a handoff after connection generation rotation")
	_, err = platformrepo.New(conn).ConsumePlatformMCPSetupHandoff(ctx, platformrepo.ConsumePlatformMCPSetupHandoffParams{
		HandoffHash:          setupHandoffHash(rotatedGenerationHandoff.Value),
		OrganizationID:       principal.OrganizationID,
		ProjectID:            handoffBinding.ProjectID,
		RegistrationID:       handoffBinding.RegistrationID,
		ConnectionID:         uuid.NullUUID{UUID: connectionID, Valid: true},
		ConnectionGeneration: uuid.NullUUID{UUID: oldGeneration, Valid: true},
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
		ID:                     secondConnectionID,
		OrganizationID:         principal.OrganizationID,
		SubjectUrn:             userSubjectURN(principal.UserID),
		OauthClientID:          secondClient.ID,
		ActiveGeneration:       secondGeneration,
		AuthorizationExpiresAt: timestamp(time.Now().UTC().Add(90 * 24 * time.Hour)),
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
		ConnectionID:         uuid.NullUUID{UUID: secondConnectionID, Valid: true},
		ConnectionGeneration: uuid.NullUUID{UUID: secondGeneration, Valid: true},
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
		ID:                     foreignConnectionID,
		OrganizationID:         principal.OrganizationID,
		SubjectUrn:             userSubjectURN(foreignUserID),
		OauthClientID:          foreignClient.ID,
		ActiveGeneration:       foreignGeneration,
		AuthorizationExpiresAt: timestamp(time.Now().UTC().Add(90 * 24 * time.Hour)),
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
	_, err = store.CompleteRegistrationWithRemoteURL(ctx, principal, project, replayedRequest, replayedReceipt, remoteURL)
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

func assertSetupMilestone(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, mcpKey string, attemptID uuid.UUID, milestone string) {
	t.Helper()

	count, err := testrepo.New(conn).CountPlatformMCPSetupMilestoneFixture(ctx, testrepo.CountPlatformMCPSetupMilestoneFixtureParams{
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: projectID, Valid: true},
		McpKey:         mcpKey,
		AttemptID:      uuid.NullUUID{UUID: attemptID, Valid: true},
		Milestone:      milestone,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func testOperationBudget() OperationBudget {
	return OperationBudget{Connection: allowOperationLimiter{}, Organization: allowOperationLimiter{}}
}

func registrationRequest(project ResolvedProject, catalogReference, idempotencyKey string) CatalogRegistrationRequest {
	return CatalogRegistrationRequest{
		ProjectSlug:      project.Slug,
		SourceKind:       "catalog",
		CatalogProvider:  "fixture",
		CatalogReference: catalogReference,
		IdempotencyKey:   idempotencyKey,
		InputHash:        catalogRegistrationInputHash(project.Slug, "catalog", "fixture", catalogReference),
	}
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
	seedRegistrationEligibleCohort(t, ctx, conn, projectRow.ID)

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
	now := time.Now().UTC()
	_, err = platformrepo.New(conn).CreatePlatformMCPConnection(ctx, platformrepo.CreatePlatformMCPConnectionParams{
		ID:                     connectionID,
		OrganizationID:         organizationID,
		SubjectUrn:             userSubjectURN(userID),
		OauthClientID:          oauthClient.ID,
		ActiveGeneration:       generation,
		AuthorizationExpiresAt: timestamp(now.Add(90 * 24 * time.Hour)),
	})
	require.NoError(t, err)
	_, err = platformrepo.New(conn).CreatePlatformMCPSession(ctx, platformrepo.CreatePlatformMCPSessionParams{
		ID:                   uuid.New(),
		OrganizationID:       organizationID,
		ConnectionID:         connectionID,
		OauthClientID:        oauthClient.ID,
		ConnectionGeneration: generation,
		Jti:                  "jti-" + uuid.NewString(),
		RefreshTokenHash:     "refresh-" + uuid.NewString(),
		ExpiresAt:            timestamp(now.Add(time.Hour)),
		RefreshExpiresAt:     timestamp(now.Add(30 * 24 * time.Hour)),
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

func seedRegistrationEligibleCohort(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) {
	t.Helper()

	issuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		Slug:               "cohort-issuer-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	remote, err := remotemcprepo.New(conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           "https://cohort.example.test/mcp",
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText("Registration cohort server"),
		Slug:                conv.ToPGText("cohort-server-" + uuid.NewString()[:8]),
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: remote.ID, Valid: true},
		Visibility:          "private",
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:   projectID,
		McpServerID: uuid.NullUUID{UUID: server.ID, Valid: true},
		Slug:        "cohort-endpoint-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)
}

// The project assistant holds no OAuth connection. Its writes must still land,
// attributed to the real user and its acting surface, and must still replay
// idempotently — the property that breaks first if a read path joins through
// the connection it does not have.
// The assistant issues a handoff with no connection and the dashboard, which
// authenticates under its own session, redeems it. This is the whole point of
// the connection-less surfaces: neither step can key on a connection.
func TestSetupHandoffRoundTripsWithoutAConnection(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_handoff_no_connection")
	require.NoError(t, err)

	connected, project := seedRegistrationLifecycle(t, ctx, conn)
	assistant := Principal{
		UserID:         connected.UserID,
		OrganizationID: connected.OrganizationID,
		ConnectionID:   "",
		Generation:     "",
		ClientID:       "gram-project-assistant",
		Surface:        SurfaceProjectAssistant,
	}
	require.False(t, assistant.HasConnection())

	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 5})
	require.NoError(t, err)

	request := registrationRequest(project, "assistant-handoff", "assistant-handoff-key")
	receipt, err := store.BeginReceipt(ctx, assistant, project, request, time.Now().UTC())
	require.NoError(t, err)
	receipt, err = store.ConvergeRegistration(ctx, assistant, project, request, receipt)
	require.NoError(t, err)
	receipt, err = store.CompleteRegistrationWithRemoteURL(ctx, assistant, project, request, receipt, "https://reviewed.example.test/assistant-handoff")
	require.NoError(t, err)
	require.True(t, receipt.RegistrationID.Valid)

	binding := SetupHandoffBinding{
		ProjectID:        project.ID,
		RegistrationID:   receipt.RegistrationID.UUID,
		ProviderKey:      request.CatalogProvider,
		CatalogReference: request.CatalogReference,
		Intent:           "dashboard_source_settings",
	}
	issued, err := store.IssueSetupHandoff(ctx, assistant, binding, time.Now().UTC())
	require.NoError(t, err, "a connection-less surface must be able to issue a handoff")

	// The dashboard finds it by user, with no connection on the row to match.
	start, err := platformrepo.New(conn).GetPlatformMCPSetupHandoffForDashboardStart(ctx, platformrepo.GetPlatformMCPSetupHandoffForDashboardStartParams{
		HandoffHash:    setupHandoffHash(issued.Value),
		OrganizationID: assistant.OrganizationID,
		UserID:         conv.ToPGText(assistant.UserID),
		SubjectUrn:     userSubjectURN(assistant.UserID),
	})
	require.NoError(t, err, "the dashboard must find a connection-less handoff")
	require.False(t, start.ConnectionID.Valid)

	dashboard := Principal{
		UserID:         assistant.UserID,
		OrganizationID: assistant.OrganizationID,
		ConnectionID:   "",
		Generation:     "",
		ClientID:       "",
		Surface:        SurfaceDashboard,
	}
	consumed, err := store.ConsumeSetupHandoff(ctx, dashboard, binding, issued.Value)
	require.NoError(t, err, "the dashboard must be able to redeem a connection-less handoff")
	require.Equal(t, issued.ID, consumed.ID)

	_, err = store.ConsumeSetupHandoff(ctx, dashboard, binding, issued.Value)
	require.Error(t, err, "a redeemed handoff must not be redeemable twice")
}

// A connection-less surface records catalogue evidence against its user, and
// repeat searches must not append a row per search.
func TestCatalogExploredEvidenceIsIdempotentWithoutAConnection(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_catalog_evidence_no_connection")
	require.NoError(t, err)

	connected, _ := seedRegistrationLifecycle(t, ctx, conn)
	assistant := Principal{
		UserID:         connected.UserID,
		OrganizationID: connected.OrganizationID,
		ConnectionID:   "",
		Generation:     "",
		ClientID:       "gram-project-assistant",
		Surface:        SurfaceProjectAssistant,
	}

	onboarding := &OnboardingService{db: conn}
	require.NoError(t, onboarding.RecordCatalogExplored(ctx, assistant))
	require.NoError(t, onboarding.RecordCatalogExplored(ctx, assistant), "a repeat search must stay idempotent")
}

func TestRegistrationStoreWritesWithoutAConnection(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_registration_no_connection")
	require.NoError(t, err)

	connected, project := seedRegistrationLifecycle(t, ctx, conn)
	assistant := Principal{
		UserID:         connected.UserID,
		OrganizationID: connected.OrganizationID,
		ConnectionID:   "",
		Generation:     "",
		ClientID:       "gram-project-assistant",
		Surface:        SurfaceProjectAssistant,
	}
	require.False(t, assistant.HasConnection())

	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 5})
	require.NoError(t, err)

	request := registrationRequest(project, "assistant-registered", "assistant-key")
	receipt, err := store.BeginReceipt(ctx, assistant, project, request, time.Now().UTC())
	require.NoError(t, err, "an assistant write must not require an OAuth connection")
	require.False(t, receipt.ConnectionID.Valid, "no connection applies, which is not the same as a zero uuid")

	receipt, err = store.ConvergeRegistration(ctx, assistant, project, request, receipt)
	require.NoError(t, err)
	receipt, err = store.CompleteRegistrationWithRemoteURL(ctx, assistant, project, request, receipt, "https://reviewed.example.test/assistant")
	require.NoError(t, err)
	require.True(t, receipt.RegistrationID.Valid)

	stored, err := platformrepo.New(conn).GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: assistant.OrganizationID,
		UserID:         conv.ToPGText(assistant.UserID),
		SubjectUrn:     userSubjectURN(assistant.UserID),
		ProjectID:      project.ID,
		Operation:      operationRegisterCatalogMCP,
		IdempotencyKey: request.IdempotencyKey,
	})
	require.NoError(t, err, "a connection-less receipt must still be readable, or its idempotency key is poisoned")
	require.False(t, stored.ConnectionID.Valid)
	require.Equal(t, assistant.UserID, stored.UserID.String)
	require.Equal(t, string(SurfaceProjectAssistant), stored.ActingSurface.String)

	replay, err := store.BeginReceipt(ctx, assistant, project, request, time.Now().UTC())
	require.NoError(t, err, "replaying the same key must return the original receipt, not a unique violation")
	require.True(t, replay.Replayed)
	require.Equal(t, receipt.ID, replay.ID)
}
