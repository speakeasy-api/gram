package platformmcp

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
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
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
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
	return NewAgentIdentityReadService(testenv.NewLogger(t), f.conn, engine, checks, allowBudget())
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
	unreached := seedAgentAccessServer(t, f.conn, other.ID, "Agent unreached server")
	readable := seedAgentAccessServer(t, f.conn, other.ID, "Agent read-grant server")
	passThrough := seedAgentAccessServer(t, f.conn, f.project.ID, "Agent pass-through server")

	fixtures := testrepo.New(f.conn)
	client, err := fixtures.CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{OrganizationID: f.principal.OrganizationID, ProjectID: uuid.NullUUID{UUID: f.project.ID, Valid: true}, ClientType: authz.AuthClientTypeRemoteSession, Name: "Each person's account"})
	require.NoError(t, err)
	require.NoError(t, fixtures.AssociateAuthClientFixture(ctx, testrepo.AssociateAuthClientFixtureParams{ProjectID: f.project.ID, OrganizationID: f.principal.OrganizationID, McpServerID: personal, AuthClientID: client}))
	forwarded, err := fixtures.CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{OrganizationID: f.principal.OrganizationID, ProjectID: uuid.NullUUID{UUID: f.project.ID, Valid: true}, ClientType: authz.AuthClientTypePassThrough, Name: "The caller's own token"})
	require.NoError(t, err)
	require.NoError(t, fixtures.AssociateAuthClientFixture(ctx, testrepo.AssociateAuthClientFixtureParams{ProjectID: f.project.ID, OrganizationID: f.principal.OrganizationID, McpServerID: passThrough, AuthClientID: forwarded}))

	f.grant(t, authz.ScopeMCPConnect, mcpConnectSelector(shared.String(), f.project.ID.String()))
	f.grant(t, authz.ScopeMCPConnect, mcpConnectSelector(authz.WildcardResource, f.project.ID.String()))
	f.grant(t, authz.ScopeMCPConnect, mcpConnectSelector(uuid.NewString(), ""))
	f.grant(t, authz.ScopeMCPRead, mcpConnectSelector(readable.String(), other.ID.String()))
	f.grant(t, authz.ScopeMCPAuth, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient, authz.SelectorKeyResourceID: authz.WildcardResource})

	callerCtx, principal := f.callerContext(t, f.principal.UserID)
	output, err := f.service(t, nil).MCPAccess(callerCtx, principal, AgentIdentityInput{AgentID: f.agentID.String()})
	require.ErrorIs(t, err, ErrUnavailable, "a service without the checks listing is unavailable")
	require.Empty(t, output.Servers)

	service := f.service(t, agentChecksFunc(nil))
	output, err = service.MCPAccess(callerCtx, principal, AgentIdentityInput{AgentID: f.agentID.String()})
	require.NoError(t, err)
	require.Equal(t, AgentIdentity{ID: f.agentID.String(), Name: "Inventory bot", Lifecycle: "active", OwnerReassignmentRequired: false}, output.Agent)
	require.Equal(t, 1, output.MissingServerGrants, "a grant naming a deleted server is counted, not listed")
	require.False(t, output.Truncated)

	byID := agentServersByID(output)
	require.NotContains(t, byID, unreached.String(), "a project-wide grant reaches only its own project")
	require.Contains(t, byID, readable.String(), "mcp:read reaches the one server it names, in another project")
	require.True(t, hasUseRule(byID[readable.String()].Rules, "agent", "resource"), "mcp:read includes connecting")
	require.Equal(t, other.ID.String(), byID[readable.String()].ProjectID)
	require.Equal(t, "Other project", byID[readable.String()].ProjectName)
	require.Equal(t, agentCredentialCallerProvided, byID[passThrough.String()].CredentialMode, "a pass-through server uses what the agent's client sends")
	require.Contains(t, byID, shared.String())
	require.Contains(t, byID, personal.String())
	require.Equal(t, "Agent shared server", byID[shared.String()].MCPName)
	require.Equal(t, "all_tools", byID[shared.String()].ConnectionAccess)
	require.True(t, hasUseRule(byID[shared.String()].Rules, "agent", "resource"), "the grant naming the server is reported")
	require.True(t, hasUseRule(byID[shared.String()].Rules, "agent", "project"), "so is the project-wide one")
	require.False(t, hasUseRule(byID[personal.String()].Rules, "agent", "resource"))
	require.True(t, hasUseRule(byID[personal.String()].Rules, "agent", "project"))
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

	ctx, principal := f.callerContext(t, f.principal.UserID, authz.NewGrant(authz.ScopeOrgRead, f.principal.OrganizationID))
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
	ctx, principal := f.callerContext(t, f.principal.UserID, authz.NewGrant(authz.ScopeOrgRead, f.principal.OrganizationID))

	_, err := forbidden.AuthorizationChecks(ctx, principal, AgentAuthorizationChecksInput{AgentID: f.agentID.String(), From: nil, To: nil})
	require.ErrorIs(t, err, ErrAgentChecksForbidden, "a checks listing that refuses the caller is reported as the dashboard reports it")
	require.Equal(t, 1, calls)

	stranger := "user_" + uuid.NewString()
	for _, tc := range []struct {
		name    string
		input   AgentAuthorizationChecksInput
		caller  string
		write   bool
		org     string
		wantErr error
	}{
		{name: "agent:write only", input: AgentAuthorizationChecksInput{AgentID: f.agentID.String(), From: nil, To: nil}, caller: stranger, write: true, wantErr: ErrAgentReadForbidden},
		{name: "malformed", input: AgentAuthorizationChecksInput{AgentID: f.agentID.String(), From: new("yesterday"), To: nil}, wantErr: ErrAgentChecksWindowInvalid},
		{name: "reversed", input: AgentAuthorizationChecksInput{AgentID: f.agentID.String(), From: new("2026-11-07T00:00:00Z"), To: new("2026-11-06T00:00:00Z")}, wantErr: ErrAgentChecksWindowInvalid},
		{name: "past retention", input: AgentAuthorizationChecksInput{AgentID: f.agentID.String(), From: new("2026-01-01T00:00:00Z"), To: new("2026-11-06T00:00:00Z")}, wantErr: ErrAgentChecksWindowInvalid},
		{name: "unknown agent", input: AgentAuthorizationChecksInput{AgentID: uuid.NewString(), From: nil, To: nil}, wantErr: ErrAgentNotAvailable},
		{name: "empty agent ID", input: AgentAuthorizationChecksInput{AgentID: "", From: nil, To: nil}, wantErr: ErrAgentNotAvailable},
		{name: "not agent:read", input: AgentAuthorizationChecksInput{AgentID: f.agentID.String(), From: nil, To: nil}, caller: stranger, wantErr: ErrAgentNotAvailable},
		{name: "another org", input: AgentAuthorizationChecksInput{AgentID: f.agentID.String(), From: nil, To: nil}, org: "org_" + uuid.NewString(), wantErr: ErrForbidden},
	} {
		callerCtx, caller := ctx, principal
		if tc.caller != "" {
			grants := []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, f.principal.OrganizationID)}
			if tc.write {
				grants = append(grants, authz.NewGrantWithSelector(authz.ScopeAgentWrite, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindAgent, authz.SelectorKeyResourceID: f.agentID.String()}))
			}
			callerCtx, caller = f.callerContext(t, tc.caller, grants...)
		}
		if tc.org != "" {
			caller.OrganizationID = tc.org
		}
		_, err := forbidden.AuthorizationChecks(callerCtx, caller, tc.input)
		require.ErrorIs(t, err, tc.wantErr, tc.name)
	}
	require.Equal(t, 1, calls, "refused requests never list checks")

	// The owner without org:read is refused for that, whatever the window, so
	// a window error never answers a caller who may not list checks.
	ownerCtx, owner := f.callerContext(t, f.principal.UserID)
	for name, input := range map[string]AgentAuthorizationChecksInput{
		"default":   {AgentID: f.agentID.String(), From: nil, To: nil},
		"malformed": {AgentID: f.agentID.String(), From: new("yesterday"), To: nil},
		"reversed":  {AgentID: f.agentID.String(), From: new("2026-11-07T00:00:00Z"), To: new("2026-11-06T00:00:00Z")},
	} {
		_, err := forbidden.AuthorizationChecks(ownerCtx, owner, input)
		require.ErrorIs(t, err, ErrAgentChecksForbidden, name)
	}
	require.Equal(t, 1, calls, "a caller without org:read never lists checks")
}

