package remotesessions_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Deny delegated management so these tests exercise the intrinsic owner path
// through the real shared authorizer, not a permissive handler callback.
type bindingOwnerOnlyEngine struct{}

func (bindingOwnerOnlyEngine) EvaluateLoadedGrants(context.Context, []authz.Grant, ...authz.Check) error {
	return oops.C(oops.CodeForbidden)
}

func TestBindingsRequireOrdinaryHuman(t *testing.T) {
	t.Parallel()
	svc := &remotesessions.Service{}
	projectID := uuid.New()
	sessionID := "ordinary-session"
	auth := &contextvalues.AuthContext{UserID: "binding-user", ActiveOrganizationID: "binding-org", ProjectID: &projectID, SessionID: &sessionID}
	ordinary := contextvalues.WithValidatedGramSession(t.Context(), auth, false)
	apiAuth := *auth
	apiAuth.APIKeyID = uuid.NewString()
	tests := map[string]context.Context{
		"missing":                 t.Context(),
		"attributed-only":         contextvalues.SetAuthContext(t.Context(), auth),
		"api-key":                 contextvalues.WithValidatedGramSession(t.Context(), &apiAuth, false),
		"support":                 contextvalues.WithValidatedSupportSession(ordinary, auth),
		"impersonated":            contextvalues.WithValidatedGramSession(t.Context(), auth, true),
		"unconfigured-authorizer": ordinary,
	}
	for name, ctx := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := svc.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{PrincipalID: conv.PtrEmpty(uuid.NewString()), UserSessionIssuerID: conv.PtrEmpty(uuid.NewString())})
			require.Error(t, err)
			_, err = svc.ListBindings(ctx, &gen.ListBindingsPayload{PrincipalID: uuid.NewString(), UserSessionIssuerID: uuid.NewString()})
			require.Error(t, err)
			_, err = svc.AttachBinding(ctx, &gen.AttachBindingPayload{PrincipalID: uuid.NewString(), UserSessionIssuerID: uuid.NewString(), RemoteSessionID: uuid.NewString()})
			require.Error(t, err)
			require.Error(t, svc.DetachBinding(ctx, &gen.DetachBindingPayload{PrincipalID: uuid.NewString(), UserSessionIssuerID: uuid.NewString(), ID: uuid.NewString()}))
		})
	}
}

