package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A committed membership does not mean anyone received anything, and a removal
// does not mean anyone stopped: publishing can fail independently, and removal
// preserves an attachment an administrator made by hand. The instructions tell
// the model not to narrate publication_state on its own, so either divergence
// reaches the administrator as silence unless this message carries it.
func TestDistributionOutcomeMessageOnlyPromisesDeliveryOncePublished(t *testing.T) {
	t.Parallel()

	added := distributionOutcomeMessage("Support", publicationStateCurrent, true, false)
	require.Contains(t, added, "the people it is shared with will get it")

	pending := distributionOutcomeMessage("Support", publicationStatePending, true, false)
	require.Contains(t, pending, "has not finished updating yet")
	require.NotContains(t, pending, "will get it")

	// repair_required cannot tell a first distribution from an update to one
	// already out there, so it reports the change as unlanded rather than
	// claiming nobody has the MCP server.
	failed := distributionOutcomeMessage("Support", publicationStateRepairRequired, true, false)
	require.Contains(t, failed, "may not have reached them yet")
	require.NotContains(t, failed, "will get it")

	removed := distributionOutcomeMessage("Support", publicationStateCurrent, false, true)
	require.Contains(t, removed, "will stop getting it")

	removeFailed := distributionOutcomeMessage("Support", publicationStateRepairRequired, false, true)
	require.Contains(t, removeFailed, "may not have reached them yet")
	require.NotContains(t, removeFailed, "will stop getting it")
}

// Removal withdraws only the attachment this flow created. When an administrator
// attached the same MCP server by hand, that one survives and people keep
// receiving it, so the message must not report the opposite.
func TestDistributionOutcomeMessageReportsAPreservedAdministratorAttachment(t *testing.T) {
	t.Parallel()

	message := distributionOutcomeMessage("Support", publicationStateCurrent, true, true)

	require.Contains(t, message, "an administrator added it to that plugin separately")
	require.Contains(t, message, "still get it")
	require.NotContains(t, message, "will stop getting it")
}
