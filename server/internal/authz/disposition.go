package authz

// DispositionFromHints classifies a tool by its annotation hints into the one
// disposition a tool call carries. Every caller that authorizes calling or
// listing a tool uses it, so both see the same value.
//
//   - read-only, unless the tool is also destructive;
//   - destructive;
//   - idempotent;
//   - open-world;
//   - unannotated when no hint is true, including when there are none.
//
// A tool that says it is both read-only and destructive is classified
// destructive, the reading that grants less, and conflicting reports it so the
// caller can log it.
func DispositionFromHints(readOnly, destructive, idempotent, openWorld *bool) (disposition string, conflicting bool) {
	isTrue := func(hint *bool) bool { return hint != nil && *hint }
	switch {
	case isTrue(readOnly) && isTrue(destructive):
		return DispositionDestructive, true
	case isTrue(readOnly):
		return DispositionReadOnly, false
	case isTrue(destructive):
		return DispositionDestructive, false
	case isTrue(idempotent):
		return DispositionIdempotent, false
	case isTrue(openWorld):
		return DispositionOpenWorld, false
	default:
		return DispositionUnannotated, false
	}
}
