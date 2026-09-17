package usersessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestOrganizationUserSessionIssuerMoveRetiersIssuerData(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := seedIssuer(t, ctx, ti, "move-retier")
	client, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "move-retier-client")
	require.NoError(t, err)
	subject := urn.NewUserSubject("move-retier-user")
	session, err := seedUserSessionForClient(t, ctx, ti.conn, issuerID, client.ID, subject)
	require.NoError(t, err)
	consent, err := seedUserSessionConsent(t, ctx, ti.conn, client.ID, subject)
	require.NoError(t, err)
	cimd, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuerCimdClient(ctx, usersessionsrepo.CreateUserSessionIssuerCimdClientParams{
		ProjectID:           *authCtx.ProjectID,
		ClientIDMetadataUri: "https://move-retier.example.com/client",
		UserSessionIssuerID: issuerID,
	})
	require.NoError(t, err)

	moved, err := ti.service.MoveIssuer(ctx, &orggen.MoveIssuerPayload{ID: issuerID.String()})
	require.NoError(t, err)
	require.Equal(t, issuerID.String(), moved.ID)
	require.Empty(t, moved.ProjectID)
	require.Equal(t, authCtx.ActiveOrganizationID, moved.OrganizationID)

	repo := usersessionsrepo.New(ti.conn)
	clientAfter, err := repo.GetUserSessionClientByID(ctx, usersessionsrepo.GetUserSessionClientByIDParams{ID: client.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.False(t, clientAfter.ProjectID.Valid)
	requireOrganizationID(t, ctx, clientAfter.OrganizationID)
	sessionAfter, err := repo.GetUserSessionByID(ctx, usersessionsrepo.GetUserSessionByIDParams{ID: session.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.False(t, sessionAfter.ProjectID.Valid)
	requireOrganizationID(t, ctx, sessionAfter.OrganizationID)
	consentAfter, err := repo.GetUserSessionConsentByID(ctx, usersessionsrepo.GetUserSessionConsentByIDParams{ID: consent.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.False(t, consentAfter.ProjectID.Valid)
	requireOrganizationID(t, ctx, consentAfter.OrganizationID)
	cimdAfter, err := repo.GetUserSessionIssuerCimdClientByID(ctx, usersessionsrepo.GetUserSessionIssuerCimdClientByIDParams{ID: cimd.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.False(t, cimdAfter.ProjectID.Valid)
	requireOrganizationID(t, ctx, cimdAfter.OrganizationID)

	projectID := authCtx.ProjectID.String()
	movedBack, err := ti.service.MoveIssuer(ctx, &orggen.MoveIssuerPayload{ID: issuerID.String(), ProjectID: &projectID})
	require.NoError(t, err)
	require.Equal(t, projectID, movedBack.ProjectID)
	clientAfter, err = repo.GetUserSessionClientByID(ctx, usersessionsrepo.GetUserSessionClientByIDParams{ID: client.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.True(t, clientAfter.ProjectID.Valid)
	require.Equal(t, *authCtx.ProjectID, clientAfter.ProjectID.UUID)
	sessionAfter, err = repo.GetUserSessionByID(ctx, usersessionsrepo.GetUserSessionByIDParams{ID: session.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.True(t, sessionAfter.ProjectID.Valid)
	require.Equal(t, *authCtx.ProjectID, sessionAfter.ProjectID.UUID)
	consentAfter, err = repo.GetUserSessionConsentByID(ctx, usersessionsrepo.GetUserSessionConsentByIDParams{ID: consent.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.True(t, consentAfter.ProjectID.Valid)
	require.Equal(t, *authCtx.ProjectID, consentAfter.ProjectID.UUID)
	cimdAfter, err = repo.GetUserSessionIssuerCimdClientByID(ctx, usersessionsrepo.GetUserSessionIssuerCimdClientByIDParams{ID: cimd.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.True(t, cimdAfter.ProjectID.Valid)
	require.Equal(t, *authCtx.ProjectID, cimdAfter.ProjectID.UUID)

	moveAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, issuerID.String(), moveAudit.SubjectID)
}

func TestOrganizationUserSessionIssuerMoveBlocksChildInAnotherProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuer, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "move-child-blocker", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	issuerID := uuid.MustParse(issuer.ID)
	client, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "move-child-blocker-client")
	require.NoError(t, err)
	siblingProjectID := createSiblingProject(t, ctx, ti.conn, "move-child-blocker-sibling")

	_, err = usersessionsrepo.New(ti.conn).RetierUserSessionClients(ctx, usersessionsrepo.RetierUserSessionClientsParams{
		ProjectID:           uuid.NullUUID{UUID: siblingProjectID, Valid: true},
		OrganizationID:      authCtx.ActiveOrganizationID,
		UserSessionIssuerID: issuerID,
	})
	require.NoError(t, err)

	targetProjectID := authCtx.ProjectID.String()
	_, err = ti.service.MoveIssuer(ctx, &orggen.MoveIssuerPayload{ID: issuer.ID, ProjectID: &targetProjectID})
	requireOopsCode(t, err, oops.CodeConflict)

	clientAfter, err := usersessionsrepo.New(ti.conn).GetUserSessionClientByID(ctx, usersessionsrepo.GetUserSessionClientByIDParams{ID: client.ID, ProjectID: siblingProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, siblingProjectID, clientAfter.ProjectID.UUID)
}

func TestOrganizationUserSessionIssuerMigrateWidensScopeAndPreservesData(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sourceID := seedIssuer(t, ctx, ti, "migrate-source")
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		Slug:                 "migrate-target",
		AuthnChallengeMode:   "interactive",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	targetID := uuid.MustParse(target.ID)

	client, err := seedUserSessionClient(t, ctx, ti.conn, sourceID, "migrate-client")
	require.NoError(t, err)
	subject := urn.NewUserSubject("migrate-user")
	session, err := seedUserSessionForClient(t, ctx, ti.conn, sourceID, client.ID, subject)
	require.NoError(t, err)
	consent, err := seedUserSessionConsent(t, ctx, ti.conn, client.ID, subject)
	require.NoError(t, err)
	cimd, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuerCimdClient(ctx, usersessionsrepo.CreateUserSessionIssuerCimdClientParams{
		ProjectID:           *authCtx.ProjectID,
		ClientIDMetadataUri: "https://migrate.example.com/client",
		UserSessionIssuerID: sourceID,
	})
	require.NoError(t, err)
	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		Name: "Migration toolset", Slug: "migration-toolset", Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{}, McpSlug: pgtype.Text{}, McpEnabled: false,
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: sourceID, Valid: true}, Slug: toolset.Slug, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	mcpServerID := uuid.New()
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: mcpServerID, ProjectID: *authCtx.ProjectID,
		Name: pgtype.Text{String: "Migration server", Valid: true}, Slug: pgtype.Text{String: "migration-server", Valid: true},
		EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: uuid.NullUUID{UUID: sourceID, Valid: true}, RemoteMcpServerID: uuid.NullUUID{},
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ToolVariationsGroupID: uuid.NullUUID{}, Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: target.ID})
	require.NoError(t, err)
	require.True(t, preflight.CanMigrate)
	require.Equal(t, 1, preflight.ClientCount)
	require.Equal(t, 1, preflight.SessionCount)
	require.Equal(t, 1, preflight.ConsentCount)
	require.Equal(t, 1, preflight.CimdClientCount)
	require.Empty(t, preflight.ConflictingClientIds)
	require.Len(t, preflight.Warnings, 1)
	require.Equal(t, "authn_challenge_mode", preflight.Warnings[0].Field)
	require.NotEmpty(t, preflight.WarningsFingerprint)

	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: target.ID})
	requireOopsCode(t, err, oops.CodeConflict)

	result, err := ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: target.ID, ConfirmedWarningsFingerprint: &preflight.WarningsFingerprint})
	require.NoError(t, err)
	require.True(t, result.SourceDeleted)
	require.Equal(t, target.ID, result.Issuer.ID)
	require.Equal(t, 1, result.ClientsMigrated)
	require.Equal(t, 1, result.SessionsMigrated)
	require.Equal(t, 1, result.ConsentsMigrated)
	require.Equal(t, 1, result.CimdClientsMigrated)

	repo := usersessionsrepo.New(ti.conn)
	clientAfter, err := repo.GetUserSessionClientByID(ctx, usersessionsrepo.GetUserSessionClientByIDParams{ID: client.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, targetID, clientAfter.UserSessionIssuerID)
	require.False(t, clientAfter.ProjectID.Valid)
	requireOrganizationID(t, ctx, clientAfter.OrganizationID)
	sessionAfter, err := repo.GetUserSessionByID(ctx, usersessionsrepo.GetUserSessionByIDParams{ID: session.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, targetID, sessionAfter.UserSessionIssuerID)
	require.False(t, sessionAfter.ProjectID.Valid)
	consentAfter, err := repo.GetUserSessionConsentByID(ctx, usersessionsrepo.GetUserSessionConsentByIDParams{ID: consent.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, targetID, consentAfter.UserSessionIssuerID)
	require.False(t, consentAfter.ProjectID.Valid)
	cimdAfter, err := repo.GetUserSessionIssuerCimdClientByID(ctx, usersessionsrepo.GetUserSessionIssuerCimdClientByIDParams{ID: cimd.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, targetID, cimdAfter.UserSessionIssuerID)
	require.False(t, cimdAfter.ProjectID.Valid)
	toolsetAfter, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndOrganization(ctx, toolsetsrepo.GetToolsetByIDAndOrganizationParams{ID: toolset.ID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.True(t, toolsetAfter.UserSessionIssuerID.Valid)
	require.Equal(t, targetID, toolsetAfter.UserSessionIssuerID.UUID)
	mcpServerAfter, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: mcpServerID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.True(t, mcpServerAfter.UserSessionIssuerID.Valid)
	require.Equal(t, targetID, mcpServerAfter.UserSessionIssuerID.UUID)

	_, err = ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: sourceID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
	migrateAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerMigrate)
	require.NoError(t, err)
	require.Equal(t, sourceID.String(), migrateAudit.SubjectID)
}

func TestOrganizationUserSessionIssuerMigratePreflightBlocksClientIDConflicts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "conflict-source", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "conflict-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	_, err = seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(source.ID), "duplicate-client")
	require.NoError(t, err)
	_, err = seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(target.ID), "duplicate-client")
	require.NoError(t, err)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: source.ID, TargetID: target.ID})
	require.NoError(t, err)
	require.False(t, preflight.CanMigrate)
	require.Equal(t, []string{"duplicate-client"}, preflight.ConflictingClientIds)

	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: source.ID, TargetID: target.ID})
	requireOopsCode(t, err, oops.CodeConflict)
	_, err = ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: source.ID})
	require.NoError(t, err)
}

func TestOrganizationUserSessionIssuerMigrateNormalizesLegacyProjectIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sourceID := seedLegacyProjectIssuer(t, ctx, ti.conn, "legacy-migrate-source")
	targetID := seedIssuer(t, ctx, ti, "legacy-migrate-target")
	client, err := seedUserSessionClient(t, ctx, ti.conn, sourceID, "legacy-migrate-client")
	require.NoError(t, err)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	require.NoError(t, err)
	require.True(t, preflight.CanMigrate)
	require.Equal(t, 1, preflight.ClientCount)
	require.Empty(t, preflight.WarningsFingerprint)

	result, err := ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	require.NoError(t, err)
	require.True(t, result.SourceDeleted)
	require.Equal(t, targetID.String(), result.Issuer.ID)

	clientAfter, err := usersessionsrepo.New(ti.conn).GetUserSessionClientByID(ctx, usersessionsrepo.GetUserSessionClientByIDParams{ID: client.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, targetID, clientAfter.UserSessionIssuerID)
}

func TestOrganizationUserSessionIssuerMigratePreservesRemoteBindings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sourceID := seedIssuer(t, ctx, ti, "remote-migrate-source")
	targetID := seedIssuer(t, ctx, ti, "remote-migrate-target")
	remoteRepo := remotesessionsrepo.New(ti.conn)
	remoteIssuer, err := remoteRepo.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              "remote-migrate-issuer",
		Issuer:                            "https://remote-migrate.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://remote-migrate.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://remote-migrate.example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
	})
	require.NoError(t, err)
	remoteClient, err := remoteRepo.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID: remoteIssuer.ID,
		ClientID:              "remote-migrate-client",
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, remoteRepo.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: remoteClient.ID,
		UserSessionIssuerID:   sourceID,
	}))

	subject := urn.NewUserSubject("remote-migrate-user")
	remoteSession, err := remoteRepo.UpsertRemoteSession(ctx, remotesessionsrepo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   sourceID,
		RemoteSessionClientID: remoteClient.ID,
		AccessTokenEncrypted:  "test-ciphertext",
		Scopes:                []string{"openid"},
	})
	require.NoError(t, err)
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		Name:           "Remote migration agent",
	})
	require.NoError(t, err)
	fixtures := testrepo.New(ti.conn)
	principalBindingID, err := fixtures.InsertPrincipalRemoteSessionBindingFixture(ctx, testrepo.InsertPrincipalRemoteSessionBindingFixtureParams{
		ProjectID:             *authCtx.ProjectID,
		OrganizationID:        authCtx.ActiveOrganizationID,
		PrincipalID:           agent.ID,
		UserSessionIssuerID:   sourceID,
		RemoteSessionClientID: remoteClient.ID,
		RemoteSessionID:       remoteSession.ID,
		GrantGeneration:       remoteSession.GrantGeneration,
		AttachedBySubjectID:   "remote-migrate-user",
	})
	require.NoError(t, err)
	emaBindingID, err := fixtures.InsertRemoteSessionEMABindingFixture(ctx, testrepo.InsertRemoteSessionEMABindingFixtureParams{
		ProjectID:             *authCtx.ProjectID,
		OrganizationID:        authCtx.ActiveOrganizationID,
		UserSessionIssuerID:   sourceID,
		RemoteSessionIssuerID: remoteIssuer.ID,
		Resource:              "https://resource.example.com",
		RemoteSessionClientID: conv.ToNullUUID(remoteClient.ID),
	})
	require.NoError(t, err)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	require.NoError(t, err)
	require.True(t, preflight.CanMigrate)
	require.Equal(t, 1, preflight.RemoteSessionCount)

	result, err := ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	require.NoError(t, err)
	require.Equal(t, 1, result.RemoteSessionsMigrated)

	remoteSessionAfter, err := remoteRepo.GetRemoteSessionByID(ctx, remotesessionsrepo.GetRemoteSessionByIDParams{ID: remoteSession.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, targetID, remoteSessionAfter.UserSessionIssuerID)
	principalIssuerID, err := fixtures.GetPrincipalRemoteSessionBindingIssuerFixture(ctx, principalBindingID)
	require.NoError(t, err)
	require.Equal(t, targetID, principalIssuerID)
	emaBinding, err := fixtures.GetRemoteSessionEMABindingFixture(ctx, emaBindingID)
	require.NoError(t, err)
	require.Equal(t, targetID, emaBinding.UserSessionIssuerID)
	require.Equal(t, int64(2), emaBinding.Generation)
	linkCount, err := remoteRepo.CountRemoteSessionClientUserSessionIssuerBindings(ctx, remotesessionsrepo.CountRemoteSessionClientUserSessionIssuerBindingsParams{RemoteSessionClientID: remoteClient.ID, UserSessionIssuerID: targetID})
	require.NoError(t, err)
	require.Equal(t, int64(1), linkCount)
}

func TestOrganizationUserSessionIssuerMigrateScopeAndRBAC(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	source, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "scope-source", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	projectTargetID := seedIssuer(t, ctx, ti, "scope-project-target")

	_, err = ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: source.ID, TargetID: projectTargetID.String()})
	requireOopsCode(t, err, oops.CodeBadRequest)

	organizationTarget, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "rbac-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	readCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	_, err = ti.service.GetIssuerMigratePreflight(readCtx, &orggen.GetIssuerMigratePreflightPayload{SourceID: source.ID, TargetID: organizationTarget.ID})
	require.NoError(t, err)
	_, err = ti.service.MigrateIssuer(readCtx, &orggen.MigrateIssuerPayload{SourceID: source.ID, TargetID: organizationTarget.ID})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.MoveIssuer(readCtx, &orggen.MoveIssuerPayload{ID: source.ID})
	requireOopsCode(t, err, oops.CodeForbidden)
}
