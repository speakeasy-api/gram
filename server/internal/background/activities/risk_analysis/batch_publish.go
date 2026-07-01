package risk_analysis

import (
	"context"
	"log/slog"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

// publishAckTimeout keeps shadow-mode publish drains below the heartbeat window.
const publishAckTimeout = 10 * time.Second

func drainPublishAcks(ctx context.Context, logger *slog.Logger, warnMsg string, results []gcp.PublishResult) {
	waitParent := context.WithoutCancel(ctx)
	for _, res := range results {
		waitCtx, cancel := context.WithTimeout(waitParent, publishAckTimeout)
		_, err := res.Get(waitCtx)
		cancel()
		if err != nil {
			logger.WarnContext(ctx, warnMsg, attr.SlogError(err))
		}
		activity.RecordHeartbeat(ctx, "publish_ack")
	}
}
