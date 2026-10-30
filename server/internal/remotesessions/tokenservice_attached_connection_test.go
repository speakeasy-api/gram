package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The attached connection is read through the exact, scoped binding lookup
// token resolution uses, so the auth client check names the owner of the
// connection the token would come from, and nothing else.
func TestAuthClientAttachedConnection(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, organizationID := *auth.ProjectID, auth.ActiveOrganizationID
	manager := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
	owner := urn.NewUserSubject(auth.UserID)

	type attachment struct {
		actor   urn.Principal
		req     remotesessions.AuthClientTokenRequest
		source  repo.RemoteSession
		agentID uuid.UUID
	}
	attach := func(t *testing.T, name string) attachment {
		t.Helper()
		issuer := createUserSessionIssuer(t, ctx, ti.conn, "attached-owner-"+name)
		client, _ := seedActiveClient(t, ctx, ti.conn, projectID, issuer, organizationID, "attached-owner-"+name)
		source := insertRemoteSession(t, ctx, ti.conn, owner, issuer.String(), client.String())
		agentID := uuid.New()
		_, err := testrepo.New(ti.conn).CreateAttachmentAgentFixture(ctx, testrepo.CreateAttachmentAgentFixtureParams{ID: agentID, OrganizationID: organizationID, OwnerUserID: auth.UserID, Name: "attached-owner-" + name})
		require.NoError(t, err)
		_, err = testrepo.New(ti.conn).InsertAttachmentFixture(ctx, testrepo.InsertAttachmentFixtureParams{ProjectID: projectID, OrganizationID: organizationID, PrincipalID: agentID, UserSessionIssuerID: issuer, RemoteSessionClientID: client, RemoteSessionID: source.ID, GrantGeneration: source.GrantGeneration, AttachedBySubjectID: owner.String()})
		require.NoError(t, err)
		return attachment{
			actor:   urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()),
			req:     remotesessions.AuthClientTokenRequest{ProjectID: projectID, OrganizationID: organizationID, UserSessionIssuerID: issuer, Subject: urn.NewAgentSubject(agentID), RegistrationID: client, SkipUnusable: false},
			source:  source,
			agentID: agentID,
		}
	}
	sessionOf := func(a attachment) context.Context {
		return contextvalues.WithAuthenticatedActor(ctx, auth, a.actor)
	}
	requireNone := func(t *testing.T, ctx context.Context, req remotesessions.AuthClientTokenRequest, msg string) {
		t.Helper()
		conn, attached, err := manager.AuthClientAttachedConnection(ctx, req)
		require.NoError(t, err, msg)
		require.False(t, attached, msg)
		require.Equal(t, uuid.Nil, conn.RemoteSessionID, msg)
		require.Empty(t, conn.OwnerUserID, msg)
	}
	requireOwnerIn := func(t *testing.T, callerCtx context.Context, a attachment) {
		t.Helper()
		conn, attached, err := manager.AuthClientAttachedConnection(callerCtx, a.req)
		require.NoError(t, err)
		require.True(t, attached)
		require.Equal(t, remotesessions.AttachedConnection{RemoteSessionID: a.source.ID, OwnerUserID: auth.UserID, Bound: true}, conn)
	}
	requireOwner := func(t *testing.T, a attachment) {
		t.Helper()
		requireOwnerIn(t, sessionOf(a), a)
	}

	t.Run("an exact attachment names its connection and owner", func(t *testing.T) {
		t.Parallel()
		a := attach(t, "exact")
		requireOwner(t, a)
		requireOwnerIn(t, contextvalues.WithPrincipalAPIKeyAuthorization(ctx, auth, a.actor, contextvalues.PrincipalCredential{}), a)
	})

	t.Run("a user is governed by their own subject, not attachments", func(t *testing.T) {
		t.Parallel()
		a := attach(t, "user")
		req := a.req
		req.Subject = owner
		requireNone(t, ctx, req, "a user's own connection is never read through a binding")
	})

	t.Run("tenant, sign-in and registration scope the attachment", func(t *testing.T) {
		t.Parallel()
		a := attach(t, "scope")
		otherIssuer := createUserSessionIssuer(t, ctx, ti.conn, "attached-owner-scope-other")
		for name, mutate := range map[string]func(*remotesessions.AuthClientTokenRequest){
			"another project":      func(r *remotesessions.AuthClientTokenRequest) { r.ProjectID = uuid.New() },
			"another organization": func(r *remotesessions.AuthClientTokenRequest) { r.OrganizationID = "org_other" },
			"another sign-in":      func(r *remotesessions.AuthClientTokenRequest) { r.UserSessionIssuerID = otherIssuer },
			"another registration": func(r *remotesessions.AuthClientTokenRequest) { r.RegistrationID = uuid.New() },
		} {
			req := a.req
			mutate(&req)
			requireNone(t, sessionOf(a), req, name)
		}
		requireOwner(t, a)
	})

	t.Run("an unscoped or mismatched caller is refused", func(t *testing.T) {
		t.Parallel()
		a := attach(t, "refused")
		for name, mutate := range map[string]func(*remotesessions.AuthClientTokenRequest){
			"no sign-in":      func(r *remotesessions.AuthClientTokenRequest) { r.UserSessionIssuerID = uuid.Nil },
			"no registration": func(r *remotesessions.AuthClientTokenRequest) { r.RegistrationID = uuid.Nil },
			"another agent":   func(r *remotesessions.AuthClientTokenRequest) { r.Subject = urn.NewAgentSubject(uuid.New()) },
			"a workload": func(r *remotesessions.AuthClientTokenRequest) {
				r.Subject = urn.NewWorkloadSubject(uuid.New(), "external")
			},
			"no subject": func(r *remotesessions.AuthClientTokenRequest) { r.Subject = urn.SessionSubject{} },
		} {
			req := a.req
			mutate(&req)
			_, attached, err := manager.AuthClientAttachedConnection(sessionOf(a), req)
			require.ErrorIs(t, err, remotesessions.ErrInvalidAuthorizationRequest, name)
			require.False(t, attached, name)
		}
	})

	t.Run("a revoked attachment serves nothing until restored", func(t *testing.T) {
		t.Parallel()
		a := attach(t, "revoked")
		_, err := testrepo.New(ti.conn).RevokeAgentAttachmentsFixture(ctx, testrepo.RevokeAgentAttachmentsFixtureParams{ProjectID: projectID, PrincipalID: a.agentID})
		require.NoError(t, err)
		requireNone(t, sessionOf(a), a.req, "a revoked binding names no owner")
		conn, _, err := manager.AuthClientAttachedConnection(sessionOf(a), a.req)
		require.NoError(t, err)
		require.True(t, conn.Bound, "a revoked binding still governs the agent")
		other := a.req
		other.RegistrationID = uuid.New()
		conn, _, err = manager.AuthClientAttachedConnection(sessionOf(a), other)
		require.NoError(t, err)
		require.False(t, conn.Bound, "a registration never bound reports no binding")
		_, err = testrepo.New(ti.conn).RestoreAgentAttachmentsFixture(ctx, testrepo.RestoreAgentAttachmentsFixtureParams{ProjectID: projectID, PrincipalID: a.agentID})
		require.NoError(t, err)
		requireOwner(t, a)
	})

	t.Run("a reconnected grant keeps its source row's owner", func(t *testing.T) {
		t.Parallel()
		a := attach(t, "reconnected")
		reconnected := insertRemoteSession(t, ctx, ti.conn, owner, a.req.UserSessionIssuerID.String(), a.req.RegistrationID.String())
		require.Equal(t, a.source.ID, reconnected.ID)
		require.Equal(t, a.source.GrantGeneration+1, reconnected.GrantGeneration)
		// An attachment follows its source row's current credentials, and a
		// reconnect on that row is the same owner's connection.
		requireOwner(t, a)
	})

	t.Run("a deleted connection serves nothing", func(t *testing.T) {
		t.Parallel()
		a := attach(t, "deleted")
		_, err := testrepo.New(ti.conn).SoftDeleteAttachmentSourceFixture(ctx, a.source.ID)
		require.NoError(t, err)
		requireNone(t, sessionOf(a), a.req, "a deleted connection names no owner")
	})
}
