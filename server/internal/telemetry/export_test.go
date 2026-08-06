package telemetry

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// QueryLegacyForTest mirrors Service.Query as it was before the semantic
// rewiring, reading the retained repo.QueryAttributeMetricsTable/Timeseries
// path directly. The parity tests compare it against the production (now
// semantic) Service.Query; it exists only while both paths do. skill_version
// payloads are not supported (they never left the legacy path in production).
func (s *Service) QueryLegacyForTest(ctx context.Context, payload *telem_gen.QueryPayload) (*telem_gen.QueryResult, error) {
	scope, err := s.resolveOrgQueryScope(ctx, payload.From, payload.To, nil)
	if err != nil {
		return nil, err
	}
	timeStart, timeEnd := scope.timeStart, scope.timeEnd

	groupBy := ""
	if payload.GroupBy != nil {
		groupBy = *payload.GroupBy
	}
	sortBy := payload.SortBy
	if sortBy == "" {
		sortBy = defaultQuerySortBy
	}
	topN := payload.TopN
	if topN == 0 {
		topN = defaultQueryTopN
	}

	interval := calculateInterval(timeStart, timeEnd)
	if payload.GranularitySeconds != nil && *payload.GranularitySeconds > 0 {
		interval = *payload.GranularitySeconds
	}
	if interval < minIntervalSeconds {
		interval = minIntervalSeconds
	}

	filters := make([]repo.AttributeMetricsFilter, 0, len(payload.Filters))
	for _, f := range payload.Filters {
		if f == nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "filters must not contain null entries")
		}
		filters = append(filters, repo.AttributeMetricsFilter{Dimension: f.Dimension, Values: f.Values})
	}

	params := repo.AttributeMetricsQueryParams{
		ProjectIDs:      scope.projectIDs,
		TimeStart:       timeStart,
		TimeEnd:         timeEnd,
		GroupBy:         groupBy,
		SortBy:          sortBy,
		Filters:         filters,
		IntervalSeconds: interval,
	}

	var (
		tableRows []repo.AttributeMetricsRow
		tsRows    []repo.AttributeMetricsTimePoint
	)
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		var egErr error
		tableRows, egErr = s.chRepo.QueryAttributeMetricsTable(egCtx, params)
		if egErr != nil {
			return fmt.Errorf("analytics table query: %w", egErr)
		}
		return nil
	})
	eg.Go(func() error {
		var egErr error
		tsRows, egErr = s.chRepo.QueryAttributeMetricsTimeseries(egCtx, params)
		if egErr != nil {
			return fmt.Errorf("analytics timeseries query: %w", egErr)
		}
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error running analytics query")
	}

	return buildQueryResult(groupBy, interval, timeStart, timeEnd, topN, tableRows, tsRows), nil
}

// InvalidateForTest drops the cached snapshot for the user so tests can
// simulate TTL expiry deterministically.
func (r *UserInfoResolver) InvalidateForTest(ctx context.Context, organizationID string, userID string) error {
	//nolint:wrapcheck // test-only helper
	return r.cache.DeleteByKey(ctx, userInfoSnapshotCacheKey(organizationID, userID))
}

// WaitForPublishDrains blocks until every ack-drain goroutine spawned by
// PublishLogs so far has finished — i.e. all publish results are resolved and
// the duration metric is recorded. Test-only synchronization barrier: callers
// must have already returned from the PublishLogs (or LogBulk) call whose
// drain they await, so the WaitGroup Add happens-before this Wait.
func (p *LogPublisher) WaitForPublishDrains() {
	p.drains.Wait()
}