func TestBindingsOwnershipReachabilityAndExactSession(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = contextvalues.WithValidatedGramSession(ctx, auth, false)
	authorizer := agentmanagement.NewAuthorizer(bindingOwnerOnlyEngine{})
	ti.service.SetBindingAuthorizer(func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
		_, _, err := authorizer.RequireAgentOwnerForUpdate(ctx, tx, id, agentmanagement.OwnedAgentAuthorize)
		if err != nil {
			return fmt.Errorf("authorize attachment owner: %w", err)
		}
		return nil
	})
	agent, err := agentrepo.New(ti.conn).CreateAgent(ctx, agentrepo.CreateAgentParams{OrganizationID: auth.ActiveOrganizationID, OwnerUserID: auth.UserID, Name: "Session attachment agent"})
	require.NoError(t, err)
	issuer := createRemoteIssuer(t, ctx, ti, "binding-provider", "")
	config := createUserSessionIssuer(t, ctx, ti.conn, "binding-config")
	otherConfig := createUserSessionIssuer(t, ctx, ti.conn, "binding-unreachable-config")
	sourceConfig := config
	client := createRemoteClient(t, ctx, ti, issuer, config.String(), "binding-client")
	mine := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), sourceConfig.String(), client)
	mine.UpstreamEmail = conv.ToPGText("existing-account@example.com")
	mine.UpstreamDisplayName = conv.ToPGText("Existing upstream account")
	mine.IdentitySource = conv.ToPGText(remotesessions.IdentitySourceIDToken)
	_, err = testrepo.New(ti.conn).SetAttachmentSourceIdentityFixture(ctx, testrepo.SetAttachmentSourceIdentityFixtureParams{ID: mine.ID, UpstreamEmail: mine.UpstreamEmail, UpstreamDisplayName: mine.UpstreamDisplayName, IdentitySource: mine.IdentitySource, UserSessionIssuerID: sourceConfig})
	require.NoError(t, err)
	theirs := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("another-user"), sourceConfig.String(), client)

	// A reachable client does not make a sibling issuer's session attachable.
	siblingConfig := createUserSessionIssuer(t, ctx, ti.conn, "binding-sibling-config")
	siblingIssuer := createRemoteIssuer(t, ctx, ti, "binding-sibling-provider", "")
	siblingClient := createRemoteClient(t, ctx, ti, siblingIssuer, config.String(), "binding-sibling-client")
	sibling := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), siblingConfig.String(), siblingClient)
	_, err = repo.New(ti.conn).AttachPrincipalRemoteSessionBinding(ctx, repo.AttachPrincipalRemoteSessionBindingParams{
		ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID,
		UserSessionIssuerID: config, SubjectUrn: sibling.SubjectUrn.String(), RemoteSessionID: sibling.ID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "reject a mismatched issuer before the exact-session FK")

	candidates, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{PrincipalID: conv.PtrEmpty(agent.ID.String()), UserSessionIssuerID: conv.PtrEmpty(config.String())})
	require.NoError(t, err)
	require.Len(t, candidates.Items, 1)
	require.Equal(t, mine.ID.String(), candidates.Items[0].ID)
	require.Equal(t, "existing-account@example.com", *candidates.Items[0].UpstreamEmail)
	require.Equal(t, "Existing upstream account", *candidates.Items[0].UpstreamDisplayName)
	require.Equal(t, remotesessions.IdentitySourceIDToken, *candidates.Items[0].IdentitySource)
	canonical, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{})
	require.NoError(t, err)
	for _, session := range canonical.Items {
		if session.ID == mine.ID.String() {
			require.Equal(t, session, candidates.Items[0])
		} else if session.ID == theirs.ID.String() {
			require.Nil(t, session.UpstreamEmail, "unknown upstream identity is never inferred from the Gram subject")
			require.Nil(t, session.UpstreamDisplayName)
			require.Nil(t, session.IdentitySource)
		}
	}
	ownerCtx := withExactAccessGrants(t, ctx, ti.conn)
	require.NotNil(t, auth.ProjectSlug)
	ownerCtx, err = ti.service.APIKeyAuth(context.WithValue(ownerCtx, goa.MethodKey, "listRemoteSessions"), *auth.ProjectSlug, &security.APIKeyScheme{Name: constants.ProjectSlugSecuritySchema})
	require.NoError(t, err, "eligible-session route delegates project authorization to the owner-aware handler")

	_, err = ti.service.ListRemoteSessions(ownerCtx, &gen.ListRemoteSessionsPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	eligiblePayload := &gen.ListRemoteSessionsPayload{PrincipalID: conv.PtrEmpty(agent.ID.String()), UserSessionIssuerID: conv.PtrEmpty(config.String()), Limit: conv.PtrEmpty(1)}
	eligible, err := ti.service.ListRemoteSessions(ownerCtx, eligiblePayload)
	require.NoError(t, err, "owners do not need project read permission for eligible sessions")
	require.Equal(t, candidates.Items, eligible.Items)
	require.NotNil(t, eligible.NextCursor)
	eligiblePayload.Cursor = eligible.NextCursor
	lastPage, err := ti.service.ListRemoteSessions(ownerCtx, eligiblePayload)
	require.NoError(t, err)
	require.Empty(t, lastPage.Items)
	eligiblePayload.Cursor = nil
	eligiblePayload.SubjectUrn = conv.PtrEmpty(urn.NewUserSubject("another-user").String())
	filtered, err := ti.service.ListRemoteSessions(ownerCtx, eligiblePayload)
	require.NoError(t, err)
	require.Empty(t, filtered.Items, "subject filters cannot broaden eligible listing")
	require.EqualValues(t, 1, mine.GrantGeneration, "pre-rollout sessions start at the migration default")
	_, err = ti.service.AttachBinding(ctx, &gen.AttachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), RemoteSessionID: theirs.ID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.AttachBinding(ctx, &gen.AttachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: otherConfig.String(), RemoteSessionID: mine.ID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{PrincipalID: conv.PtrEmpty(uuid.NewString()), UserSessionIssuerID: conv.PtrEmpty(config.String())})
	requireOopsCode(t, err, oops.CodeForbidden)

	attach := &gen.AttachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), RemoteSessionID: mine.ID.String()}
	binding, err := ti.service.AttachBinding(ctx, attach)
	require.NoError(t, err)
	again, err := ti.service.AttachBinding(ctx, attach)
	require.NoError(t, err)
	require.Equal(t, binding.ID, again.ID)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	locked, err := repo.New(tx).LockPrincipalRemoteSessionBindings(ctx, repo.LockPrincipalRemoteSessionBindingsParams{
		ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config,
	})
	require.NoError(t, err)
	require.Len(t, locked, 1)
	// A client deadline can race with server-side completion. Keep the
	// contender rollback-only so releasing the admission lock cannot commit it.
	competingTx := testenv.BeginTx(t, ctx, ti.conn)
	blockedCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, err = repo.New(competingTx).DetachPrincipalRemoteSessionBinding(blockedCtx, repo.DetachPrincipalRemoteSessionBindingParams{
		ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID,
		UserSessionIssuerID: config, SubjectUrn: mine.SubjectUrn.String(), ID: uuid.MustParse(binding.ID),
	})
	require.ErrorIs(t, err, context.DeadlineExceeded, "session admission must serialize attachment revocation")
	_ = competingTx.Rollback(ctx) // Cancellation may already have closed the connection.
	require.NoError(t, tx.Rollback(ctx))

	list, err := ti.service.ListBindings(ctx, &gen.ListBindingsPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String()})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	require.Equal(t, mine.ID.String(), list.Items[0].RemoteSessionID)
	require.Equal(t, candidates.Items[0], binding.RemoteSession)
	require.Equal(t, candidates.Items[0], list.Items[0].RemoteSession)
	attachedSource, err := repo.New(ti.conn).GetPrincipalRemoteSessionBinding(ctx, repo.GetPrincipalRemoteSessionBindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config, RemoteSessionClientID: mine.RemoteSessionClientID})
	require.NoError(t, err)
	require.Equal(t, mine, attachedSource, "attaching an existing grant from a different reachable issuer must not reauthenticate or mutate it")

	func() {
		t.Log("inaccessible source issuer is excluded")
		_, err := testrepo.New(ti.conn).SoftDeleteAttachmentConfigFixture(ctx, testrepo.SoftDeleteAttachmentConfigFixtureParams{ID: sourceConfig, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}})
		require.NoError(t, err)
		defer func() {
			_, err := testrepo.New(ti.conn).RestoreAttachmentConfigFixture(ctx, testrepo.RestoreAttachmentConfigFixtureParams{ID: sourceConfig, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}})
			require.NoError(t, err)
		}()
		candidates, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{PrincipalID: conv.PtrEmpty(agent.ID.String()), UserSessionIssuerID: conv.PtrEmpty(config.String())})
		require.NoError(t, err)
		require.Empty(t, candidates.Items)
		_, err = ti.service.AttachBinding(ctx, attach)
		requireOopsCode(t, err, oops.CodeNotFound)
		listed, err := ti.service.ListBindings(ctx, &gen.ListBindingsPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String()})
		require.NoError(t, err)
		require.Len(t, listed.Items, 1)
		require.Equal(t, binding.ID, listed.Items[0].ID)
		require.Nil(t, listed.Items[0].RemoteSession, "inaccessible source identity must be omitted")
	}()

	func() {
		t.Log("owner transfer hides prior owner bindings")
		nextOwner := "binding-next-owner-" + uuid.NewString()
		seedUser(t, ctx, ti.conn, nextOwner, "next-owner@example.com", "Next owner")
		_, err := testrepo.New(ti.conn).CreateAttachmentMembershipFixture(ctx, testrepo.CreateAttachmentMembershipFixtureParams{OrganizationID: auth.ActiveOrganizationID, UserID: pgtype.Text{String: nextOwner, Valid: true}})
		require.NoError(t, err)
		_, err = testrepo.New(ti.conn).SetAttachmentAgentOwnerFixture(ctx, testrepo.SetAttachmentAgentOwnerFixtureParams{OwnerUserID: nextOwner, ID: agent.ID, OrganizationID: auth.ActiveOrganizationID})
		require.NoError(t, err)
		defer func() {
			_, err := testrepo.New(ti.conn).SetAttachmentAgentOwnerFixture(ctx, testrepo.SetAttachmentAgentOwnerFixtureParams{OwnerUserID: auth.UserID, ID: agent.ID, OrganizationID: auth.ActiveOrganizationID})
			require.NoError(t, err)
		}()
		payload := &gen.ListBindingsPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String()}
		_, err = ti.service.ListBindings(ctx, payload)
		requireOopsCode(t, err, oops.CodeForbidden)
		nextAuth := *auth
		nextAuth.UserID = nextOwner
		nextCtx := contextvalues.WithValidatedGramSession(ctx, &nextAuth, false)
		listed, err := ti.service.ListBindings(nextCtx, payload)
		require.NoError(t, err)
		require.Empty(t, listed.Items, "new owners must not see even the old owner's binding identifiers")
	}()

	func() {
		t.Log("global clients preserve caller ownership and issuer reachability")
		// Set the scope before attaching: a live binding pins its client's scope.
		config := createUserSessionIssuer(t, ctx, ti.conn, "binding-global-config")
		issuer := createRemoteIssuer(t, ctx, ti, "binding-global-provider", "")
		client := createRemoteClient(t, ctx, ti, issuer, config.String(), "binding-global-client")
		mine := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), config.String(), client)
		theirs := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("another-user"), config.String(), client)
		attach := &gen.AttachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), RemoteSessionID: mine.ID.String()}
		_, err := testrepo.New(ti.conn).MakeAttachmentClientGlobalFixture(ctx, uuid.MustParse(client))
		require.NoError(t, err)
		_, err = testrepo.New(ti.conn).MakeAttachmentIssuerGlobalFixture(ctx, uuid.MustParse(issuer))
		require.NoError(t, err)
		candidates, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{PrincipalID: conv.PtrEmpty(agent.ID.String()), UserSessionIssuerID: conv.PtrEmpty(config.String())})
		require.NoError(t, err)
		require.Len(t, candidates.Items, 1)
		require.Equal(t, mine.ID.String(), candidates.Items[0].ID)
		attached, err := ti.service.AttachBinding(ctx, attach)
		require.NoError(t, err)
		require.Equal(t, mine.ID.String(), attached.RemoteSessionID)
		discovered, err := repo.New(ti.conn).ListRemoteSessionClientsForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsForUserSessionIssuerParams{ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), UserSessionIssuerID: config})
		require.NoError(t, err)
		require.Len(t, discovered, 1, "global attachment clients must also be discoverable by MCP")
		require.Equal(t, mine.RemoteSessionClientID, discovered[0].ClientID)
		_, err = ti.service.AttachBinding(ctx, &gen.AttachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), RemoteSessionID: theirs.ID.String()})
		requireOopsCode(t, err, oops.CodeNotFound)
	}()

	// Reconnection must not silently move the binding to a different exact row.
	_, err = repo.New(ti.conn).RevokeRemoteSession(ctx, repo.RevokeRemoteSessionParams{ID: mine.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, err)
	candidates, err = ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{PrincipalID: conv.PtrEmpty(agent.ID.String()), UserSessionIssuerID: conv.PtrEmpty(config.String())})
	require.NoError(t, err)
	require.Empty(t, candidates.Items, "revoked upstream sessions are not attachment candidates")
	list, err = ti.service.ListBindings(ctx, &gen.ListBindingsPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String()})
	require.NoError(t, err)
	require.Len(t, list.Items, 1, "revoked source bindings remain discoverable for detach")
	require.Equal(t, binding.ID, list.Items[0].ID)
	require.Nil(t, list.Items[0].RemoteSession, "revoked source bindings expose no source or subject identity")
	_, err = repo.New(ti.conn).GetPrincipalRemoteSessionBinding(ctx, repo.GetPrincipalRemoteSessionBindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config, RemoteSessionClientID: mine.RemoteSessionClientID})
	require.ErrorIs(t, err, pgx.ErrNoRows, "an identity-free tombstone must not authorize runtime use")
	replacement := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), sourceConfig.String(), client)
	attach.RemoteSessionID = replacement.ID.String()
	_, err = ti.service.AttachBinding(ctx, attach)
	requireOopsCode(t, err, oops.CodeConflict)
	detach := &gen.DetachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), ID: list.Items[0].ID}
	require.NoError(t, ti.service.DetachBinding(ctx, detach))
	require.NoError(t, ti.service.DetachBinding(ctx, detach))
	rebound, err := ti.service.AttachBinding(ctx, attach)
	require.NoError(t, err)
	require.NotEqual(t, binding.ID, rebound.ID)
	require.Equal(t, replacement.ID.String(), rebound.RemoteSessionID)
}

func TestListRemoteSessionsRequiresPairedEligibilityFilters(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{ProjectID: &projectID})
	svc := &remotesessions.Service{}
	for _, payload := range []*gen.ListRemoteSessionsPayload{
		{PrincipalID: conv.PtrEmpty(uuid.NewString())},
		{UserSessionIssuerID: conv.PtrEmpty(uuid.NewString())},
	} {
		_, err := svc.ListRemoteSessions(ctx, payload)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}
