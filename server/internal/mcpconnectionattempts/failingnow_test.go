package mcpconnectionattempts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

type failingFixture struct {
	t       *testing.T
	ch      driver.Conn
	org     string
	project uuid.UUID
	server  uuid.UUID
	now     time.Time
}

func newFailingFixture(t *testing.T) *failingFixture {
	t.Helper()
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	return &failingFixture{t: t, ch: ch, org: "org-" + uuid.NewString(), project: uuid.New(), server: uuid.New(), now: time.Now().UTC()}
}

func (f *failingFixture) at(ago time.Duration) time.Time { return f.now.Add(-ago) }

// attempt writes a one-row attempt that started ago before now and ended at once.
func (f *failingFixture) attempt(scope Scope, ago time.Duration, step Step, what Happened, opts ...Option) {
	f.t.Helper()
	at := f.at(ago)
	r, err := RowAt(AttemptAt(uuid.New(), at, scope), step, what, true, at, opts...)
	require.NoError(f.t, err)
	require.NoError(f.t, AppendRows(f.t.Context(), f.ch, []Row{r}))
}

func (f *failingFixture) direct(app App) Scope { return ServerScope(f.org, f.project, f.server, app) }

// conn is p's connection to a server, gateway or toolset, as the served table keys it.
func (f *failingFixture) conn(p urn.SessionSubject, kind string, server uuid.UUID, app App) Served {
	return Served{
		OrganizationID: f.org, ProjectID: f.project, ServerKind: kind, ServerID: server.String(), MemberID: "",
		SubjectURN: p.String(), AppKey: app.key, ClientFamily: "", RemoteSessionClientID: "", ServedAt: time.Time{}, AnsweredAt: time.Time{},
	}
}

// onServer is p's connection to the direct server.
func (f *failingFixture) onServer(p urn.SessionSubject, app App) Served {
	return f.conn(p, serverKindMCP, f.server, app)
}

// served writes a request on s served ago before now that Speakeasy answered.
func (f *failingFixture) served(s Served, ago time.Duration) {
	f.t.Helper()
	s.ServedAt = f.at(ago)
	require.NoError(f.t, appendServedInto(f.t.Context(), f.ch, lastServedTable, []Served{s}))
}

// answered writes a request on s served ago before now that the server answered.
func (f *failingFixture) answered(s Served, ago time.Duration) {
	f.t.Helper()
	s.ServedAt, s.AnsweredAt = f.at(ago), f.at(ago)
	require.NoError(f.t, appendServedInto(f.t.Context(), f.ch, lastServedTable, []Served{s}))
}

func (f *failingFixture) read(filter FailingNowFilter) []FailingPerson {
	f.t.Helper()
	if filter.OrganizationID == "" {
		filter.OrganizationID = f.org
	}
	got, err := ReadFailingNow(f.t.Context(), f.ch, logsOn, filter, f.now)
	require.NoError(f.t, err)
	return got
}

func (f *failingFixture) failing() []FailingPerson { return f.read(FailingNowFilter{}) }

// client sets the client a row's User-Agent named.
func client(family string) Option { return Option{apply: func(r *Row) { r.ClientFamily = family }} }

// version sets the client version a row's User-Agent named.
func version(v string) Option { return Option{apply: func(r *Row) { r.ClientVersion = v }} }

func person() urn.SessionSubject { return urn.NewUserSubject("user-" + uuid.NewString()) }

func TestFailingNow_ARetryLoopIsOneEpisode(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	for _, ago := range []time.Duration{40 * time.Minute, 30 * time.Minute, 20 * time.Minute} {
		f.attempt(f.direct(AppDCR), ago, StepInboundToken, CodeRefreshTokenExpired, Subject(&p), Option{apply: func(r *Row) { r.Repeats = 9 }})
	}
	got := f.failing()
	require.Len(t, got, 1)
	require.Equal(t, uint64(30), got[0].Failures)
	require.Equal(t, f.at(40*time.Minute), got[0].OpenedAt.UTC())
	require.Equal(t, f.at(20*time.Minute), got[0].LastFailureAt.UTC())
	require.Equal(t, PartAppSignIn, got[0].Part)
	require.True(t, got[0].LastWorkedAt.IsZero())
	require.Equal(t, p.String(), got[0].SubjectURN)
}

func TestFailingNow_BlockedDeclinedAndUnfinishedNeverFail(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepMCPRequest, CodePolicyBlocked, Subject(&p))
	f.attempt(GatewayScope(f.org, f.project, uuid.New(), AppDCR), 30*time.Minute, StepMCPRequest, CodeNeedsSignIn, Subject(&p), Member(uuid.New()))
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepMCPRequest, CodeMCPToolCallsPaused, Subject(&p))
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepProviderSignIn, CodeProviderDenied, Subject(&p))
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepInboundSignIn, CodeExpired, Subject(&p))
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepProviderSignIn, CodeLoginExpired, Subject(&p))
	require.Empty(t, f.failing())
}

func TestFailingNow_RowsWithNoPersonAreNotCounted(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepInboundToken, CodeRefreshTokenExpired)
	anonymous := urn.NewAnonymousSubject(uuid.NewString())
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepInboundToken, CodeRefreshTokenExpired, Subject(&anonymous))
	require.Empty(t, f.failing())
}

func TestFailingNow_GracePeriods(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	early, late, outage, setup := person(), person(), person(), person()
	f.attempt(f.direct(AppDCR), 5*time.Minute, StepInboundToken, CodeRefreshTokenExpired, Subject(&early))
	f.attempt(f.direct(AppDCR), 15*time.Minute, StepInboundToken, CodeRefreshTokenExpired, Subject(&late))
	f.attempt(f.direct(AppDCR), 20*time.Minute, StepProviderRefresh, CodeProviderUnavailable, Subject(&outage))
	f.attempt(f.direct(AppDCR), time.Minute, StepMCPRequest, CodeStaticKeyRejected, Subject(&setup))

	got := map[string]bool{}
	for _, p := range f.failing() {
		got[p.SubjectURN] = true
	}
	require.Equal(t, map[string]bool{late.String(): true, setup.String(): true}, got)
}

// How recently someone must have tried is the caller's choice; the read itself lists every broken part.
func TestFailingNow_TriedSinceKeepsWhoTriedSinceThen(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	stopped, trying := person(), person()
	f.attempt(f.direct(AppDCR), 3*24*time.Hour, StepMCPRequest, CodeStaticKeyRejected, Subject(&stopped))
	f.attempt(f.direct(AppDCR), 3*time.Hour, StepMCPRequest, CodeStaticKeyRejected, Subject(&trying))
	f.attempt(f.direct(AppDCR), 59*time.Minute, StepMCPRequest, CodeStaticKeyRejected, Subject(&trying))

	got := map[string]time.Time{}
	for _, p := range f.failing() {
		got[p.SubjectURN] = p.OpenedAt.UTC()
	}
	require.Equal(t, map[string]time.Time{stopped.String(): f.at(3 * 24 * time.Hour), trying.String(): f.at(3 * time.Hour)}, got, "a failure days old is still broken")

	recent := f.read(FailingNowFilter{OrganizationID: "", ProviderHost: "", ServerID: "", TriedSince: f.at(time.Hour)})
	require.Len(t, recent, 1)
	require.Equal(t, trying.String(), recent[0].SubjectURN)
	require.Equal(t, f.at(3*time.Hour), recent[0].OpenedAt.UTC(), "the run still opens at the first failure")
}

