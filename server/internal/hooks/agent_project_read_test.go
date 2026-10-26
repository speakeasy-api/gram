package hooks

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var hooksProjectScheme = &security.APIKeyScheme{Name: constants.ProjectSlugSecuritySchema, Scopes: []string{}, RequiredScopes: []string{"hooks"}}

// keySchemeStub answers the key scheme with an already-authenticated context
// and runs the real authorizer for the project scheme.
type keySchemeStub struct {
	key  stubAuthorizer
	real *auth.Auth
}

func (a keySchemeStub) Authorize(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	if schema.Name == constants.KeySecurityScheme {
		return a.key()
	}
	return a.real.Authorize(ctx, key, schema)
}

func (a keySchemeStub) AuthorizeWithHandlerProjectAccess(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return a.real.AuthorizeWithHandlerProjectAccess(ctx, key, schema)
}

// hooksOnlyAgentKey admits an agent key bound to no project, whose agent holds
// org:hooks_ingest on hooksProjectID alone and no project:read, as a hooks-only
// agent identity does. The service gets the real authorizer and engine.
func hooksOnlyAgentKey(t *testing.T, ctx context.Context, ti *testInstance, hooksProjectID uuid.UUID) context.Context {
	t.Helper()
	authCtx, actor, ownerUserID := agentAuthContext(t, ctx, ti)

	selector := authz.NewSelector(authz.ScopeOrgHooksIngest, authCtx.ActiveOrganizationID)
	selector[authz.SelectorKeyProjectID] = hooksProjectID.String()
	bounded, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   actor,
		Scope:          string(authz.ScopeOrgHooksIngest),
		Selectors:      bounded,
	})
	require.NoError(t, err)
	seedHooksIngestGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, ownerUserID))

	policy, err := runtimepolicy.NewDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion2, []authz.Grant{authz.NewGrant(authz.ScopeOrgHooksIngest, authCtx.ActiveOrganizationID)})
	require.NoError(t, err)
	raw, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion2, policy)
	require.NoError(t, err)

	engine := authz.NewEngine(testenv.NewLogger(t), ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), authz.EngineOpts{
		DevMode:                          false,
		AdmitPrincipalCredential:         runtimepolicy.AdmitPrincipalCredential,
		AdmitPrincipalCredentialWithDBTX: runtimepolicy.AdmitPrincipalCredentialWithDBTX,
	})
	ti.service.authz = engine

	authCtx.ProjectID = nil
	authCtx.ProjectSlug = nil
	keyCtx, err := engine.PrepareContext(contextvalues.WithPrincipalCredentialAuthorization(t.Context(), authCtx, actor, contextvalues.PrincipalCredential{
		AuthorizerUserID:       ownerUserID,
		DelegatedGrants:        raw,
		DelegatedGrantsVersion: int32(runtimepolicy.DelegatedPolicyVersion2),
	}))
	require.NoError(t, err)
	ti.service.auth = keySchemeStub{key: func() (context.Context, error) { return keyCtx, nil }, real: auth.New(testenv.NewLogger(t), ti.conn, ti.sessionManager, engine)}
	return keyCtx
}

func testProjectSlug(t *testing.T, ctx context.Context) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectSlug)
	return *authCtx.ProjectSlug
}

func createOrgProject(t *testing.T, ctx context.Context, ti *testInstance) projectsrepo.Project {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "hooks-unreadable-project",
		Slug:           "hooks-unreadable-" + uuid.NewString()[:8],
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	return project
}

// A hooks-only agent key sends hooks to the project its org:hooks_ingest grant
// names without holding project:read there, which the generic project gate
// still demands.
func TestAPIKeyAuth_HooksOnlyAgentKeySkipsProjectRead(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"cursor", "codex", "logs", "metrics"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			keyCtx := context.WithValue(hooksOnlyAgentKey(t, ctx, ti, *authCtx.ProjectID), goa.MethodKey, method)
			slug := testProjectSlug(t, ctx)

			_, err := ti.service.auth.Authorize(keyCtx, slug, hooksProjectScheme)
			requireShareableCode(t, err, oops.CodeForbidden)

			authed, err := ti.service.APIKeyAuth(keyCtx, slug, hooksProjectScheme)
			require.NoError(t, err)
			resolved, ok := contextvalues.GetAuthContext(authed)
			require.True(t, ok)
			require.NotNil(t, resolved.ProjectID)
			require.Equal(t, *authCtx.ProjectID, *resolved.ProjectID)
		})
	}
}

// Without project:read, the project-bounded hooks grant is all that admits an
// agent key, so it reaches no other project in the organization.
func TestAPIKeyAuth_HooksOnlyAgentKeyRefusedOutsideItsGrantedProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	other := createOrgProject(t, ctx, ti)
	keyCtx := context.WithValue(hooksOnlyAgentKey(t, ctx, ti, *authCtx.ProjectID), goa.MethodKey, "cursor")

	_, err := ti.service.APIKeyAuth(keyCtx, other.Slug, hooksProjectScheme)
	requireShareableCode(t, err, oops.CodeForbidden)
}

// The optional-auth plugin path (claude, ingest) applies the same rule.
func TestAuthorizePluginRequest_HooksOnlyAgentKeySkipsProjectRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	other := createOrgProject(t, ctx, ti)
	hooksOnlyAgentKey(t, ctx, ti, *authCtx.ProjectID)

	authed, err := ti.service.authorizePluginRequest(t.Context(), "gram_local_test", testProjectSlug(t, ctx))
	require.NoError(t, err)
	resolved, ok := contextvalues.GetAuthContext(authed)
	require.True(t, ok)
	require.Equal(t, *authCtx.ProjectID, *resolved.ProjectID)

	_, err = ti.service.authorizePluginRequest(t.Context(), "gram_local_test", other.Slug)
	require.ErrorIs(t, err, errAgentHooksDenied)
	requireShareableCode(t, err, oops.CodeForbidden)
}

// projectGateRecorder records which project authorization a request took.
type projectGateRecorder struct {
	gated, ungated int
}

func (r *projectGateRecorder) Authorize(ctx context.Context, _ string, _ *security.APIKeyScheme) (context.Context, error) {
	r.gated++
	return ctx, nil
}

func (r *projectGateRecorder) AuthorizeWithHandlerProjectAccess(ctx context.Context, _ string, _ *security.APIKeyScheme) (context.Context, error) {
	r.ungated++
	return ctx, nil
}

// Only an agent key skips the project:read gate; a human's request keeps it.
func TestAuthorizeHooksProject_HumansKeepProjectReadGate(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	slug := testProjectSlug(t, ctx)

	human := &projectGateRecorder{gated: 0, ungated: 0}
	ti.service.auth = human
	_, err := ti.service.authorizeHooksProject(ctx, slug, hooksProjectScheme)
	require.NoError(t, err)
	require.Equal(t, projectGateRecorder{gated: 1, ungated: 0}, *human)

	agent := &projectGateRecorder{gated: 0, ungated: 0}
	ti.service.auth = agent
	_, err = ti.service.authorizeHooksProject(grantedAgentContext(t, ctx, ti), slug, hooksProjectScheme)
	require.NoError(t, err)
	require.Equal(t, projectGateRecorder{gated: 0, ungated: 1}, *agent)
}
