package falsepositivesweep

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
)

// chunkSize keeps each ClickHouse id IN-list under the default max_query_size.
const chunkSize = 1000

// Apply appends ClickHouse suppression copies for every live finding not yet dismissed.
func Apply(ctx context.Context, ch *chrepo.Queries, organizationID string, projectID uuid.UUID, ids []uuid.UUID, reasons []string, dryRun bool) (int64, error) {
	if len(ids) != len(reasons) {
		return 0, fmt.Errorf("finding ids and reasons have different lengths")
	}
	var changed int64
	for start := 0; start < len(ids); start += chunkSize {
		end := min(start+chunkSize, len(ids))
		n, err := applyChunk(ctx, ch, organizationID, projectID, ids[start:end], reasons[start:end], dryRun)
		changed += n
		if err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func applyChunk(ctx context.Context, ch *chrepo.Queries, organizationID string, projectID uuid.UUID, ids []uuid.UUID, reasons []string, dryRun bool) (int64, error) {
	states, err := ch.ListFalsePositiveStates(ctx, organizationID, projectID.String(), ids)
	if err != nil {
		return 0, fmt.Errorf("list ClickHouse finding states: %w", err)
	}
	reasonByID := make(map[uuid.UUID]string, len(ids))
	for i, id := range ids {
		reasonByID[id] = reasons[i]
	}

	groups := make(map[string][]uuid.UUID)
	var changed int64
	for _, state := range states {
		reason, ok := reasonByID[state.ID]
		if !ok || state.RuleSuppressed || state.Dismissed {
			continue
		}
		groups[reason] = append(groups[reason], state.ID)
		changed++
	}
	if dryRun || changed == 0 {
		return changed, nil
	}

	now := chrepo.FormatCHTime(time.Now())
	groupReasons := make([]string, 0, len(groups))
	for reason := range groups {
		groupReasons = append(groupReasons, reason)
	}
	sort.Strings(groupReasons)
	for _, reason := range groupReasons {
		if err := ch.AppendAutomatedFalsePositiveSuppression(ctx, organizationID, projectID.String(), now, now, reason, groups[reason]); err != nil {
			return 0, fmt.Errorf("append ClickHouse suppression copies: %w", err)
		}
	}
	return changed, nil
}
