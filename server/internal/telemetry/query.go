package telemetry

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/telemetryerrs"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

// minIntervalSeconds is the finest timeseries bucket telemetry.query supports.
// The source aggregate (attribute_metrics_summaries) is bucketed hourly, so
// anything finer would just return sparse hourly data.
const minIntervalSeconds int64 = 3600

const (
	defaultQuerySortBy       = "total_cost"
	defaultQueryTopN         = 10
	skillVersionRawRetention = 90 * 24 * time.Hour
)

var unsafeSkillVersionGroupDimensions = map[string]bool{
	"model":           true,
	"query_source":    true,
	"skill_name":      true,
	"agent_name":      true,
	"mcp_server_name": true,
	"mcp_tool_name":   true,
}

// otherGroupLabel is the default synthetic group value that holds the rolled-up
// remainder beyond top_n. If a real group already uses this value, the response
// picks a suffixed label so the synthetic rollup cannot collide with user data.
const otherGroupLabel = "Other"

type listSessionsCursor struct {
	SortValue  float64 `json:"sort_value"`
	GramChatID string  `json:"gram_chat_id"`
}

// orgQueryScope is the resolved, authorized input shared by the org-scoped
// analytics endpoints: the parsed time window and the organization's projects.
type orgQueryScope struct {
	timeStart    int64
	timeEnd      int64
	projectUUIDs []uuid.UUID
	projectIDs   []string
}

// resolveOrgQueryScope authorizes the caller for org-wide telemetry reads
// (queries span every project in the organization), requires logs to be
// enabled, parses the time window, and resolves the org's projects —
// optionally narrowed to one. A project id outside the caller's organization
// simply resolves to no projects (and an all-zero result) rather than acting
// as an existence probe.
func (s *Service) resolveOrgQueryScope(ctx context.Context, from, to string, projectID *string) (orgQueryScope, error) {
	var scope orgQueryScope

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return scope, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return scope, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return scope, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}
	if !logsEnabled {
		return scope, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	scope.timeStart, scope.timeEnd, err = parseTimeRange(&from, &to)
	if err != nil {
		return scope, err
	}

	projects, err := s.projectsRepo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return scope, oops.E(oops.CodeUnexpected, err, "failed to list organization projects")
	}
	scope.projectUUIDs = make([]uuid.UUID, 0, len(projects))
	scope.projectIDs = make([]string, 0, len(projects))
	for _, p := range projects {
		if projectID != nil && *projectID != "" && p.ID.String() != *projectID {
			continue
		}
		scope.projectUUIDs = append(scope.projectUUIDs, p.ID)
		scope.projectIDs = append(scope.projectIDs, p.ID.String())
	}

	return scope, nil
}

