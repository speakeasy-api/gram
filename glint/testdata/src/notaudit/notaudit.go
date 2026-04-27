package notaudit

// LogTypedSnapshotBadEvent matches the Log*Event name pattern but lives in a
// non-audit package, so all three audit-event rules must skip it.
type LogTypedSnapshotBadEvent struct {
	OrganizationID string
	ResourceID     string
	ResourceURN    string
	SnapshotBefore any
	SnapshotAfter  any
}
