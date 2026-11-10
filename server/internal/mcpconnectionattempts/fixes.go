package mcpconnectionattempts

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
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

	// FixedByAdminChange is a fix after the customer changed something the attempts
	// used: a person, an API key, or one of the customer's agents or workloads.
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

// fixKey is the person, app and connection a success must share with the failure it
// fixes. An attempt that reached no server is told apart by its project and provider
// connection, so one provider's refresh never fixes another's.
type fixKey struct {
	organizationID string
	subjectURN     string
	appKey         string
	serverKind     string
	serverID       string
	projectID      string
	remoteClientID string
}

func fixKeyOf(a AttemptSummary) fixKey {
	key := fixKey{organizationID: a.OrganizationID, subjectURN: a.SubjectURN, appKey: a.AppKey, serverKind: a.ServerKind, serverID: a.ServerID, projectID: "", remoteClientID: ""}
	if a.ServerID == "" {
		key.projectID, key.remoteClientID = a.ProjectID.String(), a.RemoteSessionClientID
	}
	return key
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

// Next pairs a failure with the earliest success by the same person, app and connection
// that started after the failing row, within FixLookahead of the failure's start. A
// failure with no person has none.
func (s Successes) Next(failure AttemptSummary) (Fix, bool) {
	fix := Fix{Failure: failure, Failed: failure.End(s.now), Success: AttemptSummary{}} //nolint:exhaustruct // filled when found
	if failure.SubjectURN == "" {
		return fix, false
	}
	limit := failure.StartedAt.Add(FixLookahead)
	found := false
	for _, c := range s.byKey[fixKeyOf(failure)] {
		if c.AttemptID == failure.AttemptID || !c.StartedAt.After(fix.Failed.At) || !c.StartedAt.Before(limit) {
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

	// ByAdmin says the customer made the change, not Speakeasy.
	ByAdmin bool

	// At is when the change was made.
	At time.Time
}

// readOnlyWords mark audit actions that read rather than change. An action counts as a
// change unless a word after the colon, split on - and _, is listed, so a new write
// action counts without being added here.
var readOnlyWords = []string{"read", "view", "list", "get", "access", "export", "reveal", "unmask", "observe", "call"}

// IsConfigChange reports whether an audit action changed configuration rather than read it.
func IsConfigChange(action string) bool {
	_, tail, ok := strings.Cut(action, ":")
	if !ok || tail == "" {
		return false
	}
	return !slices.ContainsFunc(strings.FieldsFunc(tail, func(r rune) bool { return r == '-' || r == '_' }), func(word string) bool {
		return slices.Contains(readOnlyWords, word)
	})
}

// IsAdminActor reports whether an audit actor is the customer: a person, an API key,
// or the customer's agent or workload, unless Speakeasy staff acted. user:system and
// system actors are Speakeasy.
func IsAdminActor(actorType, actorID string, internal bool) bool {
	if internal {
		return false
	}
	switch actorType {
	case string(urn.PrincipalTypeUser):
		return actorID != "system"
	case string(urn.PrincipalTypeAPIKey), string(urn.PrincipalTypeAgent), string(urn.PrincipalTypeWorkload):
		return true
	default:
		return false
	}
}

// ListChanges reads one organization's configuration changes on the given subjects between two times.
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
		if !IsConfigChange(entry.Action) {
			continue
		}
		changes = append(changes, Change{
			SubjectID: entry.SubjectID,
			ByAdmin:   IsAdminActor(entry.ActorType, entry.ActorID, entry.IsInternal),
			At:        entry.CreatedAt.Time,
		})
	}
	return changes, nil
}
