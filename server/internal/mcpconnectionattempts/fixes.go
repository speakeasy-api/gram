package mcpconnectionattempts

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// FixedBy is what fixed a failed attempt.
type FixedBy string

const (
	// FixedByNothing is a failure nothing fixed, or an attempt that did not fail.
	FixedByNothing FixedBy = ""

	// FixedByAdminChange is a fix after an admin changed something the attempts used.
	FixedByAdminChange FixedBy = "admin_change"

	// FixedBySystemChange is a fix after Speakeasy changed something the attempts used.
	FixedBySystemChange FixedBy = "system_change"

	// FixedByReconnect is a fix where the person signed in again.
	FixedByReconnect FixedBy = "reconnect"

	// FixedByRetry is a fix by trying again with nothing else changed.
	FixedByRetry FixedBy = "retry"
)

// fixedByValues are every FixedBy value.
var fixedByValues = []FixedBy{FixedByNothing, FixedByAdminChange, FixedBySystemChange, FixedByReconnect, FixedByRetry}

// fixKey is the person, app and server a success must share with the failure it fixes.
type fixKey struct {
	organizationID string
	subjectURN     string
	appKey         string
	serverID       string
}

func fixKeyOf(a AttemptSummary) fixKey {
	return fixKey{organizationID: a.OrganizationID, subjectURN: a.SubjectURN, appKey: a.AppKey, serverID: a.ServerID}
}

// Successes are connected attempts grouped by person, app and server, for pairing failures.
type Successes struct {
	byKey map[fixKey][]AttemptSummary
	now   time.Time
}

// IndexSuccesses keeps the candidates that connected, as decided at now.
func IndexSuccesses(candidates []AttemptSummary, now time.Time) Successes {
	index := Successes{byKey: map[fixKey][]AttemptSummary{}, now: now}
	for _, c := range candidates {
		if c.SubjectURN != "" && c.End(now).Outcome == OutcomeConnected {
			index.byKey[fixKeyOf(c)] = append(index.byKey[fixKeyOf(c)], c)
		}
	}
	return index
}

// Fix is a failure and the success that followed it.
type Fix struct {
	// Failure is the failed attempt.
	Failure AttemptSummary

	// Failed is how the failure ended.
	Failed Ending

	// Success is the earliest connected attempt after it.
	Success AttemptSummary
}

// Next pairs a failure with the earliest success by the same person, app and server
// that started after it, within FixLookahead. A failure with no person has none.
func (s Successes) Next(failure AttemptSummary) (Fix, bool) {
	fix := Fix{Failure: failure, Failed: failure.End(s.now), Success: AttemptSummary{}} //nolint:exhaustruct // filled when found
	if failure.SubjectURN == "" {
		return fix, false
	}
	limit := failure.StartedAt.Add(FixLookahead)
	found := false
	for _, c := range s.byKey[fixKeyOf(failure)] {
		if c.AttemptID == failure.AttemptID || !c.StartedAt.After(failure.StartedAt) || !c.StartedAt.Before(limit) {
			continue
		}
		if !found || cmp.Or(c.StartedAt.Compare(fix.Success.StartedAt), slices.Compare(c.AttemptID[:], fix.Success.AttemptID[:])) < 0 {
			fix.Success, found = c, true
		}
	}
	return fix, found
}

// NextSuccess pairs a failure with the earliest success among candidates, as Successes.Next does.
func NextSuccess(failure AttemptSummary, candidates []AttemptSummary, now time.Time) (Fix, bool) {
	return IndexSuccesses(candidates, now).Next(failure)
}

// Targets is everything a change could have fixed: what either attempt touched.
func (f Fix) Targets() []string {
	ids := slices.Concat(f.Failure.Touched, f.Success.Touched)
	slices.Sort(ids)
	return slices.Compact(ids)
}

// FixedBy names what fixed the failure, given the audit changes in its organization.
// Only changes after the failing row and before the success count. An admin's change
// wins over Speakeasy's, a change wins over signing in again, and anything else is a retry.
func (f Fix) FixedBy(changes []Change) FixedBy {
	targets := f.Targets()
	system := false
	for _, c := range changes {
		if _, ok := slices.BinarySearch(targets, c.SubjectID); !ok {
			continue
		}
		if !c.At.After(f.Failed.At) || !c.At.Before(f.Success.StartedAt) {
			continue
		}
		if c.ByAdmin {
			return FixedByAdminChange
		}
		system = true
	}
	switch {
	case system:
		return FixedBySystemChange
	case f.Success.SignedIn:
		return FixedByReconnect
	default:
		return FixedByRetry
	}
}

// Change is one audit entry on something an attempt touched.
type Change struct {
	// SubjectID is the audited server, gateway, auth client, issuer or remote session client.
	SubjectID string

	// ByAdmin says the customer's admin made the change, not Speakeasy.
	ByAdmin bool

	// At is when the change was made.
	At time.Time
}

// IsAdminActor reports whether an audit actor is the customer's admin: a person or an
// API key, unless Speakeasy staff acted. user:system, system, agent and workload
// actors are Speakeasy.
func IsAdminActor(actorType, actorID string, internal bool) bool {
	if internal {
		return false
	}
	switch actorType {
	case string(urn.PrincipalTypeUser):
		return actorID != "system"
	case string(urn.PrincipalTypeAPIKey):
		return true
	default:
		return false
	}
}

// ListChanges reads one organization's audit entries on the given subjects between two times.
func ListChanges(ctx context.Context, db auditrepo.DBTX, organizationID string, subjects []string, after, before time.Time) ([]Change, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	entries, err := auditrepo.New(db).ListAuditActorsOnSubjects(ctx, auditrepo.ListAuditActorsOnSubjectsParams{
		OrganizationID: organizationID,
		SubjectIds:     subjects,
		After:          conv.ToPGTimestamptz(after),
		Before:         conv.ToPGTimestamptz(before),
	})
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	changes := make([]Change, 0, len(entries))
	for _, entry := range entries {
		changes = append(changes, Change{
			SubjectID: entry.SubjectID,
			ByAdmin:   IsAdminActor(entry.ActorType, entry.ActorID, entry.IsInternal),
			At:        entry.CreatedAt.Time,
		})
	}
	return changes, nil
}
