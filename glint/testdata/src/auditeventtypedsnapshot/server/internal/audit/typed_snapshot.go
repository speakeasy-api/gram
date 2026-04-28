package audit

type SnapshotPayload struct {
	Field string
}

// LogTypedSnapshotBadEvent uses bare any/interface{} snapshot fields and
// should be flagged on each.
type LogTypedSnapshotBadEvent struct {
	ID             string
	SnapshotBefore any         // want "use typed struct field type, such as \\*types.Example, instead of arbitrary typed data"
	SnapshotAfter  interface{} // want "use typed struct field type, such as \\*types.Example, instead of arbitrary typed data"
}

// LogTypedSnapshotGroupedBadEvent declares both snapshot fields on one line
// and exercises the multi-name field case.
type LogTypedSnapshotGroupedBadEvent struct {
	ID                            string
	SnapshotBefore, SnapshotAfter any // want "use typed struct field type, such as \\*types.Example, instead of arbitrary typed data"
}

// LogTypedSnapshotGoodEvent uses a typed snapshot and must not be flagged.
type LogTypedSnapshotGoodEvent struct {
	ID             string
	SnapshotBefore *SnapshotPayload
	SnapshotAfter  *SnapshotPayload
}

// LogTypedSnapshotNamedInterfaceEvent uses an interface that carries methods,
// which is permitted because the rule only flags the bare empty interface.
type marshaler interface {
	Marshal() ([]byte, error)
}

type LogTypedSnapshotNamedInterfaceEvent struct {
	ID             string
	SnapshotBefore marshaler
	SnapshotAfter  marshaler
}

// NotALogEventStruct holds an `any` snapshot field but does not match the
// Log*Event naming convention, so the rule must not flag it.
type NotALogEventStruct struct {
	SnapshotBefore any
	SnapshotAfter  any
}

// LogNoSnapshotEvent has no snapshot fields and must not be flagged.
type LogNoSnapshotEvent struct {
	ID string
}
