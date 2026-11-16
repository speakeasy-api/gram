package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The observability relay sends hooks with an agent API key and Gram-Project.
// These tests run the real key and project authorization the hooks routes use.

// hooksAgentKey is an agent API key and the agent it authenticates as.
type hooksAgentKey struct {
	key   string
	agent urn.Principal
}

// sessionID returns the id the agent's events are stored under.
func (k hooksAgentKey) sessionID(clientSessionID string) string {
	return k.agent.String() + ":" + clientSessionID
}

func seedPrincipalGrant(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, principal urn.Principal, grant authz.Grant) {
	t.Helper()
	selectors, err := grant.Selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(grant.Scope),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

// projectHooksGrant is org:hooks_ingest bounded to one project.
func projectHooksGrant(organizationID string, projectID uuid.UUID) authz.Grant {
	selector := authz.NewSelector(authz.ScopeOrgHooksIngest, organizationID)
	selector[authz.SelectorKeyProjectID] = projectID.String()
	return authz.NewGrantWithSelector(authz.ScopeOrgHooksIngest, selector)
}

// newHooksAgentKey issues an agent API key whose agent and delegated policy
// hold exactly grants, and gives the service the real authorizer with an
// engine that admits agent credentials. The owner may read every project and
// send hooks to any of them, so the agent's grants alone decide.
func newHooksAgentKey(t *testing.T, ctx context.Context, ti *testInstance, grants ...authz.Grant) hooksAgentKey {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	organizationID := authCtx.ActiveOrganizationID

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: organizationID, OwnerUserID: authCtx.UserID, Name: "Hooks relay agent",
	})
	require.NoError(t, err)
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	for _, grant := range grants {
		seedPrincipalGrant(t, ctx, ti, organizationID, actor, grant)
	}
	owner := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	seedPrincipalGrant(t, ctx, ti, organizationID, owner, authz.NewGrant(authz.ScopeOrgHooksIngest, organizationID))
	seedPrincipalGrant(t, ctx, ti, organizationID, owner, authz.NewGrant(authz.ScopeProjectRead, authz.WildcardResource))

	policy, err := runtimepolicy.NewDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion2, grants)
	require.NoError(t, err)
	rawPolicy, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion2, policy)
	require.NoError(t, err)

	key := "gram_local_" + uuid.NewString()
	keyHash, err := auth.GetAPIKeyHash(key)
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{
		OrganizationID:         organizationID,
		CreatedByUserID:        authCtx.UserID,
		Name:                   "hooks-relay-agent-key",
		KeyPrefix:              key[:16],
		KeyHash:                keyHash,
		SubjectUrn:             pgtype.Text{String: actor.String(), Valid: true},
		DelegatedGrants:        rawPolicy,
		DelegatedGrantsVersion: pgtype.Int4{Int32: int32(runtimepolicy.DelegatedPolicyVersion2), Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	engine := authz.NewEngine(testenv.NewLogger(t), ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), authz.EngineOpts{
		DevMode:                          false,
		AdmitPrincipalCredential:         runtimepolicy.AdmitPrincipalCredential,
		AdmitPrincipalCredentialWithDBTX: runtimepolicy.AdmitPrincipalCredentialWithDBTX,
	})
	ti.service.authz = engine
	ti.service.auth = auth.New(testenv.NewLogger(t), ti.conn, ti.sessionManager, engine)
	return hooksAgentKey{key: key, agent: actor}
}

// otherProject creates a second project in the test organization.
func otherProject(t *testing.T, ctx context.Context, ti *testInstance) projectsrepo.Project {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "hooks-other-project",
		Slug:           "hooks-other-" + uuid.NewString()[:8],
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	return project
}

// authenticateAgentKey runs the security Goa runs for method on a request
// carrying key and Gram-Project: the key, then the project.
func authenticateAgentKey(t *testing.T, ti *testInstance, method, key, projectSlug string) (context.Context, error) {
	t.Helper()
	ctx := context.WithValue(t.Context(), goa.MethodKey, method)
	ctx, err := ti.service.APIKeyAuth(ctx, key, &security.APIKeyScheme{
		Name: constants.KeySecurityScheme, Scopes: []string{}, RequiredScopes: []string{"hooks"},
	})
	if err != nil {
		return ctx, err
	}
	return ti.service.APIKeyAuth(ctx, projectSlug, &security.APIKeyScheme{
		Name: constants.ProjectSlugSecuritySchema, Scopes: []string{}, RequiredScopes: []string{"hooks"},
	})
}

func agentClaudePrompt(key, projectSlug, sessionID string) *gen.ClaudePayload {
	payload := claudePluginPrompt(sessionID)
	payload.ApikeyToken = &key
	payload.ProjectSlugInput = &projectSlug
	return payload
}

func agentIngestPrompt(key, projectSlug, sessionID string) *gen.IngestPayload {
	prompt := "relay prompt"
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.ApikeyToken = &key
	payload.ProjectSlugInput = &projectSlug
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	return payload
}

// An agent key whose only grant is org:hooks_ingest on a project passes the
// cursor, codex and OTEL routes for that project without project:read.
func TestAPIKeyAuth_AgentKeyWithOnlyTheHooksGrantReachesItsProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	agentKey := newHooksAgentKey(t, ctx, ti, projectHooksGrant(authCtx.ActiveOrganizationID, *authCtx.ProjectID))

	for _, method := range []string{"cursor", "codex", "logs", "metrics"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			admitted, err := authenticateAgentKey(t, ti, method, agentKey.key, *authCtx.ProjectSlug)
			require.NoError(t, err)
			admittedAuth, ok := contextvalues.GetAuthContext(admitted)
			require.True(t, ok)
			require.Equal(t, *authCtx.ProjectID, *admittedAuth.ProjectID)
		})
	}
}

