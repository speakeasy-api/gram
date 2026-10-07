package platformmcp

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/plugins"
)

// The publish_signal values a write reports once its transaction commits, for
// a write that may have changed what the project's plugin packages contain.
const (
	// publishSignalNotRequested means no signal was sent: either the write
	// could not have changed a package, or the transaction recorded a durable
	// publication request that already covers it.
	publishSignalNotRequested = "not_requested"

	// publishSignalUnavailable means a signal was needed but this server has no
	// publisher to send it with.
	publishSignalUnavailable = "unavailable"

	// publishSignalRequestFailed means the signal was sent and refused.
	publishSignalRequestFailed = "request_failed"

	// publishSignalBestEffortRequested means the debounced publish was asked
	// for. It is best effort: the publish itself runs in the background.
	publishSignalBestEffortRequested = "best_effort_requested"
)

// signalPublishAfterCommit asks for the project's plugin packages to be
// republished after a write that changed them has committed, and reports the
// publish_signal value for the result. publication is the outcome the write
// recorded for its durable publication request: an enqueued request already
// covers the publish, so only every other outcome is signalled. Whether the
// write could have changed a package at all is the caller's decision; call
// this only once it has.
func signalPublishAfterCommit(ctx context.Context, publisher plugins.PluginPublishSignaler, publication string, projectID uuid.UUID, userID string) string {
	outcome := plugins.ProjectPublicationRequestOutcome(publication)
	switch {
	case outcome == plugins.ProjectPublicationEnqueued:
		return publishSignalNotRequested
	case publisher == nil:
		return publishSignalUnavailable
	case plugins.SignalPluginPublishAfterRequest(ctx, publisher, outcome, projectID, userID) != nil:
		return publishSignalRequestFailed
	default:
		return publishSignalBestEffortRequested
	}
}