// An attempt with no final row reads as failed only after its live window, so TriedSince counts from then.
// TriedSince applies to the latest failure of the open run, not an older one that ended and became known late.
func TestFailingNow_TriedSinceAppliesToTheOpenRunsLatestFailure(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	started := f.at(90 * time.Minute)
	stopped, err := RowAt(AttemptAt(uuid.New(), started, f.direct(AppDCR)), StepInboundToken, CodeRefreshTokenExpired, false, started.Add(time.Minute), Subject(&p))
	require.NoError(t, err)
	require.NoError(t, AppendRows(t.Context(), f.ch, []Row{stopped}))
	f.attempt(f.direct(AppDCR), 80*time.Minute, StepInboundToken, ResultRefreshed, Subject(&p))
	f.attempt(f.direct(AppDCR), 70*time.Minute, StepInboundToken, CodeRefreshTokenExpired, Subject(&p))

	require.Len(t, f.failing(), 1)
	require.Empty(t, f.read(FailingNowFilter{OrganizationID: "", ProviderHost: "", ServerID: "", TriedSince: f.at(40 * time.Minute)}))
}

func TestFailingNow_AnAttemptThatStoppedOnAFailureIsCountedFromWhenItReadsAsFailed(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	started := f.at(90 * time.Minute)
	stopped, err := RowAt(AttemptAt(uuid.New(), started, f.direct(AppDCR)), StepInboundConsent, CodeSessionChanged, false, started.Add(time.Minute), Subject(&p))
	require.NoError(t, err)
	stillLive, err := RowAt(AttemptAt(uuid.New(), f.at(30*time.Minute), f.direct(AppOtherMetadataURL)), StepInboundConsent, CodeSessionChanged, false, f.at(29*time.Minute), Subject(&p))
	require.NoError(t, err)
	require.NoError(t, AppendRows(t.Context(), f.ch, []Row{stopped, stillLive}))

	got := f.read(FailingNowFilter{OrganizationID: "", ProviderHost: "", ServerID: "", TriedSince: f.at(35 * time.Minute)})
	require.Len(t, got, 1, "the attempt half an hour old may still finish, and the stopped one read as failed half an hour ago")
	require.Equal(t, AppDCR.String(), got[0].AppKey)
	require.Equal(t, started.Add(time.Minute), got[0].OpenedAt.UTC())
	require.Empty(t, f.read(FailingNowFilter{OrganizationID: "", ProviderHost: "", ServerID: "", TriedSince: f.at(25 * time.Minute)}))
}

func TestFailingNow_AnAppSignInFailureEndsOnThatAppsSignIn(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, other := person(), person()
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepInboundToken, CodeRefreshTokenExpired, Subject(&p), client(FamilyCodexCLI))
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepProviderToken, ResultSignedIn, Subject(&p))
	f.attempt(f.direct(AppOtherMetadataURL), 25*time.Minute, StepInboundToken, ResultCodeIssued, Subject(&p))
	f.attempt(ServerScope(f.org, f.project, uuid.New(), AppDCR), 20*time.Minute, StepInboundToken, ResultCodeIssued, Subject(&p))
	f.attempt(f.direct(AppDCR), 18*time.Minute, StepInboundToken, ResultRefreshed, Subject(&p), client(FamilyClaudeCode))
	f.attempt(f.direct(AppDCR), 17*time.Minute, StepInboundToken, ResultRefreshed, Subject(&other), client(FamilyCodexCLI))
	got := f.failing()
	require.Len(t, got, 1, "a service sign-in, and another app's, server's, client's or person's sign-in do not show this app signed in")
	require.Equal(t, PartAppSignIn, got[0].Part)

	f.attempt(f.direct(AppDCR), 10*time.Minute, StepInboundToken, ResultRefreshReplayed, Subject(&p))
	require.Empty(t, f.failing(), "a sign-in that names no client is the app's")
}

func TestFailingNow_AnAppSignInFailureEndsOnThatAppsOwnServedRequest(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, other := person(), person()
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepInboundToken, CodeRefreshTokenExpired, Subject(&p), client(FamilyCodexCLI))
	f.answered(f.onServer(other, AppDCR), 30*time.Minute)
	f.answered(f.conn(p, serverKindMCP, uuid.New(), AppDCR), 30*time.Minute)
	f.answered(f.onServer(p, AppOtherMetadataURL), 30*time.Minute)
	otherClient := f.onServer(p, AppDCR)
	otherClient.ClientFamily = FamilyClaudeCode
	f.answered(otherClient, 30*time.Minute)
	f.attempt(f.direct(AppDCR), 25*time.Minute, StepProviderToken, ResultSignedIn, Subject(&p))
	got := f.failing()
	require.Len(t, got, 1, "another person's, server's, app's or client's request, and a service sign-in, do not show this app signed in")
	require.Equal(t, f.at(30*time.Minute), got[0].ServerLastAnsweredAt.UTC(), "the server answers others")

	sameClient := f.onServer(p, AppDCR)
	sameClient.ClientFamily = FamilyCodexCLI
	f.served(sameClient, 20*time.Minute)
	require.Empty(t, f.failing(), "a request Speakeasy served got past the app's sign-in")
}

// An app that cannot sign in stays listed while other apps use the server.
func TestFailingNow_AnAppFaultIsNotEndedByTheServerAnswering(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepInboundAuthorize, CodeRedirectURIMismatch, Subject(&p))
	f.answered(f.onServer(person(), AppDCR), 30*time.Minute)
	require.Len(t, f.failing(), 1)
}

// The app signs in to the gateway, so a request for any member, or the gateway itself, shows it signed in.
func TestFailingNow_AGatewayAppSignInFailureEndsOnAnyOwnRequestThroughIt(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	for _, member := range []string{uuid.NewString(), ""} {
		p := person()
		gateway := uuid.New()
		f.attempt(GatewayScope(f.org, f.project, gateway, AppDCR), 40*time.Minute, StepInboundToken, CodeRefreshTokenExpired, Subject(&p))
		f.served(f.conn(p, serverKindMeta, uuid.New(), AppDCR), 30*time.Minute)
		require.Len(t, f.failing(), 1, "another gateway")

		s := f.conn(p, serverKindMeta, gateway, AppDCR)
		s.MemberID = member
		f.served(s, 20*time.Minute)
		require.Empty(t, f.failing(), "member %q", member)
	}
}

func TestFailingNow_ASuccessAndALaterFailureOpenANewRun(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	f.attempt(f.direct(AppDCR), 50*time.Minute, StepInboundToken, CodeRefreshTokenExpired, Subject(&p))
	f.attempt(f.direct(AppDCR), 45*time.Minute, StepInboundToken, ResultCodeIssued, Subject(&p))
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepInboundToken, CodeRefreshTokenRevoked, Subject(&p))
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepInboundToken, CodeRefreshTokenRevoked, Subject(&p))

	got := f.failing()
	require.Len(t, got, 1)
	require.Equal(t, f.at(40*time.Minute), got[0].OpenedAt.UTC())
	require.Equal(t, f.at(45*time.Minute), got[0].LastWorkedAt.UTC())
	require.Equal(t, uint64(2), got[0].Failures)
	require.Equal(t, CodeRefreshTokenRevoked.String(), got[0].OpeningReason)
}

