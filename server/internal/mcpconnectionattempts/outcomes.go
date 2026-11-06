package mcpconnectionattempts

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// These are the read-time rules for how an attempt ended and what fixed a failure.
// Every reader uses them: the daily rollup and the per-person or per-server views.

const (
	// AttemptLiveWindow is how long an attempt with no final row may still be in
	// progress: each store restarts a 10-minute clock and a code lasts 10 more, so a
	// browser sign-in can stay live about 60 minutes.
	AttemptLiveWindow = 60 * time.Minute

	// RecoveryWindow is how long after a failed attempt a success by the same person,
	// app and server still counts as fixing it. Seven days covers a failure on a
	// Friday that an admin fixes the next week, and stays well inside the 30-day raw
	// retention so a day can be rebuilt with its whole window.
	RecoveryWindow = 7 * 24 * time.Hour
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

	// FinalStep, FinalOutcome and FinalReason come from the earliest final row.
	FinalStep string `ch:"final_step"`

	// FinalOutcome is the earliest final row's outcome.
	FinalOutcome string `ch:"final_outcome"`

	// FinalReason is the earliest final row's reason.
	FinalReason string `ch:"final_reason"`

	// LastStep and LastReason come from the latest row.
	LastStep string `ch:"last_step"`

	// LastReason is the latest row's reason.
	LastReason string `ch:"last_reason"`

	// SubjectURN is the person, empty when no row names one.
	SubjectURN string `ch:"subject"`

	// AppKey is the app.
	AppKey string `ch:"app"`

	// ServerID is the server or gateway, empty when the attempt reached none.
	ServerID string `ch:"server"`

	// ProviderHost is a provider any row names.
	ProviderHost string `ch:"provider"`

	// ClientFamily is the AI app's client when a row names one, otherwise the browser.
	ClientFamily string `ch:"client"`

	// Weight is how many attempts this one stands for: one plus the rows the limiter held back.
	Weight uint64 `ch:"weight"`

	// SignedIn says the person signed in during the attempt.
	SignedIn bool `ch:"signed_in"`

	// Touched are the server, members, auth client, issuers and remote session client the attempt used.
	Touched []string `ch:"touched"`
}

