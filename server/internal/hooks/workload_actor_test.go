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
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz/mcpauthztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
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

// newHooksWorkload admits a workload assigned to a new agent, bound to the
// test project when bindProject is set, and gives the service an engine that
// admits workloads.
func newHooksWorkload(t *testing.T, ctx context.Context, ti *testInstance, bindProject bool) hooksWorkload {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)

	params := agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID, OwnerUserID: authCtx.UserID,
		ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Name: "Headless hooks agent",
	}
	if bindProject {
		params.ProjectID = uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}
	}
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, params)
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
	workload := newHooksWorkload(t, ctx, ti, false)

	admitted, err := authenticateWorkload(t, ti, "cursor", workload.token, workload.projectSlug)
	require.NoError(t, err)

	actor, ok := contextvalues.AuthenticatedActor(admitted)
	require.True(t, ok)
	require.Equal(t, workload.actor.String(), actor.String(), "hooks are attributed to the assigned agent")
	require.True(t, isAgentActor(admitted))
	authCtx, ok := contextvalues.GetAuthContext(admitted)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectSlug)
	require.Equal(t, workload.projectSlug, *authCtx.ProjectSlug)

	toolName := "Shell"
	toolUseID := "tool-" + uuid.NewString()
	_, err = ti.service.Cursor(admitted, &gen.CursorPayload{HookEventName: "postToolUse", ToolName: &toolName, ToolUseID: &toolUseID})
	require.NoError(t, err, "an agent actor needs no user email")
}

// An agent bound to a project supplies it when the request names none, and a
// request naming another is refused.
func TestCodex_WorkloadTokenUsesItsAgentsProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti, true)

	admitted, err := authenticateWorkload(t, ti, "codex", workload.token, "")
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(admitted)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectSlug)
	require.Equal(t, workload.projectSlug, *authCtx.ProjectSlug)

	_, err = authenticateWorkload(t, ti, "codex", workload.token, "another-project")
	requireShareableCode(t, err, oops.CodeForbidden)
}

// The token reaches only the ingestion methods an agent key may.
func TestJWTAuth_WorkloadTokenRefusedOnOtherMethods(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti, false)

	_, err := ti.service.JWTAuth(context.WithValue(t.Context(), goa.MethodKey, "claude"), workload.token, workloadTokenScheme)
	requireShareableCode(t, err, oops.CodeForbidden)
}

// A workload whose agent lacks org:hooks_ingest is refused, as an agent key
// without it is.
func TestCursor_WorkloadTokenRefusedWithoutTheHooksGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti, false)

	selectors, err := authz.NewSelector(authz.ScopeOrgHooksIngest, workload.organizationID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).DeletePrincipalGrantByIdentity(ctx, accessrepo.DeletePrincipalGrantByIdentityParams{
		OrganizationID: workload.organizationID,
		PrincipalUrn:   workload.actor,
		Scope:          string(authz.ScopeOrgHooksIngest),
		Selectors:      selectors,
	})
	require.NoError(t, err)

	_, err = authenticateWorkload(t, ti, "cursor", workload.token, workload.projectSlug)
	require.Error(t, err)
}

// Withdrawing the workload's agent assignment refuses its next ingest, though
// its token has not expired: admission re-reads the assignment per request.
func TestCodex_WithdrawnWorkloadIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti, false)

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

// A token not signed by the platform key is refused on every ingest method.
func TestJWTAuth_ForgedWorkloadTokenIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti, false)

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
}
