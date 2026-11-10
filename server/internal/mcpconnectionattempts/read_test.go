package mcpconnectionattempts

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TestSQLAgreesWithSummarize checks the ClickHouse reduction against the Go reference.
func TestSQLAgreesWithSummarize(t *testing.T) {
	t.Parallel()
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	org := "org-" + uuid.NewString()
	now := time.Now().UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -2)
	at := func(server uuid.UUID, app App, started time.Time) Attempt {
		return AttemptAt(uuid.New(), started, ServerScope(org, uuid.New(), server, app))
	}
	row := func(a Attempt, step Step, what Happened, final bool, after time.Duration, opts ...Option) Row {
		t.Helper()
		r, err := RowAt(a, step, what, final, a.StartedAt().Add(after), opts...)
		require.NoError(t, err)
		return r
	}
	server, member := uuid.New(), uuid.New()
	subject := urn.NewUserSubject("user-" + uuid.NewString())
	person := Subject(&subject)
	app := AppOf("https://claude.ai/oauth/claude-code-client-metadata", ClientIDAdmitted)

	var rows []Row
	add := func(r Row) { rows = append(rows, r) }

	a1 := at(server, app, day.Add(time.Hour))
	first := row(a1, StepInboundAuthorize, ResultAccepted, false, 0)
	first.ClientFamily = FamilyBrowser
	add(first)
	signIn := row(a1, StepProviderSignIn, ResultSignedIn, false, time.Minute, person, Provider("https://accounts.google.com"))
	signIn.RemoteSessionIssuerID, signIn.RemoteSessionClientID = "issuer-1", "remote-client-1"
	add(signIn)
	final := row(a1, StepInboundToken, ResultCodeIssued, true, 2*time.Minute, person)
	final.ClientFamily, final.AuthClientID, final.UserSessionIssuerID = FamilyCodexCLI, "auth-client-1", "user-issuer-1"
	add(final)
	add(row(a1, StepDiscoveryMember, CodeNeedsSignIn, false, 3*time.Minute, Member(member)))

	a2 := at(server, app, day.Add(2*time.Hour))
	add(row(a2, StepInboundAuthorize, ResultAccepted, false, 0))
	add(row(a2, StepProviderSignIn, CodeProviderRateLimited, false, time.Minute, person))

	a3 := at(server, AppDCR, day.Add(3*time.Hour))
	once := row(a3, StepMCPRequest, CodeInvalidBearerToken, true, 0, person)
	once.Repeats = 9
	add(once)

	// A gateway whose members sign in to different providers, two of them at the same time.
	gateway := AttemptAt(uuid.New(), day.Add(4*time.Hour), GatewayScope(org, uuid.New(), uuid.New(), app))
	add(row(gateway, StepDiscoveryMember, CodeNeedsSignIn, false, 2*time.Minute, Member(uuid.New()), Provider("https://slack.com")))
	add(row(gateway, StepDiscoveryMember, CodeNeedsSignIn, false, time.Minute, Member(uuid.New()), Provider("https://mcp.linear.app")))
	add(row(gateway, StepDiscoveryMember, ResultListed, false, time.Minute, Member(uuid.New()), Provider("https://github.com")))
	add(row(gateway, StepDiscoveryListing, ResultComplete, true, 3*time.Minute))
	add(row(gateway, StepMCPRequest, CodeInvalidBearerToken, true, 3*time.Minute))

	// Two people, and two clients at the same time.
	other := urn.NewUserSubject("user-" + uuid.NewString())
	twoPeople := at(server, app, day.Add(5*time.Hour))
	later := row(twoPeople, StepInboundToken, CodePKCEMismatch, false, 2*time.Minute, Subject(&other))
	later.ClientFamily = FamilyCursor
	earlier := row(twoPeople, StepInboundSignIn, ResultSignedIn, false, time.Minute, person)
	earlier.ClientFamily = FamilyZed
	tied := row(twoPeople, StepInboundSignIn, ResultSignedIn, false, time.Minute, Subject(&other))
	tied.ClientFamily = FamilyClaudeCode
	add(later)
	add(earlier)
	add(tied)
	require.NoError(t, AppendRows(t.Context(), ch, rows))

	byAttempt := map[uuid.UUID][]Row{}
	for _, r := range rows {
		byAttempt[r.AttemptID] = append(byAttempt[r.AttemptID], r)
	}

	var fromSQL []AttemptSummary
	filter := AttemptFilter{StartedFrom: day, StartedBefore: day.AddDate(0, 0, 1), OrganizationID: org}
	require.NoError(t, ReadAttempts(t.Context(), ch, filter, func(s AttemptSummary) { fromSQL = append(fromSQL, s) }))
	require.Len(t, fromSQL, len(byAttempt))
	for _, got := range fromSQL {
		want := Summarize(byAttempt[got.AttemptID])
		got.StartedAt, want.StartedAt = got.StartedAt.UTC(), want.StartedAt.UTC()
		got.LastAt, want.LastAt = got.LastAt.UTC(), want.LastAt.UTC()
		got.FinalAt, want.FinalAt = got.FinalAt.UTC(), want.FinalAt.UTC()
		if !got.HasFinal {
			got.FinalAt = time.Time{}
		}
		if len(got.Touched) == 0 {
			got.Touched = nil
		}
		require.Equal(t, want, got, "attempt %s", got.AttemptID)
		require.Equal(t, want.End(now), got.End(now))
	}

	// Selecting by a person keeps whole attempts, including rows that do not name them.
	byPerson := filter
	byPerson.PeopleFailingFrom, byPerson.PeopleFailingBefore = day, day.AddDate(0, 0, 1)
	var personal []AttemptSummary
	require.NoError(t, ReadAttempts(t.Context(), ch, byPerson, func(s AttemptSummary) { personal = append(personal, s) }))
	ids := make([]uuid.UUID, 0, len(personal))
	for _, got := range personal {
		ids = append(ids, got.AttemptID)
	}
	require.ElementsMatch(t, []uuid.UUID{a1.ID(), a2.ID(), a3.ID(), twoPeople.ID()}, ids, "the gateway attempt names no person")
	for _, got := range personal {
		want := Summarize(byAttempt[got.AttemptID])
		require.Equal(t, want.LastStep, got.LastStep, "attempt %s", got.AttemptID)
		require.Equal(t, want.HasFinal, got.HasFinal, "attempt %s", got.AttemptID)
		require.Equal(t, want.Touched, got.Touched, "attempt %s", got.AttemptID)
	}
}

