package mcpconnectionhealth_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func (ti *testInstance) listProject(t *testing.T, ctx context.Context, limit *int) *gen.ListProjectSuspectedProblemsResult {
	t.Helper()
	res, err := ti.service.ListProjectSuspectedProblems(ctx, &gen.ListProjectSuspectedProblemsPayload{Limit: limit})
	require.NoError(t, err)
	return res
}

// mcpServer creates an MCP server in project proxying url, on the MCP sign-in issuer.
func (ti *testInstance) mcpServer(t *testing.T, project, issuer uuid.UUID, slug, url string) uuid.UUID {
	t.Helper()
	q := testrepo.New(ti.conn)
	remote, server := uuid.New(), uuid.New()
	require.NoError(t, q.CreateRemoteMCPBackendFixture(t.Context(), testrepo.CreateRemoteMCPBackendFixtureParams{ID: remote, ProjectID: project, Url: url}))
	require.NoError(t, q.CreateRemoteBackedMCPServerFixture(t.Context(), testrepo.CreateRemoteBackedMCPServerFixtureParams{
		ID: server, ProjectID: project, Slug: conv.ToPGText(slug), RemoteMcpServerID: uuid.NullUUID{UUID: remote, Valid: true}, Visibility: "private",
	}))
	n, err := repo.New(ti.conn).SetServerIssuerFixture(t.Context(), repo.SetServerIssuerFixtureParams{IssuerID: uuid.NullUUID{UUID: issuer, Valid: true}, ID: server, ProjectID: project})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	return server
}

// otherProject creates another project in the organization with its own MCP sign-in.
func (ti *testInstance) otherProject(t *testing.T) (uuid.UUID, uuid.UUID) {
	t.Helper()
	project, err := testrepo.New(ti.conn).CreateProjectFixture(t.Context(), testrepo.CreateProjectFixtureParams{ID: uuid.New(), Name: "Other", Slug: "other-" + uuid.NewString()[:8], OrganizationID: ti.org})
	require.NoError(t, err)
	issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(t.Context(), usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          project,
		OrganizationID:     pgtype.Text{String: ti.org, Valid: true},
		Slug:               "other-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: 0, Days: 14, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	return project, issuer.ID
}

// asProject is the same caller reading another project with scopes there.
func asProject(t *testing.T, ctx context.Context, project uuid.UUID, scopes ...authz.Scope) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	clone := *authCtx
	clone.ProjectID = &project
	return withProjectGrants(t, contextvalues.SetAuthContext(ctx, &clone), scopes...)
}

// failDirect writes a failed request by a new person on an MCP server in project, ago before now.
func (ti *testInstance) failDirect(t *testing.T, project, server uuid.UUID, ago time.Duration, code mcpconnectionattempts.Code) urn.SessionSubject {
	t.Helper()
	p := urn.NewUserSubject("user-" + uuid.NewString())
	ti.attemptAs(t, mcpconnectionattempts.ServerScope(ti.org, project, server, mcpconnectionattempts.AppDCR), p, ago, mcpconnectionattempts.StepMCPRequest, code)
	return p
}

func projectIDs(ps []*gen.ProjectSuspectedProblem) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.ID
	}
	return out
}

func signInIDs(ps []*gen.SuspectedProblem) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.ID
	}
	return out
}

func TestListProjectSuspectedProblems_NotFoundWhenTheFlagIsOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.flags.targetOrganizations()

	_, err := ti.service.ListProjectSuspectedProblems(ctx, &gen.ListProjectSuspectedProblemsPayload{Limit: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListProjectSuspectedProblems_WithoutLogReadIsEmpty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	require.Len(t, ti.listProject(t, withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead), nil).Problems, 1)

	res := ti.listProject(t, withProjectGrants(t, ctx, authz.ScopeProjectRead), nil)
	require.True(t, res.LoggingEnabled)
	require.Equal(t, gen.ConnectionViewerScope("own"), res.ViewerScope)
	require.Empty(t, res.Problems)
	require.Zero(t, res.ProblemsTotal)
}

func TestListProjectSuspectedProblems_SaysLoggingIsOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	ti.logsOn.Store(false)

	res := ti.listProject(t, ctx, nil)
	require.False(t, res.LoggingEnabled)
	require.Empty(t, res.Problems)
	require.Zero(t, res.ProblemsTotal)
}

