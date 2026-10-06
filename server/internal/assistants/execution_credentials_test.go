package assistants

import (
	"context"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const turnUserResource = "https://api.example/mcp"

func TestPrincipalCredentialsUseTheirAuthorizersUpstreamSessions(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_credentials")
	require.NoError(t, err)
	ctx := t.Context()
	project := newProvisioningProject(t, db, "execution-credentials")
	seedProjectRead(t, db, "user-1", project)
	seedProjectRead(t, db, "user-2", project)
	core := newProvisioningCore(t, db)
	assistant, err := core.CreateAssistant(ctx, "org-test", project, "user-1", "Credentials", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true)
	require.NoError(t, err)
	_, err = toolsetsrepo.New(db).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{OrganizationID: "org-test", ProjectID: project, Name: "Business", Slug: "business", Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{}, McpSlug: pgtype.Text{}, McpEnabled: true})
	require.NoError(t, err)
	thread := seedThreadWithEvent(t, db, assistant.ID, "execution-credentials", "execution-credentials", eventStatusPending)
	redisClient, err := assistantsInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	engine := authz.NewEngine(testenv.NewLogger(t), db, authztest.ChallengeLoggingAlwaysDisabled, nil, authz.EngineOpts{AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential, AdmitWorkloadSession: runtimepolicy.AdmitWorkloadSession})
	manager := assistanttokens.New("test-secret", db, engine, newTestPrincipalCredentials(t, db), cache.NewRedisCacheAdapter(redisClient))
	core.assistantTokens = manager
	var dispatched atomic.Pointer[string]
	core.runtime = testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnToken: &dispatched}

	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	base, err := url.Parse("http://localhost")
	require.NoError(t, err)
	sessions := remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), db, enc, policy, nil, cache.NoopCache, base)
	q := remoterepo.New(db)
	issuer, err := usersessionsrepo.New(db).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{ProjectID: project, OrganizationID: conv.ToPGText("org-test"), Slug: "turn-user-sessions", AuthnChallengeMode: "interactive", SessionDuration: pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true}})
	require.NoError(t, err)
	upstream, err := q.CreateRemoteSessionIssuer(ctx, remoterepo.CreateRemoteSessionIssuerParams{ProjectID: conv.ToNullUUID(project), Slug: "turn-user-upstream", Issuer: "https://issuer.example", AuthorizationEndpoint: conv.ToPGText("https://issuer.example/authorize"), TokenEndpoint: conv.ToPGText("https://issuer.example/token"), ScopesSupported: []string{"read"}, GrantTypesSupported: []string{"authorization_code"}, ResponseTypesSupported: []string{"code"}, TokenEndpointAuthMethodsSupported: []string{"none"}})
	require.NoError(t, err)
	client, err := q.CreateRemoteSessionClient(ctx, remoterepo.CreateRemoteSessionClientParams{ProjectID: conv.ToNullUUID(project), OrganizationID: conv.ToPGText("org-test"), RemoteSessionIssuerID: upstream.ID, ClientID: "turn-user-client", TokenEndpointAuthMethod: conv.ToPGText("none")})
	require.NoError(t, err)
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remoterepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: client.ID, UserSessionIssuerID: issuer.ID}))

	authenticate := func(source, payload string) context.Context {
		t.Helper()
		_, err := core.processEventTurn(ctx, assistantThreadRecord{ID: thread, ProjectID: project, AssistantID: assistant.ID, SourceKind: source}, assistant, assistantRuntimeRecord{}, assistantThreadEventRecord{ID: uuid.New(), EventID: uuid.NewString(), NormalizedPayloadJSON: []byte(payload)})
		require.NoError(t, err)
		authed, _, err := manager.AuthorizeRuntime(ctx, *dispatched.Load())
		require.NoError(t, err)
		return authed
	}
	agentSubject := urn.NewAgentSubject(uuid.MustParse(*assistant.AgentID))
	resolve := func(caller context.Context) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
		return sessions.ResolveAccessTokens(caller, project, "org-test", issuer.ID, agentSubject)
	}
	callers := map[string]context.Context{}
	for _, user := range []string{"user-1", "user-2"} {
		callers[user] = authenticate(sourceKindDashboard, `{"text":"hi","user_id":"`+user+`"}`)
		encrypted, err := enc.Encrypt([]byte("private-" + user))
		require.NoError(t, err)
		_, err = q.UpsertRemoteSession(ctx, remoterepo.UpsertRemoteSessionParams{SubjectUrn: urn.NewUserSubject(user), UserSessionIssuerID: issuer.ID, RemoteSessionClientID: client.ID, AccessTokenEncrypted: encrypted, Scopes: []string{}, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Resource: conv.ToPGText(turnUserResource)})
		require.NoError(t, err)
	}
	for _, user := range []string{"user-1", "user-2"} {
		tokens, err := resolve(callers[user])
		require.NoError(t, err)
		require.Equal(t, "private-"+user, tokens[upstream.ID].Token, "each credential uses its own authorizer's session")
		actor, ok := contextvalues.AuthenticatedActor(callers[user])
		require.True(t, ok)
		require.Equal(t, urn.PrincipalTypeAgent, actor.Type, "the agent acts; only the upstream session is the user's")
	}

	workload := authenticate(sourceKindCron, `{}`)
	_, err = resolve(workload)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "a credential with no authorizer never uses anyone's session")

	_, err = q.SoftDeleteRemoteSessionBySubjectAndClient(ctx, remoterepo.SoftDeleteRemoteSessionBySubjectAndClientParams{SubjectUrn: urn.NewUserSubject("user-2"), RemoteSessionClientID: client.ID, UserSessionIssuerID: issuer.ID, ProjectID: project, OrganizationID: "org-test"})
	require.NoError(t, err)
	_, err = resolve(callers["user-2"])
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "missing consent never falls back to the owner's session")
}
