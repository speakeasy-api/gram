package attemptlog_test

import (
	"context"
	"encoding/base64"
	"log"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/attemptlog"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{ClickHouse: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

// fixture is one organization and project with a provider and two servers on it.
type fixture struct {
	t       *testing.T
	ch      driver.Conn
	org     string
	project uuid.UUID
	issuer  uuid.UUID
	server  uuid.UUID
	other   uuid.UUID
	now     time.Time
	rows    []mcpconnectionattempts.Row
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	return &fixture{
		t:       t,
		ch:      ch,
		org:     "org-" + uuid.NewString(),
		project: uuid.New(),
		issuer:  uuid.New(),
		server:  uuid.New(),
		other:   uuid.New(),
		now:     time.Now().UTC().Truncate(time.Second),
	}
}

func (f *fixture) scope() attemptlog.Scope {
	return attemptlog.Scope{
		OrganizationID:  f.org,
		ProjectID:       f.project,
		IssuerID:        f.issuer,
		IssuerServerIDs: []string{f.server.String()},
		From:            f.now.Add(-24 * time.Hour),
		To:              f.now.Add(time.Minute),
	}
}

func (f *fixture) attempt(org string, project, server uuid.UUID, started time.Time) mcpconnectionattempts.Attempt {
	return mcpconnectionattempts.AttemptAt(uuid.New(), started, mcpconnectionattempts.ServerScope(org, project, server, mcpconnectionattempts.AppDCR))
}

func (f *fixture) row(a mcpconnectionattempts.Attempt, step mcpconnectionattempts.Step, what mcpconnectionattempts.Happened, final bool, after time.Duration, opts ...mcpconnectionattempts.Option) *mcpconnectionattempts.Row {
	f.t.Helper()
	r, err := mcpconnectionattempts.RowAt(a, step, what, final, a.StartedAt().Add(after), opts...)
	require.NoError(f.t, err)
	f.rows = append(f.rows, r)
	return &f.rows[len(f.rows)-1]
}

func (f *fixture) insert() {
	f.t.Helper()
	require.NoError(f.t, mcpconnectionattempts.AppendRows(f.t.Context(), f.ch, f.rows))
	f.rows = nil
}

func person(id string) mcpconnectionattempts.Option {
	subject := urn.NewUserSubject(id)
	return mcpconnectionattempts.Subject(&subject)
}

func list(t *testing.T, f *fixture, s attemptlog.Scope, filters attemptlog.Filters) []attemptlog.Row {
	t.Helper()
	rows, next, err := attemptlog.List(t.Context(), f.ch, s, filters, nil, attemptlog.MaxPageSize, f.now)
	require.NoError(t, err)
	require.Nil(t, next)
	return rows
}

func TestList_FoldsRepeatsOfTheSameFailure(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	start := f.now.Add(-3 * time.Hour)
	for i := range 3 {
		a := f.attempt(f.org, f.project, f.server, start.Add(time.Duration(i)*5*time.Minute))
		r := f.row(a, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderUnavailable, true, time.Second, person("u1"))
		if i == 1 {
			r.Repeats = 4
		}
	}
	// The same failure an hour later is a new run.
	late := f.attempt(f.org, f.project, f.server, start.Add(90*time.Minute))
	f.row(late, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderUnavailable, true, time.Second, person("u1"))
	// Successes never fold.
	for i := range 2 {
		a := f.attempt(f.org, f.project, f.server, start.Add(time.Duration(i)*time.Minute))
		f.row(a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second, person("u1"))
	}
	f.insert()

	rows := list(t, f, f.scope(), attemptlog.Filters{})
	require.Len(t, rows, 4)
	require.Equal(t, uint64(1), rows[0].Count)
	require.Equal(t, string(attemptlog.OutcomeFailed), rows[0].Outcome)
	folded := rows[1]
	require.Equal(t, uint64(7), folded.Count)
	require.True(t, folded.FirstStartedAt.Equal(start))
	require.Equal(t, mcpconnectionattempts.CodeProviderUnavailable.String(), folded.Reason)
	require.Equal(t, string(attemptlog.StepRequest), folded.Step)
	require.Equal(t, string(attemptlog.OutcomeConnected), rows[2].Outcome)
	require.Equal(t, string(attemptlog.StepSignIn), rows[2].Step)
}

func TestList_PagesNewestFirst(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for i := range 5 {
		a := f.attempt(f.org, f.project, f.server, f.now.Add(-time.Duration(i+2)*time.Hour))
		f.row(a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second)
	}
	f.insert()

	var seen []uuid.UUID
	var after *attemptlog.Cursor
	for range 3 {
		rows, next, err := attemptlog.List(t.Context(), f.ch, f.scope(), attemptlog.Filters{}, after, 2, f.now)
		require.NoError(t, err)
		for _, r := range rows {
			seen = append(seen, r.ID)
		}
		if next == nil {
			break
		}
		decoded, err := attemptlog.DecodeCursor(next.Encode())
		require.NoError(t, err)
		after = &decoded
	}
	require.Len(t, seen, 5)
	all := list(t, f, f.scope(), attemptlog.Filters{})
	for i := range all {
		require.Equal(t, all[i].ID, seen[i])
	}
}

func TestList_NeverReadsAnotherTenant(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	mine := f.attempt(f.org, f.project, f.server, f.now.Add(-2*time.Hour))
	f.row(mine, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second)
	otherOrg := f.attempt("org-"+uuid.NewString(), f.project, f.server, f.now.Add(-2*time.Hour))
	f.row(otherOrg, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second, mcpconnectionattempts.UserSessionIssuer(f.issuer))
	otherProject := f.attempt(f.org, uuid.New(), f.server, f.now.Add(-2*time.Hour))
	f.row(otherProject, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second, mcpconnectionattempts.UserSessionIssuer(f.issuer))
	f.insert()

	rows := list(t, f, f.scope(), attemptlog.Filters{})
	require.Len(t, rows, 1)
	require.Equal(t, mine.ID(), rows[0].ID)

	s := f.scope()
	s.AttemptID = otherOrg.ID()
	row, steps, err := attemptlog.Get(t.Context(), f.ch, s, f.now)
	require.NoError(t, err)
	require.Nil(t, row)
	require.Nil(t, steps)
}

func TestList_AttributesAttemptsToTheProvider(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	onServer := f.attempt(f.org, f.project, f.server, f.now.Add(-2*time.Hour))
	f.row(onServer, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken, true, time.Second)
	named := f.attempt(f.org, f.project, f.other, f.now.Add(-3*time.Hour))
	f.row(named, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, false, 0, mcpconnectionattempts.UserSessionIssuer(f.issuer))
	f.row(named, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second)
	elsewhere := f.attempt(f.org, f.project, f.server, f.now.Add(-4*time.Hour))
	f.row(elsewhere, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second, mcpconnectionattempts.UserSessionIssuer(uuid.New()))
	unrelated := f.attempt(f.org, f.project, f.other, f.now.Add(-5*time.Hour))
	f.row(unrelated, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second)
	f.insert()

	rows := list(t, f, f.scope(), attemptlog.Filters{})
	ids := []uuid.UUID{}
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	require.ElementsMatch(t, []uuid.UUID{onServer.ID(), named.ID()}, ids)

	locked := f.scope()
	locked.ServerID = f.other.String()
	rows = list(t, f, locked, attemptlog.Filters{})
	require.Len(t, rows, 1)
	require.Equal(t, named.ID(), rows[0].ID)
}

func TestList_DerivesOutcomesWithoutAFinalStep(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	live := f.attempt(f.org, f.project, f.server, f.now.Add(-10*time.Minute))
	f.row(live, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, false, 0)
	quiet := f.attempt(f.org, f.project, f.server, f.now.Add(-2*time.Hour))
	f.row(quiet, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, false, 0)
	stopped := f.attempt(f.org, f.project, f.server, f.now.Add(-3*time.Hour))
	f.row(stopped, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, false, 0)
	f.row(stopped, mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderOAuthInvalidScope, false, time.Minute)
	f.insert()

	byID := map[uuid.UUID]attemptlog.Row{}
	for _, r := range list(t, f, f.scope(), attemptlog.Filters{}) {
		byID[r.ID] = r
	}
	require.Equal(t, string(attemptlog.OutcomeInProgress), byID[live.ID()].Outcome)
	require.Equal(t, string(attemptlog.OutcomeDidntFinish), byID[quiet.ID()].Outcome)
	require.Equal(t, string(attemptlog.OutcomeFailed), byID[stopped.ID()].Outcome)
	require.Equal(t, string(attemptlog.StepServiceSignIn), byID[stopped.ID()].Step)
	require.Equal(t, mcpconnectionattempts.CodeProviderOAuthInvalidScope.String(), byID[stopped.ID()].Reason)
}

func TestList_OwnSubjectsSeeOnlyTheirAttempts(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	mine := f.attempt(f.org, f.project, f.server, f.now.Add(-2*time.Hour))
	f.row(mine, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second, person("me"))
	theirs := f.attempt(f.org, f.project, f.server, f.now.Add(-3*time.Hour))
	f.row(theirs, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second, person("them"))
	anonymous := f.attempt(f.org, f.project, f.server, f.now.Add(-4*time.Hour))
	f.row(anonymous, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeCIMDAdmissionDenied, true, 0)
	f.insert()

	s := f.scope()
	s.OwnSubjects = []string{urn.NewUserSubject("me").String()}
	rows := list(t, f, s, attemptlog.Filters{})
	require.Len(t, rows, 1)
	require.Equal(t, mine.ID(), rows[0].ID)
}

func TestCount_EachFacetIgnoresItsOwnFilter(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for i := range 3 {
		a := f.attempt(f.org, f.project, f.server, f.now.Add(-time.Duration(i+2)*time.Hour))
		f.row(a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second, person("u1"))
	}
	failed := f.attempt(f.org, f.project, f.server, f.now.Add(-6*time.Hour))
	r := f.row(failed, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderServerError, true, time.Second, person("u2"))
	r.Repeats = 9
	r.ClientFamily, r.ClientVersion = mcpconnectionattempts.FamilyCursor, "2.0.1"
	blocked := f.attempt(f.org, f.project, f.server, f.now.Add(-7*time.Hour))
	b := f.row(blocked, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeCIMDAdmissionDenied, true, 0)
	b.DeviceKey = "7c1e0000000000000000000000000000"
	f.insert()

	counts, err := attemptlog.Count(t.Context(), f.ch, f.scope(), attemptlog.Filters{Outcomes: []string{string(attemptlog.OutcomeFailed)}}, f.now)
	require.NoError(t, err)
	require.False(t, counts.Truncated)
	require.Equal(t, uint64(10), counts.Total)
	require.ElementsMatch(t, []attemptlog.FacetValue{
		{Value: string(attemptlog.OutcomeFailed), Attempts: 10},
		{Value: string(attemptlog.OutcomeConnected), Attempts: 3},
		{Value: string(attemptlog.OutcomeBlocked), Attempts: 1},
	}, counts.Facets[attemptlog.FacetOutcomes])
	require.Equal(t, []attemptlog.FacetValue{{Value: string(attemptlog.StepRequest), Attempts: 10}}, counts.Facets[attemptlog.FacetSteps])
	require.Equal(t, []attemptlog.FacetValue{{Value: "cursor@2.0.1", Attempts: 10}}, counts.Facets[attemptlog.FacetAppVersions])
	require.Empty(t, counts.Facets[attemptlog.FacetNetworks])

	all, err := attemptlog.Count(t.Context(), f.ch, f.scope(), attemptlog.Filters{}, f.now)
	require.NoError(t, err)
	require.Equal(t, uint64(14), all.Total)
	require.Equal(t, []attemptlog.FacetValue{{Value: b.DeviceKey, Attempts: 1}}, all.Facets[attemptlog.FacetNetworks])

	rows := list(t, f, f.scope(), attemptlog.Filters{Apps: []string{mcpconnectionattempts.FamilyCursor}})
	require.Len(t, rows, 1)
	require.Equal(t, failed.ID(), rows[0].ID)
}

func TestGet_ReturnsTheAttemptAndItsSteps(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	a := f.attempt(f.org, f.project, f.server, f.now.Add(-2*time.Hour))
	f.row(a, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, false, 0, mcpconnectionattempts.UserSessionIssuer(f.issuer))
	f.row(a, mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.ResultSignedIn, false, 7*time.Second, person("u1"))
	last := f.row(a, mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderOAuthInvalidScope, true, 13*time.Second, person("u1"), mcpconnectionattempts.Provider("https://linear.app"))
	last.TraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	f.insert()

	s := attemptlog.Scope{OrganizationID: f.org, ProjectID: f.project, AttemptID: a.ID(), From: f.now.Add(-attemptlog.Retention), To: f.now.Add(time.Hour)}
	row, steps, err := attemptlog.Get(t.Context(), f.ch, s, f.now)
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Equal(t, string(attemptlog.OutcomeFailed), row.Outcome)
	require.Equal(t, "linear.app", row.Service)
	require.Equal(t, last.TraceID, row.TraceID)
	require.Equal(t, []string{f.issuer.String()}, row.IssuerIDs)
	require.Len(t, steps, 3)
	require.Equal(t, mcpconnectionattempts.StepInboundAuthorize.String(), steps[0].StoredStep)
	require.True(t, steps[2].Final)
}

func TestSignIns_CountsSignInsAndRenewalsPerBucket(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	hour := f.now.Truncate(time.Hour).Add(-3 * time.Hour)
	ok := f.attempt(f.org, f.project, f.server, hour.Add(5*time.Minute))
	f.row(ok, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second, mcpconnectionattempts.Grant(oauthwire.GrantTypeAuthorizationCode))
	renewed := f.attempt(f.org, f.project, f.server, hour.Add(10*time.Minute))
	r := f.row(renewed, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenExpired, true, time.Second, mcpconnectionattempts.Grant(oauthwire.GrantTypeRefreshToken))
	r.Repeats = 2
	request := f.attempt(f.org, f.project, f.server, hour.Add(15*time.Minute))
	f.row(request, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken, true, time.Second)
	f.insert()

	s := f.scope()
	s.From, s.To = hour.Add(-time.Hour), hour.Add(2*time.Hour)
	buckets, err := attemptlog.SignIns(t.Context(), f.ch, s, attemptlog.BucketHour, f.now)
	require.NoError(t, err)
	require.Len(t, buckets, 3)
	require.Zero(t, buckets[0].SignInsSucceeded+buckets[0].RenewalsDidNot)
	require.Equal(t, uint64(1), buckets[1].SignInsSucceeded)
	require.Equal(t, uint64(0), buckets[1].SignInsDidNot)
	require.Equal(t, uint64(3), buckets[1].RenewalsDidNot)
}

func TestDenied_CountsPeopleAndNetworks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for i, key := range []string{"aa", "bb", "aa"} {
		a := f.attempt(f.org, f.project, f.server, f.now.Add(-time.Duration(i+2)*time.Hour))
		r := f.row(a, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeCIMDAdmissionDenied, true, 0)
		r.DeviceKey = key
	}
	signedIn := f.attempt(f.org, f.project, f.server, f.now.Add(-time.Hour))
	f.row(signedIn, mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeAIToolBlocked, true, 0, person("u1"))
	f.insert()

	clients, err := attemptlog.Denied(t.Context(), f.ch, f.scope(), f.now)
	require.NoError(t, err)
	require.Len(t, clients, 2)
	require.Equal(t, mcpconnectionattempts.CodeAIToolBlocked.String(), clients[0].Reason)
	require.Equal(t, uint64(1), clients[0].People)
	require.Equal(t, uint64(3), clients[1].Attempts)
	require.Equal(t, uint64(2), clients[1].Networks)
	require.Equal(t, uint64(0), clients[1].People)
}

