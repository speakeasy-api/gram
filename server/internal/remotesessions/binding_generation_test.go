package remotesessions_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func configureBindingOwnerAuthorizer(ti *testInstance) {
	authorizer := agentmanagement.NewAuthorizer(bindingOwnerOnlyEngine{})
	ti.service.SetBindingAuthorizer(func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
		_, _, err := authorizer.RequireAgentOwnerForUpdate(ctx, tx, id, agentmanagement.OwnedAgentAuthorize)
		if err != nil {
			return fmt.Errorf("authorize attachment owner: %w", err)
		}
		return nil
	})
}

func TestBindingsReconnectWithoutDisconnectRequiresExplicitDetach(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = contextvalues.WithValidatedGramSession(ctx, auth, false)
	configureBindingOwnerAuthorizer(ti)
	agent, err := agentrepo.New(ti.conn).CreateAgent(ctx, agentrepo.CreateAgentParams{OrganizationID: auth.ActiveOrganizationID, OwnerUserID: auth.UserID, Name: "Reconnect agent"})
	require.NoError(t, err)
	issuer := createRemoteIssuer(t, ctx, ti, "binding-generation-provider", "")
	config := createUserSessionIssuer(t, ctx, ti.conn, "binding-generation-config")
	client := createRemoteClient(t, ctx, ti, issuer, config.String(), "binding-generation-client")
	subject := urn.NewUserSubject(auth.UserID)
	source := insertRemoteSession(t, ctx, ti.conn, subject, config.String(), client)
	payload := &gen.AttachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), RemoteSessionID: source.ID.String()}
	binding, err := ti.service.AttachBinding(ctx, payload)
	require.NoError(t, err)
	q := repo.New(ti.conn)
	lookup := repo.GetPrincipalRemoteSessionBindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config, RemoteSessionClientID: source.RemoteSessionClientID}
	_, err = q.GetPrincipalRemoteSessionBinding(ctx, lookup)
	require.NoError(t, err)
	// Token refresh updates tokens without changing the attached authorization.
	refreshed, err := q.UpdateRemoteSessionTokensIfUnchanged(ctx, repo.UpdateRemoteSessionTokensIfUnchangedParams{
		SubjectUrn: source.SubjectUrn, RemoteSessionClientID: source.RemoteSessionClientID,
		ExpectedUpdatedAt: source.UpdatedAt, AccessTokenEncrypted: source.AccessTokenEncrypted,
		AccessExpiresAt: source.AccessExpiresAt, RefreshTokenEncrypted: source.RefreshTokenEncrypted,
		Scopes: source.Scopes,
	})
	require.NoError(t, err)
	require.Equal(t, source.GrantGeneration, refreshed.GrantGeneration)
	_, err = q.GetPrincipalRemoteSessionBinding(ctx, lookup)
	require.NoError(t, err)
	// The OAuth callback upsert replaces the grant on the SAME active row.
	replacement := insertRemoteSession(t, ctx, ti.conn, subject, config.String(), client)
	require.Equal(t, source.ID, replacement.ID)
	require.Equal(t, source.GrantGeneration+1, replacement.GrantGeneration)
	_, err = q.GetPrincipalRemoteSessionBinding(ctx, lookup)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = ti.service.AttachBinding(ctx, payload)
	requireOopsCode(t, err, oops.CodeConflict)
	listed, err := ti.service.ListBindings(ctx, &gen.ListBindingsPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String()})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1, "a stale binding remains until explicit detach")
	require.Equal(t, binding.ID, listed.Items[0].ID)
	require.Nil(t, listed.Items[0].RemoteSession, "a replaced grant is an unavailable binding, not a connected session")
	require.NoError(t, ti.service.DetachBinding(ctx, &gen.DetachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), ID: binding.ID}))
	rebound, err := ti.service.AttachBinding(ctx, payload)
	require.NoError(t, err)
	require.NotEqual(t, binding.ID, rebound.ID)
	attached, err := q.GetPrincipalRemoteSessionBinding(ctx, lookup)
	require.NoError(t, err)
	require.Equal(t, replacement.GrantGeneration, attached.GrantGeneration)
	// Audit records carry exact binding provenance and remain project scoped.
	var auditCount int64
	auditCount, err = testrepo.New(ti.conn).CountAttachmentAuditEventsFixture(ctx, testrepo.CountAttachmentAuditEventsFixtureParams{ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, OrganizationID: auth.ActiveOrganizationID, SubjectID: source.ID.String(), ActorID: auth.UserID, PrincipalID: agent.ID.String(), BindingID: binding.ID, ReplacementBindingID: rebound.ID})
	require.NoError(t, err)
	require.EqualValues(t, 3, auditCount)
}