// A run holds one part's failures, so an older failure of another part does not open it or use up its grace.
func TestFailingNow_ARunHoldsOnlyTheFailuresOfItsPart(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	f.attempt(f.direct(AppDCR), 6*time.Hour, StepInboundToken, CodeRefreshTokenExpired, Subject(&p))
	f.attempt(f.direct(AppDCR), 6*time.Hour-time.Minute, StepInboundToken, ResultCodeIssued, Subject(&p))
	f.attempt(f.direct(AppDCR), 2*time.Minute, StepProviderRefresh, CodeInvalidGrant, Subject(&p))
	require.Empty(t, f.failing(), "the service sign-in failed two minutes ago, inside its grace")

	q := person()
	f.attempt(f.direct(AppDCR), 6*time.Hour, StepInboundToken, CodeRefreshTokenExpired, Subject(&q))
	f.attempt(f.direct(AppDCR), 6*time.Hour-time.Minute, StepInboundToken, ResultCodeIssued, Subject(&q))
	f.attempt(f.direct(AppDCR), 20*time.Minute, StepProviderRefresh, CodeInvalidGrant, Subject(&q))
	got := f.failing()
	require.Len(t, got, 1)
	require.Equal(t, PartServiceSignIn, got[0].Part)
	require.Equal(t, f.at(20*time.Minute), got[0].OpenedAt.UTC())
	require.Equal(t, uint64(1), got[0].Failures)
	require.Equal(t, CodeInvalidGrant.String(), got[0].OpeningReason)
}

// Another part failing and recovering later does not hide a part that is still broken.
func TestFailingNow_APartStaysBrokenWhileAnotherRecovers(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	f.attempt(f.direct(AppDCR), 50*time.Minute, StepProviderRefresh, CodeInvalidGrant, Subject(&p))
	f.attempt(f.direct(AppDCR), 20*time.Minute, StepProviderRefresh, CodeInvalidGrant, Subject(&p))
	f.attempt(f.direct(AppDCR), 15*time.Minute, StepInboundToken, CodeRefreshTokenExpired, Subject(&p))
	f.attempt(f.direct(AppDCR), 12*time.Minute, StepInboundToken, ResultCodeIssued, Subject(&p))
	f.served(f.onServer(p, AppDCR), 11*time.Minute)
	got := f.failing()
	require.Len(t, got, 1, "a request Speakeasy served did not use the service sign-in")
	require.Equal(t, PartServiceSignIn, got[0].Part)
	require.Equal(t, f.at(50*time.Minute), got[0].OpenedAt.UTC())
	require.Equal(t, uint64(2), got[0].Failures)
}

// The app signs in to the gateway, so a rejected token on calls to two members is one failure run.
func TestFailingNow_ARejectedBearerOnTwoMembersIsOneEpisode(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	scope := GatewayScope(f.org, f.project, uuid.New(), AppUnverified)
	f.attempt(scope, 40*time.Minute, StepMCPRequest, CodeInvalidBearerToken, Subject(&p), Member(uuid.New()))
	f.attempt(scope, 30*time.Minute, StepMCPRequest, CodeInvalidBearerToken, Subject(&p), Member(uuid.New()))
	got := f.failing()
	require.Len(t, got, 1)
	require.Empty(t, got[0].MemberID)
	require.Equal(t, uint64(2), got[0].Failures)
}

// Gate rejections are filed under no verified app; the sign-in that fixes them names the real one.
func TestFailingNow_ARejectedBearerEndsOnASignInByAnyAppOnTheServer(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	f.attempt(f.direct(AppUnverified), 40*time.Minute, StepMCPRequest, CodeInvalidBearerToken, Subject(&p))
	f.attempt(ServerScope(f.org, f.project, uuid.New(), AppDCR), 30*time.Minute, StepInboundToken, ResultCodeIssued, Subject(&p))
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepProviderToken, ResultSignedIn, Subject(&p))
	got := f.failing()
	require.Len(t, got, 1, "another server's sign-in and a service sign-in do not show the app signed in")
	require.Equal(t, AppUnverified.String(), got[0].AppKey)
	require.Equal(t, PartAppSignIn, got[0].Part)

	f.attempt(f.direct(AppDCR), 20*time.Minute, StepInboundToken, ResultCodeIssued, Subject(&p))
	require.Empty(t, f.failing())
}

// A failure filed under no verified app still names its client, so another client's sign-in or request does not end it.
func TestFailingNow_ARejectedBearerEndsOnlyOnTheSameClient(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	f.attempt(f.direct(AppUnverified), 40*time.Minute, StepMCPRequest, CodeInvalidBearerToken, Subject(&p), client(FamilyClaudeCode))
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepInboundToken, ResultRefreshed, Subject(&p), client(FamilyCursor))
	cursor := f.onServer(p, AppDCR)
	cursor.ClientFamily = FamilyCursor
	f.served(cursor, 30*time.Minute)
	require.Len(t, f.failing(), 1, "the person's Cursor working does not show their Claude Code signed in")

	claude := f.onServer(p, AppDCR)
	claude.ClientFamily = FamilyClaudeCode
	f.served(claude, 20*time.Minute)
	require.Empty(t, f.failing())

	q := person()
	f.attempt(f.direct(AppUnverified), 40*time.Minute, StepMCPRequest, CodeInvalidBearerToken, Subject(&q), client(FamilyClaudeCode))
	f.attempt(f.direct(AppOtherMetadataURL), 20*time.Minute, StepInboundToken, ResultRefreshed, Subject(&q), client(FamilyClaudeCode))
	require.Empty(t, f.failing(), "the same client's sign-in under any app")
}

// Served rows carry the real app a rejected token's row could not name.
func TestFailingNow_ARejectedBearerEndsOnThePersonsOwnRequestByAnyApp(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	f.attempt(f.direct(AppUnverified), 40*time.Minute, StepMCPRequest, CodeInvalidBearerToken, Subject(&p))
	f.answered(f.onServer(person(), AppDCR), 30*time.Minute)
	f.answered(f.conn(p, serverKindMCP, uuid.New(), AppDCR), 30*time.Minute)
	require.Len(t, f.failing(), 1, "another person's request, and this person's on another server, do not show this person signed in")

	f.served(f.onServer(p, AppDCR), 20*time.Minute)
	require.Empty(t, f.failing())
}