func TestSignIns_SixHourBucketsStartOnUTCBoundaries(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	day := time.Date(f.now.Year(), f.now.Month(), f.now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -2)
	a := f.attempt(f.org, f.project, f.server, day.Add(7*time.Hour))
	f.row(a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second)
	f.insert()

	s := f.scope()
	s.From, s.To = day, day.Add(24*time.Hour)
	buckets, err := attemptlog.SignIns(t.Context(), f.ch, s, attemptlog.BucketSixHours, f.now)
	require.NoError(t, err)
	require.Len(t, buckets, 4)
	require.True(t, buckets[1].Start.Equal(day.Add(6*time.Hour)))
	require.Equal(t, uint64(1), buckets[1].SignInsSucceeded)
}

func TestSourceOf_NamesTheServerForRejectedCredentials(t *testing.T) {
	t.Parallel()
	failed := attemptlog.OutcomeFailed
	require.Equal(t, attemptlog.SourceServer, attemptlog.SourceOf(mcpconnectionattempts.CodeRejectedByMember.String(), failed))
	require.Equal(t, attemptlog.SourceService, attemptlog.SourceOf(mcpconnectionattempts.CodeProviderOAuthInvalidClient.String(), failed))
	require.Equal(t, attemptlog.SourceApp, attemptlog.SourceOf(mcpconnectionattempts.CodePKCEMismatch.String(), failed))
	require.Equal(t, attemptlog.SourceSpeakeasySetting, attemptlog.SourceOf(mcpconnectionattempts.CodeCIMDAdmissionDenied.String(), attemptlog.OutcomeBlocked))
	require.Equal(t, attemptlog.SourceIdentityProvider, attemptlog.SourceOf(mcpconnectionattempts.CodeIDPUnavailable.String(), failed))
	require.Equal(t, attemptlog.Source(""), attemptlog.SourceOf("", failed))
}

