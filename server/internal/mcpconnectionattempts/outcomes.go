package mcpconnectionattempts

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

// These are the read-time rules for how an attempt ended and what fixed a failure.
// Every reader uses them: the daily rollup and the per-person or per-server views.

const (
	// AttemptLiveWindow is how long an attempt with no final row may still be in
	// progress: each store restarts a 10-minute clock and a code lasts 10 more, so a
	// browser sign-in can stay live about 60 minutes.
	AttemptLiveWindow = 60 * time.Minute

	// FixLookahead is how long a later success can still fix a failure: a Friday failure fixed the next week.
	FixLookahead = 7 * 24 * time.Hour
)

// stepResultSignedIn marks a row where the person signed in.
var stepResultSignedIn = ResultSignedIn.name

// AttemptSummary is one attempt reduced from its rows. The ch tags match the
// columns of the SQL that reduces attempts in ClickHouse, which must agree with
// Summarize. They differ from the raw column names, which an aggregate alias must not shadow.
type AttemptSummary struct {
	// OrganizationID owns the attempt.
	OrganizationID string `ch:"organization_id"`

	// AttemptID is shared by the attempt's rows.
	AttemptID uuid.UUID `ch:"attempt_id"`

	// StartedAt is when the attempt started.
	StartedAt time.Time `ch:"started_at"`

	// HasFinal says a row ended the attempt.
	HasFinal bool `ch:"has_final"`

	// FinalStep is the step of the earliest final row.
	FinalStep string `ch:"final_step"`

	// FinalOutcome is the earliest final row's outcome.
	FinalOutcome string `ch:"final_outcome"`

	// FinalReason is the earliest final row's reason.
	FinalReason string `ch:"final_reason"`

	// FinalAt is the earliest final row's time, meaningful only with HasFinal.
	FinalAt time.Time `ch:"final_at"`

	// LastStep is the step of the latest row.
	LastStep string `ch:"last_step"`

	// LastReason is the latest row's reason.
	LastReason string `ch:"last_reason"`

	// LastAt is the latest row's time.
	LastAt time.Time `ch:"last_at"`

	// SubjectURN is the earliest person named, empty when no row names one.
	SubjectURN string `ch:"subject"`

	// AppKey is the app.
	AppKey string `ch:"app"`

	// ServerID is the server or gateway, empty when the attempt reached none.
	ServerID string `ch:"server"`

	// ServerKind is the kind of server, or provider_session or identity_provider for none.
	ServerKind string `ch:"kind"`

	// ProjectID is the project, nil for none.
	ProjectID uuid.UUID `ch:"project"`

	// RemoteSessionClientID is the earliest provider connection named, empty when none.
	RemoteSessionClientID string `ch:"remote_client"`

	// ProviderHost is the provider on the row that decided how the attempt ended (its
	// earliest final row, else its latest row), else the earliest provider named. A
	// gateway attempt that checks several members belongs to the member that ended it.
	ProviderHost string `ch:"provider"`

	// ClientFamily is the earliest AI app client named, otherwise the browser.
	ClientFamily string `ch:"client"`

	// Weight is how many attempts this one stands for: one plus the rows the limiter held back.
	Weight uint64 `ch:"weight"`

	// SignedIn says the person signed in during the attempt.
	SignedIn bool `ch:"signed_in"`

	// Touched are the server, members, auth client, issuers and remote session client the attempt used.
	Touched []string `ch:"touched"`
}

// rowOrder orders rows by time, then by the given values, so equal times break the
// same way in Go and in the SQL that sorts by the same tuple.
func rowOrder(a, b Row, values func(Row) []string) int {
	if c := a.EventTime.Compare(b.EventTime); c != 0 {
		return c
	}
	return slices.Compare(values(a), values(b))
}

func endingValues(r Row) []string { return []string{r.Step, r.Outcome, r.Reason} }

// earliest returns the earliest value among rows where keep holds, ties broken by the smallest value.
func earliest(rows []Row, keep func(Row) bool, value func(Row) string) string {
	var best *Row
	for i := range rows {
		if !keep(rows[i]) {
			continue
		}
		if best == nil || rowOrder(rows[i], *best, func(r Row) []string { return []string{value(r)} }) < 0 {
			best = &rows[i]
		}
	}
	if best == nil {
		return ""
	}
	return value(*best)
}

