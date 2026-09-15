package background

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The run timeout exists to outlive a full-length run *and* the compensation
// that resolves the report row afterwards. If it ever stops doing that, a
// workflow timeout kills the compensation along with the run and the report
// stays in running forever — the page polls it, and its Run button never
// re-enables. The relationship was a sentence in a comment; this is the
// sentence as an assertion, so raising one of the budgets fails here instead
// of in production.
func TestMcpResearchWorkflow_RunTimeoutOutlivesItsActivities(t *testing.T) {
	t.Parallel()

	require.Greater(t, mcpResearchScheduleToCloseTimeout, mcpResearchRunActivityTimeout,
		"schedule-to-close must leave room for queue time on top of a full run")

	// The compensation's window has to fit its own retries, or the retry
	// policy is a promise the schedule-to-close will not keep.
	require.GreaterOrEqual(t,
		mcpResearchCompensationScheduleToClose,
		mcpResearchCompensationBudget(),
		"the compensation window must fit every attempt and the backoff between them",
	)

	// The invariant rests on the bounded windows, not on the retry budget:
	// queue time counts against the workflow and against neither
	// StartToClose, so only schedule-to-close bounds what a saturated queue
	// can cost.
	require.Greater(t,
		mcpResearchRunTimeout,
		mcpResearchScheduleToCloseTimeout+mcpResearchCompensationScheduleToClose,
		"the workflow must outlive a maximally slow run plus its compensation",
	)
}

// The compensation budget counts what a worst case actually costs: every
// attempt running to its own timeout, plus the backoff waited between them.
func TestMcpResearchCompensationBudget_CountsAttemptsAndBackoff(t *testing.T) {
	t.Parallel()

	policy := mcpResearchCompensationRetryPolicy()

	// 5 attempts × 30s, plus backoff of 5s, 10s, 20s, 40s between them.
	require.Equal(t,
		5*mcpResearchCompensationAttemptTimeout+75*time.Second,
		mcpResearchCompensationBudget(),
	)
	require.EqualValues(t, 5, policy.MaximumAttempts)
}