func TestSourceOf_NoneForSuccessLiveAttemptsOrThePersonsOwnDoing(t *testing.T) {
	t.Parallel()
	require.Equal(t, attemptlog.Source(""), attemptlog.SourceOf(mcpconnectionattempts.CodeRefreshedUnverified.String(), attemptlog.OutcomeConnected))
	require.Equal(t, attemptlog.Source(""), attemptlog.SourceOf(mcpconnectionattempts.CodeProviderUnavailable.String(), attemptlog.OutcomeInProgress))
	require.Equal(t, attemptlog.Source(""), attemptlog.SourceOf(mcpconnectionattempts.CodeCanceled.String(), attemptlog.OutcomeDeclined))
	// Recorded for more than one party, so they name none.
	unattributed := []string{
		mcpconnectionattempts.CodeSharedCredentialRejected.String(),
		mcpconnectionattempts.CodeSignInProviderError.String(),
		mcpconnectionattempts.CodeProviderDenied.String(),
		mcpconnectionattempts.CodeIssuerMismatch.String(),
		mcpconnectionattempts.CodeProviderRateLimited.String(),
		mcpconnectionattempts.CodeProviderServerError.String(),
		mcpconnectionattempts.CodeStaticKeyRejected.String(),
		mcpconnectionattempts.CodeIneligibleIdentity.String(),
		mcpconnectionattempts.CodeProviderUnavailable.String(),
		mcpconnectionattempts.CodeProviderUnreachable.String(),
		mcpconnectionattempts.CodeProviderRejected.String(),
		mcpconnectionattempts.CodeUpstreamEMARefused.String(),
	}
	for _, code := range unattributed {
		require.Empty(t, attemptlog.SourceOf(code, attemptlog.OutcomeFailed), code)
	}
	for _, code := range mcpconnectionattempts.Codes() {
		meaning, ok := mcpconnectionattempts.MeaningOf(code)
		require.True(t, ok)
		if meaning.WhoFixes != mcpconnectionattempts.WhoPerson && meaning.WhoFixes != mcpconnectionattempts.WhoNobody && !slices.Contains(unattributed, code) {
			require.NotEmpty(t, attemptlog.SourceOf(code, attemptlog.OutcomeFailed), code)
		}
	}
}

