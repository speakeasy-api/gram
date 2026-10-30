package hooks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz/mcpauthztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	workloadpolicyrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

var (
	workloadTokenScheme   = &security.JWTScheme{Name: constants.DeviceAgentTokenSecurityScheme, Scopes: []string{}, RequiredScopes: []string{}}
	workloadProjectScheme = &security.APIKeyScheme{Name: constants.ProjectSlugSecuritySchema, Scopes: []string{}, RequiredScopes: []string{}}
)

// hooksWorkload is an admitted workload assigned to an agent that holds the
// hooks grant, and a device-agent token for it.
type hooksWorkload struct {
	actor          urn.Principal
	token          string
	projectSlug    string
	organizationID string
	issuerID       uuid.UUID
	subject        string
}

// newHooksWorkload admits a workload assigned to a new agent that holds
// org:hooks_ingest across the organization, and gives the service an engine
// that admits workloads.
func newHooksWorkload(t *testing.T, ctx context.Context, ti *testInstance) hooksWorkload {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID, OwnerUserID: authCtx.UserID,
		ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Name: "Headless hooks agent",
	})
	require.NoError(t, err)
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	seedHooksIngestGrant(t, ctx, ti, authCtx.ActiveOrganizationID, actor)
	projectRead, err := authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   actor,
		Scope:          string(authz.ScopeProjectRead),
		Selectors:      projectRead,
	})
	require.NoError(t, err)

	// Workload admission also bounds every request by the agent owner's live
	// permissions. The owner may reach every project and send hooks to any of
	// them, so the agent's grants alone decide in these tests.
	owner := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	seedHooksIngestGrant(t, ctx, ti, authCtx.ActiveOrganizationID, owner)
	ownerProjectRead, err := authz.NewSelector(authz.ScopeProjectRead, authz.WildcardResource).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   owner,
		Scope:          string(authz.ScopeProjectRead),
		Selectors:      ownerProjectRead,
	})
	require.NoError(t, err)

	subject := "device:" + uuid.NewString()
	fixtures := testrepo.New(ti.conn)
	issuerID, err := fixtures.CreateWorkloadIssuerFixture(ctx, testrepo.CreateWorkloadIssuerFixtureParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Name:           "devices",
		Issuer:         workloadidentity.DeviceIssuerURL(ti.service.serverURL.String(), authCtx.ActiveOrganizationID),
		JwksUri:        workloadidentity.DeviceIssuerURL(ti.service.serverURL.String(), authCtx.ActiveOrganizationID) + "/jwks.json",
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.CreateWorkloadIdentityAdmissionFixture(ctx, testrepo.CreateWorkloadIdentityAdmissionFixtureParams{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		WorkloadIssuerID: issuerID,
		Subject:          subject,
	}))
	require.NoError(t, fixtures.CreateWorkloadAgentAssignmentFixture(ctx, testrepo.CreateWorkloadAgentAssignmentFixtureParams{
		OrganizationID:   authCtx.ActiveOrganizationID,
		WorkloadIssuerID: issuerID,
		Subject:          subject,
		MatchKind:        string(workloadidentity.MatchKindExact),
		AgentID:          agent.ID,
	}))

	engine := authz.NewEngine(testenv.NewLogger(t), ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), authz.EngineOpts{
		DevMode:                          false,
		AdmitPrincipalCredential:         runtimepolicy.AdmitPrincipalCredential,
		AdmitPrincipalCredentialWithDBTX: runtimepolicy.AdmitPrincipalCredentialWithDBTX,
		AdmitWorkloadSession:             runtimepolicy.AdmitWorkloadSession,
	})
	ti.service.authz = engine
	ti.service.auth = auth.New(testenv.NewLogger(t), ti.conn, ti.sessionManager, engine)

	token, _, err := ti.service.deviceAgentTokens.Mint(authCtx.ActiveOrganizationID, urn.NewWorkloadPrincipal(issuerID, subject), time.Now())
	require.NoError(t, err)
	return hooksWorkload{actor: actor, token: token, projectSlug: *authCtx.ProjectSlug, organizationID: authCtx.ActiveOrganizationID, issuerID: issuerID, subject: subject}
}