func TestResolveAuthorization_AttachmentReconnectDuringRefresh(t *testing.T) {
	t.Parallel()
	for _, reattach := range []bool{false, true} {
		name := "stale-binding"
		if reattach {
			name = "concurrent-reattachment"
		}
		t.Run(name, func(t *testing.T) { t.Parallel(); testAttachmentReconnectDuringRefresh(t, reattach) })
	}
}

func testAttachmentReconnectDuringRefresh(t *testing.T, reattach bool) {
	t.Helper()
	arrived, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	ctx, env := newSyntheticExpiryEnv(t, "binding-refresh-reconnect", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") == "refresh_token" {
			close(arrived)
			<-release
			_, _ = w.Write([]byte(`{"access_token":"rotated-old-grant","refresh_token":"rotated-refresh","token_type":"Bearer","expires_in":3600}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"initial-grant","refresh_token":"initial-refresh","token_type":"Bearer","expires_in":3600}`))
	})
	// Release before the fixture closes its HTTP server, including assertion failures.
	t.Cleanup(unblock)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	// The synthetic OAuth fixture uses an arbitrary subject; attachments require
	// the personal grant to belong to the agent's current owner.
	env.subject = urn.NewUserSubject(auth.UserID)
	env.session.SubjectUrn = env.subject
	_, err := testrepo.New(env.db).SetAttachmentSourceSubjectFixture(ctx, testrepo.SetAttachmentSourceSubjectFixtureParams{SubjectUrn: env.subject, ID: env.session.ID})
	require.NoError(t, err)
	agent, err := agentrepo.New(env.db).CreateAgent(ctx, agentrepo.CreateAgentParams{OrganizationID: env.organizationID, OwnerUserID: auth.UserID, Name: "Refresh reconnect agent"})
	require.NoError(t, err)
	binding, err := env.q.AttachPrincipalRemoteSessionBinding(ctx, repo.AttachPrincipalRemoteSessionBindingParams{ProjectID: env.projectID, OrganizationID: env.organizationID, PrincipalID: agent.ID, UserSessionIssuerID: env.session.UserSessionIssuerID, SubjectUrn: env.subject.String(), RemoteSessionID: env.session.ID})
	require.NoError(t, err)
	agentCtx := contextvalues.WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()))
	resolve := func() (remotesessions.ResolvedAuthorization, error) {
		return env.mgr.ResolveAuthorization(agentCtx, env.projectID, env.organizationID, env.session.UserSessionIssuerID, env.issuerID, urn.NewAgentSubject(agent.ID), "")
	}
	_, err = resolve()
	require.NoError(t, err)
	require.NoError(t, env.q.SetRemoteSessionAccessExpiresAt(ctx, repo.SetRemoteSessionAccessExpiresAtParams{ID: env.session.ID, ProjectID: conv.ToNullUUID(env.projectID), AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Hour))}))
	type result struct {
		authorization remotesessions.ResolvedAuthorization
		err           error
	}
	done := make(chan result, 1)
	go func() { authorization, err := resolve(); done <- result{authorization, err} }()
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("attached session did not begin refresh")
	}
	// Simulate the callback's fresh grant while the old grant's token request is blocked.
	reconnected, err := env.q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{SubjectUrn: env.subject, UserSessionIssuerID: env.session.UserSessionIssuerID, RemoteSessionClientID: env.clientID, AccessTokenEncrypted: env.session.AccessTokenEncrypted, RefreshTokenEncrypted: env.session.RefreshTokenEncrypted, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: env.session.Scopes, Resource: env.session.Resource})
	require.NoError(t, err)
	require.Equal(t, env.session.ID, reconnected.ID)
	require.Equal(t, env.session.GrantGeneration+1, reconnected.GrantGeneration)
	if reattach {
		_, err = env.q.DetachPrincipalRemoteSessionBinding(ctx, repo.DetachPrincipalRemoteSessionBindingParams{ProjectID: env.projectID, OrganizationID: env.organizationID, PrincipalID: agent.ID, UserSessionIssuerID: env.session.UserSessionIssuerID, SubjectUrn: env.subject.String(), ID: binding.ID})
		require.NoError(t, err)
		_, err = env.q.AttachPrincipalRemoteSessionBinding(ctx, repo.AttachPrincipalRemoteSessionBindingParams{ProjectID: env.projectID, OrganizationID: env.organizationID, PrincipalID: agent.ID, UserSessionIssuerID: env.session.UserSessionIssuerID, SubjectUrn: env.subject.String(), RemoteSessionID: env.session.ID})
		require.NoError(t, err)
	}
	unblock()
	select {
	case got := <-done:
		require.ErrorIs(t, got.err, remotesessions.ErrNoValidToken)
		require.Empty(t, got.authorization.AccessToken, "in-flight refresh must not authorize a stale attachment")
	case <-time.After(10 * time.Second):
		t.Fatal("attached session refresh did not complete")
	}
	_, err = resolve()
	if reattach {
		require.NoError(t, err, "a new request may use the explicitly reattached grant")
	} else {
		require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	}
}

