package mcpconnectionhealth_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// failOn writes one failed request by person on a gateway, ago before now.
func (ti *testInstance) failOn(t *testing.T, org string, project, gateway uuid.UUID, person urn.SessionSubject, ago time.Duration, code mcpconnectionattempts.Code, opts ...mcpconnectionattempts.Option) uuid.UUID {
	t.Helper()
	return ti.attemptAs(t, mcpconnectionattempts.GatewayScope(org, project, gateway, mcpconnectionattempts.AppDCR), person, ago, mcpconnectionattempts.StepMCPRequest, code, opts...)
}

// attemptAs writes one finished attempt by person on scope, ago before now.
func (ti *testInstance) attemptAs(t *testing.T, scope mcpconnectionattempts.Scope, person urn.SessionSubject, ago time.Duration, step mcpconnectionattempts.Step, what mcpconnectionattempts.Happened, opts ...mcpconnectionattempts.Option) uuid.UUID {
	t.Helper()
	at := ti.now.Add(-ago)
	a := mcpconnectionattempts.AttemptAt(uuid.New(), at, scope)
	row, err := mcpconnectionattempts.RowAt(a, step, what, true, at.Add(time.Second), append(opts, mcpconnectionattempts.Subject(&person))...)
	require.NoError(t, err)
	require.NoError(t, mcpconnectionattempts.AppendRows(t.Context(), ti.ch, []mcpconnectionattempts.Row{row}))
	return a.ID()
}

// provider creates another MCP authentication provider in the project with a gateway on it.
func (ti *testInstance) provider(t *testing.T, name string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(t.Context(), usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          ti.project,
		OrganizationID:     pgtype.Text{String: ti.org, Valid: true},
		Slug:               "other-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: 0, Days: 14, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	gateway, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(t.Context(), metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      ti.org,
		ProjectID:           ti.project,
		Name:                name,
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Visibility:          "private",
	})
	require.NoError(t, err)
	return issuer.ID, gateway.ID
}

// failPeople has n new people fail on a gateway with code, ago before now.
func (ti *testInstance) failPeople(t *testing.T, gateway uuid.UUID, n int, ago time.Duration, code mcpconnectionattempts.Code) []urn.SessionSubject {
	t.Helper()
	people := make([]urn.SessionSubject, n)
	for i := range people {
		people[i] = urn.NewUserSubject("user-" + uuid.NewString())
		ti.failOn(t, ti.org, ti.project, gateway, people[i], ago, code)
	}
	return people
}

// gateway creates another gateway on the provider.
func (ti *testInstance) gateway(t *testing.T, name string) uuid.UUID {
	t.Helper()
	server, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(t.Context(), metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      ti.org,
		ProjectID:           ti.project,
		Name:                name,
		UserSessionIssuerID: uuid.NullUUID{UUID: ti.issuer, Valid: true},
		Visibility:          "private",
	})
	require.NoError(t, err)
	return server.ID
}

func (ti *testInstance) listProblems(t *testing.T, ctx context.Context, serverID *string, part *gen.SuspectedProblemPart) *gen.ListSuspectedProblemsResult {
	t.Helper()
	res, err := ti.service.ListSuspectedProblems(ctx, &gen.ListSuspectedProblemsPayload{UserSessionIssuerID: ti.issuer.String(), ServerID: serverID, Part: part})
	require.NoError(t, err)
	return res
}

func (ti *testInstance) getProblem(t *testing.T, ctx context.Context, id string) *gen.GetSuspectedProblemResult {
	t.Helper()
	res, err := ti.service.GetSuspectedProblem(ctx, &gen.GetSuspectedProblemPayload{UserSessionIssuerID: ti.issuer.String(), ID: id})
	require.NoError(t, err)
	return res
}

