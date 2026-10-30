package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	agentgen "github.com/speakeasy-api/gram/server/gen/agent"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agent"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// newGitHubActionsGrantFixture writes what the GitHub Actions guided setup
// writes: the issuer and a wildcard over one repository, both at the
// organization tier, assigned to one agent. That agent holds what the setup
// grants it: mcp:connect on the fixture's server, org:device_agent_sync, and
// org:hooks_ingest for the server's project alone.
//
// Nobody can sign as GitHub in a test, so a dev-idp stands in for its issuer;
// the subjects are GitHub's. The fixture's subject is a pull request job.
func newGitHubActionsGrantFixture(t *testing.T, owner, repo string) deviceAgentGrantFixture {
	t.Helper()

	issuer := devidptest.Launch(t, devidptest.LaunchOpts{EnableWorkOS: false, Key: nil, TLS: true})
	jwksURI := oauthtest.DiscoverWorkloadJWKSURI(t, issuer)

	ctx, ti := newTestMCPServiceWithPoolConfigAndTemporal(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{
		SessionTTL:         0,
		LiveSessionCap:     0,
		InitializeRate:     ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		RequestRate:        ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		MaxRequestLifetime: 0,
	}, nil, nil, true, mcp.MetaRuntimeConfig{MemberCallTimeout: 0, ValidationTimeout: 0}, testenv.NewTracerProvider(t), nil, nil, "https://speakeasy.example.test", guardian.WithTLSRootCAs(issuer.RootCAs()))
	fx := newWorkloadConsentFixture(t, ctx, ti)

	agent := createConsentAgent(t, ctx, ti, fx, "Release bot")
	agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, agentPrincipal, fx.target.MCPResourceID)
	seedPrincipalGrant(t, ctx, ti, fx.orgID, agentPrincipal, authz.ScopeOrgDeviceAgentSync, authz.WildcardResource)
	seedProjectHooksIngestGrant(t, ti, fx.orgID, agentPrincipal, fx.target.ProjectID)
	// A workload's reach is also bounded by its agent owner's, who in an
	// organization would be an administrator.
	agentOwner := urn.NewPrincipal(urn.PrincipalTypeUser, fx.userID)
	seedPrincipalGrant(t, ctx, ti, fx.orgID, agentOwner, authz.ScopeOrgDeviceAgentSync, authz.WildcardResource)
	seedPrincipalGrant(t, ctx, ti, fx.orgID, agentOwner, authz.ScopeOrgHooksIngest, authz.WildcardResource)

	rule := catalogRule(t, "github-actions", map[string]string{"owner": owner, "repo": repo})
	fixtures := testrepo.New(ti.conn)
	issuerID, err := fixtures.CreateWorkloadIssuerFixture(ctx, testrepo.CreateWorkloadIssuerFixtureParams{
		OrganizationID: fx.orgID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Name:           "GitHub Actions",
		Issuer:         issuer.OAuth21URL,
		JwksUri:        jwksURI,
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.CreateWorkloadIdentityRuleFixture(ctx, testrepo.CreateWorkloadIdentityRuleFixtureParams{
		OrganizationID:   fx.orgID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		WorkloadIssuerID: issuerID,
		Subject:          rule,
		MatchKind:        "wildcard",
	}))
	require.NoError(t, fixtures.CreateWorkloadAgentAssignmentFixture(ctx, testrepo.CreateWorkloadAgentAssignmentFixtureParams{
		OrganizationID:   fx.orgID,
		WorkloadIssuerID: issuerID,
		Subject:          rule,
		MatchKind:        "wildcard",
		AgentID:          agent.ID,
	}))

	slug := fx.toolset.McpSlug.String
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, slug)

	f := workloadGrantFixture{
		ctx:              ctx,
		ti:               ti,
		fx:               fx,
		issuer:           issuer,
		issuerID:         issuerID,
		agentID:          agent.ID,
		assignmentID:     uuid.Nil,
		subject:          strings.TrimSuffix(rule, "*") + "pull_request",
		resource:         strings.TrimSuffix(ti.serverURL.String(), "/") + "/mcp/" + slug,
		advertisedIssuer: advertisedIssuer,
	}
	return deviceAgentGrantFixture{
		workloadGrantFixture: f,
		tokenURL:             deviceagenttoken.TokenURL(ti.serverURL.String(), fx.orgID),
	}
}