func TestAgentMCPAccessCountsRoleGrants(t *testing.T) {
	t.Parallel()

	f := newAgentIdentityFixture(t, "platform_mcp_agent_access_roles")
	ctx := t.Context()
	server := seedAgentAccessServer(t, f.conn, f.project.ID, "Agent role server")
	role := seedAccessRole(t, ctx, f.conn, f.principal.OrganizationID, "agent-operators", "Agent operators")
	selector, err := json.Marshal(mcpConnectSelector(server.String(), f.project.ID.String()))
	require.NoError(t, err)
	_, err = accessrepo.New(f.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: f.principal.OrganizationID, PrincipalUrn: role, Scope: string(authz.ScopeMCPConnect), Selectors: selector})
	require.NoError(t, err)
	_, err = accessrepo.New(f.conn).UpsertAgentRoleAssignment(ctx, accessrepo.UpsertAgentRoleAssignmentParams{OrganizationID: f.principal.OrganizationID, RoleUrn: role.String(), AgentID: f.agentID})
	require.NoError(t, err)

	callerCtx, principal := f.callerContext(t, f.principal.UserID)
	output, err := f.service(t, agentChecksFunc(nil)).MCPAccess(callerCtx, principal, AgentIdentityInput{AgentID: f.agentID.String()})
	require.NoError(t, err)
	byID := agentServersByID(output)
	require.Contains(t, byID, server.String(), "an agent that reaches a server only through a role lists it")
	require.True(t, slices.ContainsFunc(byID[server.String()].Rules, func(rule access.MCPAccessRuleSummary) bool {
		return rule.AccessLevel == "use" && rule.SourceKind == "role" && rule.SourceName == "Agent operators" && rule.AppliesTo == "resource"
	}), "the rule names the role")
	require.Zero(t, output.MissingServerGrants, "a role's grants are not the agent's own")
}