func TestListSuspectedProblems_ListsTheProvidersProblems(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)

	res := ti.listProblems(t, ctx, nil, nil)
	require.True(t, res.LoggingEnabled)
	require.Len(t, res.Problems, 1)
	p := res.Problems[0]
	require.Equal(t, gen.SuspectedProblemRule("same_server"), p.Rule)
	require.Equal(t, gen.SuspectedProblemPart("service"), p.Part)
	require.Equal(t, "server", p.Scope.Kind)
	require.Equal(t, "Linear", p.Scope.Name)
	require.Equal(t, int64(2), p.PeopleCount)
	require.Nil(t, p.PeopleOnServerCount)
	require.Equal(t, "2 people can't connect to Linear", p.Headline)
	require.Equal(t, "The server, or the API one of its tools calls, rejected the credential saved for it.", p.Fact)
}

func TestListSuspectedProblems_FiltersByPart(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeInvalidBearerToken)

	require.Len(t, ti.listProblems(t, ctx, nil, nil).Problems, 2)
	speakeasy := ti.listProblems(t, ctx, nil, new(gen.SuspectedProblemPart("speakeasy"))).Problems
	require.Len(t, speakeasy, 1)
	require.Equal(t, "2 people can't sign in to Linear", speakeasy[0].Headline)
	service := ti.listProblems(t, ctx, nil, new(gen.SuspectedProblemPart("service"))).Problems
	require.Len(t, service, 1)
	require.Equal(t, gen.SuspectedProblemPart("service"), service[0].Part)
}

func TestListSuspectedProblems_FiltersByServerAndCountsItsPeople(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	other := ti.gateway(t, "GitHub")
	ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)

	onIt := ti.listProblems(t, ctx, new(ti.server.String()), nil).Problems
	require.Len(t, onIt, 1)
	require.Equal(t, int64(2), *onIt[0].PeopleOnServerCount)
	require.Empty(t, ti.listProblems(t, ctx, new(other.String()), nil).Problems)
}

func TestListSuspectedProblems_NeverShowsAnotherProjectOrOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	for range 2 {
		ti.failOn(t, ti.org, uuid.New(), ti.server, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
		ti.failOn(t, "org-"+uuid.NewString(), ti.project, ti.server, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	}
	ti.failPeople(t, ti.server, 1, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)

	require.Empty(t, ti.listProblems(t, ctx, nil, nil).Problems, "one person here, the rest elsewhere")
}

func TestSuspectedProblems_WithoutLogReadAreEmpty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	admin := withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	member := withProjectGrants(t, ctx, authz.ScopeProjectRead)
	ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	listed := ti.listProblems(t, admin, nil, nil).Problems
	require.Len(t, listed, 1)

	require.Equal(t, gen.ConnectionViewerScope("all"), ti.listProblems(t, admin, nil, nil).ViewerScope)
	res := ti.listProblems(t, member, nil, nil)
	require.True(t, res.LoggingEnabled)
	require.Equal(t, gen.ConnectionViewerScope("own"), res.ViewerScope)
	require.Empty(t, res.Problems)
	got := ti.getProblem(t, member, listed[0].ID)
	require.Equal(t, gen.ConnectionViewerScope("own"), got.ViewerScope)
	require.False(t, got.Found)
	require.Nil(t, got.Problem)
}

func TestSuspectedProblems_SayLoggingIsOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	id := ti.listProblems(t, ctx, nil, nil).Problems[0].ID
	ti.logsOn.Store(false)

	res := ti.listProblems(t, ctx, nil, nil)
	require.False(t, res.LoggingEnabled)
	require.Empty(t, res.Problems)
	got := ti.getProblem(t, ctx, id)
	require.False(t, got.LoggingEnabled)
	require.False(t, got.Found)
}

