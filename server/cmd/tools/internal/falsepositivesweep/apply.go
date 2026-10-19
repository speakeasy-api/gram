package falsepositivesweep

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
)

const markPostgresBatch = `
UPDATE risk_results r
SET false_positive_at = now()
  , false_positive_reason = t.reason
FROM unnest($1::uuid[], $2::text[]) AS t(id, reason)
WHERE r.organization_id = $3
  AND r.project_id = $4
  AND r.id = t.id
  AND r.false_positive_at IS NULL
`

// chunkSize keeps each ClickHouse id IN-list under the default max_query_size.
const chunkSize = 1000

// Apply writes ClickHouse first, then mirrors every eligible finding to Postgres.
func Apply(ctx context.Context, pool *pgxpool.Pool, ch *chrepo.Queries, organizationID string, projectID uuid.UUID, ids []uuid.UUID, reasons []string, dryRun bool) (int64, error) {
	if len(ids) != len(reasons) {
		return 0, fmt.Errorf("finding ids and reasons have different lengths")
	}
	var changed int64
	for start := 0; start < len(ids); start += chunkSize {
		end := min(start+chunkSize, len(ids))
		n, err := applyChunk(ctx, pool, ch, organizationID, projectID, ids[start:end], reasons[start:end], dryRun)
		changed += n
		if err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func applyChunk(ctx context.Context, pool *pgxpool.Pool, ch *chrepo.Queries, organizationID string, projectID uuid.UUID, ids []uuid.UUID, reasons []string, dryRun bool) (int64, error) {

	states, err := ch.ListFalsePositiveStates(ctx, organizationID, projectID.String(), ids)
	if err != nil {
		return 0, fmt.Errorf("list ClickHouse finding states: %w", err)
	}
	reasonByID := make(map[uuid.UUID]string, len(ids))
	for i, id := range ids {
		reasonByID[id] = reasons[i]
	}

	groups := make(map[string][]uuid.UUID)
	mirrorIDs := make([]uuid.UUID, 0, len(states))
	mirrorReasons := make([]string, 0, len(states))
	for _, state := range states {
		reason, ok := reasonByID[state.ID]
		if !ok || state.RuleSuppressed {
			continue
		}
		mirrorIDs = append(mirrorIDs, state.ID)
		mirrorReasons = append(mirrorReasons, reason)
		if !state.Dismissed {
			groups[reason] = append(groups[reason], state.ID)
		}
	}

	var changed int64
	for _, groupedIDs := range groups {
		changed += int64(len(groupedIDs))
	}
	if dryRun || len(mirrorIDs) == 0 {
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

	if _, err := pool.Exec(ctx, markPostgresBatch, mirrorIDs, mirrorReasons, organizationID, projectID); err != nil {
		return 0, fmt.Errorf("mirror Postgres false positives: %w", err)
	}
	return changed, nil
}
