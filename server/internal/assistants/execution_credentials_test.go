package assistants

import (
	"context"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

// exerciseInvocationCredentials uses real admitted contexts, not a test-only
// context stamp. Both callers share the agent and resource, never credentials.
func exerciseInvocationCredentials(t *testing.T, db *pgxpool.Pool, core *ServiceCore, manager *assistanttokens.Manager, assistant assistantRecord, root, thread, resource uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	base, err := url.Parse("http://localhost")
	require.NoError(t, err)
	sessions := remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), db, enc, policy, nil, cache.NoopCache, base)
	refreshStarted, finishRefresh := make(chan struct{}), make(chan struct{})
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(refreshStarted)
		select {
		case <-finishRefresh:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh-private-user-2","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(refreshServer.Close)
	releaseRefresh := sync.OnceFunc(func() { close(finishRefresh) })
	t.Cleanup(releaseRefresh)
	q := remoterepo.New(db)
	issuer, err := usersessionsrepo.New(db).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{ProjectID: assistant.ProjectID, OrganizationID: conv.ToPGText("org-test"), Slug: "invocation-sessions", AuthnChallengeMode: "interactive", SessionDuration: pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true}})
	require.NoError(t, err)
	upstream, err := q.CreateRemoteSessionIssuer(ctx, remoterepo.CreateRemoteSessionIssuerParams{ProjectID: conv.ToNullUUID(assistant.ProjectID), Slug: "invocation-upstream", Issuer: "https://issuer.example", AuthorizationEndpoint: conv.ToPGText("https://issuer.example/authorize"), TokenEndpoint: conv.ToPGText(refreshServer.URL), ScopesSupported: []string{"read"}, GrantTypesSupported: []string{"authorization_code"}, ResponseTypesSupported: []string{"code"}, TokenEndpointAuthMethodsSupported: []string{"none"}})
	require.NoError(t, err)
	client, err := q.CreateRemoteSessionClient(ctx, remoterepo.CreateRemoteSessionClientParams{ProjectID: conv.ToNullUUID(assistant.ProjectID), OrganizationID: conv.ToPGText("org-test"), RemoteSessionIssuerID: upstream.ID, ClientID: "invocation-client", TokenEndpointAuthMethod: conv.ToPGText("none")})
	require.NoError(t, err)
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remoterepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: client.ID, UserSessionIssuerID: issuer.ID}))
	callers := map[string]context.Context{}
	for _, user := range []string{"user-1", "user-2"} {
		require.NoError(t, identityrepo.New(db).FixtureSlackExecutionMapping(ctx, identityrepo.FixtureSlackExecutionMappingParams{UserID: user, OrganizationID: assistant.OrganizationID, SlackTeamID: "TCREDENTIAL", SlackUserID: "U" + user, Generation: uuid.New()}))
		raw, err := core.captureExecution(ctx, assistant, sourceKindSlack, thread, uuid.NullUUID{UUID: root, Valid: true}, "credential-"+user, []byte(`{"team_id":"TCREDENTIAL","user_id":"U`+user+`"}`), slackSelectionForTest(t, db, "TCREDENTIAL", "U"+user))
		require.NoError(t, err)
		e, err := decodeExecution(raw)
		require.NoError(t, err)
		token, err := manager.GenerateExecution(ctx, *e)
		require.NoError(t, err)
		callers[user], err = manager.AuthorizeBusiness(ctx, token, resource, nil)
		require.NoError(t, err)
		callers[user] = contextvalues.WithAssistantBusinessResource(callers[user], "https://api.example/mcp")
		encrypted, err := enc.Encrypt([]byte("private-" + user))
		require.NoError(t, err)
		_, err = q.UpsertRemoteSession(ctx, remoterepo.UpsertRemoteSessionParams{SubjectUrn: urn.NewUserSubject(user), UserSessionIssuerID: issuer.ID, RemoteSessionClientID: client.ID, AccessTokenEncrypted: encrypted, Scopes: []string{}, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Resource: conv.ToPGText("https://api.example/mcp")})
		require.NoError(t, err)
	}
	otherIssuer, err := usersessionsrepo.New(db).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{ProjectID: assistant.ProjectID, OrganizationID: conv.ToPGText("org-test"), Slug: "other-invocation-sessions", AuthnChallengeMode: "interactive", SessionDuration: pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true}})
	require.NoError(t, err)
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remoterepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: client.ID, UserSessionIssuerID: otherIssuer.ID}))
	_, err = sessions.ResolveAccessTokens(callers["user-2"], assistant.ProjectID, "org-test", otherIssuer.ID, urn.NewUserSubject("user-2"))
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "consent from another issuer must not supply delegated credentials")
	for _, target := range []string{"", "https://other.example/mcp"} {
		wrongResource := contextvalues.WithAssistantBusinessResource(callers["user-2"], target)
		_, err := sessions.ResolveAccessTokens(wrongResource, assistant.ProjectID, "org-test", issuer.ID, urn.NewUserSubject("user-2"))
		require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "empty-resource batch resolution must enforce the server-pinned upstream")
	}
	identity, err := testIdentityService.Resolve(ctx, db, assistant.OrganizationID, assistant.ProjectID, assistant.ID, root)
	require.NoError(t, err)
	ownerSession, err := q.GetActiveRemoteSession(ctx, remoterepo.GetActiveRemoteSessionParams{SubjectUrn: urn.NewUserSubject("user-1"), RemoteSessionClientID: client.ID})
	require.NoError(t, err)
	binding, err := q.AttachPrincipalRemoteSessionBinding(ctx, remoterepo.AttachPrincipalRemoteSessionBindingParams{PrincipalID: identity.Identity.AgentID, SubjectUrn: urn.NewUserSubject("user-1").String(), ProjectID: assistant.ProjectID, OrganizationID: assistant.OrganizationID, UserSessionIssuerID: issuer.ID, RemoteSessionID: ownerSession.ID})
	require.NoError(t, err)
	bindingParams := remoterepo.ListPrincipalRemoteSessionBindingsParams{ProjectID: assistant.ProjectID, OrganizationID: assistant.OrganizationID, PrincipalID: identity.Identity.AgentID, UserSessionIssuerID: issuer.ID, SubjectUrn: urn.NewUserSubject("user-1").String()}
	before, err := q.ListPrincipalRemoteSessionBindings(ctx, bindingParams)
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.Equal(t, binding.ID, before[0].ID)
	resolve := func(user string) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
		return sessions.ResolveAccessTokens(callers[user], assistant.ProjectID, "org-test", issuer.ID, urn.NewUserSubject(user))
	}
	for _, user := range []string{"user-1", "user-2", "user-1"} {
		tokens, err := resolve(user)
		require.NoError(t, err)
		require.Equal(t, "private-"+user, tokens[upstream.ID].Token)
		actor, ok := contextvalues.AuthenticatedActor(callers[user])
		require.True(t, ok)
		require.Equal(t, urn.PrincipalTypeWorkload, actor.Type)
	}
	type outcome struct {
		user   string
		tokens map[uuid.UUID]remotesessions.UpstreamToken
		err    error
	}
	results := make(chan outcome, 8)
	var wg sync.WaitGroup
	for i := range 8 {
		user := []string{"user-1", "user-2"}[i%2]
		wg.Go(func() { tokens, err := resolve(user); results <- outcome{user, tokens, err} })
	}
	wg.Wait()
	close(results)
	for result := range results {
		require.NoError(t, result.err)
		require.Equal(t, "private-"+result.user, result.tokens[upstream.ID].Token)
	}
	_, err = sessions.ResolveAccessTokens(callers["user-2"], assistant.ProjectID, "org-test", issuer.ID, urn.NewUserSubject("user-1"))
	require.Error(t, err, "caller cannot override selected human")
	_, err = sessions.ResolveAuthorization(callers["user-2"], assistant.ProjectID, "org-test", issuer.ID, upstream.ID, urn.NewUserSubject("user-2"), "https://other.example/mcp")
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	// A refresh must not release a token after the selected user's policy was
	// revoked while the provider was blocked. No owner credential is retried.
	refresh, err := enc.Encrypt([]byte("refresh-user-2"))
	require.NoError(t, err)
	expired, err := enc.Encrypt([]byte("expired-user-2"))
	require.NoError(t, err)
	_, err = q.UpsertRemoteSession(ctx, remoterepo.UpsertRemoteSessionParams{SubjectUrn: urn.NewUserSubject("user-2"), UserSessionIssuerID: issuer.ID, RemoteSessionClientID: client.ID, AccessTokenEncrypted: expired, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Hour)), RefreshTokenEncrypted: conv.ToPGText(refresh), Scopes: []string{}, Resource: conv.ToPGText("https://api.example/mcp")})
	require.NoError(t, err)
	refreshed := make(chan error, 1)
	go func() { _, err := resolve("user-2"); refreshed <- err }()
	select {
	case <-refreshStarted:
	case err := <-refreshed:
		require.NoError(t, err, "refresh must reach provider")
		t.Fatal("refresh unexpectedly returned")
	case <-time.After(10 * time.Second):
		t.Fatal("refresh did not start")
	}
	deleteExecutionTestScope(t, db, urn.NewPrincipal(urn.PrincipalTypeUser, "user-2"), authz.ScopeMCPConnect)
	releaseRefresh()
	require.Error(t, <-refreshed, "authority revalidated after provider refresh")
	putExecutionTestGrant(t, db, urn.NewPrincipal(urn.PrincipalTypeUser, "user-2"), authz.NewGrant(authz.ScopeMCPConnect, resource.String()))
	_, err = q.SoftDeleteRemoteSessionBySubjectAndClient(ctx, remoterepo.SoftDeleteRemoteSessionBySubjectAndClientParams{SubjectUrn: urn.NewUserSubject("user-2"), RemoteSessionClientID: client.ID, UserSessionIssuerID: issuer.ID, ProjectID: assistant.ProjectID, OrganizationID: "org-test"})
	require.NoError(t, err)
	_, err = resolve("user-2")
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "missing consent cannot use owner's valid session")
	tokens, err := resolve("user-1")
	require.NoError(t, err)
	require.Equal(t, "private-user-1", tokens[upstream.ID].Token)
	after, err := q.ListPrincipalRemoteSessionBindings(ctx, bindingParams)
	require.NoError(t, err)
	require.Equal(t, before, after, "sequential and concurrent human invocations must not replace the agent's shared binding")
	_, err = adminrepo.New(db).AdminDisableOrganization(ctx, assistant.OrganizationID)
	require.NoError(t, err)
	_, err = resolve("user-1")
	require.Error(t, err, "disabled organization cannot release a delegated credential")
	_, err = adminrepo.New(db).AdminEnableOrganization(ctx, assistant.OrganizationID)
	require.NoError(t, err)
	tokens, err = resolve("user-1")
	require.NoError(t, err)
	require.Equal(t, "private-user-1", tokens[upstream.ID].Token)

}
