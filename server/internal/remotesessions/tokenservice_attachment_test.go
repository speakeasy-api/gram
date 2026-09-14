package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestResolveAuthorization_AgentAttachment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, organizationID := *auth.ProjectID, auth.ActiveOrganizationID
	enc := testenv.NewEncryptionClient(t)
	manager := newResolveManager(t, ti.conn, enc)
	requestingIssuer := createUserSessionIssuer(t, ctx, ti.conn, "attachment-requesting")
	otherIssuer := createUserSessionIssuer(t, ctx, ti.conn, "attachment-other")
	clientID, remoteIssuerID := seedActiveClient(t, ctx, ti.conn, projectID, requestingIssuer, organizationID, "attachment-upstream")
	subject := urn.NewUserSubject(auth.UserID)
	cipher, err := enc.Encrypt([]byte("attached-token"))
	require.NoError(t, err)
	q := repo.New(ti.conn)
	source, err := q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn: subject, UserSessionIssuerID: requestingIssuer,
		RemoteSessionClientID: clientID, AccessTokenEncrypted: cipher,
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		Resource:        conv.ToPGText("https://upstream.example.com/mcp"), Scopes: []string{},
	})
	require.NoError(t, err)
	principalID := uuid.New()
	_, err = testrepo.New(ti.conn).CreateAttachmentAgentFixture(ctx, testrepo.CreateAttachmentAgentFixtureParams{ID: principalID, OrganizationID: organizationID, OwnerUserID: auth.UserID, Name: "attachment-resolver-agent"})
	require.NoError(t, err)
	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, principalID.String())
	keyCtx := contextvalues.WithPrincipalAPIKeyAuthorization(ctx, auth, agent, contextvalues.PrincipalCredential{})
	sessionCtx := contextvalues.WithAuthenticatedActor(ctx, auth, agent)
	caller := urn.NewAgentSubject(principalID)
	resolve := func(ctx context.Context) (remotesessions.ResolvedAuthorization, error) {
		return manager.ResolveAuthorization(ctx, projectID, organizationID, requestingIssuer, remoteIssuerID, caller, "")
	}
	// Existing user credentials, including the author's grant, do not grant an
	// agent access until this exact row is explicitly attached.
	_, err = resolve(keyCtx)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	require.NoError(t, manager.CheckAccessTokens(ctx, projectID, organizationID, requestingIssuer, subject))
	wasUsed, err := testrepo.New(ti.conn).AttachmentSourceWasUsedFixture(ctx, source.ID)
	require.NoError(t, err)
	require.False(t, wasUsed, "consent availability checks must not stamp upstream usage")
	direct, err := manager.ResolveAuthorization(ctx, projectID, organizationID, requestingIssuer, remoteIssuerID, subject, "")
	require.NoError(t, err)
	require.Equal(t, source.ID, direct.RemoteSessionID)

	_, err = testrepo.New(ti.conn).InsertAttachmentFixture(ctx, testrepo.InsertAttachmentFixtureParams{ProjectID: projectID, OrganizationID: organizationID, PrincipalID: principalID, UserSessionIssuerID: requestingIssuer, RemoteSessionClientID: clientID, RemoteSessionID: source.ID, GrantGeneration: source.GrantGeneration, AttachedBySubjectID: subject.String()})
	require.NoError(t, err)
	for _, callerCtx := range []context.Context{keyCtx, sessionCtx} {
		resolved, err := resolve(callerCtx)
		require.NoError(t, err)
		require.Equal(t, "attached-token", resolved.AccessToken)
		require.Equal(t, source.ID, resolved.RemoteSessionID)
		require.Equal(t, source.UpdatedAt.Time, resolved.RemoteSessionUpdatedAt)
		tokens, err := manager.ResolveAccessTokens(callerCtx, projectID, organizationID, requestingIssuer, caller)
		require.NoError(t, err)
		require.Equal(t, "https://upstream.example.com/mcp", tokens[remoteIssuerID].Resource)
		require.Equal(t, source.ID, tokens[remoteIssuerID].RemoteSessionID)
		require.Equal(t, source.UpdatedAt.Time, tokens[remoteIssuerID].RemoteSessionUpdatedAt)
		require.Equal(t, source.UpdatedAt.Time, tokens[remoteIssuerID].RemoteSessionResolvedFromUpdatedAt)
		actor, ok := contextvalues.AuthenticatedActor(callerCtx)
		require.True(t, ok)
		require.Equal(t, agent.String(), actor.String())
	}
	// Transfer must not carry the old owner's personal grant into the new
	// owner's API-key or authenticated-session credentials, even after caching.
	seedUser(t, ctx, ti.conn, "replacement-owner", "replacement@example.com", "Replacement owner")
	_, err = testrepo.New(ti.conn).AddAttachmentReplacementOwnerMembershipFixture(ctx, organizationID)
	require.NoError(t, err)
	_, err = testrepo.New(ti.conn).TransferAttachmentToReplacementOwnerFixture(ctx, testrepo.TransferAttachmentToReplacementOwnerFixtureParams{ID: principalID, OrganizationID: organizationID})
	require.NoError(t, err)
	for _, callerCtx := range []context.Context{keyCtx, sessionCtx} {
		_, err = resolve(callerCtx)
		require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	}
	_, err = testrepo.New(ti.conn).SetAttachmentAgentOwnerFixture(ctx, testrepo.SetAttachmentAgentOwnerFixtureParams{OwnerUserID: auth.UserID, ID: principalID, OrganizationID: organizationID})
	require.NoError(t, err)
	// Multiple agents can share the same exact session independently.
	secondID := uuid.New()
	_, err = testrepo.New(ti.conn).CreateAttachmentAgentFixture(ctx, testrepo.CreateAttachmentAgentFixtureParams{ID: secondID, OrganizationID: organizationID, OwnerUserID: auth.UserID, Name: "attachment-second-agent"})
	require.NoError(t, err)
	_, err = testrepo.New(ti.conn).InsertAttachmentFixture(ctx, testrepo.InsertAttachmentFixtureParams{ProjectID: projectID, OrganizationID: organizationID, PrincipalID: secondID, UserSessionIssuerID: requestingIssuer, RemoteSessionClientID: clientID, RemoteSessionID: source.ID, GrantGeneration: source.GrantGeneration, AttachedBySubjectID: subject.String()})
	require.NoError(t, err)
	secondCtx := contextvalues.WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeAgent, secondID.String()))
	resolveSecond := func() (remotesessions.ResolvedAuthorization, error) {
		return manager.ResolveAuthorization(secondCtx, projectID, organizationID, requestingIssuer, remoteIssuerID, urn.NewAgentSubject(secondID), "")
	}
	second, err := resolveSecond()
	require.NoError(t, err)
	require.Equal(t, source.ID, second.RemoteSessionID)
	// Configuration and tenant qualification are part of the attachment key.
	_, err = q.GetPrincipalRemoteSessionBinding(ctx, repo.GetPrincipalRemoteSessionBindingParams{ProjectID: uuid.New(), OrganizationID: organizationID, PrincipalID: principalID, UserSessionIssuerID: requestingIssuer, RemoteSessionClientID: clientID})
	require.Error(t, err)
	_, err = q.GetPrincipalRemoteSessionBinding(ctx, repo.GetPrincipalRemoteSessionBindingParams{ProjectID: projectID, OrganizationID: organizationID, PrincipalID: principalID, UserSessionIssuerID: otherIssuer, RemoteSessionClientID: clientID})
	require.Error(t, err)
	var used bool
	used, err = testrepo.New(ti.conn).AttachmentSourceWasUsedFixture(ctx, source.ID)
	require.NoError(t, err)
	require.True(t, used)

	_, err = testrepo.New(ti.conn).RevokeAgentAttachmentsFixture(ctx, testrepo.RevokeAgentAttachmentsFixtureParams{ProjectID: projectID, PrincipalID: principalID})
	require.NoError(t, err)
	_, err = resolve(keyCtx)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	available, err := manager.ResolveAvailableAccessTokens(keyCtx, projectID, organizationID, requestingIssuer, caller)
	require.NoError(t, err)
	require.Empty(t, available)
	_, err = resolveSecond()
	require.NoError(t, err, "detaching one agent must not revoke the shared session")
	_, err = testrepo.New(ti.conn).RestoreAgentAttachmentsFixture(ctx, testrepo.RestoreAgentAttachmentsFixtureParams{ProjectID: projectID, PrincipalID: principalID})
	require.NoError(t, err)
	_, err = testrepo.New(ti.conn).SoftDeleteAttachmentSourceFixture(ctx, source.ID)
	require.NoError(t, err)
	replacement, err := q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn: subject, UserSessionIssuerID: requestingIssuer,
		RemoteSessionClientID: clientID, AccessTokenEncrypted: cipher,
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: []string{},
	})
	require.NoError(t, err)
	require.NotEqual(t, source.ID, replacement.ID)
	_, err = resolve(sessionCtx)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "reconnect must not silently replace the attached row")
	_, err = resolveSecond()
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "shared-session revocation affects every attachment")
	used, err = testrepo.New(ti.conn).AttachmentSourceWasUsedFixture(ctx, replacement.ID)
	require.NoError(t, err)
	require.False(t, used)
}