func TestValidateRange_AllowsALast30DaysRangeComputedAMomentAgo(t *testing.T) {
	t.Parallel()
	now := time.Now()
	require.NoError(t, attemptlog.ValidateRange(now.Add(-attemptlog.Retention-time.Minute), now.Add(-time.Minute), now))
	require.Error(t, attemptlog.ValidateRange(now.Add(-attemptlog.Retention-time.Hour), now.Add(-time.Hour), now))
	require.Error(t, attemptlog.ValidateRange(now.Add(time.Hour), now.Add(2*time.Hour), now))
}

func TestList_DeclinedAttemptsNeverFold(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for i := range 3 {
		a := f.attempt(f.org, f.project, f.server, f.now.Add(-2*time.Hour+time.Duration(i)*time.Minute))
		f.row(a, mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeCanceled, true, time.Second, person("u1"))
	}
	f.insert()

	rows := list(t, f, f.scope(), attemptlog.Filters{})
	require.Len(t, rows, 3)
	for _, r := range rows {
		require.Equal(t, string(attemptlog.OutcomeDeclined), r.Outcome)
		require.Equal(t, uint64(1), r.Count)
	}
}

func TestList_FailuresWithNoPersonOrNetworkNeverFold(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for i := range 3 {
		a := f.attempt(f.org, f.project, f.server, f.now.Add(-time.Hour+time.Duration(i)*time.Minute))
		f.row(a, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderUnavailable, true, time.Second)
	}
	f.insert()

	rows := list(t, f, f.scope(), attemptlog.Filters{})
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.Equal(t, uint64(1), row.Count)
	}
}