// Query is a generic, org-scoped analytics query. Existing dimensions read the
// pre-aggregated attribute_metrics_summaries view; skill_version queries use
// session-level raw telemetry joined to asynchronously reconciled mappings.
func (s *Service) Query(ctx context.Context, payload *telem_gen.QueryPayload) (*telem_gen.QueryResult, error) {
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
	useSkillVersions := groupBy == "skill_version"
	for _, filter := range filters {
		if filter.Dimension == "skill_version" && len(filter.Values) > 0 {
			useSkillVersions = true
		}
	}
	if useSkillVersions && timeStart < time.Now().Add(-skillVersionRawRetention).UnixNano() {
		return nil, oops.E(oops.CodeBadRequest, nil, "skill_version queries are limited to 90 days of raw telemetry history")
	}
	if useSkillVersions && unsafeSkillVersionGroupDimensions[groupBy] {
		return nil, oops.E(oops.CodeBadRequest, nil, "group_by %q is not supported with skill_version because it can vary within a session", groupBy)
	}

	// The grouped table and the per-group timeseries are independent reads of
	// the same aggregate — run them concurrently.
	var (
		tableRows []repo.AttributeMetricsRow
		tsRows    []repo.AttributeMetricsTimePoint
	)
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		var egErr error
		if useSkillVersions {
			tableRows, egErr = s.chRepo.QuerySkillVersionMetricsTable(egCtx, params)
		} else {
			tableRows, egErr = s.chRepo.QueryAttributeMetricsTable(egCtx, params)
		}
		if egErr != nil {
			return fmt.Errorf("analytics table query: %w", egErr)
		}
		return nil
	})
	eg.Go(func() error {
		var egErr error
		if useSkillVersions {
			tsRows, egErr = s.chRepo.QuerySkillVersionMetricsTimeseries(egCtx, params)
		} else {
			tsRows, egErr = s.chRepo.QueryAttributeMetricsTimeseries(egCtx, params)
		}
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

// tumDetailsIntervalSeconds is the fixed bucket width of queryTumDetails —
// the billing page's details are bucketed daily.
const tumDetailsIntervalSeconds int64 = 86400

// tumBreakdownDims are the billing page's breakdown dimensions, in display
// order. Each maps to an attribute_metrics_summaries expression (see
// tumBreakdownDimExprs in the repo); the values are the public telemetry
// dimension identifiers the frontend picker uses. All the dimensions the
// observed agent traffic genuinely carries are offered: the model and agent
// surface of each session, the account's provider and team/personal
// classification, the emit-time user-identity snapshot, and the project the
// traffic was recorded under.
var tumBreakdownDims = []string{"model", "hook_source", "provider", "account_type", "email", "division_name", "department_name", "role", "project_id"}

// QueryTumDetails computes the billing page's usage details for a time range
// in one request: the tokens-under-management per-day token split and
// per-dimension breakdowns. The backing queries run concurrently.
//
// Tokens under management are the agent traffic the platform OBSERVES from
// the customer's users (Claude Code, Cursor, Codex sessions), excluding
// cache reads — never the inference Gram itself spends (risk-policy judges,
// hosted chat surfaces). Reads attribute_metrics_summaries scoped exactly
// like the billed totals (same source exclusions and measure), so the page
// reports the billed population exactly.
func (s *Service) QueryTumDetails(ctx context.Context, payload *telem_gen.QueryTumDetailsPayload) (*telem_gen.TumDetailsResult, error) {
	scope, err := s.resolveOrgQueryScope(ctx, payload.From, payload.To, payload.ProjectID)
	if err != nil {
		return nil, err
	}
	timeStart, timeEnd := scope.timeStart, scope.timeEnd

	// Billing counts usage recorded while a project was live even if the
	// project has since been soft-deleted — the card's totals do (see
	// ListBillingProjectIDsByOrganization) — while resolveOrgQueryScope lists
	// active projects only. Org-wide reads swap in the billing-aware list so
	// a deleted project's usage cannot show on the card yet vanish from the
	// breakdowns; an explicit project filter keeps the caller's choice.
	billedProjectIDs := scope.projectIDs
	if payload.ProjectID == nil || *payload.ProjectID == "" {
		authCtx, _ := contextvalues.GetAuthContext(ctx)
		billingIDs, err := usagerepo.New(s.db).ListBillingProjectIDsByOrganization(ctx, authCtx.ActiveOrganizationID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list billing projects").LogError(ctx, s.logger)
		}
		billedProjectIDs = make([]string, 0, len(billingIDs))
		for _, id := range billingIDs {
			billedProjectIDs = append(billedProjectIDs, id.String())
		}
	}

	billedParams := repo.GetTokensUnderManagementParams{
		ProjectIDs:          billedProjectIDs,
		StartUnixNano:       timeStart,
		EndUnixNano:         timeEnd,
		ExcludedHookSources: billing.GramHostedHookSourceStrings(),
	}

	var (
		dayRows []repo.TumBreakdownDayBucket
		dimRows = make([][]repo.TumBreakdownDimDayBucket, len(tumBreakdownDims))
	)
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(4)
	eg.Go(func() error {
		var egErr error
		dayRows, egErr = s.chRepo.GetTumBreakdownTotalsByDay(egCtx, billedParams)
		if egErr != nil {
			return fmt.Errorf("tum breakdown totals: %w", egErr)
		}
		return nil
	})
	for i, dim := range tumBreakdownDims {
		eg.Go(func() error {
			var egErr error
			dimRows[i], egErr = s.chRepo.GetTumBreakdownDimByDay(egCtx, billedParams, dim)
			if egErr != nil {
				return fmt.Errorf("tum breakdown dimension query (%s): %w", dim, egErr)
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error running usage details queries").LogError(ctx, s.logger)
	}

	// Zero-fill so consumers get one point per day across the whole range,
	// matching the other telemetry timeseries contracts. timeEnd is an
	// exclusive boundary (the ClickHouse read uses <), so step just inside it
	// — otherwise a day-aligned `to` would add one empty bucket past the
	// range.
	starts := bucketStarts(timeStart, timeEnd-1, tumDetailsIntervalSeconds)
	bucketIndex := make(map[int64]int, len(starts))
	for i, start := range starts {
		bucketIndex[start] = i
	}

	dayByBucket := make(map[int64]repo.TumBreakdownDayBucket, len(dayRows))
	var totalInput, totalOutput, totalCacheCreation, totalTokens int64
	for _, row := range dayRows {
		dayByBucket[row.Day.UTC().UnixNano()] = row
		totalInput += row.InputTokens
		totalOutput += row.OutputTokens
		totalCacheCreation += row.CacheCreationTokens
		totalTokens += row.TotalTokens
	}

	// Assemble each dimension's top rows plus an "Other" remainder, with the
	// daily token series aligned to the shared bucket grid. Rows recorded
	// before a dimension existed carry '' — the frontend labels them.
	const breakdownTopN = 6
	breakdowns := make([]*telem_gen.TumDetailsBreakdown, 0, len(tumBreakdownDims))
	for i, dim := range tumBreakdownDims {
		seriesByValue := make(map[string][]int64)
		totalByValue := make(map[string]int64)
		for _, row := range dimRows[i] {
			idx, ok := bucketIndex[row.Day.UTC().UnixNano()]
			if !ok {
				continue
			}
			series := seriesByValue[row.Value]
			if series == nil {
				series = make([]int64, len(starts))
				seriesByValue[row.Value] = series
			}
			series[idx] += row.Tokens
			totalByValue[row.Value] += row.Tokens
		}

		values := make([]string, 0, len(seriesByValue))
		for value := range seriesByValue {
			values = append(values, value)
		}
		slices.SortStableFunc(values, func(a, b string) int {
			return cmp.Compare(totalByValue[b], totalByValue[a])
		})

		rows := make([]*telem_gen.TumDetailsBreakdownRow, 0, breakdownTopN+1)
		otherSeries := make([]int64, len(starts))
		var otherTotal int64
		for j, value := range values {
			if j < breakdownTopN {
				rows = append(rows, &telem_gen.TumDetailsBreakdownRow{
					Value:       value,
					TotalTokens: totalByValue[value],
					Series:      seriesByValue[value],
				})
				continue
			}
			otherTotal += totalByValue[value]
			for k, v := range seriesByValue[value] {
				otherSeries[k] += v
			}
		}
		if len(values) > breakdownTopN {
			// Suffixed when a real value is already "Other", same as
			// telemetry.query's rollup row.
			label := otherGroupLabel
			for slices.Contains(values[:breakdownTopN], label) {
				label += " (other)"
			}
			rows = append(rows, &telem_gen.TumDetailsBreakdownRow{
				Value:       label,
				TotalTokens: otherTotal,
				Series:      otherSeries,
			})
		}
		breakdowns = append(breakdowns, &telem_gen.TumDetailsBreakdown{
			Key:  dim,
			Rows: rows,
		})
	}

	points := make([]*telem_gen.TumDetailsPoint, 0, len(starts))
	for _, start := range starts {
		day := dayByBucket[start]
		points = append(points, &telem_gen.TumDetailsPoint{
			BucketTimeUnixNano:  strconv.FormatInt(start, 10),
			InputTokens:         day.InputTokens,
			OutputTokens:        day.OutputTokens,
			CacheCreationTokens: day.CacheCreationTokens,
			TotalTokens:         day.TotalTokens,
		})
	}

	return &telem_gen.TumDetailsResult{
		IntervalSeconds: tumDetailsIntervalSeconds,
		Points:          points,
		Breakdowns:      breakdowns,
		Totals: &telem_gen.TumDetailsTotals{
			InputTokens:         totalInput,
			OutputTokens:        totalOutput,
			CacheCreationTokens: totalCacheCreation,
			TotalTokens:         totalTokens,
		},
	}, nil
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

	params := repo.ListSessionsParams{
		ProjectIDs:       projectIDs,
		TimeStart:        timeStart,
		TimeEnd:          timeEnd,
		Filters:          filters,
		SortBy:           sortBy,
		CursorSortValue:  cursorSortValue,
		CursorGramChatID: cursorGramChatID,
		Limit:            limit + 1,
	}
	// Named per routed path so the raw telemetry_logs scan and the
	// chat_session_summaries read stay separately visible in traces; the repo
	// forwards this span's context to ClickHouse alongside the query.
	spanName := "telemetry.listSessions.clickhouse.raw"
	if params.UsesSummaryPath() {
		spanName = "telemetry.listSessions.clickhouse.summary"
	}
	queryCtx, span := s.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	items, err := s.chRepo.ListSessions(queryCtx, params)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing sessions")
	}

	var nextCursor *string
	if len(items) > limit {
		next := encodeListSessionsCursor(items[limit-1].SortValue, items[limit-1].GramChatID)
		nextCursor = &next
		items = items[:limit]
	}

	// Chat titles live in Postgres, not the ClickHouse telemetry the session
	// metrics come from, so resolve them in a single batch and stitch them on.
	titlesByChatID := s.chatTitlesForSessions(ctx, items, projectIDs)

	sessions := make([]*telem_gen.SessionSummary, len(items))
	for i, item := range items {
		var title *string
		if t, ok := titlesByChatID[item.GramChatID]; ok {
			title = &t
		}
		sessions[i] = &telem_gen.SessionSummary{
			GramChatID:        item.GramChatID,
			ProjectID:         item.ProjectID,
			UserEmail:         item.UserEmail,
			HookSource:        item.HookSource,
			Model:             item.Model,
			Title:             title,
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

// chatTitlesForSessions resolves the Postgres chat title for each session,
// keyed by gram_chat_id. Best-effort: a session whose chat row is missing,
// untitled, or whose id doesn't parse simply gets no title, and a query error
// is logged rather than failing the whole list (titles are cosmetic). Scoped to
// the org's projects so it can never surface a title from another tenant.
func (s *Service) chatTitlesForSessions(ctx context.Context, items []repo.SessionSummary, projectIDs []string) map[string]string {
	if len(items) == 0 {
		return nil
	}

	chatIDs := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		id, err := uuid.Parse(item.GramChatID)
		if err != nil {
			continue
		}
		chatIDs = append(chatIDs, id)
	}
	if len(chatIDs) == 0 {
		return nil
	}

	projectUUIDs := make([]uuid.UUID, 0, len(projectIDs))
	for _, p := range projectIDs {
		id, err := uuid.Parse(p)
		if err != nil {
			continue
		}
		projectUUIDs = append(projectUUIDs, id)
	}

	rows, err := s.chatRepo.GetChatTitlesByIDs(ctx, chatRepo.GetChatTitlesByIDsParams{
		Ids:        chatIDs,
		ProjectIds: projectUUIDs,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to resolve chat titles for sessions", attr.SlogError(err))
		return nil
	}

	titles := make(map[string]string, len(rows))
	for _, row := range rows {
		if row.Title.Valid && row.Title.String != "" {
			titles[row.ID.String()] = row.Title.String
		}
	}
	return titles
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