// The order is the MCP sign-in's: what each person fixes last. The limit cuts the list, not the total.
func TestListProjectSuspectedProblems_KeepsTheSignInOrderAndCapsAfterIt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	for range 7 {
		ti.attemptAs(t, mcpconnectionattempts.GatewayScope(ti.org, ti.project, ti.server, mcpconnectionattempts.AppDCR), urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeInvalidRemoteSession)
	}
	me := urn.NewUserSubject(ti.userID)
	ti.failOn(t, ti.org, ti.project, ti.server, me, 30*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	ti.failOn(t, ti.org, ti.project, ti.server, me, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)

	signIn := ti.listProblems(t, ctx, nil, nil)
	all := ti.listProject(t, ctx, nil)
	require.Equal(t, signInIDs(signIn.Problems), projectIDs(all.Problems))
	require.Equal(t, int64(2), all.ProblemsTotal)
	require.Equal(t, new(gen.SuspectedProblemWhoFixes("person")), all.Problems[1].WhoFixes)
	for i, p := range all.Problems {
		require.Equal(t, signIn.Problems[i].Headline, p.Headline)
		require.Equal(t, signIn.Problems[i].PeopleCount, p.PeopleCount)
	}

	one := ti.listProject(t, ctx, new(1))
	require.Equal(t, signInIDs(signIn.Problems[:1]), projectIDs(one.Problems))
	require.Equal(t, int64(2), one.ProblemsTotal, "the total is before the limit")
}

// A service failing in two projects shows in each with that project's people only.
func TestListProjectSuspectedProblems_CountsOnlyThisProjectsPeople(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	here := withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	otherProject, otherIssuer := ti.otherProject(t)
	there := asProject(t, ctx, otherProject, authz.ScopeProjectRead, authz.ScopeLogRead)
	down := mcpconnectionattempts.CodeProviderServerError
	for project, people := range map[uuid.UUID]int{ti.project: 2, otherProject: 3} {
		issuer := ti.issuer
		if project == otherProject {
			issuer = otherIssuer
		}
		a := ti.mcpServer(t, project, issuer, "docs-"+uuid.NewString()[:8], "https://mcp.example.com/a")
		b := ti.mcpServer(t, project, issuer, "wiki-"+uuid.NewString()[:8], "https://mcp.example.com/b")
		for i := range people {
			ti.failDirect(t, project, []uuid.UUID{a, b}[i%2], time.Hour, down)
		}
	}

	mine, theirs := ti.listProject(t, here, nil).Problems, ti.listProject(t, there, nil).Problems
	require.Len(t, mine, 1)
	require.Len(t, theirs, 1)
	require.Equal(t, mine[0].ID, theirs[0].ID, "the same service in the same way")
	require.Equal(t, gen.SuspectedProblemRule("same_service"), mine[0].Rule)
	require.Equal(t, int64(2), mine[0].PeopleCount)
	require.Equal(t, "2 people can't connect to mcp.example.com", mine[0].Headline)
	require.Equal(t, int64(3), theirs[0].PeopleCount)
	require.Equal(t, "3 people can't connect to mcp.example.com", theirs[0].Headline)
	require.Equal(t, ti.issuer.String(), *mine[0].UserSessionIssuerID)
	require.Equal(t, otherIssuer.String(), *theirs[0].UserSessionIssuerID)
}