// Summarize reduces one attempt's rows, in any order. It is the reference for the
// SQL reduction. Rows with equal times keep their slice order.
func Summarize(rows []Row) AttemptSummary {
	var s AttemptSummary
	if len(rows) == 0 {
		return s
	}
	sorted := slices.Clone(rows)
	slices.SortStableFunc(sorted, func(a, b Row) int { return a.EventTime.Compare(b.EventTime) })

	s.OrganizationID, s.AttemptID, s.AppKey, s.ServerID = sorted[0].OrganizationID, sorted[0].AttemptID, sorted[0].AppKey, sorted[0].ServerID
	s.StartedAt = sorted[0].AttemptStartedAt
	touched := map[string]struct{}{}
	browser := ""
	var maxRepeats uint32
	for _, row := range sorted {
		if row.AttemptStartedAt.Before(s.StartedAt) {
			s.StartedAt = row.AttemptStartedAt
		}
		if row.IsFinal && !s.HasFinal {
			s.HasFinal, s.FinalStep, s.FinalOutcome, s.FinalReason = true, row.Step, row.Outcome, row.Reason
		}
		if s.SubjectURN == "" {
			s.SubjectURN = row.SubjectURN
		}
		if s.ProviderHost == "" {
			s.ProviderHost = row.ProviderHost
		}
		switch {
		case row.ClientFamily == FamilyBrowser:
			browser = FamilyBrowser
		case row.ClientFamily != "" && s.ClientFamily == "":
			s.ClientFamily = row.ClientFamily
		}
		maxRepeats = max(maxRepeats, row.Repeats)
		if row.StepResult == stepResultSignedIn {
			s.SignedIn = true
		}
		for _, id := range []string{row.ServerID, row.MemberID, row.AuthClientID, row.RemoteSessionIssuerID, row.UserSessionIssuerID, row.RemoteSessionClientID} {
			if id != "" {
				touched[id] = struct{}{}
			}
		}
	}
	last := sorted[len(sorted)-1]
	s.LastStep, s.LastReason = last.Step, last.Reason
	if s.ClientFamily == "" {
		s.ClientFamily = browser
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
}

// End decides how an attempt ended. Its earliest final row decides, even when rows
// follow it. Without one, an attempt past its live window whose latest row carries a
// reason stopped on that failure. Anything else was abandoned.
func (s AttemptSummary) End(now time.Time) Ending {
	if s.HasFinal {
		return Ending{Step: s.FinalStep, Outcome: Outcome(s.FinalOutcome), Reason: s.FinalReason}
	}
	if !now.Before(s.StartedAt.Add(AttemptLiveWindow)) && s.LastReason != "" {
		return Ending{Step: s.LastStep, Outcome: OutcomeFailed, Reason: s.LastReason}
	}
	return Ending{Step: s.LastStep, Outcome: OutcomeAbandoned, Reason: ""}
}

// NeedsFix reports whether an ending is a failure that a later success can fix.
func (e Ending) NeedsFix() bool {
	return e.Outcome == OutcomeFailed || e.Outcome == OutcomeBlocked
}

// FixedBy is what fixed a failed attempt.
type FixedBy string

const (
	FixedByNothing      FixedBy = ""
	FixedByAdminChange  FixedBy = "admin_change"
	FixedBySystemChange FixedBy = "system_change"
	FixedByReconnect    FixedBy = "reconnect"
	FixedByRetry        FixedBy = "retry"
)

// sameTarget reports whether two attempts share person, app and server.
func sameTarget(a, b AttemptSummary) bool {
	return a.OrganizationID == b.OrganizationID && a.SubjectURN == b.SubjectURN && a.AppKey == b.AppKey && a.ServerID == b.ServerID
}

// NextSuccess finds the earliest connected attempt by the same person, app and server
// that started after the failure, within the recovery window. now decides how each
// candidate ended. A failure with no person has nothing to pair with.
func NextSuccess(failure AttemptSummary, candidates []AttemptSummary, now time.Time) (AttemptSummary, bool) {
	var best AttemptSummary
	found := false
	if failure.SubjectURN == "" {
		return best, false
	}
	limit := failure.StartedAt.Add(RecoveryWindow)
	for _, c := range candidates {
		if c.AttemptID == failure.AttemptID || !sameTarget(failure, c) {
			continue
		}
		if !c.StartedAt.After(failure.StartedAt) || !c.StartedAt.Before(limit) {
			continue
		}
		if c.End(now).Outcome != OutcomeConnected {
			continue
		}
		if !found || c.StartedAt.Before(best.StartedAt) {
			best, found = c, true
		}
	}
	return best, found
}

// Change is one audit entry on something an attempt touched.
type Change struct {
	// SubjectID is the audited server, gateway, auth client, issuer or remote session client.
	SubjectID string

	// ByPerson says a person made the change, not Speakeasy.
	ByPerson bool

	// At is when the change was made.
	At time.Time
}

// IsPersonActor reports whether an audit actor is a person. Older background writers
// audit as user:system, which is not a person.
func IsPersonActor(actorType, actorID string) bool {
	return actorType == string(urn.PrincipalTypeUser) && actorID != "system"
}

// FixTargets is everything a change could have fixed between failure and success.
func FixTargets(failure, success AttemptSummary) []string {
	ids := slices.Concat(failure.Touched, success.Touched)
	slices.Sort(ids)
	return slices.Compact(ids)
}

// FixedByOf names what fixed a failure, given the success that followed and the audit
// changes in its organization. A person's change wins over Speakeasy's, a change wins
// over signing in again, and anything else is a plain retry.
func FixedByOf(failure, success AttemptSummary, changes []Change) FixedBy {
	targets := FixTargets(failure, success)
	system := false
	for _, c := range changes {
		if _, ok := slices.BinarySearch(targets, c.SubjectID); !ok {
			continue
		}
		if !c.At.After(failure.StartedAt) || !c.At.Before(success.StartedAt) {
			continue
		}
		if c.ByPerson {
			return FixedByAdminChange
		}
		system = true
	}
	switch {
	case system:
		return FixedBySystemChange
	case success.SignedIn:
		return FixedByReconnect
	default:
		return FixedByRetry
	}
}