func TestFailingNow_AServiceSignInFailureEndsOnThePersonsOwnAnswerFromThatMember(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, other := person(), person()
	gateway, member, otherMember := uuid.New(), uuid.New(), uuid.New()
	scope := GatewayScope(f.org, f.project, gateway, AppDCR)
	on := func(who urn.SessionSubject, m string) Served {
		s := f.conn(who, serverKindMeta, gateway, AppDCR)
		s.MemberID = m
		return s
	}
	f.attempt(scope, 40*time.Minute, StepMCPRequest, CodeInvalidRemoteSession, Subject(&p), Member(member))
	f.attempt(scope, 30*time.Minute, StepProviderToken, ResultSignedIn, Subject(&p), Member(otherMember))
	f.attempt(scope, 30*time.Minute, StepProviderToken, ResultSignedIn, Subject(&other), Member(member))
	f.attempt(scope, 30*time.Minute, StepInboundToken, ResultRefreshed, Subject(&p))
	f.answered(on(other, member.String()), 25*time.Minute)
	f.answered(on(p, otherMember.String()), 25*time.Minute)
	f.served(on(p, member.String()), 25*time.Minute)
	f.served(on(p, ""), 25*time.Minute)
	got := f.failing()
	require.Len(t, got, 1, "another member's or person's answer, the app's own sign-in, and requests Speakeasy served do not show this sign-in works")
	require.Equal(t, member.String(), got[0].MemberID)
	require.Equal(t, PartServiceSignIn, got[0].Part)
	require.Equal(t, f.at(25*time.Minute), got[0].ServerLastAnsweredAt.UTC(), "the member serves others")

	f.answered(on(p, member.String()), 20*time.Minute)
	require.Empty(t, f.failing())
}

// A hosted call the service rejected is still served by Speakeasy in the same request, so it must not end the failure.
func TestFailingNow_AServiceSignInFailureIsNotEndedByTheRequestSpeakeasyServed(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	toolset := uuid.New()
	f.attempt(ToolsetScope(f.org, f.project, toolset, AppDCR), 40*time.Minute, StepMCPRequest, CodeProviderRejected, Subject(&p))
	f.served(f.conn(p, serverKindToolset, toolset, AppDCR), 40*time.Minute-time.Millisecond)
	got := f.failing()
	require.Len(t, got, 1)
	require.Equal(t, PartServiceSignIn, got[0].Part)

	f.answered(f.conn(p, serverKindToolset, toolset, AppDCR), 20*time.Minute)
	require.Empty(t, f.failing())
}

func TestFailingNow_AServiceSignInFailureEndsOnlyOnAnAnswerOverTheSameServiceConnection(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	connection := uuid.New()
	using := func(p urn.SessionSubject, id uuid.UUID) Served {
		s := f.onServer(p, AppDCR)
		if id != uuid.Nil {
			s.RemoteSessionClientID = id.String()
		}
		return s
	}

	p := person()
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepProviderRefresh, CodeInvalidGrant, Subject(&p), RemoteSessionClient(connection))
	f.answered(using(p, uuid.New()), 30*time.Minute)
	require.Len(t, f.failing(), 1, "another service connection")
	f.answered(using(p, connection), 20*time.Minute)
	require.Empty(t, f.failing())

	q := person()
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepMCPRequest, CodeProviderRejected, Subject(&q))
	f.answered(using(q, uuid.New()), 20*time.Minute)
	require.Empty(t, f.failing(), "a failure naming no service connection is ended by any of the person's answers there")

	// A row naming no service connection may have used another one, such as a shared credential.
	r := person()
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepProviderRefresh, CodeInvalidGrant, Subject(&r), RemoteSessionClient(connection))
	f.answered(using(r, uuid.Nil), 20*time.Minute)
	got := f.failing()
	require.Len(t, got, 1)
	require.Equal(t, r.String(), got[0].SubjectURN)
}

// A refresh refused because the service sign-in needs renewing is the service's part, whatever step records it.
func TestFailingNow_ARefreshRefusedForTheServiceEndsOnTheServiceSignIn(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	for _, code := range []Code{CodeUpstreamReconnectRequired, CodeUpstreamEMARefused} {
		p, q := person(), person()
		f.attempt(f.direct(AppDCR), 40*time.Minute, StepInboundToken, code, Subject(&p))
		f.attempt(f.direct(AppDCR), 40*time.Minute, StepInboundToken, code, Subject(&q))
		f.served(f.onServer(p, AppDCR), 30*time.Minute)
		f.served(f.onServer(q, AppDCR), 30*time.Minute)
		got := f.failing()
		require.Len(t, got, 2, "%s: the app's token still being served does not show the service sign-in works", code)
		require.Equal(t, PartServiceSignIn, got[0].Part, code.String())

		f.attempt(f.direct(AppFirstParty), 20*time.Minute, StepProviderSignIn, ResultSignedIn, Subject(&p), Member(f.server))
		f.answered(f.onServer(q, AppDCR), 20*time.Minute)
		require.Empty(t, f.failing(), code.String())
	}
}

func TestFailingNow_AServiceSignInFailureEndsOnThatMembersSignIn(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	gateway, member := uuid.New(), uuid.New()
	f.attempt(GatewayScope(f.org, f.project, gateway, AppDCR), 40*time.Minute, StepMCPRequest, CodeProviderRejected, Subject(&p), Member(member))
	require.Len(t, f.failing(), 1)
	// The sign-in is its own attempt, which reaches the provider and names the member.
	f.attempt(ProviderSessionScope(f.org, f.project, AppUnverified), 30*time.Minute, StepProviderSignIn, ResultSignedIn, Subject(&p), Member(member), RemoteSessionClient(uuid.New()))
	require.Empty(t, f.failing())
}

func TestFailingNow_AServiceSignInFailureOnADirectServerEndsOnItsSignIn(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, other := person(), person()
	f.attempt(f.direct(AppDCR), 50*time.Minute, StepProviderRefresh, CodeProviderUnavailable, Subject(&p))
	f.attempt(f.direct(AppDCR), 45*time.Minute, StepMCPRequest, CodeStoredSignInUnreadable, Subject(&p))
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepProviderRefresh, ResultRefreshed, Subject(&other))
	f.attempt(ServerScope(f.org, f.project, uuid.New(), AppDCR), 40*time.Minute, StepProviderRefresh, ResultRefreshed, Subject(&p))
	got := f.failing()
	require.Len(t, got, 1, "another person's refresh, and this person's on another server, do not show this sign-in works")
	require.Equal(t, f.at(50*time.Minute), got[0].OpenedAt.UTC())
	require.Equal(t, uint64(2), got[0].Failures)

	// A check that the server lists tools proves less than a sign-in, so it does not count.
	f.attempt(f.direct(AppFirstParty), 35*time.Minute, StepProviderSignIn, ResultAllowed, Subject(&p), Member(f.server))
	require.Len(t, f.failing(), 1)

	f.attempt(f.direct(AppFirstParty), 30*time.Minute, StepProviderSignIn, ResultSignedIn, Subject(&p), Member(f.server))
	require.Empty(t, f.failing())
}

