package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/keys"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	// pluginAPIKeyReapBatchSize bounds the keys one transaction revokes.
	pluginAPIKeyReapBatchSize int32 = 200

	// pluginAPIKeyReapMaxBatchesPerAttempt bounds one attempt to 10,000 keys;
	// a larger backlog drains over the following hours.
	pluginAPIKeyReapMaxBatchesPerAttempt = 50
)

var pluginAPIKeyReap = hourlyCleanup{
	name:               "plugin-api-key-reaper",
	activityTimeout:    10 * time.Minute,
	maxAttempts:        3,
	retryInterval:      time.Minute,
	backoffCoefficient: 2,
	schedulingMargin:   7 * time.Minute,
}

// PluginAPIKeyReapWorkflow revokes stale plugin-minted API keys. One global
// hourly schedule and one activity cost about 1,440 Temporal actions per month
// per namespace (at most about 2,880 with retries), fixed rather than scaling
// with organizations or keys.
func PluginAPIKeyReapWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, pluginAPIKeyReap.activityOptions())
	var a *Activities
	return workflow.ExecuteActivity(ctx, a.ReapStalePluginAPIKeys).Get(ctx, nil)
}

// ReapStalePluginAPIKeys revokes stale plugin keys in bounded batches.
func (a *Activities) ReapStalePluginAPIKeys(ctx context.Context) error {
	total, err := reapPluginAPIKeyBatches(ctx, func(ctx context.Context, batchSize int32) (int, error) {
		return keys.ReapStalePluginAPIKeys(ctx, a.db, a.auditLogger, batchSize)
	})
	if total > 0 {
		a.logger.InfoContext(ctx, "reaped stale plugin api keys", attr.SlogValueInt(total))
	}
	return err
}

func reapPluginAPIKeyBatches(ctx context.Context, reap func(context.Context, int32) (int, error)) (int, error) {
	total := 0
	for range pluginAPIKeyReapMaxBatchesPerAttempt {
		n, err := reap(ctx, pluginAPIKeyReapBatchSize)
		if err != nil {
			return total, fmt.Errorf("reap plugin api key batch: %w", err)
		}
		total += n
		if n < int(pluginAPIKeyReapBatchSize) {
			return total, nil
		}
	}
	return total, nil
}

// AddPluginAPIKeyReapSchedule installs the hourly reaper for this task queue.
func AddPluginAPIKeyReapSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	return pluginAPIKeyReap.ensureSchedule(ctx, temporalEnv, PluginAPIKeyReapWorkflow)
}
