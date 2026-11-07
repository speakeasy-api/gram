package platformmcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type agentChecksFunc func(context.Context, *accessgen.ListChallengesPayload) (*accessgen.ListChallengesResult, error)

func (f agentChecksFunc) ListChallenges(ctx context.Context, payload *accessgen.ListChallengesPayload) (*accessgen.ListChallengesResult, error) {
	return f(ctx, payload)
}

type agentIdentityFixture struct {
	conn      *pgxpool.Pool
	principal Principal
	project   ResolvedProject
	agentID   uuid.UUID
}

// newAgentIdentityFixture seeds an organization, a project, and an agent the
// principal owns.
func newAgentIdentityFixture(t *testing.T, name string) agentIdentityFixture {
	t.Helper()

	conn, err := platformMCPInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, t.Context(), conn)
	seedAccessMember(t, t.Context(), conn, principal.OrganizationID, principal.UserID, "agent-owner@example.test")
	agent, err := agentsrepo.New(conn).CreateAgent(t.Context(), agentsrepo.CreateAgentParams{OrganizationID: principal.OrganizationID, OwnerUserID: principal.UserID, ProjectID: uuid.NullUUID{}, Name: "Inventory bot"})
	require.NoError(t, err)
	return agentIdentityFixture{conn: conn, principal: principal, project: project, agentID: agent.ID}
}

// callerContext is a Platform MCP call by userID in the fixture's organization
// holding grants.
func (f agentIdentityFixture) callerContext(t *testing.T, userID string, grants ...authz.Grant) (context.Context, Principal) {
	t.Helper()

	principal := f.principal
	principal.UserID = userID
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: userID})
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	ctx = authz.GrantsToContext(ctx, grants)
	return ctx, principal
}

func (f agentIdentityFixture) service(t *testing.T, checks AgentAuthorizationChecks) *AgentIdentityReadService {
	t.Helper()

	engine := authz.NewEngine(testenv.NewLogger(t), f.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	return NewAgentIdentityReadService(f.conn, engine, checks, allowBudget())
}

func (f agentIdentityFixture) grant(t *testing.T, scope authz.Scope, selector authz.Selector) {
	t.Helper()

	raw, err := json.Marshal(selector)
	require.NoError(t, err)
	_, err = agentsrepo.New(f.conn).CreateAgentPolicyGrant(t.Context(), agentsrepo.CreateAgentPolicyGrantParams{OrganizationID: f.principal.OrganizationID, AgentID: f.agentID, Scope: string(scope), Selectors: raw})
	require.NoError(t, err)
}

// seedAgentAccessServer adds a remote MCP server whose people sign in through
// their own user session issuer.
func seedAgentAccessServer(t *testing.T, conn *pgxpool.Pool, projectID uuid.UUID, name string) uuid.UUID {
	t.Helper()

	ctx := t.Context()
	issuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		Slug:               "agent-issuer-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	remote, err := remotemcprepo.New(conn).CreateServer(ctx, remotemcprepo.CreateServerParams{ID: uuid.New(), ProjectID: projectID, TransportType: "streamable-http", Url: "https://agent-access.example.test/mcp"})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(name),
		Slug:                conv.ToPGText("agent-access-" + uuid.NewString()[:8]),
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: remote.ID, Valid: true},
		Visibility:          "private",
	})
	require.NoError(t, err)
	return server.ID
}

func mcpConnectSelector(resourceID, projectID string) authz.Selector {
	selector := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: resourceID}
	if projectID != "" {
		selector[authz.SelectorKeyProjectID] = projectID
	}
	return selector
}

