package telemetry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/telemetryerrs"
)

// minIntervalSeconds is the finest timeseries bucket telemetry.query supports.
// The source aggregate (attribute_metrics_summaries) is bucketed hourly, so
// anything finer would just return sparse hourly data.
const minIntervalSeconds int64 = 3600

// otherGroupLabel is the default synthetic group value that holds the rolled-up
// remainder beyond top_n. If a real group already uses this value, the response
// picks a suffixed label so the synthetic rollup cannot collide with user data.
const otherGroupLabel = "Other"

type listSessionsCursor struct {
	SortValue  float64 `json:"sort_value"`
	GramChatID string  `json:"gram_chat_id"`
}

// Query is a generic, org-scoped analytics query over the pre-aggregated
// attribute_metrics_summaries view. It returns both a grouped table and a
// matching per-group hourly timeseries for the same slice of data.
func (s *Service) Query(ctx context.Context, payload *telem_gen.QueryPayload) (*telem_gen.QueryResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Org-scoped: the query spans every project in the organization.
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}
	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	projects, err := s.projectsRepo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list organization projects")
	}
	projectIDs := make([]string, 0, len(projects))
	for _, p := range projects {
		projectIDs = append(projectIDs, p.ID.String())
	}

	groupBy := ""
	if payload.GroupBy != nil {
		groupBy = *payload.GroupBy
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
		filters = append(filters, repo.AttributeMetricsFilter{Dimension: f.Dimension, Values: f.Values})
	}

	params := repo.AttributeMetricsQueryParams{
		ProjectIDs:      projectIDs,
		TimeStart:       timeStart,
		TimeEnd:         timeEnd,
		GroupBy:         groupBy,
		SortBy:          payload.SortBy,
		Filters:         filters,
		IntervalSeconds: interval,
	}

	tableRows, err := s.chRepo.QueryAttributeMetricsTable(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error running analytics table query")
	}
	tsRows, err := s.chRepo.QueryAttributeMetricsTimeseries(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error running analytics timeseries query")
	}

	return buildQueryResult(groupBy, interval, timeStart, timeEnd, payload.TopN, tableRows, tsRows), nil
}

// ListSessions returns org-scoped chat sessions for a filtered analytics slice.
func (s *Service) ListSessions(ctx context.Context, payload *telem_gen.ListSessionsPayload) (*telem_gen.ListSessionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}
	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	limit := payload.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 1000 {
		return nil, oops.E(oops.CodeBadRequest, nil, "limit must be between 1 and 1000")
	}

	sortBy := payload.SortBy
	if sortBy == "" {
		sortBy = "total_cost"
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	projects, err := s.projectsRepo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list organization projects")
	}
	projectIDs := make([]string, 0, len(projects))
	for _, p := range projects {
		projectIDs = append(projectIDs, p.ID.String())
	}

	var cursorSortValue *float64
	var cursorGramChatID string
	if payload.Cursor != nil && *payload.Cursor != "" {
		cursor, err := decodeListSessionsCursor(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor")
		}
		cursorSortValue = &cursor.SortValue
		cursorGramChatID = cursor.GramChatID
	}

	filters := make([]repo.AttributeMetricsFilter, 0, len(payload.Filters))
	for _, f := range payload.Filters {
		if f == nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "filters must not contain null entries")
		}
		filters = append(filters, repo.AttributeMetricsFilter{Dimension: f.Dimension, Values: f.Values})
	}

	items, err := s.chRepo.ListSessions(ctx, repo.ListSessionsParams{
		ProjectIDs:       projectIDs,
		TimeStart:        timeStart,
		TimeEnd:          timeEnd,
		Filters:          filters,
		SortBy:           sortBy,
		CursorSortValue:  cursorSortValue,
		CursorGramChatID: cursorGramChatID,
		Limit:            limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing sessions")
	}

	var nextCursor *string
	if len(items) > limit {
		next := encodeListSessionsCursor(items[limit-1].SortValue, items[limit-1].GramChatID)
		nextCursor = &next
		items = items[:limit]
	}

	sessions := make([]*telem_gen.SessionSummary, len(items))
	for i, item := range items {
		sessions[i] = &telem_gen.SessionSummary{
			GramChatID:        item.GramChatID,
			ProjectID:         item.ProjectID,
			UserEmail:         item.UserEmail,
			HookSource:        item.HookSource,
			Model:             item.Model,
			StartTimeUnixNano: strconv.FormatInt(item.StartTimeUnixNano, 10),
			EndTimeUnixNano:   strconv.FormatInt(item.EndTimeUnixNano, 10),
			DurationSeconds:   sanitizeFloat64(item.DurationSeconds),
			MessageCount:      item.MessageCount,
			ToolCallCount:     item.ToolCallCount,
			TotalInputTokens:  item.TotalInputTokens,
			TotalOutputTokens: item.TotalOutputTokens,
			TotalTokens:       item.TotalTokens,
			TotalCost:         sanitizeFloat64(item.TotalCost),
			Status:            item.Status,
		}
	}

	return &telem_gen.ListSessionsResult{
		Sessions:   sessions,
		NextCursor: nextCursor,
	}, nil
}

