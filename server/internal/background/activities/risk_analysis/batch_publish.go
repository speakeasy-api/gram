package risk_analysis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

// publishAckTimeout keeps per-ack publish drains below the heartbeat window.
const publishAckTimeout = 10 * time.Second

// drainPublishAcks waits for every publish result, capping each ack wait and
// heartbeating between acks so a slow broker cannot starve the activity's
// heartbeat. It drains ALL results before reporting, then returns the joined
// failures: these publishes feed the ClickHouse findings pipeline, whose
// delivery contract is at-least-once, so callers must fail the activity and
// let Temporal redrive the batch. Redriven publishes are idempotent — finding
// and scan-request ids are deterministic, so replays converge instead of
// duplicating rows.
func drainPublishAcks(ctx context.Context, errMsg string, results []gcp.PublishResult) error {
	waitParent := context.WithoutCancel(ctx)
	var errs error
	for _, res := range results {
		waitCtx, cancel := context.WithTimeout(waitParent, publishAckTimeout)
		_, err := res.Get(waitCtx)
		cancel()
		errs = errors.Join(errs, err)
		activity.RecordHeartbeat(ctx, "publish_ack")
	}
	if errs != nil {
		return fmt.Errorf("%s: %w", errMsg, errs)
	}
	return nil
}