// A scheduled refresh reaches no server, so only the service connection tells one service from another.
func TestFailingNow_ARefreshWithNoServerEndsOnTheSameServiceConnection(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	connection, otherConnection := uuid.New(), uuid.New()
	background := ProviderSessionScope(f.org, f.project, AppBackground)
	f.attempt(background, 40*time.Minute, StepProviderRefresh, CodeInvalidGrant, Subject(&p), RemoteSessionClient(connection))
	f.attempt(background, 30*time.Minute, StepProviderRefresh, ResultRefreshed, Subject(&p), RemoteSessionClient(otherConnection))
	got := f.failing()
	require.Len(t, got, 1)
	require.Empty(t, got[0].ServerID)

	// A refresh a request started names the server, and the same service connection.
	f.attempt(f.direct(AppDCR), 20*time.Minute, StepProviderRefresh, ResultAdoptedConcurrentWinner, Subject(&p), RemoteSessionClient(connection))
	require.Empty(t, f.failing())
}

// Two members of one gateway can use different services, and a row on the gateway alone names neither.
func TestFailingNow_AServiceSignInFailureOnAGatewayAloneNeedsTheSameServiceConnection(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	gateway, connection := uuid.New(), uuid.New()
	scope := GatewayScope(f.org, f.project, gateway, AppDCR)
	member := func(id string) Served {
		s := f.conn(p, serverKindMeta, gateway, AppDCR)
		s.MemberID, s.RemoteSessionClientID = uuid.NewString(), id
		return s
	}
	f.attempt(scope, 40*time.Minute, StepProviderRefresh, CodeInvalidGrant, Subject(&p), RemoteSessionClient(connection))
	f.attempt(scope, 30*time.Minute, StepProviderRefresh, ResultRefreshed, Subject(&p), RemoteSessionClient(uuid.New()))
	f.attempt(scope, 30*time.Minute, StepProviderRefresh, ResultRefreshed, Subject(&p))
	f.answered(member(uuid.NewString()), 30*time.Minute)
	f.answered(member(""), 30*time.Minute)
	require.Len(t, f.failing(), 1, "another service's refresh or answer, and ones naming no service, do not show this sign-in works")

	f.answered(member(connection.String()), 25*time.Minute)
	require.Empty(t, f.failing(), "a member's answer over the same service connection")

	q := person()
	f.attempt(scope, 20*time.Minute, StepProviderRefresh, CodeInvalidGrant, Subject(&q), RemoteSessionClient(connection))
	f.attempt(scope, 10*time.Minute, StepProviderRefresh, ResultRefreshed, Subject(&q), RemoteSessionClient(connection))
	require.Empty(t, f.failing(), "the same service connection's refresh")
}

// A gateway failure naming no service connection could be any member's, so no member's answer ends it.
func TestFailingNow_AGatewayServiceFailureNamingNoConnectionIsNotEndedByAMember(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	gateway := uuid.New()
	f.attempt(GatewayScope(f.org, f.project, gateway, AppDCR), 40*time.Minute, StepMCPRequest, CodeProviderRejected, Subject(&p))
	s := f.conn(p, serverKindMeta, gateway, AppDCR)
	s.MemberID, s.RemoteSessionClientID = uuid.NewString(), uuid.NewString()
	f.answered(s, 20*time.Minute)
	require.Len(t, f.failing(), 1)
}

// A failure at a gateway's own gate names no member or service connection; the gateway serving the person again ends it.
func TestFailingNow_AGatewayGateFailureEndsWhenTheGatewayServesThePersonAgain(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	for _, code := range []Code{CodeRemoteSessionUnavailable, CodeStoredSignInUnreadable, CodeInternalError} {
		p, other := person(), person()
		gateway := uuid.New()
		f.attempt(GatewayScope(f.org, f.project, gateway, AppDCR), 40*time.Minute, StepMCPRequest, code, Subject(&p), client(FamilyClaudeCode))
		f.served(f.conn(p, serverKindMeta, gateway, AppDCR), 45*time.Minute)
		f.served(f.conn(other, serverKindMeta, gateway, AppDCR), 30*time.Minute)
		f.served(f.conn(p, serverKindMeta, uuid.New(), AppDCR), 30*time.Minute)
		got := f.read(FailingNowFilter{OrganizationID: "", ProviderHost: "", ServerID: gateway.String(), TriedSince: time.Time{}})
		require.Len(t, got, 1, "%s: an earlier request, another person's, and another gateway's do not show the gate lets this person through", code)
		require.Equal(t, PartServiceSignIn, got[0].Part, code.String())

		f.served(f.conn(p, serverKindMeta, gateway, AppOtherMetadataURL), 20*time.Minute)
		require.Empty(t, f.read(FailingNowFilter{OrganizationID: "", ProviderHost: "", ServerID: gateway.String(), TriedSince: time.Time{}}), code.String())
	}

	// A gateway failure naming a service connection was not recorded at the gate.
	p, gateway := person(), uuid.New()
	f.attempt(GatewayScope(f.org, f.project, gateway, AppDCR), 40*time.Minute, StepProviderRefresh, CodeInvalidGrant, Subject(&p), RemoteSessionClient(uuid.New()))
	f.served(f.conn(p, serverKindMeta, gateway, AppDCR), 20*time.Minute)
	require.Len(t, f.read(FailingNowFilter{OrganizationID: "", ProviderHost: "", ServerID: gateway.String(), TriedSince: time.Time{}}), 1)
}

// The gate could not check the token, which every request there goes through, so anyone getting through ends it.
func TestFailingNow_AGateCheckOutageEndsWhenAnyMemberAnswers(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, someone := person(), person()
	gateway := uuid.New()
	f.attempt(GatewayScope(f.org, f.project, gateway, AppUnverified), 40*time.Minute, StepMCPRequest, CodeAuthCheckUnavailable, Subject(&p), client(FamilyClaudeCode))
	got := f.failing()
	require.Len(t, got, 1)
	require.Equal(t, PartServer, got[0].Part)
	require.True(t, got[0].ServerLastAnsweredAt.IsZero())

	s := f.conn(someone, serverKindMeta, gateway, AppDCR)
	s.MemberID = uuid.NewString()
	f.answered(s, 39*time.Minute)
	require.Empty(t, f.failing())
}

// The kill switch check runs only on tool calls, so another caller's answer does not show it works.
func TestFailingNow_AKillSwitchCheckOutageEndsOnlyOnTheCallersOwnAnswer(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, someone := person(), person()
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepMCPRequest, CodeKillswitchCheckUnavailable, Subject(&p))
	f.answered(f.onServer(someone, AppDCR), 30*time.Minute)
	require.Len(t, f.failing(), 1)

	f.answered(f.onServer(p, AppDCR), 20*time.Minute)
	require.Empty(t, f.failing())
}

func TestFailingNow_AServerFailureEndsWhenItAnswersAnyone(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, someone := person(), person()
	f.answered(f.onServer(someone, AppDCR), 2*time.Hour)
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepMCPRequest, CodeProviderUnreachable, Subject(&p))
	f.answered(f.conn(someone, serverKindMCP, uuid.New(), AppDCR), 35*time.Minute)
	f.served(f.onServer(someone, AppDCR), 33*time.Minute)
	throughGateway := f.conn(someone, serverKindMeta, uuid.New(), AppDCR)
	throughGateway.MemberID = f.server.String()
	f.answered(throughGateway, 32*time.Minute)
	f.attempt(f.direct(AppDCR), 31*time.Minute, StepInboundToken, ResultRefreshed, Subject(&p))
	f.attempt(f.direct(AppDCR), 31*time.Minute, StepProviderRefresh, ResultRefreshed, Subject(&p))

	got := f.failing()
	require.Len(t, got, 1, "another server answering, Speakeasy serving, the server answering through a gateway, and the person's sign-ins are not this server answering")
	require.Equal(t, PartServer, got[0].Part)
	require.Equal(t, f.at(2*time.Hour), got[0].LastWorkedAt.UTC())
	require.Equal(t, got[0].LastWorkedAt, got[0].ServerLastAnsweredAt)

	f.answered(f.onServer(someone, AppDCR), 30*time.Minute)
	require.Empty(t, f.failing())
}