// The same key sends claude and canonical ingest hooks to its project, and
// they are stored as the agent's.
func TestClaudeAndIngest_AgentKeyWithOnlyTheHooksGrantReachesItsProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	agentKey := newHooksAgentKey(t, ctx, ti, projectHooksGrant(authCtx.ActiveOrganizationID, *authCtx.ProjectID))

	claudeSession := "relay-claude-" + uuid.NewString()
	_, err := ti.service.Claude(t.Context(), agentClaudePrompt(agentKey.key, *authCtx.ProjectSlug, claudeSession))
	require.NoError(t, err)
	requireAgentChat(t, ti, agentKey.sessionID(claudeSession), *authCtx.ProjectID)

	ingestSession := "relay-ingest-" + uuid.NewString()
	_, err = ti.service.Ingest(t.Context(), agentIngestPrompt(agentKey.key, *authCtx.ProjectSlug, ingestSession))
	require.NoError(t, err)
	requireAgentChat(t, ti, agentKey.sessionID(ingestSession), *authCtx.ProjectID)
}

// The hooks grant's project bound still holds: the key is refused on every
// route for a project its grant does not name.
func TestHooks_AgentKeyRefusedForAProjectItsHooksGrantDoesNotName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	other := otherProject(t, ctx, ti)
	agentKey := newHooksAgentKey(t, ctx, ti, projectHooksGrant(authCtx.ActiveOrganizationID, *authCtx.ProjectID))

	for _, method := range []string{"cursor", "codex", "logs", "metrics"} {
		_, err := authenticateAgentKey(t, ti, method, agentKey.key, other.Slug)
		requireShareableCode(t, err, oops.CodeForbidden)
	}

	sessionID := "relay-other-project-" + uuid.NewString()
	_, err := ti.service.Claude(t.Context(), agentClaudePrompt(agentKey.key, other.Slug, sessionID))
	requireShareableCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Ingest(t.Context(), agentIngestPrompt(agentKey.key, other.Slug, sessionID))
	requireShareableCode(t, err, oops.CodeForbidden)
	requireNothingRecorded(t, ctx, ti, sessionID, other.ID)
}

// project:read does not stand in for the hooks grant: a key without
// org:hooks_ingest is refused on every route, even for a project it may read.
func TestHooks_AgentKeyWithoutTheHooksGrantIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	agentKey := newHooksAgentKey(t, ctx, ti, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))

	for _, method := range []string{"cursor", "codex", "logs", "metrics"} {
		_, err := authenticateAgentKey(t, ti, method, agentKey.key, *authCtx.ProjectSlug)
		requireShareableCode(t, err, oops.CodeForbidden)
	}

	sessionID := "relay-no-grant-" + uuid.NewString()
	_, err := ti.service.Claude(t.Context(), agentClaudePrompt(agentKey.key, *authCtx.ProjectSlug, sessionID))
	requireShareableCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Ingest(t.Context(), agentIngestPrompt(agentKey.key, *authCtx.ProjectSlug, sessionID))
	requireShareableCode(t, err, oops.CodeForbidden)
	requireNothingRecorded(t, ctx, ti, sessionID, *authCtx.ProjectID)
}

// projectGateAuthorizer answers every request and is told which project path
// ran: "gate" when project:read applies, "handler" when the hooks check
// decides instead.
type projectGateAuthorizer func(path string) (context.Context, error)

func (a projectGateAuthorizer) Authorize(context.Context, string, *security.APIKeyScheme) (context.Context, error) {
	return a("gate")
}

func (a projectGateAuthorizer) AuthorizeWithHandlerProjectAccess(context.Context, string, *security.APIKeyScheme) (context.Context, error) {
	return a("handler")
}

// Only an agent on an ingestion method resolves its project without the
// project:read gate. Every other caller keeps the gate, and an agent on any
// other method is refused before its project resolves.
func TestAPIKeyAuth_OnlyAgentIngestSkipsTheProjectReadGate(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	agentCtx := grantedAgentContext(t, ctx, ti)
	projectScheme := &security.APIKeyScheme{Name: constants.ProjectSlugSecuritySchema, Scopes: []string{}, RequiredScopes: []string{}}
	resolve := func(callerCtx context.Context, method string) ([]string, error) {
		var paths []string
		ti.service.auth = projectGateAuthorizer(func(path string) (context.Context, error) {
			paths = append(paths, path)
			return callerCtx, nil
		})
		_, err := ti.service.APIKeyAuth(context.WithValue(callerCtx, goa.MethodKey, method), "project", projectScheme)
		return paths, err
	}

	paths, err := resolve(agentCtx, "codex")
	require.NoError(t, err)
	require.Equal(t, []string{"handler"}, paths)

	paths, err = resolve(agentCtx, "getStatus")
	requireShareableCode(t, err, oops.CodeForbidden)
	require.Empty(t, paths)

	for _, method := range []string{"codex", "getStatus", "list"} {
		paths, err = resolve(ctx, method)
		require.NoError(t, err, method)
		require.Equal(t, []string{"gate"}, paths, method)
	}
}