// authenticateWorkload runs the device-agent token requirement Goa runs for
// method: the token, then the project.
func authenticateWorkload(t *testing.T, ti *testInstance, method, token, projectSlug string) (context.Context, error) {
	t.Helper()
	ctx := context.WithValue(t.Context(), goa.MethodKey, method)
	ctx, err := ti.service.JWTAuth(ctx, token, workloadTokenScheme)
	if err != nil {
		return ctx, err
	}
	return ti.service.APIKeyAuth(ctx, projectSlug, workloadProjectScheme)
}

func requireShareableCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	shareable, ok := errors.AsType[*oops.ShareableError](err)
	require.True(t, ok, "error: %v", err)
	require.Equal(t, code, shareable.Code, "error: %v", err)
}

// A workload's device-agent token sends Cursor hooks as its agent, in the
// project the request names.
func TestCursor_WorkloadTokenIngestsAsItsAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)

	admitted, err := authenticateWorkload(t, ti, "cursor", workload.token, workload.projectSlug)
	require.NoError(t, err)

	actor, ok := contextvalues.AuthenticatedActor(admitted)
	require.True(t, ok)
	require.Equal(t, urn.NewWorkloadPrincipal(workload.issuerID, workload.subject).String(), actor.String(), "the device stays the authenticated actor")
	require.True(t, isAgentActor(admitted))

	// Events name the agent as their actor, which telemetry attributes agent
	// activity by, and the device that sent them; client-supplied values for
	// either are replaced.
	attrs := withAgentActor(admitted, map[attr.Key]any{
		attr.AuthorizationActorIDKey: "spoofed",
		attr.WorkloadSubjectKey:      "spoofed",
	})
	require.Equal(t, string(urn.PrincipalTypeAgent), attrs[attr.AuthorizationActorTypeKey])
	require.Equal(t, workload.actor.ID, attrs[attr.AuthorizationActorIDKey])
	require.Equal(t, workload.issuerID.String(), attrs[attr.WorkloadIssuerIDKey])
	require.Equal(t, workload.subject, attrs[attr.WorkloadSubjectKey])
	require.Equal(t, workload.actor.String()+":session-1", agentSessionID(admitted, "session-1"), "sessions belong to the agent")
	authCtx, ok := contextvalues.GetAuthContext(admitted)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectSlug)
	require.Equal(t, workload.projectSlug, *authCtx.ProjectSlug)

	toolName := "Shell"
	toolUseID := "tool-" + uuid.NewString()
	_, err = ti.service.Cursor(admitted, &gen.CursorPayload{HookEventName: "postToolUse", ToolName: &toolName, ToolUseID: &toolUseID})
	require.NoError(t, err, "an agent actor needs no user email")
}

// revokeAgentHooks deletes the organization-wide org:hooks_ingest grant
// newHooksWorkload gave the workload's agent.
func revokeAgentHooks(t *testing.T, ctx context.Context, ti *testInstance, workload hooksWorkload) {
	t.Helper()
	selectors, err := authz.NewSelector(authz.ScopeOrgHooksIngest, workload.organizationID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).DeletePrincipalGrantByIdentity(ctx, accessrepo.DeletePrincipalGrantByIdentityParams{
		OrganizationID: workload.organizationID,
		PrincipalUrn:   workload.actor,
		Scope:          string(authz.ScopeOrgHooksIngest),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

// A workload sends hooks as its agent, so like an agent key it needs the
// agent's hooks grant on the project it names and not project:read there.
func TestCodexAndIngest_WorkloadNeedsNoProjectRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	selectors, err := authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).DeletePrincipalGrantByIdentity(ctx, accessrepo.DeletePrincipalGrantByIdentityParams{
		OrganizationID: workload.organizationID,
		PrincipalUrn:   workload.actor,
		Scope:          string(authz.ScopeProjectRead),
		Selectors:      selectors,
	})
	require.NoError(t, err)

	_, err = authenticateWorkload(t, ti, "codex", workload.token, workload.projectSlug)
	require.NoError(t, err)
	_, err = ti.service.Ingest(context.Background(), workloadIngestPayload(workload, workload.projectSlug))
	require.NoError(t, err)
}

// grantAgentHooksFor replaces the agent's organization-wide hooks grant with
// one bounded to projectID.
func grantAgentHooksFor(t *testing.T, ctx context.Context, ti *testInstance, workload hooksWorkload, projectID uuid.UUID) {
	t.Helper()
	revokeAgentHooks(t, ctx, ti, workload)
	selector := authz.NewSelector(authz.ScopeOrgHooksIngest, workload.organizationID)
	selector[authz.SelectorKeyProjectID] = projectID.String()
	bounded, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: workload.organizationID,
		PrincipalUrn:   workload.actor,
		Scope:          string(authz.ScopeOrgHooksIngest),
		Selectors:      bounded,
	})
	require.NoError(t, err)
}

