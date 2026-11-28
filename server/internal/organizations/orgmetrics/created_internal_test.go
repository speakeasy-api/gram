package orgmetrics

import "testing"

// A nil receiver and a nil instrument both degrade to no-ops rather than
// panicking, so a caller never needs to guard the metric.
func TestCreatedRecord_NilSafe(t *testing.T) {
	t.Parallel()

	var m *Created
	m.Record(t.Context(), CreationSourceSignup)

	empty := &Created{logger: nil, created: nil}
	empty.Record(t.Context(), CreationSourceSignup)
}