// The served table keeps only the latest time, so an answer between two failures shows only while nothing later replaced it.
func TestFailingNow_AServerRunOpensAfterTheLastAnswer(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	f.attempt(f.direct(AppDCR), 50*time.Minute, StepMCPRequest, CodeProviderUnreachable, Subject(&p))
	f.answered(f.onServer(person(), AppDCR), 40*time.Minute)
	f.attempt(f.direct(AppDCR), 35*time.Minute, StepMCPRequest, CodeProviderUnreachable, Subject(&p))
	f.attempt(f.direct(AppDCR), 34*time.Minute, StepMCPRequest, CodeProviderUnreachable, Subject(&p))
	got := f.failing()
	require.Len(t, got, 1)
	require.Equal(t, f.at(35*time.Minute), got[0].OpenedAt.UTC())
	require.Equal(t, f.at(34*time.Minute), got[0].LastFailureAt.UTC())
	require.Equal(t, f.at(40*time.Minute), got[0].LastWorkedAt.UTC())
	require.Equal(t, uint64(2), got[0].Failures)
}

// Writes for one connection fold into its row later, so the read takes the latest of them.
func TestFailingNow_UnmergedServedRowsReadAsTheLatest(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, someone := person(), person()
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepMCPRequest, CodeProviderUnreachable, Subject(&p))
	f.answered(f.onServer(someone, AppDCR), 30*time.Minute)
	f.answered(f.onServer(someone, AppDCR), 50*time.Minute)
	require.Empty(t, f.failing(), "the older answer inserted last does not replace the newer one")
}

func TestFailingNow_AnotherOrganizationsServedRowsNeverEndAFailure(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepMCPRequest, CodeProviderUnreachable, Subject(&p))
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepMCPRequest, CodeProviderRejected, Subject(&p))
	elsewhere := f.onServer(p, AppDCR)
	elsewhere.OrganizationID = "org-" + uuid.NewString()
	f.answered(elsewhere, 20*time.Minute)
	got := f.failing()
	require.Len(t, got, 2)
	for _, g := range got {
		require.True(t, g.ServerLastAnsweredAt.IsZero())
	}
}

// Two apps registered the same way are two connections: one recovering leaves the other failing.
func TestFailingNow_OneAppRecoveringLeavesAnotherFailing(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	f.attempt(f.direct(AppDCR), 50*time.Minute, StepInboundToken, CodeRefreshTokenExpired, Subject(&p), client(FamilyCodexCLI))
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepInboundToken, CodeRefreshTokenExpired, Subject(&p), client(FamilyClaudeCode))
	f.attempt(f.direct(AppDCR), 20*time.Minute, StepInboundToken, ResultRefreshed, Subject(&p), client(FamilyClaudeCode))
	got := f.failing()
	require.Len(t, got, 1)
	require.Equal(t, FamilyCodexCLI, got[0].ClientFamily)
	require.Equal(t, f.at(50*time.Minute), got[0].OpenedAt.UTC())
}

// A timed-out attempt is decided by its last row, so that row names the member that failed.
func TestFailingNow_AnUnfinishedAttemptNamesTheMemberOfItsLastRow(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	gateway, first, last := uuid.New(), uuid.New(), uuid.New()
	started := f.at(90 * time.Minute)
	attempt := AttemptAt(uuid.New(), started, GatewayScope(f.org, f.project, gateway, AppDCR))
	a, err := RowAt(attempt, StepMCPRequest, CodeProviderRateLimited, false, started.Add(time.Minute), Subject(&p), Member(first))
	require.NoError(t, err)
	b, err := RowAt(attempt, StepMCPRequest, CodeProviderUnreachable, false, started.Add(2*time.Minute), Subject(&p), Member(last))
	require.NoError(t, err)
	require.NoError(t, AppendRows(t.Context(), f.ch, []Row{a, b}))
	answer := f.conn(person(), serverKindMeta, gateway, AppDCR)
	answer.MemberID = first.String()
	f.answered(answer, 20*time.Minute)

	got := f.failing()
	require.Len(t, got, 1, "the first member working does not show the last one does")
	require.Equal(t, last.String(), got[0].MemberID)
	require.Equal(t, PartServer, got[0].Part)
}

func TestFailingNow_AMembersFailureEndsOnlyWhenThatMemberAnswersThroughTheGateway(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, someone := person(), person()
	gateway, member, otherMember := uuid.New(), uuid.New(), uuid.New()
	through := func(g, m uuid.UUID) Served {
		s := f.conn(someone, serverKindMeta, g, AppDCR)
		s.MemberID = m.String()
		return s
	}
	f.attempt(GatewayScope(f.org, f.project, gateway, AppDCR), 40*time.Minute, StepMCPRequest, CodeProviderUnreachable, Subject(&p), Member(member))
	f.attempt(GatewayScope(f.org, f.project, gateway, AppDCR), 40*time.Minute, StepMCPRequest, CodeProviderUnreachable, Subject(&p), Member(otherMember))
	f.answered(through(gateway, otherMember), 30*time.Minute)
	f.answered(through(uuid.New(), member), 30*time.Minute)
	f.served(f.conn(someone, serverKindMeta, gateway, AppDCR), 30*time.Minute)

	got := f.failing()
	require.Len(t, got, 1, "the other member answered, this one did not answer through this gateway")
	require.Equal(t, member.String(), got[0].MemberID)
	require.Len(t, f.read(FailingNowFilter{OrganizationID: "", ProviderHost: "", ServerID: member.String(), TriedSince: time.Time{}}), 1)
	require.Empty(t, f.read(FailingNowFilter{OrganizationID: "", ProviderHost: "", ServerID: uuid.NewString(), TriedSince: time.Time{}}))

	f.answered(through(gateway, member), 20*time.Minute)
	require.Empty(t, f.failing())
}

// A gateway's own check failing names no member, and any member answering through it shows the check works.
func TestFailingNow_AGatewaysFailureEndsWhenAnyMemberAnswersThroughIt(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, someone := person(), person()
	gateway := uuid.New()
	through := func(g uuid.UUID) Served {
		s := f.conn(someone, serverKindMeta, g, AppDCR)
		s.MemberID = uuid.NewString()
		return s
	}
	f.attempt(GatewayScope(f.org, f.project, gateway, AppDCR), 40*time.Minute, StepMCPRequest, CodeAuthCheckUnavailable, Subject(&p))
	f.answered(through(uuid.New()), 35*time.Minute)
	f.served(f.conn(someone, serverKindMeta, gateway, AppDCR), 35*time.Minute)
	require.Len(t, f.failing(), 1, "another gateway's member, and the gateway serving its own request")

	f.answered(through(gateway), 30*time.Minute)
	require.Empty(t, f.failing())
}