func TestSuspectedProblems_NotFoundWhenTheFlagIsOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.flags.targetOrganizations()

	_, err := ti.service.ListSuspectedProblems(ctx, &gen.ListSuspectedProblemsPayload{UserSessionIssuerID: ti.issuer.String(), ServerID: nil, Part: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.GetSuspectedProblem(ctx, &gen.GetSuspectedProblemPayload{UserSessionIssuerID: ti.issuer.String(), ID: "x", ServerID: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetSuspectedProblem_UnknownOrClearedIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	people := ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeInvalidBearerToken)
	id := ti.listProblems(t, ctx, nil, nil).Problems[0].ID
	require.True(t, ti.getProblem(t, ctx, id).Found)
	require.False(t, ti.getProblem(t, ctx, "not-a-problem").Found)

	ti.insert(t, ti.org, ti.project, ti.now.Add(-time.Minute), mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, mcpconnectionattempts.Subject(&people[0]))
	got := ti.getProblem(t, ctx, id)
	require.True(t, got.LoggingEnabled)
	require.False(t, got.Found, "one person signed in since, so the problem cleared")
}

func TestGetSuspectedProblem_FiltersOpenExactlyItsAttempts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	other := ti.gateway(t, "GitHub")
	p1, p2 := urn.NewUserSubject("user-"+uuid.NewString()), urn.NewUserSubject("user-"+uuid.NewString())
	want := []string{
		ti.failOn(t, ti.org, ti.project, ti.server, p1, 2*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected).String(),
		ti.failOn(t, ti.org, ti.project, ti.server, p2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected).String(),
	}
	// Noise: the same person failing another way, and someone else failing the same way elsewhere.
	ti.failOn(t, ti.org, ti.project, ti.server, p1, 30*time.Minute, mcpconnectionattempts.CodeInvalidBearerToken)
	ti.failOn(t, ti.org, ti.project, other, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	// A success, a run that ended before the problem began, a gateway member, another project.
	ti.attemptAs(t, mcpconnectionattempts.GatewayScope(ti.org, ti.project, ti.server, mcpconnectionattempts.AppDCR), p1, 20*time.Minute, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued)
	ti.failOn(t, ti.org, ti.project, ti.server, p2, 5*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	require.NoError(t, mcpconnectionattempts.AppendServed(t.Context(), ti.ch, []mcpconnectionattempts.Served{{
		OrganizationID: ti.org, ProjectID: ti.project, ServerKind: "meta", ServerID: ti.server.String(), MemberID: "", SubjectURN: p2.String(),
		AppKey: mcpconnectionattempts.AppDCR.String(), ClientFamily: "", RemoteSessionClientID: "", ServedAt: ti.now.Add(-4 * time.Hour), AnsweredAt: ti.now.Add(-4 * time.Hour),
	}}))
	ti.failOn(t, ti.org, ti.project, ti.server, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.CodeStaticKeyRejected, mcpconnectionattempts.Member(uuid.New()))
	ti.failOn(t, ti.org, uuid.New(), ti.server, p1, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)

	listed := ti.listProblems(t, ctx, nil, nil).Problems
	require.Len(t, listed, 1)
	got := ti.getProblem(t, ctx, listed[0].ID)
	require.True(t, got.Found)
	f := got.Problem.Filters
	require.Equal(t, []gen.ConnectionOutcome{"failed"}, f.Outcomes)
	require.Equal(t, []string{ti.server.String()}, f.ServerIds)
	require.ElementsMatch(t, []string{p1.String(), p2.String()}, f.People)
	require.Equal(t, []string{mcpconnectionattempts.CodeStaticKeyRejected.String()}, f.Reasons)

	rows, err := ti.service.ListAttempts(ctx, &gen.ListAttemptsPayload{
		UserSessionIssuerID: ti.issuer.String(),
		From:                f.From,
		To:                  ti.rangeTo(),
		Filters: &gen.ConnectionAttemptFilters{
			Outcomes: f.Outcomes, Steps: f.Steps, ServerIds: f.ServerIds, MemberIds: f.MemberIds, People: f.People,
			Networks: f.Networks, Apps: f.Apps, AppVersions: f.AppVersions, Reasons: f.Reasons, Services: f.Services,
		},
	})
	require.NoError(t, err)
	var ids []string
	for _, r := range rows.Attempts {
		ids = append(ids, r.ID)
	}
	require.ElementsMatch(t, want, ids)

	d := got.Problem
	require.Len(t, d.Messages, 1)
	require.Equal(t, int64(2), d.Messages[0].Attempts)
	require.Equal(t, "The server rejected the saved key.", d.Messages[0].Message)
	require.NotNil(t, d.Messages[0].AdminMessage)
	require.Len(t, d.Servers, 1)
	require.Equal(t, "Linear", *d.Servers[0].Server.Name)
	require.Equal(t, int64(2), d.PeopleTotal)
	require.Len(t, d.People, 2)
	require.Equal(t, p1.String(), d.People[0].Person.SubjectUrn, "earliest failure first")
	require.Nil(t, d.Source, "a saved key rejection names no single party")
	var failed int64
	for _, b := range d.FailedPer10Minutes {
		failed += b.Attempts
	}
	require.Equal(t, int64(2), failed)
	require.False(t, d.CountsWidened)
}

func TestGetSuspectedProblem_CapsPeople(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.failPeople(t, ti.server, 51, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)

	listed := ti.listProblems(t, ctx, nil, nil).Problems
	require.Len(t, listed, 1)
	require.Nil(t, listed[0].Filters.People, "51 people are over the filter's cap")
	got := ti.getProblem(t, ctx, listed[0].ID)
	require.Len(t, got.Problem.People, 50)
	require.Equal(t, int64(51), got.Problem.PeopleTotal)
	require.True(t, got.Problem.CountsWidened, "the people filter was left out")
	require.Equal(t, int64(51), got.Problem.PeopleCount)
}

func TestListSuspectedProblems_OnePersonNamesTheServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	me := urn.NewUserSubject(ti.userID)
	ti.failOn(t, ti.org, ti.project, ti.server, me, 30*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	ti.failOn(t, ti.org, ti.project, ti.server, me, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)

	listed := ti.listProblems(t, ctx, nil, nil).Problems
	require.Len(t, listed, 1)
	p := listed[0]
	require.Equal(t, gen.SuspectedProblemRule("one_person"), p.Rule)
	require.Equal(t, "person", p.Scope.Kind)
	require.NotEqual(t, "Someone", p.Scope.Name, "the caller's own name resolves")
	require.Equal(t, p.Scope.Name+" can't connect", p.Headline)
	require.True(t, strings.HasPrefix(p.Fact, "On Linear. "), p.Fact)
	require.False(t, slices.Contains(strings.Fields(p.Headline), mcpconnectionattempts.CodeStaticKeyRejected.String()))
}

func TestSuspectedProblems_NeverShowAnOrganizationGateway(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	gateway := uuid.New()
	for range 2 {
		ti.attemptAs(t, mcpconnectionattempts.OrganizationGatewayScope(ti.org, gateway, mcpconnectionattempts.AppDCR), urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStaticKeyRejected)
	}
	require.Empty(t, ti.listProblems(t, ctx, nil, nil).Problems)
}

func TestGetSuspectedProblem_OnOneServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	other := ti.gateway(t, "GitHub")
	ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	id := ti.listProblems(t, ctx, nil, nil).Problems[0].ID

	get := func(server uuid.UUID) *gen.GetSuspectedProblemResult {
		res, err := ti.service.GetSuspectedProblem(ctx, &gen.GetSuspectedProblemPayload{UserSessionIssuerID: ti.issuer.String(), ID: id, ServerID: new(server.String())})
		require.NoError(t, err)
		return res
	}
	onIt := get(ti.server)
	require.True(t, onIt.Found)
	require.Equal(t, int64(2), *onIt.Problem.PeopleOnServerCount)
	require.False(t, get(other).Found)
}

// The same problem in another project or organization has an id this project never finds.
func TestGetSuspectedProblem_NeverFindsAnotherProjectsOrOrganizationsProblem(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	otherProject, otherOrg := uuid.New(), "org-"+uuid.NewString()
	for range 2 {
		ti.failOn(t, ti.org, otherProject, ti.server, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
		ti.failOn(t, otherOrg, ti.project, ti.server, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	}
	for _, scope := range []problems.ReadScope{
		{OrganizationID: ti.org, ProjectID: otherProject, WholeOrganization: false, Keep: nil},
		{OrganizationID: otherOrg, ProjectID: ti.project, WholeOrganization: false, Keep: nil},
	} {
		there, err := problems.Read(t.Context(), ti.ch, ti.conn, mcpconnectionattemptstest.LogsOn, scope, time.Time{}, time.Now())
		require.NoError(t, err)
		require.Len(t, there, 1)
		require.False(t, ti.getProblem(t, ctx, there[0].ID).Found)
	}
}

// A person failing on two providers' servers shows on each with only that provider's server.
func TestSuspectedProblems_SplitByProvider(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	otherIssuer, otherGateway := ti.provider(t, "Notion")
	p := urn.NewUserSubject("user-" + uuid.NewString())
	for _, gateway := range []uuid.UUID{ti.server, otherGateway} {
		ti.failOn(t, ti.org, ti.project, gateway, p, 30*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
		ti.failOn(t, ti.org, ti.project, gateway, p, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	}
	for issuer, gateway := range map[uuid.UUID]uuid.UUID{ti.issuer: ti.server, otherIssuer: otherGateway} {
		res, err := ti.service.ListSuspectedProblems(ctx, &gen.ListSuspectedProblemsPayload{UserSessionIssuerID: issuer.String(), ServerID: nil, Part: nil})
		require.NoError(t, err)
		require.Len(t, res.Problems, 1)
		require.Equal(t, []string{gateway.String()}, res.Problems[0].Filters.ServerIds)
		got, err := ti.service.GetSuspectedProblem(ctx, &gen.GetSuspectedProblemPayload{UserSessionIssuerID: issuer.String(), ID: res.Problems[0].ID, ServerID: nil})
		require.NoError(t, err)
		require.True(t, got.Found)
		require.Len(t, got.Problem.Servers, 1)
		require.Equal(t, gateway.String(), got.Problem.Servers[0].Server.ID)
		require.Equal(t, int64(2), got.Problem.Servers[0].Attempts)
	}
}

// A failed sign-in on the provider's own authorization server belongs to that provider only.
func TestSuspectedProblems_ListAFailedSignInToTheProvider(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	otherIssuer, _ := ti.provider(t, "Notion")
	p := urn.NewUserSubject("user-" + uuid.NewString())
	scope := mcpconnectionattempts.AuthorizationServerScope(ti.org, ti.project, ti.issuer, mcpconnectionattempts.AppDCR)
	for _, ago := range []time.Duration{30 * time.Hour, time.Hour} {
		ti.attemptAs(t, scope, p, ago, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodePKCEMismatch)
	}
	listed := ti.listProblems(t, ctx, nil, nil).Problems
	require.Len(t, listed, 1)
	require.Equal(t, gen.SuspectedProblemRule("one_person"), listed[0].Rule)
	res, err := ti.service.ListSuspectedProblems(ctx, &gen.ListSuspectedProblemsPayload{UserSessionIssuerID: otherIssuer.String(), ServerID: nil, Part: nil})
	require.NoError(t, err)
	require.Empty(t, res.Problems)
}

// One row per person, with every server and app they fail on.
func TestGetSuspectedProblem_OneRowPerPerson(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	other := ti.gateway(t, "GitHub")
	p := urn.NewUserSubject("user-" + uuid.NewString())
	for _, ago := range []time.Duration{30 * time.Hour, time.Hour} {
		ti.failOn(t, ti.org, ti.project, ti.server, p, ago, mcpconnectionattempts.CodeStaticKeyRejected)
		ti.failOn(t, ti.org, ti.project, other, p, ago, mcpconnectionattempts.CodeStaticKeyRejected)
		for _, app := range []mcpconnectionattempts.App{mcpconnectionattempts.AppDCR, mcpconnectionattempts.AppOtherMetadataURL} {
			ti.attemptAs(t, mcpconnectionattempts.GatewayScope(ti.org, ti.project, ti.server, app), p, ago, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken)
		}
	}
	listed := ti.listProblems(t, ctx, nil, nil).Problems
	require.Len(t, listed, 2)
	for _, l := range listed {
		got := ti.getProblem(t, ctx, l.ID)
		require.True(t, got.Found)
		require.Len(t, got.Problem.People, 1)
		require.Equal(t, int64(1), got.Problem.PeopleTotal)
		row := got.Problem.People[0]
		if l.Part == "service" {
			require.Len(t, row.Servers, 2, "two servers fold into one row")
		} else {
			require.Len(t, row.Apps, 2, "two apps fold into one row")
		}
	}
}