func TestDenied_LeavesOutRefusalsOfThePerson(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for i, code := range []mcpconnectionattempts.Code{mcpconnectionattempts.CodeNotOrgMember, mcpconnectionattempts.CodeSignInRevokedByAdmin, mcpconnectionattempts.CodeCIMDAdmissionDenied} {
		a := f.attempt(f.org, f.project, f.server, f.now.Add(-time.Duration(i+1)*time.Hour))
		f.row(a, mcpconnectionattempts.StepInboundAuthorize, code, true, 0, person("u1"))
	}
	f.insert()

	clients, err := attemptlog.Denied(t.Context(), f.ch, f.scope(), f.now)
	require.NoError(t, err)
	require.Len(t, clients, 1)
	require.Equal(t, mcpconnectionattempts.CodeCIMDAdmissionDenied.String(), clients[0].Reason)
}

func TestList_ASignInAndARenewalWithTheSameReasonNeverFold(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for i, grant := range []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken} {
		a := f.attempt(f.org, f.project, f.server, f.now.Add(-time.Hour+time.Duration(i)*time.Minute))
		f.row(a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeResourceMismatch, true, time.Second, person("u1"), mcpconnectionattempts.Grant(grant))
	}
	f.insert()

	rows := list(t, f, f.scope(), attemptlog.Filters{})
	require.Len(t, rows, 2)
	require.ElementsMatch(t, []string{string(attemptlog.StepSignIn), string(attemptlog.StepSignInRenewal)}, []string{rows[0].Step, rows[1].Step})
}