func TestBindingsOrganizationIssuerAndClientAttachIndependentlyAcrossProjects(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	configureBindingOwnerAuthorizer(ti)
	agent, err := agentrepo.New(ti.conn).CreateAgent(ctx, agentrepo.CreateAgentParams{OrganizationID: auth.ActiveOrganizationID, OwnerUserID: auth.UserID, Name: "Shared project agent"})
	require.NoError(t, err)
	config := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "binding-shared-config")
	issuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, auth.ActiveOrganizationID, "binding-shared-provider")
	client := seedOrgLevelRemoteClient(t, ctx, ti.conn, auth.ActiveOrganizationID, issuer, "binding-shared-client", config)
	source := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), config.String(), client.String())
	otherProject := createProject(t, ctx, ti.conn, "binding-second-project")
	otherAuth := *auth
	otherAuth.ProjectID = &otherProject
	contexts := []context.Context{contextvalues.WithValidatedGramSession(ctx, auth, false), contextvalues.WithValidatedGramSession(ctx, &otherAuth, false)}
	payload := &gen.AttachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), RemoteSessionID: source.ID.String()}
	first, err := ti.service.AttachBinding(contexts[0], payload)
	require.NoError(t, err)
	second, err := ti.service.AttachBinding(contexts[1], payload)
	require.NoError(t, err, "the same org principal/config/client can be attached in two projects")
	require.NotEqual(t, first.ID, second.ID)
	for i, projectCtx := range contexts {
		listed, err := ti.service.ListBindings(projectCtx, &gen.ListBindingsPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String()})
		require.NoError(t, err)
		require.Len(t, listed.Items, 1)
		require.Equal(t, []string{first.ID, second.ID}[i], listed.Items[0].ID)
		again, err := ti.service.AttachBinding(projectCtx, payload)
		require.NoError(t, err)
		require.Equal(t, listed.Items[0].ID, again.ID)
	}
	require.NoError(t, ti.service.DetachBinding(contexts[0], &gen.DetachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), ID: first.ID}))
	q := repo.New(ti.conn)
	lookup := repo.GetPrincipalRemoteSessionBindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config, RemoteSessionClientID: client}
	_, err = q.GetPrincipalRemoteSessionBinding(ctx, lookup)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	lookup.ProjectID = otherProject
	attached, err := q.GetPrincipalRemoteSessionBinding(ctx, lookup)
	require.NoError(t, err, "detaching one project must not revoke another project's attachment")
	require.Equal(t, source.ID, attached.ID)
}
