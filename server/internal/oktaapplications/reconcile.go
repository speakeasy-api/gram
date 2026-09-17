// Package oktaapplications keeps a per-connection snapshot of the Okta
// applications and their user and group assignments, reconciled on a schedule.
//
// Consumer contract: rows are keyed by Okta application, user, and group ids,
// never by label. Read only rows with removed_at IS NULL on a connection whose
// status is verified, and treat a snapshot older than the connection's sync
// interval as stale. The snapshot is tenant-wide directory data: reads are
// org:admin, it is never joined into telemetry, and connection revocation
// deletes it.
package oktaapplications

import (
	"slices"
	"time"
)

// Principal kinds recorded on assignments.
const (
	PrincipalKindUser  = "user"
	PrincipalKindGroup = "group"
)

// Application is one Okta application as observed by a run.
type Application struct {
	ID          string
	Label       string
	Name        string
	SignOnMode  string
	Status      string
	Features    []string
	Created     time.Time
	LastUpdated time.Time
}

// Assignment is one principal assigned to an application.
type Assignment struct {
	AppID       string
	Kind        string
	PrincipalID string
	// Scope is USER for a direct user assignment and GROUP for a group-derived
	// one; empty for group assignments.
	Scope string
}

// AssignmentKey identifies an assignment row.
type AssignmentKey struct {
	AppID       string
	Kind        string
	PrincipalID string
}

func (a Assignment) key() AssignmentKey {
	return AssignmentKey{AppID: a.AppID, Kind: a.Kind, PrincipalID: a.PrincipalID}
}

// Snapshot is what one run observed in Okta after skipping internal apps.
type Snapshot struct {
	Applications []Application
	Assignments  []Assignment

	// Skipped lists the Okta-internal application ids left out.
	Skipped []string

	// ApplicationsTruncated is set when the application listing hit a cap;
	// applications missing from a truncated listing are not removed.
	ApplicationsTruncated bool

	// IncompleteAssignments holds application ids whose assignment listing
	// hit a cap; their missing assignments are not removed.
	IncompleteAssignments map[string]bool
}

// Truncated reports whether any listing in the snapshot hit a cap.
func (s Snapshot) Truncated() bool {
	return s.ApplicationsTruncated || len(s.IncompleteAssignments) > 0
}

// Diff is the change a snapshot implies against the live rows.
type Diff struct {
	AddedApplications   []string
	RemovedApplications []string
	AddedAssignments    []AssignmentKey
	RemovedAssignments  []AssignmentKey
}

// Reconcile computes the diff between the live rows and a snapshot. Apps
// absent from a truncated application listing stay live; assignments absent
// from an incomplete per-app listing stay live; assignments of a removed app
// are removed with it.
func Reconcile(liveApps []string, liveAssignments []AssignmentKey, snap Snapshot) Diff {
	seenApps := make(map[string]bool, len(snap.Applications))
	for _, app := range snap.Applications {
		seenApps[app.ID] = true
	}
	live := make(map[string]bool, len(liveApps))
	for _, id := range liveApps {
		live[id] = true
	}

	var diff Diff
	for _, app := range snap.Applications {
		if !live[app.ID] {
			diff.AddedApplications = append(diff.AddedApplications, app.ID)
		}
	}
	removedApps := map[string]bool{}
	if !snap.ApplicationsTruncated {
		for _, id := range liveApps {
			if !seenApps[id] {
				diff.RemovedApplications = append(diff.RemovedApplications, id)
				removedApps[id] = true
			}
		}
	}

	seenAssignments := make(map[AssignmentKey]bool, len(snap.Assignments))
	for _, a := range snap.Assignments {
		seenAssignments[a.key()] = true
	}
	liveAssignmentSet := make(map[AssignmentKey]bool, len(liveAssignments))
	for _, k := range liveAssignments {
		liveAssignmentSet[k] = true
	}
	for _, a := range snap.Assignments {
		if !liveAssignmentSet[a.key()] {
			diff.AddedAssignments = append(diff.AddedAssignments, a.key())
		}
	}
	for _, k := range liveAssignments {
		if seenAssignments[k] {
			continue
		}
		switch {
		case removedApps[k.AppID]:
			diff.RemovedAssignments = append(diff.RemovedAssignments, k)
		case seenApps[k.AppID] && !snap.IncompleteAssignments[k.AppID]:
			diff.RemovedAssignments = append(diff.RemovedAssignments, k)
		}
	}

	slices.Sort(diff.AddedApplications)
	slices.Sort(diff.RemovedApplications)
	slices.SortFunc(diff.AddedAssignments, compareAssignmentKeys)
	slices.SortFunc(diff.RemovedAssignments, compareAssignmentKeys)
	return diff
}

func compareAssignmentKeys(a, b AssignmentKey) int {
	if c := compareStrings(a.AppID, b.AppID); c != 0 {
		return c
	}
	if c := compareStrings(a.Kind, b.Kind); c != 0 {
		return c
	}
	return compareStrings(a.PrincipalID, b.PrincipalID)
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// internalApplication is an Okta-managed application excluded from the
// snapshot, matched on the exact template name (labels are admin-editable)
// and, when set, the sign-on mode.
type internalApplication struct {
	Name       string
	SignOnMode string
}

// InternalApplications are the Okta-managed apps every org carries.
var InternalApplications = []internalApplication{
	{Name: "okta_enduser", SignOnMode: ""},
	{Name: "okta_browser_plugin", SignOnMode: ""},
	{Name: "saasure", SignOnMode: ""},
	{Name: "okta_admin_console", SignOnMode: ""},
	{Name: "okta_flow_sso", SignOnMode: ""},
}

// IsInternalApplication reports whether an app is Okta-managed and skipped.
func IsInternalApplication(name, signOnMode string) bool {
	for _, rule := range InternalApplications {
		if rule.Name != name {
			continue
		}
		if rule.SignOnMode == "" || rule.SignOnMode == signOnMode {
			return true
		}
	}
	return false
}
