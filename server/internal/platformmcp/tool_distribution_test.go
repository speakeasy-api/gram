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

	added := distributionOutcomeMessage("Docs", "Support", publicationStateCurrent, true, false)
	require.Contains(t, added, "the people it is shared with will get it")

	pending := distributionOutcomeMessage("Docs", "Support", publicationStatePending, true, false)
	require.Contains(t, pending, "has not finished updating yet")
	require.NotContains(t, pending, "will get it")

	// repair_required cannot tell a first distribution from an update to one
	// already out there, so it reports the change as unlanded rather than
	// claiming nobody has the MCP server.
	failed := distributionOutcomeMessage("Docs", "Support", publicationStateRepairRequired, true, false)
	require.Contains(t, failed, "may not have reached them yet")
	require.NotContains(t, failed, "will get it")

	removed := distributionOutcomeMessage("Docs", "Support", publicationStateCurrent, false, true)
	require.Contains(t, removed, "will stop getting it")

	removeFailed := distributionOutcomeMessage("Docs", "Support", publicationStateRepairRequired, false, true)
	require.Contains(t, removeFailed, "may not have reached them yet")
	require.NotContains(t, removeFailed, "will stop getting it")

	for _, message := range []string{added, pending, failed, removed, removeFailed} {
		require.Contains(t, message, `The MCP server "Docs"`)
	}
}

// Removal withdraws only the attachment this flow created. When an administrator
// attached the same MCP server by hand, that one survives and people keep
// receiving it, so the message must not report the opposite.
func TestDistributionOutcomeMessageReportsAPreservedAdministratorAttachment(t *testing.T) {
	t.Parallel()

	message := distributionOutcomeMessage("Docs", "Support", publicationStateCurrent, true, true)

	require.Contains(t, message, "an administrator added it to that plugin separately")
	require.Contains(t, message, "still get it")
	require.NotContains(t, message, "will stop getting it")
	require.Contains(t, message, `The MCP server "Docs"`)
}

// The administrator may have added several servers in one chat, so every
// outcome says which server it is about.
func TestDistributionOutcomeMessageNamesTheServer(t *testing.T) {
	t.Parallel()

	require.Contains(t, distributionOutcomeMessage("Cloudflare Docs", "Support", publicationStateCurrent, true, false), "The MCP server \"Cloudflare Docs\" is now part of the Support plugin")
	require.Contains(t, distributionOutcomeMessage("Cloudflare Docs", "Support", publicationStateCurrent, false, true), "The MCP server \"Cloudflare Docs\" is no longer part of the Support plugin")
	require.Contains(t, distributionOutcomeMessage("", "Support", publicationStateCurrent, true, false), "This MCP server is now part of the Support plugin")
}