// otherReadableProject creates a second project in the workload's organization
// that its agent may read, so the agent's hooks grant alone decides whether
// hooks reach it.
func otherReadableProject(t *testing.T, ctx context.Context, ti *testInstance, workload hooksWorkload) projectsrepo.Project {
	t.Helper()
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "hooks-other-project",
		Slug:           "hooks-other-" + uuid.NewString()[:8],
		OrganizationID: workload.organizationID,
	})
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeProjectRead, project.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: workload.organizationID,
		PrincipalUrn:   workload.actor,
		Scope:          string(authz.ScopeProjectRead),
		Selectors:      selectors,
	})
	require.NoError(t, err)
	return project
}

// A workload names its project on every hooks request. One that names none is
// refused, even in an organization whose only project would otherwise be
// chosen for it.
func TestCodex_WorkloadWithoutGramProjectIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)

	for _, project := range []string{"", "  "} {
		_, err := authenticateWorkload(t, ti, "codex", workload.token, project)
		requireShareableCode(t, err, oops.CodeForbidden)
		require.ErrorContains(t, err, "hooks must name the project they are sent to")
	}
}

// The agent's org:hooks_ingest grant decides which projects the workload's
// hooks may name: the granted project is accepted, any other refused, even
// one its agent may read.
func TestCodex_WorkloadHooksFollowTheAgentsProjectGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)
	other := otherReadableProject(t, ctx, ti, workload)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	grantAgentHooksFor(t, ctx, ti, workload, *authCtx.ProjectID)

	_, err := authenticateWorkload(t, ti, "codex", workload.token, workload.projectSlug)
	require.NoError(t, err)

	_, err = authenticateWorkload(t, ti, "codex", workload.token, other.Slug)
	requireShareableCode(t, err, oops.CodeForbidden)
}

// An agent granted hooks across the organization lets its workload send to any
// project there it names: the token itself bounds no project.
func TestCodex_WorkloadReachesEveryProjectAnOrgWideGrantCovers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)
	other := otherReadableProject(t, ctx, ti, workload)

	for _, project := range []string{workload.projectSlug, other.Slug} {
		admitted, err := authenticateWorkload(t, ti, "codex", workload.token, project)
		require.NoError(t, err)
		authCtx, ok := contextvalues.GetAuthContext(admitted)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectSlug)
		require.Equal(t, project, *authCtx.ProjectSlug)
	}
}

// The token reaches only the ingestion methods an agent key may.
func TestJWTAuth_WorkloadTokenRefusedOnOtherMethods(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)

	_, err := ti.service.JWTAuth(context.WithValue(t.Context(), goa.MethodKey, "claude"), workload.token, workloadTokenScheme)
	requireShareableCode(t, err, oops.CodeForbidden)
}

// A workload refused at its project keeps that refusal when the request also
// carries an API key: the key requirement Goa tries next does not authorize
// on top of the workload's admission.
func TestCodex_WorkloadRefusedAtItsProjectIsNotRescuedByAKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)
	other := otherReadableProject(t, ctx, ti, workload)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	grantAgentHooksFor(t, ctx, ti, workload, *authCtx.ProjectID)

	refusedCtx, err := authenticateWorkload(t, ti, "codex", workload.token, other.Slug)
	requireShareableCode(t, err, oops.CodeForbidden)

	_, err = ti.service.APIKeyAuth(refusedCtx, "gram_key_not_a_real_key", &security.APIKeyScheme{
		Name:           constants.KeySecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{"hooks"},
	})
	requireShareableCode(t, err, oops.CodeForbidden)
}