// seedProjectHooksIngestGrant grants org:hooks_ingest for one project, the
// project the setup's workflow names for hook events.
func seedProjectHooksIngestGrant(t *testing.T, ti *testInstance, organizationID string, principal urn.Principal, projectID uuid.UUID) {
	t.Helper()

	selector := authz.NewSelector(authz.ScopeOrgHooksIngest, authz.WildcardResource)
	selector[authz.SelectorKeyProjectID] = projectID.String()
	require.NoError(t, authz.ValidateSelector(authz.ScopeOrgHooksIngest, selector))
	selectors, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(authz.ScopeOrgHooksIngest),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

// deviceAgentAccessToken reads the access token out of a device-agent token
// response body.
func deviceAgentAccessToken(t *testing.T, body []byte) string {
	t.Helper()

	var resp map[string]any
	require.NoError(t, json.Unmarshal(body, &resp))
	token, ok := resp["access_token"].(string)
	require.True(t, ok)
	return token
}

// One rule, one agent: a pull request job exchanges its token at the
// device-agent endpoint, polls and sends hooks as the assigned agent, and the
// same job identity exchanges at the project's MCP server as that same
// workload principal. The organization tier is what lets both endpoints read
// the one rule.
func TestGitHubActionsRule_OneAgentForPluginsMCPAndHooks(t *testing.T) {
	t.Parallel()

	f := newGitHubActionsGrantFixture(t, "acme", "payments-api")

	w := f.exchangeDeviceAgent(t, f.assertion(t, f.tokenURL))
	requireDeviceAgentToken(t, w, f.fx.orgID, f.issuerID, f.subject)

	accessToken := deviceAgentAccessToken(t, w.Body.Bytes())
	callerIssuer, err := sharedCallerIssuer()
	require.NoError(t, err)
	tokens := deviceagenttoken.New(callerIssuer, f.ti.conn)
	admitted, assigned, err := tokens.Admit(t.Context(), accessToken, f.ti.authzEngine)
	require.NoError(t, err)
	require.Equal(t, f.agentID.String(), assigned.Principal.ID, "the job acts as the agent the rule assigns")

	// The real getPlugins handler, behind its own device-agent token
	// authentication, polls as that agent. The hooks handler is driven in
	// internal/hooks (TestClaude_GitHubActionsJob*), whose service this
	// package's fixtures cannot build; here the exchanged token's hooks reach
	// is checked against the engine that handler uses.
	plugins := agent.NewService(f.ti.logger, f.ti.tracerProvider, f.ti.conn, f.ti.sessionManager, f.ti.authzEngine, f.ti.audit,
		f.ti.productFeatures, f.ti.serverURL.String(), orghost.New(orghost.Config{ServerURL: f.ti.serverURL, SiteURL: nil}),
		assetstest.NewTestBlobStore(t), nil, nil, tokens)
	polling, err := plugins.JWTAuth(context.WithValue(t.Context(), goa.MethodKey, "getPlugins"), accessToken, &security.JWTScheme{Name: constants.DeviceAgentTokenSecurityScheme, Scopes: []string{}, RequiredScopes: []string{}})
	require.NoError(t, err)
	polled, err := plugins.GetPlugins(polling, &agentgen.GetPluginsPayload{})
	require.NoError(t, err)
	require.NotNil(t, polled.Principal)
	require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()).String(), polled.Principal.Urn, "getPlugins answers for the agent the rule assigns")

	// Hooks for the workflow's project.
	require.NoError(t, f.ti.authzEngine.Require(admitted, authz.HooksIngestCheck(f.fx.orgID, f.fx.target.ProjectID.String())))

	// Hooks for any other project are refused: the grant names one project.
	err = f.ti.authzEngine.Require(admitted, authz.HooksIngestCheck(f.fx.orgID, uuid.NewString()))
	shareable, ok := errors.AsType[*oops.ShareableError](err)
	require.True(t, ok, "error: %v", err)
	require.Equal(t, oops.CodeForbidden, shareable.Code)

	// The MCP token the job's helper asks for is minted for the same
	// workload principal, so it resolves to the same assignment and agent.
	f.requireWorkloadSession(t, f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource), f.advertisedIssuer)
	require.Len(t, f.workloadSessions(t), 1)
}