func TestAgentMCPAccessListsNothingForAnAgentThatCannotSignIn(t *testing.T) {
	t.Parallel()

	f := newAgentIdentityFixture(t, "platform_mcp_agent_access_suspended")
	server := seedAgentAccessServer(t, f.conn, f.project.ID, "Agent suspended server")
	f.grant(t, authz.ScopeMCPConnect, mcpConnectSelector(server.String(), f.project.ID.String()))
	service := f.service(t, agentChecksFunc(nil))
	callerCtx, principal := f.callerContext(t, f.principal.UserID)

	output, err := service.MCPAccess(callerCtx, principal, AgentIdentityInput{AgentID: f.agentID.String()})
	require.NoError(t, err)
	require.Contains(t, agentServersByID(output), server.String())

	_, err = agentsrepo.New(f.conn).SuspendAgent(t.Context(), agentsrepo.SuspendAgentParams{OrganizationID: f.principal.OrganizationID, ID: f.agentID})
	require.NoError(t, err)
	output, err = service.MCPAccess(callerCtx, principal, AgentIdentityInput{AgentID: f.agentID.String()})
	require.NoError(t, err)
	require.Equal(t, "suspended", output.Agent.Lifecycle)
	require.False(t, output.Agent.OwnerReassignmentRequired)
	require.Empty(t, output.Servers, "a suspended agent's keys are refused, so it reaches no server")
	require.Zero(t, output.MissingServerGrants)
}

func TestAgentMCPAccessHonorsBlocks(t *testing.T) {
	t.Parallel()

	f := newAgentIdentityFixture(t, "platform_mcp_agent_access_blocks")
	open := seedAgentAccessServer(t, f.conn, f.project.ID, "Agent open server")
	blocked := seedAgentAccessServer(t, f.conn, f.project.ID, "Agent blocked server")
	disabled := seedAgentAccessServer(t, f.conn, f.project.ID, "Agent disabled server")
	require.NoError(t, testrepo.New(f.conn).SetMCPServerVisibilityFixture(t.Context(), testrepo.SetMCPServerVisibilityFixtureParams{Visibility: "disabled", ID: disabled, ProjectID: f.project.ID}))
	f.grant(t, authz.ScopeMCPConnect, mcpConnectSelector(authz.WildcardResource, f.project.ID.String()))
	f.grant(t, authz.ScopeMCPBlockedConnect, mcpConnectSelector(blocked.String(), f.project.ID.String()))

	callerCtx, principal := f.callerContext(t, f.principal.UserID)
	output, err := f.service(t, agentChecksFunc(nil)).MCPAccess(callerCtx, principal, AgentIdentityInput{AgentID: f.agentID.String()})
	require.NoError(t, err)
	byID := agentServersByID(output)
	require.Contains(t, byID, open.String())
	require.NotContains(t, byID, blocked.String(), "an mcp:blocked_connect grant takes the server away")
	require.NotContains(t, byID, disabled.String(), "no route serves a disabled server, whatever the grants")
}