func TestReadAttemptsNeedsAStartRange(t *testing.T) {
	t.Parallel()
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	err = ReadAttempts(t.Context(), ch, AttemptFilter{OrganizationID: "org-any"}, func(AttemptSummary) {})
	require.Error(t, err)
}

func TestReadAttemptsNeedsBothEndsOfThePeopleRange(t *testing.T) {
	t.Parallel()
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	now := time.Now().UTC()
	for _, filter := range []AttemptFilter{
		{StartedFrom: now.Add(-time.Hour), StartedBefore: now, PeopleFailingFrom: now.Add(-time.Hour)},
		{StartedFrom: now.Add(-time.Hour), StartedBefore: now, PeopleFailingBefore: now},
	} {
		require.Error(t, ReadAttempts(t.Context(), ch, filter, func(AttemptSummary) {}))
	}
}

func TestReadAttemptsFixCandidatesOnly(t *testing.T) {
	t.Parallel()
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	org := "org-" + uuid.NewString()
	day := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -2)
	scope := ServerScope(org, uuid.New(), uuid.New(), AppDCR)
	connected, failed, open := AttemptAt(uuid.New(), day.Add(time.Hour), scope), AttemptAt(uuid.New(), day.Add(time.Hour), scope), AttemptAt(uuid.New(), day.Add(time.Hour), scope)
	var rows []Row
	for _, step := range []struct {
		attempt Attempt
		what    Happened
		final   bool
	}{
		{connected, ResultCodeIssued, true},
		{failed, CodeInvalidGrant, true},
		{open, ResultAccepted, false},
	} {
		row, err := RowAt(step.attempt, StepInboundToken, step.what, step.final, day.Add(time.Hour+time.Minute))
		require.NoError(t, err)
		rows = append(rows, row)
	}
	require.NoError(t, AppendRows(t.Context(), ch, rows))

	filter := AttemptFilter{StartedFrom: day, StartedBefore: day.AddDate(0, 0, 1), OrganizationID: org, FixCandidatesOnly: true}
	got := map[uuid.UUID]Outcome{}
	require.NoError(t, ReadAttempts(t.Context(), ch, filter, func(s AttemptSummary) {
		got[s.AttemptID] = s.End(time.Now()).Outcome
	}))
	require.Equal(t, map[uuid.UUID]Outcome{connected.ID(): OutcomeConnected, failed.ID(): OutcomeFailed}, got, "the open attempt has no reason")
}

func TestReadAttemptsBreaksAProviderTieLikeSummarize(t *testing.T) {
	t.Parallel()
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	org := "org-" + uuid.NewString()
	day := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -2)
	scope := GatewayScope(org, uuid.New(), uuid.New(), AppDCR)
	at := day.Add(time.Hour + time.Minute)

	var rows []Row
	for _, tied := range []struct {
		attempt Attempt
		what    Happened
		final   bool
	}{
		{AttemptAt(uuid.New(), day.Add(time.Hour), scope), CodeProviderUnavailable, true},
		{AttemptAt(uuid.New(), day.Add(time.Hour), scope), CodeProviderUnavailable, false},
	} {
		for _, host := range []string{"https://linear.app", "https://accounts.google.com"} {
			row, err := RowAt(tied.attempt, StepProviderToken, tied.what, tied.final, at, Provider(host))
			require.NoError(t, err)
			rows = append(rows, row)
		}
	}
	require.NoError(t, AppendRows(t.Context(), ch, rows))

	byAttempt := map[uuid.UUID][]Row{}
	for _, r := range rows {
		byAttempt[r.AttemptID] = append(byAttempt[r.AttemptID], r)
	}
	filter := AttemptFilter{StartedFrom: day, StartedBefore: day.AddDate(0, 0, 1), OrganizationID: org}
	seen := 0
	require.NoError(t, ReadAttempts(t.Context(), ch, filter, func(s AttemptSummary) {
		seen++
		require.Equal(t, Summarize(byAttempt[s.AttemptID]).ProviderHost, s.ProviderHost, "attempt %s", s.AttemptID)
	}))
	require.Equal(t, 2, seen)
}