// The organization gateway's view of the project is the project's; the gateway for the whole organization is no project's.
func TestListProjectSuspectedProblems_ListsTheOrganizationGatewaysProjectView(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	orgIssuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(t.Context(), usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:     conv.ToPGText(ti.org),
		Slug:               "org-gateway-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	projectView := orggateway.ProjectGatewayID(ti.org, ti.project)
	ti.failPeople(t, projectView, 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	for range 3 {
		ti.attemptAs(t, mcpconnectionattempts.OrganizationGatewayScope(ti.org, orggateway.GatewayID(ti.org), mcpconnectionattempts.AppDCR), urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStaticKeyRejected)
	}

	res := ti.listProject(t, ctx, nil)
	require.Len(t, res.Problems, 1)
	p := res.Problems[0]
	require.Equal(t, int64(2), p.PeopleCount, "only the people on the project's view")
	require.Equal(t, orgIssuer.ID.String(), *p.UserSessionIssuerID)
	require.Nil(t, p.Server)
	require.Equal(t, []string{p.ID}, signInIDs(ti.listFor(t, ctx, orgIssuer.ID)), "the organization's MCP sign-in lists it in this project")
}

func TestListProjectSuspectedProblems_NeverShowsAnotherProjectOrOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	otherProject, _ := ti.otherProject(t)
	for range 2 {
		ti.failOn(t, ti.org, otherProject, ti.server, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
		ti.failOn(t, "org-"+uuid.NewString(), ti.project, ti.server, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	}
	ti.failPeople(t, ti.server, 1, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)

	res := ti.listProject(t, ctx, nil)
	require.Empty(t, res.Problems, "one person here, the rest elsewhere")
	require.Zero(t, res.ProblemsTotal)
	require.Len(t, ti.listProject(t, asProject(t, ctx, otherProject, authz.ScopeProjectRead, authz.ScopeLogRead), nil).Problems, 1, "the other project has its own")
}

// Each rule links to an MCP sign-in, and to an MCP server reached directly, whose pages list the same problem.
func TestListProjectSuspectedProblems_LinksToPagesThatListIt(t *testing.T) {
	t.Parallel()
	type setup struct {
		issuer uuid.UUID
		server uuid.UUID
	}
	for _, tc := range []struct {
		name string
		rule string
		make func(t *testing.T, ti *testInstance) setup
	}{
		{"same server", "same_server", func(t *testing.T, ti *testInstance) setup {
			t.Helper()
			issuer, _ := ti.provider(t, "Notion")
			server := ti.mcpServer(t, ti.project, issuer, "notion", "https://notion.example.com/mcp")
			ti.failDirect(t, ti.project, server, 2*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
			ti.failDirect(t, ti.project, server, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
			return setup{issuer: issuer, server: server}
		}},
		{"same service", "same_service", func(t *testing.T, ti *testInstance) setup {
			t.Helper()
			first := ti.mcpServer(t, ti.project, ti.issuer, "docs", "https://mcp.example.com/a")
			second := ti.mcpServer(t, ti.project, ti.issuer, "wiki", "https://mcp.example.com/b")
			ti.failDirect(t, ti.project, first, 2*time.Hour, mcpconnectionattempts.CodeProviderServerError)
			ti.failDirect(t, ti.project, second, time.Hour, mcpconnectionattempts.CodeProviderServerError)
			return setup{issuer: ti.issuer, server: first}
		}},
		{"same app version on a gateway", "same_app_version", func(t *testing.T, ti *testInstance) setup {
			t.Helper()
			for range 2 {
				ti.failWith(t, ti.server, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, time.Second, mcpconnectionattempts.CodeInvalidBearerToken, mcpconnectionattempts.FamilyCursor, "2.0.1")
			}
			ti.answeredFor(t, ti.server, mcpconnectionattempts.FamilyClaudeCode, 30*time.Minute)
			return setup{issuer: ti.issuer, server: uuid.Nil}
		}},
		{"one person at an authorization server", "one_person", func(t *testing.T, ti *testInstance) setup {
			t.Helper()
			issuer, _ := ti.provider(t, "Notion")
			p := urn.NewUserSubject("user-" + uuid.NewString())
			for _, ago := range []time.Duration{30 * time.Hour, time.Hour} {
				ti.attemptAs(t, mcpconnectionattempts.AuthorizationServerScope(ti.org, ti.project, issuer, mcpconnectionattempts.AppDCR), p, ago, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodePKCEMismatch)
			}
			return setup{issuer: issuer, server: uuid.Nil}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
			want := tc.make(t, ti)

			res := ti.listProject(t, ctx, nil)
			require.Len(t, res.Problems, 1)
			p := res.Problems[0]
			require.Equal(t, gen.SuspectedProblemRule(tc.rule), p.Rule)
			require.NotNil(t, p.UserSessionIssuerID)
			require.Equal(t, want.issuer.String(), *p.UserSessionIssuerID)
			require.Contains(t, signInIDs(ti.listFor(t, ctx, want.issuer)), p.ID, "the MCP sign-in lists it")
			if want.server == uuid.Nil {
				require.Nil(t, p.Server)
				return
			}
			require.NotNil(t, p.Server)
			require.Equal(t, want.server.String(), p.Server.ID)
			require.NotNil(t, p.Server.Slug)
			onServer, err := ti.service.ListSuspectedProblems(ctx, &gen.ListSuspectedProblemsPayload{UserSessionIssuerID: want.issuer.String(), ServerID: new(want.server.String()), Part: nil})
			require.NoError(t, err)
			require.True(t, slices.ContainsFunc(onServer.Problems, func(s *gen.SuspectedProblem) bool { return s.ID == p.ID }), "the server's Sessions tab lists it")
		})
	}
}