// Summarize reduces one attempt's rows, in any order. It is the reference for the
// SQL reduction, including how rows at the same time are broken.
func Summarize(rows []Row) AttemptSummary {
	var s AttemptSummary
	if len(rows) == 0 {
		return s
	}
	s.OrganizationID, s.AttemptID, s.AppKey, s.ServerID = rows[0].OrganizationID, rows[0].AttemptID, rows[0].AppKey, rows[0].ServerID
	s.ServerKind, s.ProjectID = rows[0].ServerKind, rows[0].ProjectID
	s.StartedAt = rows[0].AttemptStartedAt

	var final, last *Row
	touched := map[string]struct{}{}
	browser := false
	var maxRepeats uint32
	for i := range rows {
		row := &rows[i]
		if row.AttemptStartedAt.Before(s.StartedAt) {
			s.StartedAt = row.AttemptStartedAt
		}
		if row.IsFinal && (final == nil || rowOrder(*row, *final, endingValues) < 0) {
			final = row
		}
		if last == nil || rowOrder(*row, *last, func(r Row) []string { return []string{r.Step, r.Reason} }) > 0 {
			last = row
		}
		browser = browser || row.ClientFamily == FamilyBrowser
		maxRepeats = max(maxRepeats, row.Repeats)
		s.SignedIn = s.SignedIn || row.StepResult == stepResultSignedIn
		for _, id := range []string{row.ServerID, row.MemberID, row.AuthClientID, row.RemoteSessionIssuerID, row.UserSessionIssuerID, row.RemoteSessionClientID} {
			if id != "" {
				touched[id] = struct{}{}
			}
		}
	}
	if final != nil {
		s.HasFinal, s.FinalStep, s.FinalOutcome, s.FinalReason, s.FinalAt = true, final.Step, final.Outcome, final.Reason, final.EventTime
	}
	s.LastStep, s.LastReason, s.LastAt = last.Step, last.Reason, last.EventTime
	s.SubjectURN = earliest(rows, func(r Row) bool { return r.SubjectURN != "" }, func(r Row) string { return r.SubjectURN })
	s.RemoteSessionClientID = earliest(rows, func(r Row) bool { return r.RemoteSessionClientID != "" }, func(r Row) string { return r.RemoteSessionClientID })
	s.ProviderHost = last.ProviderHost
	if final != nil {
		s.ProviderHost = final.ProviderHost
	}
	if s.ProviderHost == "" {
		s.ProviderHost = earliest(rows, func(r Row) bool { return r.ProviderHost != "" }, func(r Row) string { return r.ProviderHost })
	}
	s.ClientFamily = earliest(rows, func(r Row) bool { return r.ClientFamily != "" && r.ClientFamily != FamilyBrowser }, func(r Row) string { return r.ClientFamily })
	if s.ClientFamily == "" && browser {
		s.ClientFamily = FamilyBrowser
	}
	s.Weight = uint64(maxRepeats) + 1
	for id := range touched {
		s.Touched = append(s.Touched, id)
	}
	slices.Sort(s.Touched)
	return s
}

// Ending is where and how an attempt ended.
type Ending struct {
	// Step is the step it ended at.
	Step string

	// Outcome is connected, failed, blocked, declined or abandoned.
	Outcome Outcome

	// Reason is the reason code, empty for success and abandoned.
	Reason string

	// At is the time of the row that decided it.
	At time.Time
}

// End decides how an attempt ended. Its earliest final row decides, even when rows
// follow it. Without one, an attempt past its live window whose latest row carries a
// reason stopped there: blocked or declined when the reason means that, else failed.
// Anything else was abandoned.
func (s AttemptSummary) End(now time.Time) Ending {
	if s.HasFinal {
		return Ending{Step: s.FinalStep, Outcome: Outcome(s.FinalOutcome), Reason: s.FinalReason, At: s.FinalAt}
	}
	if !now.Before(s.StartedAt.Add(AttemptLiveWindow)) && s.LastReason != "" {
		outcome := OutcomeFailed
		if meaning, ok := MeaningOf(s.LastReason); ok && (meaning.Outcome == OutcomeBlocked || meaning.Outcome == OutcomeDeclined) {
			outcome = meaning.Outcome
		}
		return Ending{Step: s.LastStep, Outcome: outcome, Reason: s.LastReason, At: s.LastAt}
	}
	return Ending{Step: s.LastStep, Outcome: OutcomeAbandoned, Reason: "", At: s.LastAt}
}

// NeedsFix reports whether an ending is a failure that a later success can fix.
func (e Ending) NeedsFix() bool {
	return e.Outcome == OutcomeFailed || e.Outcome == OutcomeBlocked
}