func TestFailingNow_AListingFailureEndsWhenTheMemberAnswers(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	gateway, member := uuid.New(), uuid.New()
	f.attempt(GatewayScope(f.org, f.project, gateway, AppDCR), 40*time.Minute, StepDiscoveryMember, CodeTunnelUnavailable, Subject(&p), Member(member))

	got := f.failing()
	require.Len(t, got, 1)
	require.Equal(t, member.String(), got[0].MemberID)
	require.Equal(t, PartServer, got[0].Part)

	s := f.conn(person(), serverKindMeta, gateway, AppDCR)
	s.MemberID = member.String()
	f.answered(s, 10*time.Minute)
	require.Empty(t, f.failing())
}

func TestFailingNow_AToolsetFailureEndsWhenItsBackendAnswers(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, someone := person(), person()
	toolset := uuid.New()
	f.attempt(ToolsetScope(f.org, f.project, toolset, AppDCR), 40*time.Minute, StepMCPRequest, CodeProviderServerError, Subject(&p))
	f.answered(f.conn(someone, serverKindToolset, uuid.New(), AppDCR), 30*time.Minute)
	f.answered(f.conn(someone, serverKindMCP, toolset, AppDCR), 30*time.Minute)
	f.served(f.conn(someone, serverKindToolset, toolset, AppDCR), 30*time.Minute)
	got := f.failing()
	require.Len(t, got, 1, "another toolset, another kind, and a listing Speakeasy served did not answer")
	require.True(t, got[0].ServerLastAnsweredAt.IsZero())

	f.answered(f.conn(someone, serverKindToolset, toolset, AppDCR), 20*time.Minute)
	require.Empty(t, f.failing())
}

func TestFailingNow_TheProviderFilterReadsTheAttemptsProvider(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, q := person(), person()
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepProviderRefresh, CodeInvalidGrant, Subject(&p), Provider("https://auth.example.com"))
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepProviderRefresh, CodeInvalidGrant, Subject(&q), Provider("https://other.example.com"))
	// A listing row's host is its member's upstream, not a provider.
	f.attempt(GatewayScope(f.org, f.project, uuid.New(), AppDCR), 40*time.Minute, StepDiscoveryMember, CodeTunnelUnavailable, Subject(&q), Member(uuid.New()), Provider("https://auth.example.com"))

	got := f.read(FailingNowFilter{OrganizationID: "", ProviderHost: "auth.example.com", ServerID: "", TriedSince: time.Time{}})
	require.Len(t, got, 1)
	require.Equal(t, p.String(), got[0].SubjectURN)
	require.Len(t, f.failing(), 3)
}

// A company sign-in is renewed for a person, not an app or a server.
// First-party sign-in has no token step: it connects on consent.
func TestFailingNow_AFirstPartyFailureEndsOnAFirstPartySignIn(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, q := person(), person()
	f.attempt(f.direct(AppFirstParty), 40*time.Minute, StepInboundConsent, CodeConnectionCatalogExpired, Subject(&p))
	f.attempt(f.direct(AppUnverified), 40*time.Minute, StepInboundConsent, CodeConnectionCatalogExpired, Subject(&q))
	f.attempt(f.direct(AppCodeMode), 30*time.Minute, StepInboundConsent, ResultComplete, Subject(&q))
	got := f.failing()
	require.Len(t, got, 2, "another app's consent completing is not a sign-in")

	f.attempt(f.direct(AppFirstParty), 20*time.Minute, StepInboundConsent, ResultComplete, Subject(&p))
	got = f.failing()
	require.Len(t, got, 1)
	require.Equal(t, q.String(), got[0].SubjectURN)
}

func TestFailingNow_ACompanySignInRenewalEndsOnTheNextRenewal(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	connection := uuid.New()
	company := IdentityProviderScope(f.org, AppUnverified)
	f.attempt(company, 40*time.Minute, StepSignInRenewal, CodeRenewalRefused, Subject(&p), RemoteSessionClient(connection))
	f.attempt(company, 30*time.Minute, StepSignInRenewal, ResultRenewed, Subject(&p), RemoteSessionClient(uuid.New()))
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepInboundToken, ResultCodeIssued, Subject(&p))
	require.Len(t, f.failing(), 1, "another company connection's renewal and a server sign-in are not this renewal")

	f.attempt(company, 20*time.Minute, StepSignInRenewal, ResultRenewed, Subject(&p), RemoteSessionClient(connection))
	require.Empty(t, f.failing())
}

func TestFailingNow_AnOrganizationWithLogsOffGetsNothing(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepInboundToken, CodeRefreshTokenExpired, Subject(&p))
	got, err := ReadFailingNow(t.Context(), f.ch, func(context.Context, string) (bool, error) { return false, nil }, FailingNowFilter{OrganizationID: f.org, ProviderHost: "", ServerID: "", TriedSince: time.Time{}}, f.now)
	require.ErrorIs(t, err, ErrLogsOff)
	require.Empty(t, got)
}

// Evidence must be strictly later than the failure to end it.
func TestFailingNow_EvidenceAtTheSameTimeAsTheFailureDoesNotEndIt(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, q, someone := person(), person(), person()
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepInboundToken, CodeRefreshTokenExpired, Subject(&p))
	f.served(f.onServer(p, AppDCR), 40*time.Minute)

	toolset := uuid.New()
	f.attempt(ToolsetScope(f.org, f.project, toolset, AppDCR), 50*time.Minute, StepMCPRequest, CodeProviderServerError, Subject(&q))
	f.attempt(ToolsetScope(f.org, f.project, toolset, AppDCR), 40*time.Minute, StepMCPRequest, CodeProviderServerError, Subject(&q))
	f.answered(f.conn(someone, serverKindToolset, toolset, AppDCR), 40*time.Minute)

	got := map[string]FailingPerson{}
	for _, g := range f.failing() {
		got[g.SubjectURN] = g
	}
	require.Len(t, got, 2)
	require.Equal(t, uint64(1), got[p.String()].Failures)
	require.Equal(t, f.at(40*time.Minute), got[p.String()].LastWorkedAt.UTC())
	require.Equal(t, uint64(1), got[q.String()].Failures, "the failure at the answer's time counts in the run")
	require.Equal(t, f.at(40*time.Minute), got[q.String()].OpenedAt.UTC())
}

func TestFailingNow_TheServerFilterKeepsADirectServer(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, q := person(), person()
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepMCPRequest, CodeProviderUnreachable, Subject(&p))
	f.attempt(ServerScope(f.org, f.project, uuid.New(), AppDCR), 40*time.Minute, StepMCPRequest, CodeProviderUnreachable, Subject(&q))
	got := f.read(FailingNowFilter{OrganizationID: "", ProviderHost: "", ServerID: f.server.String(), TriedSince: time.Time{}})
	require.Len(t, got, 1)
	require.Equal(t, p.String(), got[0].SubjectURN)
}