func TestAgentMCPAccessListsGrantedServersAndCredentialModes(t *testing.T) {
	t.Parallel()

	f := newAgentIdentityFixture(t, "platform_mcp_agent_access")
	ctx := t.Context()
	shared := seedAgentAccessServer(t, f.conn, f.project.ID, "Agent shared server")
	personal := seedAgentAccessServer(t, f.conn, f.project.ID, "Agent personal server")
	other, err := projectsrepo.New(f.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: f.principal.OrganizationID})
	require.NoError(t, err)
	seedAgentAccessServer(t, f.conn, other.ID, "Agent unreached server")

	fixtures := testrepo.New(f.conn)
	client, err := fixtures.CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{OrganizationID: f.principal.OrganizationID, ProjectID: uuid.NullUUID{UUID: f.project.ID, Valid: true}, ClientType: authz.AuthClientTypeRemoteSession, Name: "Each person's account"})
	require.NoError(t, err)
	require.NoError(t, fixtures.AssociateAuthClientFixture(ctx, testrepo.AssociateAuthClientFixtureParams{ProjectID: f.project.ID, OrganizationID: f.principal.OrganizationID, McpServerID: personal, AuthClientID: client}))

	f.grant(t, authz.ScopeMCPConnect, mcpConnectSelector(shared.String(), f.project.ID.String()))
	f.grant(t, authz.ScopeMCPConnect, mcpConnectSelector(authz.WildcardResource, f.project.ID.String()))
	f.grant(t, authz.ScopeMCPConnect, mcpConnectSelector(uuid.NewString(), ""))
	f.grant(t, authz.ScopeMCPAuth, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient, authz.SelectorKeyResourceID: authz.WildcardResource})

	callerCtx, principal := f.callerContext(t, f.principal.UserID)
	output, err := f.service(t, nil).MCPAccess(callerCtx, principal, AgentIdentityInput{AgentID: f.agentID.String()})
	require.ErrorIs(t, err, ErrUnavailable, "a service without the checks listing is unavailable")
	require.Empty(t, output.Servers)

	service := f.service(t, agentChecksFunc(nil))
	output, err = service.MCPAccess(callerCtx, principal, AgentIdentityInput{AgentID: f.agentID.String()})
	require.NoError(t, err)
	require.Equal(t, AgentIdentity{ID: f.agentID.String(), Name: "Inventory bot", Lifecycle: "active"}, output.Agent)
	require.Equal(t, 1, output.MissingServerGrants, "a grant naming a deleted server is counted, not listed")
	require.False(t, output.Truncated)

	byID := make(map[string]AgentMCPServerAccess, len(output.Servers))
	for _, server := range output.Servers {
		require.Equal(t, f.project.ID.String(), server.ProjectID, "a project-wide grant reaches only its own project")
		byID[server.ResourceID] = server
	}
	require.Contains(t, byID, shared.String())
	require.Contains(t, byID, personal.String())
	require.Equal(t, agentGrantServer, byID[shared.String()].Grant, "the narrowest grant is reported")
	require.Equal(t, agentGrantProject, byID[personal.String()].Grant)
	require.Equal(t, agentCredentialShared, byID[shared.String()].CredentialMode, "a server that asks no person to sign in uses a shared credential")
	require.Equal(t, agentCredentialNotAssigned, byID[personal.String()].CredentialMode, "a personal server with no connection assigned acts for no one")
	require.Empty(t, byID[personal.String()].OnBehalfOf)
}

func TestAgentMCPAccessUsesTheDashboardAgentPermission(t *testing.T) {
	t.Parallel()

	f := newAgentIdentityFixture(t, "platform_mcp_agent_access_rbac")
	service := f.service(t, agentChecksFunc(nil))
	input := AgentIdentityInput{AgentID: f.agentID.String()}
	agentResource := func(scope authz.Scope) authz.Grant {
		return authz.NewGrantWithSelector(scope, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindAgent, authz.SelectorKeyResourceID: f.agentID.String()})
	}

	for name, grants := range map[string][]authz.Grant{
		"no grants":       nil,
		"agent:read only": {agentResource(authz.ScopeAgentRead)},
		"another agent":   {authz.NewGrantWithSelector(authz.ScopeAgentWrite, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindAgent, authz.SelectorKeyResourceID: uuid.NewString()})},
	} {
		ctx, principal := f.callerContext(t, "user_"+uuid.NewString(), grants...)
		_, err := service.MCPAccess(ctx, principal, input)
		require.ErrorIs(t, err, ErrAgentNotAvailable, name)
	}

	ctx, principal := f.callerContext(t, "user_"+uuid.NewString(), agentResource(authz.ScopeAgentWrite))
	_, err := service.MCPAccess(ctx, principal, input)
	require.NoError(t, err, "agent:write on the agent reads it, as the Permissions tab does")

	ctx, principal = f.callerContext(t, f.principal.UserID)
	for _, missing := range []string{uuid.NewString(), "not-a-uuid", ""} {
		_, err := service.MCPAccess(ctx, principal, AgentIdentityInput{AgentID: missing})
		require.ErrorIs(t, err, ErrAgentNotAvailable, "an unknown agent reads like one the caller may not see")
	}

	mismatched := principal
	mismatched.UserID = "user_" + uuid.NewString()
	_, err = service.MCPAccess(ctx, mismatched, input)
	require.ErrorIs(t, err, ErrForbidden, "the call must act as the authenticated caller")
}

