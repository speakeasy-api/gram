package mcpconnectionhealth_test

import (
	"context"
	"fmt"
	"net/http"
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
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth/repo"
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

func TestSuspectedProblems_CheckTheProviderWhenLoggingIsOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.logsOn.Store(false)

	_, err := ti.service.ListSuspectedProblems(ctx, &gen.ListSuspectedProblemsPayload{UserSessionIssuerID: "not-a-uuid", ServerID: nil, Part: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.ListSuspectedProblems(ctx, &gen.ListSuspectedProblemsPayload{UserSessionIssuerID: uuid.NewString(), ServerID: nil, Part: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.GetSuspectedProblem(ctx, &gen.GetSuspectedProblemPayload{UserSessionIssuerID: uuid.NewString(), ID: "x", ServerID: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
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
	members, err := repo.New(ti.conn).ListMemberNames(ctx, repo.ListMemberNamesParams{OrganizationID: ti.org, Ids: []string{ti.userID}})
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.NotEmpty(t, members[0].DisplayName)
	require.Equal(t, members[0].DisplayName, p.Scope.Name, "the caller's own name resolves")
	require.Equal(t, members[0].DisplayName+" can't connect to Linear", p.Headline)
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
		{OrganizationID: ti.org, ProjectID: otherProject, WholeOrganization: false},
		{OrganizationID: otherOrg, ProjectID: ti.project, WholeOrganization: false},
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

// failWith writes a failed request by person on a gateway through a named app, started
// took before it failed ago before now.
func (ti *testInstance) failWith(t *testing.T, gateway uuid.UUID, person urn.SessionSubject, ago, took time.Duration, code mcpconnectionattempts.Code, family, version string, opts ...mcpconnectionattempts.Option) uuid.UUID {
	t.Helper()
	at := ti.now.Add(-ago)
	a := mcpconnectionattempts.AttemptAt(uuid.New(), at.Add(-took), mcpconnectionattempts.GatewayScope(ti.org, ti.project, gateway, mcpconnectionattempts.AppDCR))
	row, err := mcpconnectionattempts.RowAt(a, mcpconnectionattempts.StepMCPRequest, code, true, at, append(opts, mcpconnectionattempts.Subject(&person))...)
	require.NoError(t, err)
	row.ClientFamily, row.ClientVersion = family, version
	require.NoError(t, mcpconnectionattempts.AppendRows(t.Context(), ti.ch, []mcpconnectionattempts.Row{row}))
	return a.ID()
}

// answeredFor writes a request of another app that the gateway answered ago before now.
func (ti *testInstance) answeredFor(t *testing.T, gateway uuid.UUID, family string, ago time.Duration) {
	t.Helper()
	require.NoError(t, mcpconnectionattempts.AppendServed(t.Context(), ti.ch, []mcpconnectionattempts.Served{{
		OrganizationID: ti.org, ProjectID: ti.project, ServerKind: "meta", ServerID: gateway.String(), MemberID: "",
		SubjectURN: urn.NewUserSubject("user-" + uuid.NewString()).String(), AppKey: mcpconnectionattempts.AppDCR.String(), ClientFamily: family,
		RemoteSessionClientID: "", ServedAt: ti.now.Add(-ago), AnsweredAt: ti.now.Add(-ago),
	}}))
}

func (ti *testInstance) listFor(t *testing.T, ctx context.Context, issuer uuid.UUID) []*gen.SuspectedProblem {
	t.Helper()
	res, err := ti.service.ListSuspectedProblems(ctx, &gen.ListSuspectedProblemsPayload{UserSessionIssuerID: issuer.String(), ServerID: nil, Part: nil})
	require.NoError(t, err)
	return res.Problems
}

func (ti *testInstance) reassign(t *testing.T, gateway, issuer uuid.UUID) {
	t.Helper()
	n, err := repo.New(ti.conn).SetGatewayIssuerFixture(t.Context(), repo.SetGatewayIssuerFixtureParams{IssuerID: uuid.NullUUID{UUID: issuer, Valid: true}, ID: gateway, ProjectID: ti.project})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}

// Failures recorded under a provider stay with it after their server moves to another.
func TestSuspectedProblems_RecordedProviderWinsAfterAReassignment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	otherIssuer, _ := ti.provider(t, "Notion")
	for range 2 {
		ti.failOn(t, ti.org, ti.project, ti.server, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, mcpconnectionattempts.CodeStaticKeyRejected, mcpconnectionattempts.UserSessionIssuer(ti.issuer))
	}
	ti.reassign(t, ti.server, otherIssuer)

	listed := ti.listFor(t, ctx, ti.issuer)
	require.Len(t, listed, 1, "the attempts name this provider")
	got := ti.getProblem(t, ctx, listed[0].ID)
	require.True(t, got.Found)
	require.Len(t, got.Problem.Messages, 1)
	require.Equal(t, int64(2), got.Problem.Messages[0].Attempts)
	require.Empty(t, ti.listFor(t, ctx, otherIssuer), "the new provider has none of these attempts")
}

// Failures that name no provider belong to the server's provider now.
func TestSuspectedProblems_UnrecordedProviderFollowsTheServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	otherIssuer, _ := ti.provider(t, "Notion")
	ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	ti.reassign(t, ti.server, otherIssuer)

	require.Empty(t, ti.listFor(t, ctx, ti.issuer))
	require.Len(t, ti.listFor(t, ctx, otherIssuer), 1)
}

// An attempt that ran a while before it failed is still inside its problem's filters.
func TestGetSuspectedProblem_FiltersReachTheStartOfASlowAttempt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	var want []string
	for range 2 {
		want = append(want, ti.failWith(t, ti.server, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, 30*time.Minute, mcpconnectionattempts.CodeStaticKeyRejected, "", "").String())
	}
	listed := ti.listProblems(t, ctx, nil, nil).Problems
	require.Len(t, listed, 1)
	got := ti.getProblem(t, ctx, listed[0].ID)
	require.True(t, got.Found)
	f := got.Problem.Filters
	asOf, err := time.Parse(time.RFC3339Nano, got.Problem.AsOf)
	require.NoError(t, err)
	rows, err := ti.service.ListAttempts(ctx, &gen.ListAttemptsPayload{
		UserSessionIssuerID: ti.issuer.String(), From: f.From, To: asOf.Add(time.Second).Format(time.RFC3339),
		Filters: &gen.ConnectionAttemptFilters{Outcomes: f.Outcomes, ServerIds: f.ServerIds, People: f.People, Reasons: f.Reasons},
	})
	require.NoError(t, err)
	var ids []string
	for _, r := range rows.Attempts {
		ids = append(ids, r.ID)
	}
	require.ElementsMatch(t, want, ids)
	var failed int64
	for _, m := range got.Problem.Messages {
		failed += m.Attempts
	}
	require.Equal(t, int64(2), failed)
}

// A read that runs out of time answers temporarily unavailable, whichever read stalled.
func TestSuspectedProblems_AStalledReadIsUnavailable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.failPeople(t, ti.server, 2, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	mcpconnectionhealth.SetProblemsDeadline(ti.service, time.Nanosecond)

	_, err := ti.service.ListSuspectedProblems(ctx, &gen.ListSuspectedProblemsPayload{UserSessionIssuerID: ti.issuer.String(), ServerID: nil, Part: nil})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
	require.Equal(t, http.StatusServiceUnavailable, shareable.HTTPStatus(ctx))
	_, err = ti.service.GetSuspectedProblem(ctx, &gen.GetSuspectedProblemPayload{UserSessionIssuerID: ti.issuer.String(), ID: "x", ServerID: nil})
	requireOopsCode(t, err, oops.CodeUnavailable)
}

// Servers are capped at 50, and a person's folded servers at 5, each with a total.
func TestGetSuspectedProblem_CapsServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	p := urn.NewUserSubject("user-" + uuid.NewString())
	gateways := []uuid.UUID{ti.server}
	for i := range 50 {
		gateways = append(gateways, ti.gateway(t, fmt.Sprintf("Server %d", i)))
	}
	for _, gateway := range gateways {
		ti.failOn(t, ti.org, ti.project, gateway, p, 30*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
		ti.failOn(t, ti.org, ti.project, gateway, p, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	}
	listed := ti.listProblems(t, ctx, nil, nil).Problems
	require.Len(t, listed, 1)
	got := ti.getProblem(t, ctx, listed[0].ID)
	require.True(t, got.Found)
	require.Len(t, got.Problem.Servers, 50)
	require.Equal(t, int64(51), got.Problem.ServersTotal)
	require.Len(t, got.Problem.People, 1)
	require.Len(t, got.Problem.People[0].Servers, 5)
	require.Equal(t, int64(51), got.Problem.People[0].ServersTotal)
	require.True(t, got.Problem.CountsWidened, "51 servers are over the filter's cap")
}

// One person on each provider's server with the same app version is a project-wide pattern no provider lists.
func TestSuspectedProblems_AnAppVersionSplitAcrossProvidersIsNotListed(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	otherIssuer, otherGateway := ti.provider(t, "Notion")
	for _, gateway := range []uuid.UUID{ti.server, otherGateway} {
		ti.failWith(t, gateway, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, time.Second, mcpconnectionattempts.CodeInvalidBearerToken, mcpconnectionattempts.FamilyClaudeCode, "2.0.1")
		ti.answeredFor(t, gateway, mcpconnectionattempts.FamilyCodexCLI, 30*time.Minute)
	}
	projectWide, err := problems.Read(t.Context(), ti.ch, ti.conn, mcpconnectionattemptstest.LogsOn, problems.ReadScope{OrganizationID: ti.org, ProjectID: ti.project}, time.Time{}, time.Now())
	require.NoError(t, err)
	require.Len(t, projectWide, 1, "across the project the two people are one app version problem")
	require.Equal(t, problems.RuleSameAppVersion, projectWide[0].Rule)
	require.Empty(t, ti.listFor(t, ctx, ti.issuer))
	require.Empty(t, ti.listFor(t, ctx, otherIssuer))
}

// Another app working on the other provider's server takes no failures on this provider's servers.
func TestSuspectedProblems_AnAppVersionQualifiedElsewhereIsNotListed(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	_, otherGateway := ti.provider(t, "Notion")
	second := ti.gateway(t, "GitHub")
	for _, gateway := range []uuid.UUID{ti.server, second, otherGateway} {
		ti.failWith(t, gateway, urn.NewUserSubject("user-"+uuid.NewString()), time.Hour, time.Second, mcpconnectionattempts.CodeInvalidBearerToken, mcpconnectionattempts.FamilyClaudeCode, "2.0.1")
	}
	ti.answeredFor(t, otherGateway, mcpconnectionattempts.FamilyCodexCLI, 30*time.Minute)
	projectWide, err := problems.Read(t.Context(), ti.ch, ti.conn, mcpconnectionattemptstest.LogsOn, problems.ReadScope{OrganizationID: ti.org, ProjectID: ti.project}, time.Time{}, time.Now())
	require.NoError(t, err)
	require.Empty(t, projectWide, "an app version takes only the one person on the server where another app worked")
	require.Empty(t, ti.listFor(t, ctx, ti.issuer), "this provider's two people have no other app working on their servers")
}

// A stalled name lookup, after every read before it finished, answers temporarily unavailable.
func TestSuspectedProblems_AStalledNameLookupIsUnavailable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	me := urn.NewUserSubject(ti.userID)
	ti.failOn(t, ti.org, ti.project, ti.server, me, 30*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	ti.failOn(t, ti.org, ti.project, ti.server, me, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	mcpconnectionhealth.SetProblemsDeadline(ti.service, 5*time.Second)
	require.Len(t, ti.listProblems(t, ctx, nil, nil).Problems, 1, "inside the budget the one-person problem is named")

	// Only the people lookup reads users: hold it until the budget runs out.
	tx, err := ti.conn.Begin(t.Context()) //nolint:glint // notestingrawsql: the table lock that stalls the name lookup is the behaviour under test.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) }()
	_, err = tx.Exec(t.Context(), "LOCK TABLE users IN ACCESS EXCLUSIVE MODE") //nolint:glint // notestingrawsql: a table lock has no SQLc form.
	require.NoError(t, err)

	_, err = ti.service.ListSuspectedProblems(ctx, &gen.ListSuspectedProblemsPayload{UserSessionIssuerID: ti.issuer.String(), ServerID: nil, Part: nil})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
	require.Equal(t, http.StatusServiceUnavailable, shareable.HTTPStatus(ctx))
}

// A person failing on one server from two apps is one row with both apps.
func TestGetSuspectedProblem_FoldsEveryAppOfARun(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	p := urn.NewUserSubject("user-" + uuid.NewString())
	ti.failWith(t, ti.server, p, 2*time.Hour, time.Second, mcpconnectionattempts.CodeStaticKeyRejected, mcpconnectionattempts.FamilyClaudeCode, "2.1.0")
	ti.failWith(t, ti.server, p, time.Hour, time.Second, mcpconnectionattempts.CodeStaticKeyRejected, mcpconnectionattempts.FamilyCursor, "2.0.1")
	ti.failPeople(t, ti.server, 1, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)

	listed := ti.listProblems(t, ctx, nil, nil).Problems
	require.Len(t, listed, 1)
	got := ti.getProblem(t, ctx, listed[0].ID)
	require.True(t, got.Found)
	i := slices.IndexFunc(got.Problem.People, func(r *gen.SuspectedProblemPerson) bool { return r.Person.SubjectUrn == p.String() })
	require.GreaterOrEqual(t, i, 0)
	row := got.Problem.People[i]
	require.Equal(t, int64(2), row.AppsTotal)
	var names []string
	for _, a := range row.Apps {
		names = append(names, a.Name+" "+*a.Version)
	}
	require.ElementsMatch(t, []string{"Claude Code 2.1.0", "Cursor 2.0.1"}, names)
	require.Equal(t, int64(2), row.Attempts)
}

// A person failing from more apps than a run keeps still reports every app in the total.
func TestGetSuspectedProblem_CountsAppsPastTheCap(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	p := urn.NewUserSubject("user-" + uuid.NewString())
	versions := mcpconnectionattempts.MaxEpisodeApps + 2
	for i := range versions {
		ti.failWith(t, ti.server, p, time.Duration(i+1)*time.Minute, time.Second, mcpconnectionattempts.CodeStaticKeyRejected, mcpconnectionattempts.FamilyClaudeCode, fmt.Sprintf("2.1.%d", i))
	}
	ti.failPeople(t, ti.server, 1, time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)

	listed := ti.listProblems(t, ctx, nil, nil).Problems
	require.Len(t, listed, 1)
	got := ti.getProblem(t, ctx, listed[0].ID)
	require.True(t, got.Found)
	i := slices.IndexFunc(got.Problem.People, func(r *gen.SuspectedProblemPerson) bool { return r.Person.SubjectUrn == p.String() })
	require.GreaterOrEqual(t, i, 0)
	require.Equal(t, int64(versions), got.Problem.People[i].AppsTotal)
	require.Len(t, got.Problem.People[i].Apps, mcpconnectionattempts.MaxEpisodeApps)
}

// Nothing is recorded while logs are off, so failures from before they were last turned on are not listed.
func TestSuspectedProblems_StartWhenLogsWereLastTurnedOn(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.failPeople(t, ti.server, 2, 3*time.Hour, mcpconnectionattempts.CodeStaticKeyRejected)
	require.Len(t, ti.listProblems(t, ctx, nil, nil).Problems, 1)

	require.NoError(t, repo.New(ti.conn).TurnLogsOnFixture(t.Context(), repo.TurnLogsOnFixtureParams{OrganizationID: ti.org, CreatedAt: pgtype.Timestamptz{Time: ti.now.Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true}}))
	require.Empty(t, ti.listProblems(t, ctx, nil, nil).Problems, "the failures are from before logs were turned back on")
}
