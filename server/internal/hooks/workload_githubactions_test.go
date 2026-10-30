package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// newGitHubActionsHooksWorkload sets up what the GitHub Actions guided setup
// writes, as hooks see it: GitHub's issuer and a wildcard over one repository
// at the organization tier, assigned to an agent whose hooks grant names the
// workflow's project alone. It returns a pull request job's device-agent
// token, minted by the issuer the device-agent token endpoint uses.
func newGitHubActionsHooksWorkload(t *testing.T, ctx context.Context, ti *testInstance) hooksWorkload {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)
	organizationID := authCtx.ActiveOrganizationID

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: organizationID, OwnerUserID: authCtx.UserID,
		ProjectID: uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}, Name: "Release bot",
	})
	require.NoError(t, err)
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	grant := projectHooksGrant(organizationID, *authCtx.ProjectID)
	selectors, err := grant.Selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   actor,
		Scope:          string(grant.Scope),
		Selectors:      selectors,
	})
	require.NoError(t, err)
	// The owner, an administrator in practice, may send hooks anywhere, so the
	// agent's project-bound grant alone decides.
	owner := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	seedHooksIngestGrant(t, ctx, ti, organizationID, owner)
	ownerProjectRead, err := authz.NewSelector(authz.ScopeProjectRead, authz.WildcardResource).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   owner,
		Scope:          string(authz.ScopeProjectRead),
		Selectors:      ownerProjectRead,
	})
	require.NoError(t, err)

	rule := "repo:acme/payments-api:*"
	fixtures := testrepo.New(ti.conn)
	issuerID, err := fixtures.CreateWorkloadIssuerFixture(ctx, testrepo.CreateWorkloadIssuerFixtureParams{
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Name:           "GitHub Actions",
		Issuer:         "https://token.actions.githubusercontent.com",
		JwksUri:        "https://token.actions.githubusercontent.com/.well-known/jwks",
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.CreateWorkloadIdentityRuleFixture(ctx, testrepo.CreateWorkloadIdentityRuleFixtureParams{
		OrganizationID:   organizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		WorkloadIssuerID: issuerID,
		Subject:          rule,
		MatchKind:        string(workloadidentity.MatchKindWildcard),
	}))
	require.NoError(t, fixtures.CreateWorkloadAgentAssignmentFixture(ctx, testrepo.CreateWorkloadAgentAssignmentFixtureParams{
		OrganizationID:   organizationID,
		WorkloadIssuerID: issuerID,
		Subject:          rule,
		MatchKind:        string(workloadidentity.MatchKindWildcard),
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

	subject := "repo:acme/payments-api:pull_request"
	token, _, err := ti.service.deviceAgentTokens.Mint(organizationID, urn.NewWorkloadPrincipal(issuerID, subject), time.Now())
	require.NoError(t, err)
	return hooksWorkload{actor: actor, token: token, projectSlug: *authCtx.ProjectSlug, organizationID: organizationID, issuerID: issuerID, subject: subject}
}

// workloadClaudePrompt is a Claude Code prompt event a job's hooks relay
// sends with its device-agent token, naming projectSlug.
func workloadClaudePrompt(token, projectSlug, sessionID string) *gen.ClaudePayload {
	payload := claudePluginPrompt(sessionID)
	bearer := "Bearer " + token
	payload.ApikeyToken = nil
	payload.DeviceAgentToken = &bearer
	payload.ProjectSlugInput = &projectSlug
	return payload
}

// A GitHub Actions job's hooks reach the Claude hook handler as the agent the
// repository's rule assigns, in the project the workflow names.
func TestClaude_GitHubActionsJobSendsHooksAsItsAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newGitHubActionsHooksWorkload(t, ctx, ti)

	// The path the Claude hook takes for a device-agent token.
	admitted, err := ti.service.authorizeWorkloadPluginRequest(t.Context(), workload.token, workload.projectSlug)
	require.NoError(t, err)
	agent, ok := contextvalues.EffectiveAgent(admitted)
	require.True(t, ok)
	require.Equal(t, workload.actor.String(), agent.String(), "the job acts as the agent the rule assigns")

	sessionID := "github-actions-" + uuid.NewString()
	_, err = ti.service.Claude(t.Context(), workloadClaudePrompt(workload.token, workload.projectSlug, sessionID))
	require.NoError(t, err)
}

// The agent's hooks grant names the workflow's project, so a job that names
// another project is refused there and nothing is recorded.
func TestClaude_GitHubActionsJobRefusedForAnotherProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newGitHubActionsHooksWorkload(t, ctx, ti)
	other := otherProject(t, ctx, ti)

	sessionID := "github-actions-other-" + uuid.NewString()
	_, err := ti.service.Claude(t.Context(), workloadClaudePrompt(workload.token, other.Slug, sessionID))
	requireShareableCode(t, err, oops.CodeForbidden)
	requireNothingRecorded(t, ctx, ti, sessionID, other.ID)
}