func TestFailingNow_ALogsCheckErrorFailsTheRead(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	broken := errors.New("settings unavailable")
	_, err := ReadFailingNow(t.Context(), f.ch, func(context.Context, string) (bool, error) { return false, broken }, FailingNowFilter{OrganizationID: f.org, ProviderHost: "", ServerID: "", TriedSince: time.Time{}}, f.now)
	require.ErrorIs(t, err, broken)
	require.NotErrorIs(t, err, ErrLogsOff)
}

func TestFailingNow_AGatewayFailureWithNoMemberReportsWhenAnyMemberLastAnswered(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p, someone := person(), person()
	gateway := uuid.New()
	f.attempt(GatewayScope(f.org, f.project, gateway, AppDCR), 40*time.Minute, StepMCPRequest, CodeAuthCheckUnavailable, Subject(&p))
	for _, ago := range []time.Duration{3 * time.Hour, 2 * time.Hour} {
		s := f.conn(someone, serverKindMeta, gateway, AppDCR)
		s.MemberID = uuid.NewString()
		f.answered(s, ago)
	}
	f.served(f.conn(someone, serverKindMeta, gateway, AppDCR), 30*time.Minute)
	got := f.failing()
	require.Len(t, got, 1)
	require.Equal(t, f.at(2*time.Hour), got[0].ServerLastAnsweredAt.UTC(), "the latest member answer; the gateway serving its own request is not an answer")
}

func TestFailingNow_AnEpisodeNamesItsLatestFailure(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	f.attempt(f.direct(AppDCR), 40*time.Minute, StepProviderRefresh, CodeInvalidGrant, Subject(&p), Provider("https://old.example.com"), client(FamilyCursor), version("1.0.0"))
	f.attempt(f.direct(AppDCR), 30*time.Minute, StepMCPRequest, CodeProviderRejected, Subject(&p), Provider("https://auth.example.com"), client(FamilyCursor), version("1.2.0"))
	got := f.failing()
	require.Len(t, got, 1)
	require.Equal(t, StepMCPRequest.String(), got[0].Step)
	require.Equal(t, "1.2.0", got[0].ClientVersion)
	require.Equal(t, "auth.example.com", got[0].ProviderHost)
	require.Equal(t, CodeInvalidGrant.String(), got[0].OpeningReason)
	require.Equal(t, CodeProviderRejected.String(), got[0].Reason)
}

// More server ids than one batch are read in batches, every row kept and none from other servers.
func TestReadServedByFamily_ReadsInBatches(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	failing := make([]FailingPerson, 0, servedByFamilyBatch+1)
	for range servedByFamilyBatch + 1 {
		failing = append(failing, FailingPerson{Episode: Episode{ServerKind: serverKindMCP, ServerID: uuid.NewString()}})
	}
	first, last := f.conn(person(), serverKindMCP, uuid.MustParse(failing[0].ServerID), AppDCR), f.conn(person(), serverKindMCP, uuid.MustParse(failing[servedByFamilyBatch].ServerID), AppDCR)
	f.served(first, time.Minute)
	f.answered(last, 2*time.Minute)
	f.served(f.conn(person(), serverKindMCP, uuid.New(), AppDCR), time.Minute)

	got, err := ReadServedByFamily(t.Context(), f.ch, f.org, failing)
	require.NoError(t, err)
	byServer := map[string]ServedByFamily{}
	for _, s := range got {
		byServer[s.ServerID] = s
	}
	require.Len(t, byServer, 2)
	require.True(t, byServer[first.ServerID].LastAnsweredAt.IsZero(), "served by Speakeasy only")
	require.Equal(t, f.at(2*time.Minute), byServer[last.ServerID].LastAnsweredAt.UTC())
}

func TestReadServedByFamily_NeverReturnsAnotherOrganizationsRows(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	other := newFailingFixture(t)
	other.server = f.server
	mine := f.onServer(person(), AppDCR)
	mine.ClientFamily = FamilyCursor
	theirs := other.onServer(person(), AppDCR)
	theirs.ClientFamily = FamilyClaudeCode
	f.served(mine, time.Minute)
	other.served(theirs, time.Minute)

	got, err := ReadServedByFamily(t.Context(), f.ch, f.org, []FailingPerson{{Episode: Episode{ServerKind: serverKindMCP, ServerID: f.server.String()}}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, FamilyCursor, got[0].ClientFamily)
	require.Equal(t, f.at(time.Minute), got[0].LastServedAt.UTC())
}

// The version comes from the row that named the client, never from another row of the attempt.
func TestFailingNow_TheVersionComesFromTheRowThatNamedTheClient(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	p := person()
	started := f.at(time.Hour)
	attempt := AttemptAt(uuid.New(), started, f.direct(AppDCR))
	named, err := RowAt(attempt, StepMCPRequest, CodeStaticKeyRejected, false, started, Subject(&p), client(FamilyCursor))
	require.NoError(t, err)
	generic, err := RowAt(attempt, StepMCPRequest, CodeStaticKeyRejected, true, started.Add(time.Second), Subject(&p), client(FamilyScript), version("8.7"))
	require.NoError(t, err)
	require.NoError(t, AppendRows(t.Context(), f.ch, []Row{named, generic}))

	got := f.failing()
	require.Len(t, got, 1)
	require.Equal(t, FamilyCursor, got[0].ClientFamily)
	require.Empty(t, got[0].ClientVersion)
}

func TestEpisode_BackingServerIDAndOnMCPServer(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	cases := []struct {
		name    string
		episode Episode
		backing string
		onMCP   bool
	}{
		{"a direct server", Episode{ServerKind: serverKindMCP, ServerID: strings.ToUpper(id.String())}, id.String(), true},
		{"a gateway member", Episode{ServerKind: serverKindMeta, ServerID: "gw", MemberID: id.String()}, id.String(), true},
		{"a gateway with no member", Episode{ServerKind: serverKindMeta, ServerID: id.String()}, "", true},
		{"a toolset", Episode{ServerKind: serverKindToolset, ServerID: id.String()}, "", true},
		{"an authorization server", Episode{ServerKind: serverKindAuthorizationServer, ServerID: "https://auth.example.com"}, "", false},
		{"no server", Episode{ServerKind: serverKindIdentityProvider, ServerID: ""}, "", false},
		{"an MCP server with no id", Episode{ServerKind: serverKindMCP, ServerID: ""}, "", false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.backing, tc.episode.BackingServerID(), tc.name)
		require.Equal(t, tc.onMCP, tc.episode.OnMCPServer(), tc.name)
	}
}

func TestReadServedByFamily_ReadsOnlyMCPServers(t *testing.T) {
	t.Parallel()
	f := newFailingFixture(t)
	issuer := Served{OrganizationID: f.org, ProjectID: f.project, ServerKind: serverKindAuthorizationServer, ServerID: "https://auth.example.com", SubjectURN: person().String(), ClientFamily: FamilyCursor}
	f.served(issuer, time.Minute)

	got, err := ReadServedByFamily(t.Context(), f.ch, f.org, []FailingPerson{{Episode: Episode{ServerKind: serverKindAuthorizationServer, ServerID: issuer.ServerID}}})
	require.NoError(t, err)
	require.Empty(t, got)
}
