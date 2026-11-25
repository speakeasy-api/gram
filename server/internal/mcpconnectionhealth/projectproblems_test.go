package mcpconnectionhealth_test

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	problemsrepo "github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	unproxiedrepo "github.com/speakeasy-api/gram/server/internal/unproxiedmcp/repo"
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
	remote := uuid.New()
	require.NoError(t, testrepo.New(ti.conn).CreateRemoteMCPBackendFixture(t.Context(), testrepo.CreateRemoteMCPBackendFixtureParams{ID: remote, ProjectID: project, Url: url}))
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(t.Context(), mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: project, Name: conv.ToPGText(slug), Slug: conv.ToPGText(slug),
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer, Valid: true}, RemoteMcpServerID: uuid.NullUUID{UUID: remote, Valid: true},
		Visibility: "private",
	})
	require.NoError(t, err)
	return server.ID
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

func idsOf[T any](ps []*T, id func(*T) string) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = id(p)
	}
	return out
}

func projectID(p *gen.ProjectSuspectedProblem) string { return p.ID }

// requireSameRow asserts the page lists the project row's problem with the same words and counts.
func requireSameRow(t *testing.T, row *gen.ProjectSuspectedProblem, page []*gen.SuspectedProblem, msg string) {
	t.Helper()
	i := slices.IndexFunc(page, func(p *gen.SuspectedProblem) bool { return p.ID == row.ID })
	require.GreaterOrEqual(t, i, 0, msg)
	require.Equal(t, row.Headline, page[i].Headline, msg)
	require.Equal(t, row.Fact, page[i].Fact, msg)
	require.Equal(t, row.PeopleCount, page[i].PeopleCount, msg)
	require.Equal(t, row.Filters, page[i].Filters, msg)
}
func signInID(p *gen.SuspectedProblem) string { return p.ID }

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
	ti.failPeople(t, ti.gateway(t, "GitHub"), 3, time.Hour, mcpconnectionattempts.CodeInvalidBearerToken)

	signIn := ti.listProblems(t, ctx, nil, nil)
	require.Len(t, signIn.Problems, 3)
	all := ti.listProject(t, ctx, nil)
	require.Equal(t, idsOf(signIn.Problems, signInID), idsOf(all.Problems, projectID))
	require.Equal(t, int64(3), all.ProblemsTotal)
	require.Equal(t, new(gen.SuspectedProblemWhoFixes("person")), all.Problems[2].WhoFixes)
	for i, p := range all.Problems {
		require.Equal(t, signIn.Problems[i].Headline, p.Headline)
		require.Equal(t, signIn.Problems[i].PeopleCount, p.PeopleCount)
		require.Nil(t, p.PeopleOnServerCount)
	}

	two := ti.listProject(t, ctx, new(2))
	require.Equal(t, idsOf(signIn.Problems[:2], signInID), idsOf(two.Problems, projectID))
	require.Equal(t, int64(3), two.ProblemsTotal, "the total is before the limit")
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
	ti.failPeople(t, orggateway.ProjectGatewayID(ti.org, ti.project), 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	for range 3 {
		ti.attemptAs(t, mcpconnectionattempts.OrganizationGatewayScope(ti.org, orggateway.GatewayID(ti.org), mcpconnectionattempts.AppDCR), urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStaticKeyRejected)
	}

	res := ti.listProject(t, ctx, nil)
	require.Len(t, res.Problems, 1)
	p := res.Problems[0]
	require.Equal(t, int64(2), p.PeopleCount, "only the people on the project's view")
	require.Equal(t, orgIssuer.ID.String(), *p.UserSessionIssuerID)
	require.Nil(t, p.Server)
	require.Equal(t, []string{p.ID}, idsOf(ti.listFor(t, ctx, orgIssuer.ID), signInID), "the organization's MCP sign-in lists it in this project")
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

// failTwice has one new person fail on scope a day ago and an hour ago, long enough to be a problem alone.
func (ti *testInstance) failTwice(t *testing.T, scope mcpconnectionattempts.Scope, step mcpconnectionattempts.Step, code mcpconnectionattempts.Code, opts ...mcpconnectionattempts.Option) {
	t.Helper()
	p := urn.NewUserSubject("user-" + uuid.NewString())
	for _, ago := range []time.Duration{30 * time.Hour, time.Hour} {
		ti.attemptAs(t, scope, p, ago, step, code, opts...)
	}
}

// Each problem links to the MCP sign-in, and the MCP server, whose own list has the same problem.
func TestListProjectSuspectedProblems_LinksToPagesThatListIt(t *testing.T) {
	t.Parallel()
	type want struct {
		issuer uuid.UUID
		server uuid.UUID
	}
	for _, tc := range []struct {
		name string
		rule string
		make func(t *testing.T, ti *testInstance) want
	}{
		{"same server", "same_server", func(t *testing.T, ti *testInstance) want {
			t.Helper()
			issuer, _ := ti.provider(t, "Notion")
			server := ti.mcpServer(t, ti.project, issuer, "notion", "https://notion.example.com/mcp")
			ti.failDirect(t, ti.project, server, 2*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
			ti.failDirect(t, ti.project, server, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
			return want{issuer: issuer, server: server}
		}},
		{"same service on two servers", "same_service", func(t *testing.T, ti *testInstance) want {
			t.Helper()
			first := ti.mcpServer(t, ti.project, ti.issuer, "docs", "https://mcp.example.com/a")
			second := ti.mcpServer(t, ti.project, ti.issuer, "wiki", "https://mcp.example.com/b")
			ti.failDirect(t, ti.project, first, 2*time.Hour, mcpconnectionattempts.CodeProviderServerError)
			ti.failDirect(t, ti.project, second, time.Hour, mcpconnectionattempts.CodeProviderServerError)
			return want{issuer: ti.issuer, server: uuid.Nil}
		}},
		{"same app version on a gateway", "same_app_version", func(t *testing.T, ti *testInstance) want {
			t.Helper()
			for range 2 {
				ti.failWith(t, ti.server, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, time.Second, mcpconnectionattempts.CodeInvalidBearerToken, mcpconnectionattempts.FamilyCursor, "2.0.1")
			}
			ti.answeredFor(t, ti.server, mcpconnectionattempts.FamilyClaudeCode, 30*time.Minute)
			return want{issuer: ti.issuer, server: uuid.Nil}
		}},
		{"a gateway member on another MCP sign-in", "same_server", func(t *testing.T, ti *testInstance) want {
			t.Helper()
			otherIssuer, _ := ti.provider(t, "Notion")
			member := ti.mcpServer(t, ti.project, otherIssuer, "notion", "https://notion.example.com/mcp")
			for range 2 {
				ti.failOn(t, ti.org, ti.project, ti.server, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.CodeStaticKeyRejected, mcpconnectionattempts.Member(member))
			}
			return want{issuer: ti.issuer, server: uuid.Nil}
		}},
		{"a gateway member and the same server directly", "same_server", func(t *testing.T, ti *testInstance) want {
			t.Helper()
			member := ti.mcpServer(t, ti.project, ti.issuer, "notion", "https://notion.example.com/mcp")
			ti.failOn(t, ti.org, ti.project, ti.server, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.CodeStaticKeyRejected, mcpconnectionattempts.Member(member))
			ti.failDirect(t, ti.project, member, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
			return want{issuer: ti.issuer, server: uuid.Nil}
		}},
		{"one person at an authorization server", "one_person", func(t *testing.T, ti *testInstance) want {
			t.Helper()
			issuer, _ := ti.provider(t, "Notion")
			ti.failTwice(t, mcpconnectionattempts.AuthorizationServerScope(ti.org, ti.project, issuer, mcpconnectionattempts.AppDCR), mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodePKCEMismatch)
			return want{issuer: issuer, server: uuid.Nil}
		}},
		{"a refresh that records its MCP sign-in", "one_person", func(t *testing.T, ti *testInstance) want {
			t.Helper()
			ti.failTwice(t, mcpconnectionattempts.ProviderSessionScope(ti.org, ti.project, mcpconnectionattempts.AppBackground), mcpconnectionattempts.StepProviderRefresh, mcpconnectionattempts.CodeProviderServerError, mcpconnectionattempts.UserSessionIssuer(ti.issuer))
			return want{issuer: ti.issuer, server: uuid.Nil}
		}},
		{"a deleted server", "same_server", func(t *testing.T, ti *testInstance) want {
			t.Helper()
			server := ti.mcpServer(t, ti.project, ti.issuer, "notion", "https://notion.example.com/mcp")
			ti.failDirect(t, ti.project, server, 2*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
			ti.failDirect(t, ti.project, server, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
			n, err := problemsrepo.New(ti.conn).SoftDeleteMCPServerFixture(t.Context(), problemsrepo.SoftDeleteMCPServerFixtureParams{ID: server, ProjectID: ti.project})
			require.NoError(t, err)
			require.Equal(t, int64(1), n)
			return want{issuer: ti.issuer, server: uuid.Nil}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			ctx = withServerRead(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
			w := tc.make(t, ti)

			res := ti.listProject(t, ctx, nil)
			require.Len(t, res.Problems, 1)
			p := res.Problems[0]
			require.Equal(t, gen.SuspectedProblemRule(tc.rule), p.Rule)
			require.NotNil(t, p.UserSessionIssuerID)
			require.Equal(t, w.issuer.String(), *p.UserSessionIssuerID)
			requireSameRow(t, p, ti.listFor(t, ctx, w.issuer), "the MCP sign-in lists it")
			if w.server == uuid.Nil {
				require.Nil(t, p.Server)
				return
			}
			require.NotNil(t, p.Server)
			require.Equal(t, w.server.String(), p.Server.ID)
			require.NotNil(t, p.Server.Slug)
			onServer, err := ti.service.ListSuspectedProblems(ctx, &gen.ListSuspectedProblemsPayload{UserSessionIssuerID: w.issuer.String(), ServerID: new(w.server.String()), Part: nil})
			require.NoError(t, err)
			requireSameRow(t, p, onServer.Problems, "the server's Sessions tab lists it")
		})
	}
}

// A problem no single MCP sign-in lists is still listed, with no link.
func TestListProjectSuspectedProblems_ListsWithoutALinkWhatNoPageLists(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		rule string
		make func(t *testing.T, ti *testInstance)
	}{
		{"a service across two MCP sign-ins", "same_service", func(t *testing.T, ti *testInstance) {
			t.Helper()
			otherIssuer, _ := ti.provider(t, "Notion")
			ti.failDirect(t, ti.project, ti.mcpServer(t, ti.project, ti.issuer, "docs", "https://mcp.example.com/a"), 2*time.Hour, mcpconnectionattempts.CodeProviderServerError)
			ti.failDirect(t, ti.project, ti.mcpServer(t, ti.project, otherIssuer, "wiki", "https://mcp.example.com/b"), time.Hour, mcpconnectionattempts.CodeProviderServerError)
		}},
		{"an app version across two MCP sign-ins", "same_app_version", func(t *testing.T, ti *testInstance) {
			t.Helper()
			_, otherGateway := ti.provider(t, "Notion")
			for _, gateway := range []uuid.UUID{ti.server, otherGateway} {
				ti.failWith(t, gateway, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, time.Second, mcpconnectionattempts.CodeInvalidBearerToken, mcpconnectionattempts.FamilyClaudeCode, "2.0.1")
				ti.answeredFor(t, gateway, mcpconnectionattempts.FamilyCodexCLI, 30*time.Minute)
			}
		}},
		{"a refresh that records no MCP sign-in", "one_person", func(t *testing.T, ti *testInstance) {
			t.Helper()
			ti.failTwice(t, mcpconnectionattempts.ProviderSessionScope(ti.org, ti.project, mcpconnectionattempts.AppBackground), mcpconnectionattempts.StepProviderRefresh, mcpconnectionattempts.CodeProviderServerError)
		}},
		{"a deleted MCP sign-in", "same_server", func(t *testing.T, ti *testInstance) {
			t.Helper()
			issuer, _ := ti.provider(t, "Notion")
			server := ti.mcpServer(t, ti.project, issuer, "notion", "https://notion.example.com/mcp")
			ti.failDirect(t, ti.project, server, 2*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
			ti.failDirect(t, ti.project, server, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
			require.NoError(t, testrepo.New(ti.conn).ForceSoftDeleteUserSessionIssuer(t.Context(), testrepo.ForceSoftDeleteUserSessionIssuerParams{ID: issuer, ProjectID: ti.project}))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
			tc.make(t, ti)

			res := ti.listProject(t, ctx, nil)
			require.Len(t, res.Problems, 1)
			require.Equal(t, gen.SuspectedProblemRule(tc.rule), res.Problems[0].Rule)
			require.Nil(t, res.Problems[0].UserSessionIssuerID)
			require.Nil(t, res.Problems[0].Server)
		})
	}
}

// Concurrent reads of one project share one grouped read.
func TestListProjectSuspectedProblems_SharesOneReadAcrossConcurrentCalls(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	const callers = 4
	var reads atomic.Int32
	joined := make(chan struct{}, callers)
	mcpconnectionhealth.SetProjectFlightHooks(ti.service, func() {
		reads.Add(1)
		for range callers {
			<-joined
		}
	}, func() { joined <- struct{}{} }, nil)

	var wg sync.WaitGroup
	results := make([]*gen.ListProjectSuspectedProblemsResult, callers)
	errs := make([]error, callers)
	for i := range callers {
		wg.Go(func() {
			results[i], errs[i] = ti.service.ListProjectSuspectedProblems(ctx, &gen.ListProjectSuspectedProblemsPayload{Limit: nil})
		})
	}
	wg.Wait()
	require.Equal(t, int32(1), reads.Load())
	for i := range callers {
		require.NoError(t, errs[i])
		require.Len(t, results[i].Problems, 1)
	}
}

// withServerRead grants scopes on the project and mcp:read on every MCP server.
func withServerRead(t *testing.T, ctx context.Context, scopes ...authz.Scope) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	grants := []authz.Grant{authz.NewGrant(authz.ScopeMCPRead, authz.WildcardResource)}
	for _, scope := range scopes {
		grants = append(grants, authz.NewGrant(scope, authCtx.ProjectID.String()))
	}
	return authztest.WithExactGrants(t, ctx, grants...)
}

// servedOn records another app's served request on a direct MCP server, ago before now.
func (ti *testInstance) servedOn(t *testing.T, server uuid.UUID, family string, ago time.Duration) {
	t.Helper()
	require.NoError(t, mcpconnectionattempts.AppendServed(t.Context(), ti.ch, []mcpconnectionattempts.Served{{
		OrganizationID: ti.org, ProjectID: ti.project, ServerKind: "mcp", ServerID: server.String(), MemberID: "",
		SubjectURN: urn.NewUserSubject("user-" + uuid.NewString()).String(), AppKey: mcpconnectionattempts.AppDCR.String(), ClientFamily: family,
		RemoteSessionClientID: "", ServedAt: ti.now.Add(-ago), AnsweredAt: ti.now.Add(-ago),
	}}))
}

// failDirectWith writes a failed request by person on a direct MCP server through a named app, ago before now.
func (ti *testInstance) failDirectWith(t *testing.T, server uuid.UUID, person urn.SessionSubject, ago time.Duration, family, version string) {
	t.Helper()
	at := ti.now.Add(-ago)
	a := mcpconnectionattempts.AttemptAt(uuid.New(), at, mcpconnectionattempts.ServerScope(ti.org, ti.project, server, mcpconnectionattempts.AppDCR))
	row, err := mcpconnectionattempts.RowAt(a, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken, true, at, mcpconnectionattempts.Subject(&person))
	require.NoError(t, err)
	row.ClientFamily, row.ClientVersion = family, version
	require.NoError(t, mcpconnectionattempts.AppendRows(t.Context(), ti.ch, []mcpconnectionattempts.Row{row}))
}

// A problem left over when a larger group wins across MCP sign-ins is not listed on the MCP sign-in alone, so it gets no link.
func TestListProjectSuspectedProblems_NoLinkWhenAnotherGroupWinsOnTheSignIn(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withServerRead(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	otherIssuer, _ := ti.provider(t, "Notion")
	mine := ti.mcpServer(t, ti.project, ti.issuer, "linear", "https://linear.example.com/mcp")
	theirs := ti.mcpServer(t, ti.project, otherIssuer, "notion", "https://notion.example.com/mcp")
	alice := urn.NewUserSubject("user-" + uuid.NewString())
	ti.failDirectWith(t, mine, alice, 30*time.Hour, "", "")
	ti.failDirectWith(t, mine, alice, time.Hour, "", "")
	ti.failDirectWith(t, mine, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.FamilyCursor, "2.0.1")
	for range 2 {
		ti.failDirectWith(t, theirs, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.FamilyCursor, "2.0.1")
	}
	ti.servedOn(t, mine, mcpconnectionattempts.FamilyClaudeCode, 30*time.Minute)
	ti.servedOn(t, theirs, mcpconnectionattempts.FamilyClaudeCode, 30*time.Minute)

	res := ti.listProject(t, ctx, nil)
	require.Len(t, res.Problems, 2)
	var alone *gen.ProjectSuspectedProblem
	for _, p := range res.Problems {
		require.Nil(t, p.UserSessionIssuerID, p.Rule)
		require.Nil(t, p.Server, p.Rule)
		if p.Rule == "one_person" {
			alone = p
		}
	}
	require.NotNil(t, alone, "across the project the app version takes three people and leaves one")
	onSignIn := ti.listFor(t, ctx, ti.issuer)
	require.Len(t, onSignIn, 1)
	require.Equal(t, gen.SuspectedProblemRule("same_server"), onSignIn[0].Rule, "on the MCP sign-in alone the server wins")
	require.NotEqual(t, alone.ID, onSignIn[0].ID)
}

// A server link needs a Sessions tab, and the caller's permission to open it; the MCP sign-in link needs project:read.
func TestListProjectSuspectedProblems_ServerLinkNeedsATabAndPermission(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		unproxied bool
		scopes    []authz.Scope
		mcpRead   bool
		signIn    bool
		server    bool
	}{
		{"every permission", false, []authz.Scope{authz.ScopeProjectRead, authz.ScopeLogRead}, true, true, true},
		{"an unproxied server has no Sessions tab", true, []authz.Scope{authz.ScopeProjectRead, authz.ScopeLogRead}, true, true, false},
		{"no mcp:read", false, []authz.Scope{authz.ScopeProjectRead, authz.ScopeLogRead}, false, true, false},
		{"no project:read", false, []authz.Scope{authz.ScopeLogRead}, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			server := ti.mcpServer(t, ti.project, ti.issuer, "notion", "https://notion.example.com/mcp")
			if tc.unproxied {
				backend, err := unproxiedrepo.New(ti.conn).CreateServer(t.Context(), unproxiedrepo.CreateServerParams{ID: uuid.New(), ProjectID: ti.project, Name: conv.ToPGText("Docs"), Slug: conv.ToPGText("docs"), Url: "https://docs.example.com/mcp", Description: pgtype.Text{}})
				require.NoError(t, err)
				created, err := mcpserversrepo.New(ti.conn).CreateMCPServer(t.Context(), mcpserversrepo.CreateMCPServerParams{
					ID: uuid.New(), ProjectID: ti.project, Name: conv.ToPGText("docs"), Slug: conv.ToPGText("docs"),
					UserSessionIssuerID: uuid.NullUUID{UUID: ti.issuer, Valid: true}, UnproxiedMcpServerID: uuid.NullUUID{UUID: backend.ID, Valid: true}, Visibility: "private",
				})
				require.NoError(t, err)
				server = created.ID
			}
			ti.failDirect(t, ti.project, server, 2*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
			ti.failDirect(t, ti.project, server, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)

			caller := withProjectGrants(t, ctx, tc.scopes...)
			if tc.mcpRead {
				caller = withServerRead(t, ctx, tc.scopes...)
			}
			res := ti.listProject(t, caller, nil)
			require.Len(t, res.Problems, 1)
			require.Equal(t, tc.signIn, res.Problems[0].UserSessionIssuerID != nil)
			require.Equal(t, tc.server, res.Problems[0].Server != nil)
		})
	}
}

// A read held open across logs turning off and on is not shared with a caller counting from the new start.
func TestListProjectSuspectedProblems_DoesNotShareAReadAcrossALogsToggle(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.failPeople(t, ti.server, 2, 3*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	var flights atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	mcpconnectionhealth.SetProjectFlightHooks(ti.service, func() {
		if flights.Add(1) == 1 {
			close(started)
			<-release
		}
	}, nil, nil)

	first := make(chan *gen.ListProjectSuspectedProblemsResult, 1)
	go func() {
		res, _ := ti.service.ListProjectSuspectedProblems(ctx, &gen.ListProjectSuspectedProblemsPayload{Limit: nil})
		first <- res
	}()
	<-started
	require.NoError(t, repo.New(ti.conn).TurnLogsOnFixture(t.Context(), repo.TurnLogsOnFixtureParams{OrganizationID: ti.org, CreatedAt: pgtype.Timestamptz{Time: ti.now.Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true}}))
	require.Empty(t, ti.listProject(t, ctx, nil).Problems, "counting from when logs came back on, the failures are older")
	close(release)
	held := <-first
	require.NotNil(t, held)
	require.Len(t, held.Problems, 1, "the read that started before counts from its own start")
	require.Equal(t, int32(2), flights.Load())
}

// A panic in the shared read fails every waiter, and the next read works.
func TestListProjectSuspectedProblems_APanicFailsTheWaitersOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	var flights atomic.Int32
	joined := make(chan struct{}, 8)
	mcpconnectionhealth.SetProjectFlightHooks(ti.service, func() {
		if flights.Add(1) == 1 {
			<-joined
			<-joined
			panic("injected")
		}
	}, func() { joined <- struct{}{} }, nil)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = ti.service.ListProjectSuspectedProblems(ctx, &gen.ListProjectSuspectedProblemsPayload{Limit: nil})
		})
	}
	wg.Wait()
	for _, err := range errs {
		requireOopsCode(t, err, oops.CodeUnexpected)
	}
	require.Len(t, ti.listProject(t, ctx, nil).Problems, 1)
}

// levelCounter counts log records by level.
type levelCounter struct {
	mu     sync.Mutex
	counts map[slog.Level]int
}

func (c *levelCounter) Enabled(context.Context, slog.Level) bool { return true }
func (c *levelCounter) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[r.Level]++
	return nil
}
func (c *levelCounter) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *levelCounter) WithGroup(string) slog.Handler      { return c }

func (c *levelCounter) count(level slog.Level) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[level]
}

// A stalled shared read logs one warning; the waiter that runs out of time answers unavailable without logging it again.
func TestListProjectSuspectedProblems_AStalledReadLogsOnce(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	me := urn.NewUserSubject(ti.userID)
	ti.failOn(t, ti.org, ti.project, ti.server, me, 30*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	ti.failOn(t, ti.org, ti.project, ti.server, me, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	mcpconnectionhealth.SetProblemsDeadline(ti.service, 2*time.Second)
	require.Len(t, ti.listProject(t, ctx, nil).Problems, 1, "inside the budget the one-person problem is named")

	logs := &levelCounter{counts: map[slog.Level]int{slog.LevelDebug: 0, slog.LevelInfo: 0, slog.LevelWarn: 0, slog.LevelError: 0}}
	mcpconnectionhealth.SetLogger(ti.service, slog.New(logs))
	done := make(chan struct{})
	mcpconnectionhealth.SetProjectFlightHooks(ti.service, nil, nil, func() { close(done) })
	// Only the people lookup reads users: hold it until the budget runs out.
	tx, err := ti.conn.Begin(t.Context()) //nolint:glint // notestingrawsql: the table lock that stalls the name lookup is the behaviour under test.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) }()
	_, err = tx.Exec(t.Context(), "LOCK TABLE users IN ACCESS EXCLUSIVE MODE") //nolint:glint // notestingrawsql: a table lock has no SQLc form.
	require.NoError(t, err)

	_, err = ti.service.ListProjectSuspectedProblems(ctx, &gen.ListProjectSuspectedProblemsPayload{Limit: nil})
	requireOopsCode(t, err, oops.CodeUnavailable)
	<-done
	require.Equal(t, 1, logs.count(slog.LevelWarn))
	require.Zero(t, logs.count(slog.LevelError))
}

// Logs turned off after the request's first check stop the read before anything is shown, as the read checks again.
func TestSuspectedProblems_LogsTurnedOffMidRequestShowNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		read func(t *testing.T, ctx context.Context, ti *testInstance) (bool, int)
	}{
		{"an MCP sign-in", func(t *testing.T, ctx context.Context, ti *testInstance) (bool, int) {
			t.Helper()
			res := ti.listProblems(t, ctx, nil, nil)
			return res.LoggingEnabled, len(res.Problems)
		}},
		{"the project", func(t *testing.T, ctx context.Context, ti *testInstance) (bool, int) {
			t.Helper()
			res := ti.listProject(t, ctx, nil)
			return res.LoggingEnabled, len(res.Problems)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
			ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
			ti.logsOffAfter.Store(1)
			on, shown := tc.read(t, ctx, ti)
			require.False(t, on)
			require.Zero(t, shown)
		})
	}
}

// The same id on the MCP sign-in can group other failures; a link needs the page's problem to be the same one.
func TestListProjectSuspectedProblems_NoLinkWhenThePageGroupsTheIDDifferently(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withServerRead(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	otherIssuer, _ := ti.provider(t, "Notion")
	first := ti.mcpServer(t, ti.project, ti.issuer, "linear", "https://linear.example.com/mcp")
	second := ti.mcpServer(t, ti.project, ti.issuer, "github", "https://github.example.com/mcp")
	theirs := ti.mcpServer(t, ti.project, otherIssuer, "notion", "https://notion.example.com/mcp")
	p := urn.NewUserSubject("user-" + uuid.NewString())
	for _, ago := range []time.Duration{30 * time.Hour, time.Hour} {
		ti.failDirectWith(t, first, p, ago, mcpconnectionattempts.FamilyCursor, "2.0.1")
		ti.failDirectWith(t, second, p, ago, mcpconnectionattempts.FamilyClaudeCode, "2.1.0")
	}
	ti.failDirectWith(t, theirs, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.FamilyCursor, "2.0.1")
	ti.servedOn(t, first, mcpconnectionattempts.FamilyCodexCLI, 30*time.Minute)
	ti.servedOn(t, theirs, mcpconnectionattempts.FamilyCodexCLI, 30*time.Minute)

	res := ti.listProject(t, ctx, nil)
	require.Len(t, res.Problems, 2)
	i := slices.IndexFunc(res.Problems, func(p *gen.ProjectSuspectedProblem) bool { return p.Rule == "one_person" })
	require.GreaterOrEqual(t, i, 0, "the app version across MCP sign-ins takes the first server and leaves the second")
	alone := res.Problems[i]
	require.Nil(t, alone.UserSessionIssuerID)
	require.Nil(t, alone.Server)
	onSignIn := ti.listFor(t, ctx, ti.issuer)
	j := slices.IndexFunc(onSignIn, func(p *gen.SuspectedProblem) bool { return p.ID == alone.ID })
	require.GreaterOrEqual(t, j, 0, "the MCP sign-in has the same id")
	require.NotEqual(t, alone.Headline, onSignIn[j].Headline, "with both servers in it")
}