func encodeListSessionsCursor(sortValue float64, gramChatID string) string {
	payload, err := json.Marshal(listSessionsCursor{
		SortValue:  sortValue,
		GramChatID: gramChatID,
	})
	if err != nil {
		// The cursor payload is made from primitive values and should always marshal.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeListSessionsCursor(cursor string) (listSessionsCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return listSessionsCursor{}, fmt.Errorf("decode list sessions cursor: %w", err)
	}

	var payload listSessionsCursor
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return listSessionsCursor{}, fmt.Errorf("unmarshal list sessions cursor: %w", err)
	}
	if payload.GramChatID == "" {
		return listSessionsCursor{}, fmt.Errorf("missing gram_chat_id")
	}
	return payload, nil
}

// buildQueryResult assembles the grouped table and matching per-group timeseries
// from the raw repo rows. It applies the top_n + "Other" rollup to both so they
// agree on group membership, and zero-fills the timeseries buckets in Go so the
// fill is consistent with the chosen groups.
func buildQueryResult(
	groupBy string,
	intervalSeconds int64,
	timeStart, timeEnd int64,
	topN int,
	tableRows []repo.AttributeMetricsRow,
	tsRows []repo.AttributeMetricsTimePoint,
) *telem_gen.QueryResult {
	buckets := bucketStarts(timeStart, timeEnd, intervalSeconds)

	// Decide which group values are kept and which fold into the synthetic
	// rollup. The table rows arrive ordered by sort_by descending from ClickHouse.
	kept, hasOther := selectGroups(groupBy, topN, tableRows)
	otherLabel := uniqueOtherGroupLabel(tableRows)

	// keptIndex preserves chart series ordering and lets the timeseries pass map
	// each group value to its slot. "Other" (when present) is the final slot.
	keptIndex := make(map[string]int, len(kept))
	for i, g := range kept {
		keptIndex[g] = i
	}

	// --- table ---
	table := make([]*telem_gen.QueryRow, 0, len(kept)+1)
	var otherTable repo.AttributeMetricsMeasures
	otherDimValues := map[string]map[string]struct{}{}
	for _, row := range tableRows {
		if _, ok := keptIndex[row.GroupValue]; ok || groupBy == "" {
			table = append(table, &telem_gen.QueryRow{
				GroupValue:      row.GroupValue,
				Measures:        toGenMeasures(row.Measures()),
				DimensionValues: normalizeDimensionValues(row.DimensionValues),
			})
			continue
		}
		otherTable.Add(row.Measures())
		mergeDimensionValues(otherDimValues, row.DimensionValues)
	}
	if hasOther {
		table = append(table, &telem_gen.QueryRow{
			GroupValue:      otherLabel,
			Measures:        toGenMeasures(otherTable),
			DimensionValues: flattenDimensionValues(otherDimValues),
		})
	}

	// --- timeseries ---
	// seriesBuckets[seriesValue][bucketTime] = accumulated measures
	seriesValues := append([]string{}, kept...)
	if hasOther {
		seriesValues = append(seriesValues, otherLabel)
	}
	if groupBy == "" && len(seriesValues) == 0 {
		// No group_by: always emit a single empty-keyed series.
		seriesValues = []string{""}
	}

	seriesBuckets := make(map[string]map[int64]*repo.AttributeMetricsMeasures, len(seriesValues))
	for _, v := range seriesValues {
		seriesBuckets[v] = make(map[int64]*repo.AttributeMetricsMeasures, len(buckets))
	}

	for _, point := range tsRows {
		seriesValue := point.GroupValue
		if groupBy == "" {
			seriesValue = ""
		} else if _, ok := keptIndex[seriesValue]; !ok {
			if !hasOther {
				continue
			}
			seriesValue = otherLabel
		}
		byBucket := seriesBuckets[seriesValue]
		if byBucket == nil {
			continue
		}
		m := byBucket[point.BucketTimeUnixNano]
		if m == nil {
			m = new(repo.AttributeMetricsMeasures)
			byBucket[point.BucketTimeUnixNano] = m
		}
		m.Add(point.Measures())
	}

	timeseries := make([]*telem_gen.QuerySeries, 0, len(seriesValues))
	for _, v := range seriesValues {
		byBucket := seriesBuckets[v]
		points := make([]*telem_gen.QueryPoint, 0, len(buckets))
		for _, b := range buckets {
			var measures repo.AttributeMetricsMeasures
			if m := byBucket[b]; m != nil {
				measures = *m
			}
			points = append(points, &telem_gen.QueryPoint{
				BucketTimeUnixNano: strconv.FormatInt(b, 10),
				Measures:           toGenMeasures(measures),
			})
		}
		timeseries = append(timeseries, &telem_gen.QuerySeries{
			GroupValue: v,
			Points:     points,
		})
	}

	return &telem_gen.QueryResult{
		GroupBy:         groupBy,
		IntervalSeconds: intervalSeconds,
		Table:           table,
		Timeseries:      timeseries,
	}
}

