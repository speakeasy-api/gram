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
	_, err := countPublishAcks(ctx, errMsg, results)
	return err
}

// countPublishAcks waits for every result like drainPublishAcks and also
// reports how many were acknowledged, so a caller that tolerates a partial
// publish can count the messages that did reach the topic and the ones that
// did not (len(results) - acked) separately.
func countPublishAcks(ctx context.Context, errMsg string, results []gcp.PublishResult) (acked int, err error) {
	waitParent := context.WithoutCancel(ctx)
	var errs error
	for _, res := range results {
		waitCtx, cancel := context.WithTimeout(waitParent, publishAckTimeout)
		_, err := res.Get(waitCtx)
		cancel()
		if err == nil {
			acked++
		}
		errs = errors.Join(errs, err)
		activity.RecordHeartbeat(ctx, "publish_ack")
	}
	if errs != nil {
		return acked, fmt.Errorf("%s: %w", errMsg, errs)
	}
	return acked, nil
}
