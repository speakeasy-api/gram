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

	added := distributionOutcomeMessage(outcome("Docs", "Support", publicationStateCurrent, true, false), false)
	require.Contains(t, added, "the people it is shared with will get it")

	pending := distributionOutcomeMessage(outcome("Docs", "Support", publicationStatePending, true, false), false)
	require.Contains(t, pending, "has not finished updating yet")
	require.NotContains(t, pending, "will get it")

	// repair_required cannot tell a first distribution from an update to one
	// already out there, so it reports the change as unlanded rather than
	// claiming nobody has the MCP server.
	failed := distributionOutcomeMessage(outcome("Docs", "Support", publicationStateRepairRequired, true, false), false)
	require.Contains(t, failed, "may not have reached them yet")
	require.NotContains(t, failed, "will get it")

	removed := distributionOutcomeMessage(outcome("Docs", "Support", publicationStateCurrent, false, false), true)
	require.Contains(t, removed, "will stop getting it")

	removeFailed := distributionOutcomeMessage(outcome("Docs", "Support", publicationStateRepairRequired, false, false), true)
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

	message := distributionOutcomeMessage(outcome("Docs", "Support", publicationStateCurrent, true, false), true)

	require.Contains(t, message, "an administrator added it to that plugin separately")
	require.Contains(t, message, "still get it")
	require.NotContains(t, message, "will stop getting it")
	require.Contains(t, message, `The MCP server "Docs"`)
}

// The administrator may have added several servers in one chat, so every
// outcome says which server it is about.
func TestDistributionOutcomeMessageNamesTheServer(t *testing.T) {
	t.Parallel()

	require.Contains(t, distributionOutcomeMessage(outcome("Cloudflare Docs", "Support", publicationStateCurrent, true, false), false), "The MCP server \"Cloudflare Docs\" is now part of the Support plugin")
	require.Contains(t, distributionOutcomeMessage(outcome("Cloudflare Docs", "Support", publicationStateCurrent, false, false), true), "The MCP server \"Cloudflare Docs\" is no longer part of the Support plugin")
	require.Contains(t, distributionOutcomeMessage(outcome("", "Support", publicationStateCurrent, true, false), false), "This MCP server is now part of the Support plugin")
}

func outcome(server, plugin, publicationState string, attachmentLive, awaitingManualSync bool) Distribution {
	return Distribution{
		State: "", Version: 0, AttachmentLive: attachmentLive, PublicationState: publicationState, Plugin: plugin,
		ShadowMCP: nil, MCPServer: server, AwaitingManualSync: awaitingManualSync,
	}
}

// A plugin whose auto sync is off keeps what it was last published with
// through the publish this flow runs, so even a publish that succeeded leaves
// the change waiting for a manual sync, and the message says how to send it.
func TestDistributionOutcomeMessageWaitsForAManualSyncWhenAutoSyncIsOff(t *testing.T) {
	t.Parallel()

	for _, state := range []string{publicationStateCurrent, publicationStatePending, publicationStateRepairRequired} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			added := distributionOutcomeMessage(outcome("Docs", "Support", state, true, true), false)
			require.Contains(t, added, "that plugin's auto sync is off, so the people it is shared with get it only after its project is synced by hand")
			require.Contains(t, added, "republish_plugin")
			require.NotContains(t, added, "will get it")

			removed := distributionOutcomeMessage(outcome("Docs", "Support", state, false, true), true)
			require.Contains(t, removed, "keep getting it until its project is synced by hand")
			require.NotContains(t, removed, "will stop getting it")
		})
	}

	// An attachment an administrator made by hand still stays, whatever the
	// plugin's auto sync.
	preserved := distributionOutcomeMessage(outcome("Docs", "Support", publicationStateCurrent, true, true), true)
	require.Contains(t, preserved, "an administrator added it to that plugin separately")
}