// selectGroups returns the ordered group values to keep (top_n by the SQL sort
// order) and whether an "Other" rollup group is needed. When there is no
// group_by, the single (empty-keyed) group is always kept.
func selectGroups(groupBy string, topN int, tableRows []repo.AttributeMetricsRow) (kept []string, hasOther bool) {
	if groupBy == "" {
		for _, r := range tableRows {
			kept = append(kept, r.GroupValue)
		}
		return kept, false
	}
	if topN <= 0 {
		topN = len(tableRows)
	}
	for i, r := range tableRows {
		if i < topN {
			kept = append(kept, r.GroupValue)
			continue
		}
		hasOther = true
	}
	return kept, hasOther
}

func uniqueOtherGroupLabel(tableRows []repo.AttributeMetricsRow) string {
	seen := make(map[string]struct{}, len(tableRows))
	for _, row := range tableRows {
		seen[row.GroupValue] = struct{}{}
	}
	if _, ok := seen[otherGroupLabel]; !ok {
		return otherGroupLabel
	}
	for i := 1; ; i++ {
		label := otherGroupLabel + " (" + strconv.Itoa(i) + ")"
		if _, ok := seen[label]; !ok {
			return label
		}
	}
}

// bucketStarts returns the aligned bucket start times (unix nanoseconds) that
// span [timeStart, timeEnd] at the given interval, matching the SQL
// toStartOfInterval bucketing.
func bucketStarts(timeStart, timeEnd, intervalSeconds int64) []int64 {
	intervalNanos := intervalSeconds * 1_000_000_000
	if intervalNanos <= 0 {
		return nil
	}
	alignedStart := (timeStart / intervalNanos) * intervalNanos
	alignedEnd := (timeEnd / intervalNanos) * intervalNanos
	var buckets []int64
	for b := alignedStart; b <= alignedEnd; b += intervalNanos {
		buckets = append(buckets, b)
	}
	return buckets
}

// normalizeDimensionValues returns a non-nil map for the API result. The repo
// already drops empty values and dedups per group, so kept rows pass through
// unchanged; only the nil case (e.g. unit-test rows) is normalized to {}.
func normalizeDimensionValues(m map[string][]string) map[string][]string {
	if m == nil {
		return map[string][]string{}
	}
	return m
}

// mergeDimensionValues folds one row's per-dimension value lists into the
// accumulating set-of-sets used to build the "Other" rollup row.
func mergeDimensionValues(acc map[string]map[string]struct{}, m map[string][]string) {
	for dim, values := range m {
		set := acc[dim]
		if set == nil {
			set = make(map[string]struct{}, len(values))
			acc[dim] = set
		}
		for _, v := range values {
			set[v] = struct{}{}
		}
	}
}

// flattenDimensionValues converts the accumulated set-of-sets into the API
// shape, sorting each list for deterministic output.
func flattenDimensionValues(acc map[string]map[string]struct{}) map[string][]string {
	out := make(map[string][]string, len(acc))
	for dim, set := range acc {
		values := make([]string, 0, len(set))
		for v := range set {
			values = append(values, v)
		}
		sort.Strings(values)
		out[dim] = values
	}
	return out
}

// toGenMeasures converts repo measures to the API type.
func toGenMeasures(m repo.AttributeMetricsMeasures) *telem_gen.QueryMeasures {
	return &telem_gen.QueryMeasures{
		TotalCost:                m.TotalCost,
		TotalInputTokens:         m.TotalInputTokens,
		TotalOutputTokens:        m.TotalOutputTokens,
		TotalTokens:              m.TotalTokens,
		CacheReadInputTokens:     m.CacheReadInputTokens,
		CacheCreationInputTokens: m.CacheCreationInputTokens,
		TotalToolCalls:           int64(m.TotalToolCalls), //nolint:gosec // bounded count
		TotalChats:               int64(m.TotalChats),     //nolint:gosec // bounded count
	}
}