func TestList_ANetworkFilterKeepsAttemptsWithNoKnownPersonLikeItsCount(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	known := f.attempt(f.org, f.project, f.server, f.now.Add(-2*time.Hour))
	f.row(known, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second, person("u1")).DeviceKey = "net1"
	anonymous := f.attempt(f.org, f.project, f.server, f.now.Add(-time.Hour))
	f.row(anonymous, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Second).DeviceKey = "net1"
	f.insert()

	filters := attemptlog.Filters{Networks: []string{"net1"}}
	rows := list(t, f, f.scope(), filters)
	require.Len(t, rows, 1)
	require.Equal(t, anonymous.ID(), rows[0].ID)
	counts, err := attemptlog.Count(t.Context(), f.ch, f.scope(), filters, f.now)
	require.NoError(t, err)
	require.Equal(t, uint64(1), counts.Total)
	require.Equal(t, []attemptlog.FacetValue{{Value: "net1", Attempts: 1}}, counts.Facets[attemptlog.FacetNetworks])

	empty := attemptlog.Filters{Networks: []string{""}}
	require.Empty(t, list(t, f, f.scope(), empty))
	counts, err = attemptlog.Count(t.Context(), f.ch, f.scope(), empty, f.now)
	require.NoError(t, err)
	require.Zero(t, counts.Total)
}