// A workload whose agent lacks org:hooks_ingest is refused, as an agent key
// without it is.
func TestCursor_WorkloadTokenRefusedWithoutTheHooksGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)
	revokeAgentHooks(t, ctx, ti, workload)

	_, err := authenticateWorkload(t, ti, "cursor", workload.token, workload.projectSlug)
	requireShareableCode(t, err, oops.CodeForbidden)
}

// Withdrawing the workload's agent assignment refuses its next ingest, though
// its token has not expired: admission re-reads the assignment per request.
func TestCodex_WithdrawnWorkloadIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)

	withdrawn, err := workloadpolicyrepo.New(ti.conn).SoftDeleteWorkloadAgentAssignmentForSubject(ctx, workloadpolicyrepo.SoftDeleteWorkloadAgentAssignmentForSubjectParams{
		OrganizationID:   workload.organizationID,
		WorkloadIssuerID: workload.issuerID,
		MatchKind:        string(workloadidentity.MatchKindExact),
		Subject:          workload.subject,
	})
	require.NoError(t, err)
	require.Len(t, withdrawn, 1)

	_, err = authenticateWorkload(t, ti, "codex", workload.token, workload.projectSlug)
	requireShareableCode(t, err, oops.CodeUnauthorized)
}

// An expired token is refused as unauthorized on every ingest method, so the
// device exchanges for a new one.
func TestJWTAuth_ExpiredWorkloadTokenIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)

	expired, _, err := ti.service.deviceAgentTokens.Mint(workload.organizationID, urn.NewWorkloadPrincipal(workload.issuerID, workload.subject), time.Now().Add(-2*deviceagenttoken.Lifetime))
	require.NoError(t, err)
	for _, method := range []string{"cursor", "codex", "logs", "metrics"} {
		_, err := authenticateWorkload(t, ti, method, expired, workload.projectSlug)
		requireShareableCode(t, err, oops.CodeUnauthorized)
	}
}

// A token naming an organization that no longer exists is refused as
// unauthorized rather than failing as a server error.
func TestJWTAuth_TokenForMissingOrganizationIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)

	token, _, err := ti.service.deviceAgentTokens.Mint("org-"+uuid.NewString(), urn.NewWorkloadPrincipal(workload.issuerID, workload.subject), time.Now())
	require.NoError(t, err)
	_, err = authenticateWorkload(t, ti, "codex", token, workload.projectSlug)
	requireShareableCode(t, err, oops.CodeUnauthorized)
}

// A token not signed by the platform key is refused on every ingest method.
func TestJWTAuth_ForgedWorkloadTokenIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)

	forged, _, err := deviceagenttoken.New(mcpauthztest.NewIssuer(t), nil).Mint(workload.organizationID, urn.NewWorkloadPrincipal(uuid.New(), workload.subject), time.Now())
	require.NoError(t, err)
	for _, method := range []string{"cursor", "codex", "logs", "metrics"} {
		_, err := authenticateWorkload(t, ti, method, forged, workload.projectSlug)
		requireShareableCode(t, err, oops.CodeUnauthorized)
	}
}

// A deployment with no platform signer refuses a device-agent token with a
// recorded reason, so the API key requirement Goa tries next reports it
// instead of a missing key.
func TestJWTAuth_DeploymentWithoutTokensRecordsTheRefusal(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)
	token, _, err := deviceagenttoken.New(mcpauthztest.NewIssuer(t), nil).Mint("org-"+uuid.NewString(), urn.NewWorkloadPrincipal(uuid.New(), "device:"+uuid.NewString()), time.Now())
	require.NoError(t, err)
	ti.service.deviceAgentTokens = nil

	refusedCtx, err := ti.service.JWTAuth(context.WithValue(t.Context(), goa.MethodKey, "cursor"), token, workloadTokenScheme)
	requireShareableCode(t, err, oops.CodeUnauthorized)
	require.ErrorContains(t, deviceagenttoken.AuthFailure(refusedCtx), "does not accept device-agent tokens")

	// The key requirement Goa tries next, with no key, reports the refusal
	// rather than a missing key.
	_, err = ti.service.APIKeyAuth(refusedCtx, "", &security.APIKeyScheme{
		Name: constants.KeySecurityScheme, Scopes: []string{}, RequiredScopes: []string{"hooks"},
	})
	require.ErrorContains(t, err, "device-agent token refused")
	require.ErrorContains(t, err, "does not accept device-agent tokens")
}