// The rule covers the whole repository: branch, tag, environment and pull
// request jobs all sign in.
//
// The breadth cases below exchange at the project's MCP server, which reads
// the same organization-tier rule through the same admission query. Every
// test in this package shares one organization, and so the device-agent
// endpoint's per-organization lookup budget; spending it here would make the
// package's device-agent tests flaky.
func TestGitHubActionsRule_AdmitsEveryJobOfTheRepository(t *testing.T) {
	t.Parallel()

	f := newGitHubActionsGrantFixture(t, "acme", "payments-api")
	for _, job := range []string{"ref:refs/heads/main", "ref:refs/heads/feature/x", "ref:refs/tags/v1.2.3", "environment:production", "pull_request"} {
		f.subject = "repo:acme/payments-api:" + job
		f.requireWorkloadSession(t, f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource), f.advertisedIssuer)
	}
}

// Every github.com job is signed by the same issuer, so the rule must pin the
// repository: neighbors sharing a prefix, other owners, another casing, and
// the immutable form of the same repository are all refused.
func TestGitHubActionsRule_RefusesOtherRepositories(t *testing.T) {
	t.Parallel()

	f := newGitHubActionsGrantFixture(t, "acme", "payments-api")
	for _, subject := range []string{
		"repo:acme/payments-api-evil:pull_request",
		"repo:acme/payments:pull_request",
		"repo:acme-evil/payments-api:pull_request",
		"repo:other/payments-api:pull_request",
		"repo:Acme/payments-api:pull_request",
		"repo:acme@123/payments-api@456:pull_request",
	} {
		f.subject = subject
		requireWorkloadGrantRefused(t, f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource))
	}
}

// A repository with immutable subjects is pinned by its IDs: the same names
// with another ID, which a re-registered owner or repository would carry, are
// refused.
func TestGitHubActionsRule_ImmutableSubjectsPinTheIDs(t *testing.T) {
	t.Parallel()

	f := newGitHubActionsGrantFixture(t, "acme@123", "payments-api@456")
	f.subject = "repo:acme@123/payments-api@456:ref:refs/heads/main"
	f.requireWorkloadSession(t, f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource), f.advertisedIssuer)

	for _, subject := range []string{
		"repo:acme@123/payments-api@4567:ref:refs/heads/main",
		"repo:acme@999/payments-api@456:ref:refs/heads/main",
		"repo:acme/payments-api:ref:refs/heads/main",
	} {
		f.subject = subject
		requireWorkloadGrantRefused(t, f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource))
	}
}

// Why the setup writes the organization tier and warns about a project's own
// GitHub issuer: a project-tier issuer for the same URL takes precedence in
// that project, so the project's MCP servers resolve the job to a different
// workload principal, one this rule does not admit, while the device-agent
// endpoint, which reads the organization tier only, still accepts it. That
// is the split that left a job polling as one agent and calling MCP as
// another.
func TestGitHubActionsRule_ProjectIssuerShadowsTheRuleForThatProject(t *testing.T) {
	t.Parallel()

	f := newGitHubActionsGrantFixture(t, "acme", "payments-api")
	_, err := testrepo.New(f.ti.conn).CreateWorkloadIssuerFixture(f.ctx, testrepo.CreateWorkloadIssuerFixtureParams{
		OrganizationID: f.fx.orgID,
		ProjectID:      uuid.NullUUID{UUID: f.fx.target.ProjectID, Valid: true},
		Name:           "GitHub Actions (project)",
		Issuer:         f.issuer.OAuth21URL,
		JwksUri:        oauthtest.DiscoverWorkloadJWKSURI(t, f.issuer),
	})
	require.NoError(t, err)

	requireDeviceAgentToken(t, f.exchangeDeviceAgent(t, f.assertion(t, f.tokenURL)), f.fx.orgID, f.issuerID, f.subject)
	w := f.exchange(t, f.assertion(t, f.advertisedIssuer), f.resource)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	requireWorkloadGrantRefused(t, w)
}