func TestList_AVersionBelongsToTheAppThatReportedIt(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	a := f.attempt(f.org, f.project, f.server, f.now.Add(-time.Hour))
	f.row(a, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultCodeIssued, false, time.Second).ClientFamily = mcpconnectionattempts.FamilyCursor
	late := f.row(a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, 2*time.Second)
	late.ClientFamily, late.ClientVersion = mcpconnectionattempts.FamilyClaudeCode, "8.4.0"
	f.insert()

	rows := list(t, f, f.scope(), attemptlog.Filters{})
	require.Len(t, rows, 1)
	require.Equal(t, mcpconnectionattempts.FamilyCursor, rows[0].App)
	require.Empty(t, rows[0].AppVersion)
}

func TestList_FailuresAtTwoStoredStepsOfOneGroupFold(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for i, step := range []mcpconnectionattempts.Step{mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.StepInboundConsent} {
		a := f.attempt(f.org, f.project, f.server, f.now.Add(-time.Hour+time.Duration(i)*time.Minute))
		f.row(a, step, mcpconnectionattempts.CodeInternalError, true, time.Second, person("u1"))
	}
	f.insert()

	rows := list(t, f, f.scope(), attemptlog.Filters{})
	require.Len(t, rows, 1)
	require.Equal(t, uint64(2), rows[0].Count)
}

func TestDecodeCursor_RejectsACursorWithoutATime(t *testing.T) {
	t.Parallel()
	_, err := attemptlog.DecodeCursor(base64.RawURLEncoding.EncodeToString([]byte(`{"i":"` + uuid.NewString() + `"}`)))
	require.ErrorIs(t, err, attemptlog.ErrBadCursor)
	cursor := attemptlog.Cursor{Time: time.Unix(0, 0).UTC(), ID: uuid.New()}
	decoded, err := attemptlog.DecodeCursor(cursor.Encode())
	require.NoError(t, err)
	require.Equal(t, cursor, decoded)
}

func TestList_AnIdentifiedAppWinsOverAnEarlierUnrecognizedOne(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for i, generic := range []string{mcpconnectionattempts.FamilyOther, mcpconnectionattempts.FamilyScript} {
		a := f.attempt(f.org, f.project, f.server, f.now.Add(-time.Hour+time.Duration(i)*time.Minute))
		early := f.row(a, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultCodeIssued, false, time.Second)
		early.ClientFamily, early.ClientVersion = generic, "9.9.9"
		late := f.row(a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, 2*time.Second)
		late.ClientFamily, late.ClientVersion = mcpconnectionattempts.FamilyCursor, "2.0.1"
	}
	f.insert()

	rows := list(t, f, f.scope(), attemptlog.Filters{})
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, mcpconnectionattempts.FamilyCursor, row.App)
		require.Equal(t, "2.0.1", row.AppVersion)
	}
}

func TestList_AScriptWinsOverAnUnrecognizedAppInEitherOrder(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for i, families := range [][2]string{{mcpconnectionattempts.FamilyOther, mcpconnectionattempts.FamilyScript}, {mcpconnectionattempts.FamilyScript, mcpconnectionattempts.FamilyOther}} {
		a := f.attempt(f.org, f.project, f.server, f.now.Add(-time.Hour+time.Duration(i)*time.Minute))
		f.row(a, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultCodeIssued, false, time.Second).ClientFamily = families[0]
		f.row(a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, 2*time.Second).ClientFamily = families[1]
	}
	f.insert()

	rows := list(t, f, f.scope(), attemptlog.Filters{})
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, mcpconnectionattempts.FamilyScript, row.App)
	}
}