func TestAgentMCPAccessMatchesServerAndToolsetIDs(t *testing.T) {
	t.Parallel()

	f := newAgentIdentityFixture(t, "platform_mcp_agent_access_wrappers")
	ctx := t.Context()
	seedToolsetServer := func(toolsetID uuid.UUID, id uuid.UUID, name string) {
		_, err := mcpserversrepo.New(f.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
			ID: id, ProjectID: f.project.ID, Name: conv.ToPGText(name), Slug: conv.ToPGText("agent-wrapper-" + uuid.NewString()[:8]),
			ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, Visibility: "private",
		})
		require.NoError(t, err)
	}
	seedToolset := func(name string) uuid.UUID {
		toolset, err := toolsetsrepo.New(f.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{OrganizationID: f.principal.OrganizationID, ProjectID: f.project.ID, Name: name, Slug: "agent-toolset-" + uuid.NewString()[:8]})
		require.NoError(t, err)
		return toolset.ID
	}

	// One toolset served by its canonical server and a second wrapper.
	shared := seedToolset("Agent shared toolset")
	seedToolsetServer(shared, shared, "Agent canonical server")
	wrapper := uuid.New()
	seedToolsetServer(shared, wrapper, "Agent wrapper server")
	// Another toolset served only by a wrapper with its own ID.
	legacy := seedToolset("Agent legacy toolset")
	legacyWrapper := uuid.New()
	seedToolsetServer(legacy, legacyWrapper, "Agent legacy wrapper")

	f.grant(t, authz.ScopeMCPConnect, mcpConnectSelector(wrapper.String(), f.project.ID.String()))
	f.grant(t, authz.ScopeMCPConnect, mcpConnectSelector(legacy.String(), f.project.ID.String()))

	callerCtx, principal := f.callerContext(t, f.principal.UserID, authz.NewGrant(authz.ScopeOrgRead, f.principal.OrganizationID))
	service := f.service(t, agentChecksFunc(func(_ context.Context, payload *accessgen.ListChallengesPayload) (*accessgen.ListChallengesResult, error) {
		if *payload.Outcome == "allow" {
			return &accessgen.ListChallengesResult{Total: 0, Challenges: []*accessgen.AuthzChallenge{}}, nil
		}
		check := func(id, resourceID string) *accessgen.AuthzChallenge {
			return &accessgen.AuthzChallenge{ID: id, Timestamp: "2026-11-07T12:00:00Z", Scope: "mcp:connect", ResourceKind: new(authz.ResourceKindMCP), ResourceID: &resourceID, Reason: "no_grants", PrincipalUrn: "agent:" + f.agentID.String()}
		}
		return &accessgen.ListChallengesResult{Total: 3, Challenges: []*accessgen.AuthzChallenge{check("d1", wrapper.String()), check("d2", shared.String()), check("d3", legacy.String())}}, nil
	}))

	output, err := service.MCPAccess(callerCtx, principal, AgentIdentityInput{AgentID: f.agentID.String()})
	require.NoError(t, err)
	byID := agentServersByID(output)
	require.Contains(t, byID, wrapper.String(), "a grant on a wrapper's own server ID reaches that wrapper")
	require.Equal(t, "Agent wrapper server", byID[wrapper.String()].MCPName)
	require.NotContains(t, byID, shared.String(), "and not the toolset's other server")
	require.Contains(t, byID, legacyWrapper.String(), "a grant naming the toolset ID reaches the server that serves it")
	require.True(t, hasUseRule(byID[legacyWrapper.String()].Rules, "agent", "resource"))
	require.Zero(t, output.MissingServerGrants, "neither grant names a missing server")

	checks, err := service.AuthorizationChecks(callerCtx, principal, AgentAuthorizationChecksInput{AgentID: f.agentID.String(), From: new("2026-11-01T00:00:00Z"), To: new("2026-11-08T00:00:00Z")})
	require.NoError(t, err)
	names := make(map[string]string, len(checks.Denied.Groups))
	for _, group := range checks.Denied.Groups {
		names[group.ResourceID] = group.ResourceName
	}
	require.Equal(t, map[string]string{
		wrapper.String(): "Agent wrapper server",
		shared.String():  "Agent canonical server",
		legacy.String():  "Agent legacy wrapper",
	}, names, "checks on a server's own ID and on a toolset ID are both named")
}

func agentServersByID(output AgentMCPAccessOutput) map[string]AgentMCPServerAccess {
	byID := make(map[string]AgentMCPServerAccess, len(output.Servers))
	for _, server := range output.Servers {
		byID[server.MCPID] = server
	}
	return byID
}

// hasUseRule reports whether an allowing connect rule from sourceKind covers
// appliesTo.
func hasUseRule(rules []access.MCPAccessRuleSummary, sourceKind, appliesTo string) bool {
	return slices.ContainsFunc(rules, func(rule access.MCPAccessRuleSummary) bool {
		return rule.AccessLevel == "use" && rule.SourceKind == sourceKind && rule.AppliesTo == appliesTo && rule.Effect == string(authz.GrantEffectAllows)
	})
}