func TestAgentAuthorizationChecksListsDenialsFirst(t *testing.T) {
	t.Parallel()

	f := newAgentIdentityFixture(t, "platform_mcp_agent_checks")
	server := seedAgentAccessServer(t, f.conn, f.project.ID, "Agent checked server")
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	check := func(id, scope, kind, resourceID, reason string, minute int) *accessgen.AuthzChallenge {
		return &accessgen.AuthzChallenge{ID: id, Timestamp: now.Add(-time.Duration(minute) * time.Minute).Format(time.RFC3339), Scope: scope, ResourceKind: &kind, ResourceID: &resourceID, Reason: reason, PrincipalUrn: "agent:" + f.agentID.String(), UserEmail: new("private@example.test")}
	}

	var payloads []*accessgen.ListChallengesPayload
	service := f.service(t, agentChecksFunc(func(_ context.Context, payload *accessgen.ListChallengesPayload) (*accessgen.ListChallengesResult, error) {
		payloads = append(payloads, payload)
		if *payload.Outcome == "deny" {
			return &accessgen.ListChallengesResult{Total: 5, Challenges: []*accessgen.AuthzChallenge{
				check("d1", "mcp:connect", "mcp", server.String(), "no_grants", 1),
				check("d2", "mcp:connect", "mcp", server.String(), "no_grants", 5),
				check("d3", "project:read", "project", f.project.ID.String(), "scope_unsatisfied", 9),
			}}, nil
		}
		return &accessgen.ListChallengesResult{Total: 1, Challenges: []*accessgen.AuthzChallenge{check("a1", "mcp:connect", "mcp", server.String(), "grant_matched", 2)}}, nil
	}))
	service.now = func() time.Time { return now }

	ctx, principal := f.callerContext(t, f.principal.UserID)
	output, err := service.AuthorizationChecks(ctx, principal, AgentAuthorizationChecksInput{AgentID: f.agentID.String(), From: nil, To: nil})
	require.NoError(t, err)

	require.Len(t, payloads, 2)
	for i, outcome := range []string{"deny", "allow"} {
		payload := payloads[i]
		require.Equal(t, outcome, *payload.Outcome, "denied checks are read first")
		require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()).String(), *payload.PrincipalUrn)
		require.Equal(t, []string{"org:hooks_ingest"}, payload.ExcludeScopes)
		require.Equal(t, maxAgentChecksPerOutcome, payload.Limit)
		require.Equal(t, "2026-10-31T12:00:00Z", *payload.From, "the period defaults to the week before now")
		require.Equal(t, "2026-11-07T12:00:00Z", *payload.To)
	}

	require.Equal(t, []string{"org:hooks_ingest"}, output.HiddenScopes)
	require.Equal(t, 5, output.Denied.Total)
	require.True(t, output.Denied.Truncated, "the total covers checks past the grouped page")
	require.Equal(t, []AgentCheckGroup{
		{Scope: "mcp:connect", ResourceKind: "mcp", ResourceID: server.String(), ResourceName: "Agent checked server", Count: 2, LastAt: "2026-11-07T11:59:00Z", Reason: "no_grants"},
		{Scope: "project:read", ResourceKind: "project", ResourceID: f.project.ID.String(), ResourceName: "", Count: 1, LastAt: "2026-11-07T11:51:00Z", Reason: "scope_unsatisfied"},
	}, output.Denied.Groups)
	require.Equal(t, AgentCheckOutcome{Total: 1, Truncated: false, Groups: []AgentCheckGroup{
		{Scope: "mcp:connect", ResourceKind: "mcp", ResourceID: server.String(), ResourceName: "Agent checked server", Count: 1, LastAt: "2026-11-07T11:58:00Z", Reason: "grant_matched"},
	}}, output.Allowed)

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private@example.test", "checks never carry a person's email")
}

func TestAgentAuthorizationChecksRefusals(t *testing.T) {
	t.Parallel()

	f := newAgentIdentityFixture(t, "platform_mcp_agent_checks_refusals")
	calls := 0
	forbidden := f.service(t, agentChecksFunc(func(context.Context, *accessgen.ListChallengesPayload) (*accessgen.ListChallengesResult, error) {
		calls++
		return nil, oops.C(oops.CodeForbidden)
	}))
	ctx, principal := f.callerContext(t, f.principal.UserID)

	_, err := forbidden.AuthorizationChecks(ctx, principal, AgentAuthorizationChecksInput{AgentID: f.agentID.String(), From: nil, To: nil})
	require.ErrorIs(t, err, ErrAgentChecksForbidden, "listing checks without org:read is refused as the dashboard refuses it")
	require.Equal(t, 1, calls)

	for name, input := range map[string]AgentAuthorizationChecksInput{
		"malformed":       {AgentID: f.agentID.String(), From: new("yesterday"), To: nil},
		"reversed":        {AgentID: f.agentID.String(), From: new("2026-11-07T00:00:00Z"), To: new("2026-11-06T00:00:00Z")},
		"past retention":  {AgentID: f.agentID.String(), From: new("2026-01-01T00:00:00Z"), To: new("2026-11-06T00:00:00Z")},
		"unknown agent":   {AgentID: uuid.NewString(), From: nil, To: nil},
		"not agent:read":  {AgentID: f.agentID.String(), From: nil, To: nil},
		"empty agent ID":  {AgentID: "", From: nil, To: nil},
		"another org ctx": {AgentID: f.agentID.String(), From: nil, To: nil},
	} {
		callerCtx, caller := ctx, principal
		switch name {
		case "not agent:read":
			callerCtx, caller = f.callerContext(t, "user_"+uuid.NewString(), authz.NewGrant(authz.ScopeOrgRead, f.principal.OrganizationID))
		case "another org ctx":
			caller.OrganizationID = "org_" + uuid.NewString()
		}
		_, err := forbidden.AuthorizationChecks(callerCtx, caller, input)
		require.Error(t, err, name)
	}
	require.Equal(t, 1, calls, "refused requests never list checks")
}
