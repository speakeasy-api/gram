package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A committed membership does not mean anyone received anything: publishing the
// package can fail independently, and the server instructions tell the model not
// to narrate publication_state on its own. So the message has to carry the
// delivery outcome itself, or a failed publish reaches the administrator as
// silence.
func TestDistributionOutcomeMessageOnlyPromisesDeliveryOncePublished(t *testing.T) {
	t.Parallel()

	added := distributionOutcomeMessage("Support", publicationStateCurrent, false)
	require.Contains(t, added, "the people it is shared with will get it")

	pending := distributionOutcomeMessage("Support", publicationStatePending, false)
	require.Contains(t, pending, "has not finished updating yet")
	require.NotContains(t, pending, "will get it")

	failed := distributionOutcomeMessage("Support", publicationStateRepairRequired, false)
	require.Contains(t, failed, "they do not have it yet")
	require.NotContains(t, failed, "will get it")

	removed := distributionOutcomeMessage("Support", publicationStateCurrent, true)
	require.Contains(t, removed, "will stop getting it")

	removeFailed := distributionOutcomeMessage("Support", publicationStateRepairRequired, true)
	require.Contains(t, removeFailed, "they may still have it")
	require.NotContains(t, removeFailed, "will stop getting it")
}
