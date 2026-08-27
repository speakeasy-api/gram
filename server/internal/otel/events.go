package otel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"

	gen "github.com/speakeasy-api/gram/server/gen/otel"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/telemetryerrs"
)

const (
	defaultEventLogLimit = 50
	maxEventLogLimit     = 200
)

// eventLogCursor is the keyset position of the last event on a page,
// serialized as base64 JSON into the opaque cursor string. Paging by event
// time alone can skip events sharing the boundary nanosecond — an accepted
// trade-off at nanosecond resolution.
type eventLogCursor struct {
	TimeUnixNano int64 `json:"time_unix_nano"`
}

func encodeEventLogCursor(timeUnixNano int64) string {
	payload, err := json.Marshal(eventLogCursor{
		TimeUnixNano: timeUnixNano,
	})
	if err != nil {
		// The cursor payload is made from primitive values and should always marshal.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeEventLogCursor(cursor string) (eventLogCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return eventLogCursor{}, fmt.Errorf("decode event log cursor: %w", err)
	}

	var payload eventLogCursor
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return eventLogCursor{}, fmt.Errorf("unmarshal event log cursor: %w", err)
	}
	if payload.TimeUnixNano <= 0 {
		return eventLogCursor{}, fmt.Errorf("missing time_unix_nano")
	}
	return payload, nil
}

// authorizeOrgEventRead authorizes the caller for org-wide event feed reads
// (session + org read scope + logs enabled) and parses the time window. The
// signal tables carry organization_id as a first-class column, so no project
// resolution is needed.
func (s *Service) authorizeOrgEventRead(ctx context.Context, from, to string) (orgID string, timeStart, timeEnd int64, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return "", 0, 0, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return "", 0, 0, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return "", 0, 0, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}
	if !logsEnabled {
		return "", 0, 0, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	timeStart, timeEnd, err = parseEventTimeRange(from, to)
	if err != nil {
		return "", 0, 0, err
	}
	return authCtx.ActiveOrganizationID, timeStart, timeEnd, nil
}

// validateEventKinds rejects kind values outside the closed log/span
// vocabulary. The HTTP layer already enforces the enum; this covers direct
// callers.
func validateEventKinds(kinds []string) error {
	for _, k := range kinds {
		if k != chrepo.EventKindLog && k != chrepo.EventKindSpan {
			return oops.E(oops.CodeBadRequest, nil, "invalid event kind %q", k)
		}
	}
	return nil
}

// ListEventLog returns one page of the org's merged OpenTelemetry event feed
// (logs and spans, newest first) plus a capped total count for the
// "n of m events" display.
func (s *Service) ListEventLog(ctx context.Context, payload *gen.ListEventLogPayload) (*gen.ListEventLogResult, error) {
	orgID, timeStart, timeEnd, err := s.authorizeOrgEventRead(ctx, payload.From, payload.To)
	if err != nil {
		return nil, err
	}
	if err := validateEventKinds(payload.Kinds); err != nil {
		return nil, err
	}

	limit := payload.Limit
	if limit == 0 {
		limit = defaultEventLogLimit
	}
	if limit < 1 || limit > maxEventLogLimit {
		return nil, oops.E(oops.CodeBadRequest, nil, "limit must be between 1 and %d", maxEventLogLimit)
	}

	var cursorTimeUnixNano int64
	if payload.Cursor != nil && *payload.Cursor != "" {
		cursor, err := decodeEventLogCursor(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor")
		}
		cursorTimeUnixNano = cursor.TimeUnixNano
	}

	filters := chrepo.EventLogFilters{
		OrganizationID: orgID,
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		Kinds:          payload.Kinds,
		Sources:        payload.Sources,
		Names:          payload.Names,
		Search:         conv.PtrValOr(payload.Search, ""),
	}

	// The page and the capped count are independent reads — run them
	// concurrently.
	var (
		items       []chrepo.EventLogRow
		totalCount  int64
		totalCapped bool
	)
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		var egErr error
		items, egErr = s.chRepo.ListEventLog(egCtx, chrepo.ListEventLogParams{
			EventLogFilters:    filters,
			CursorTimeUnixNano: cursorTimeUnixNano,
			Limit:              limit + 1,
		})
		if egErr != nil {
			return fmt.Errorf("event log page query: %w", egErr)
		}
		return nil
	})
	eg.Go(func() error {
		var egErr error
		totalCount, totalCapped, egErr = s.chRepo.CountEventLog(egCtx, filters)
		if egErr != nil {
			return fmt.Errorf("event log count query: %w", egErr)
		}
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing events").LogError(ctx, s.logger)
	}

	var nextCursor *string
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		next := encodeEventLogCursor(last.TimeUnixNano)
		nextCursor = &next
	}

	events := make([]*gen.EventLogEntry, 0, len(items))
	for _, item := range items {
		entry, err := toEventLogEntry(item)
		if err != nil {
			return nil, err
		}
		events = append(events, entry)
	}

	return &gen.ListEventLogResult{
		Events:           events,
		NextCursor:       nextCursor,
		TotalCount:       totalCount,
		TotalCountCapped: totalCapped,
	}, nil
}

// toEventLogEntry converts a ClickHouse feed row to the API type, parsing the
// JSON-encoded attribute payloads into objects.
func toEventLogEntry(row chrepo.EventLogRow) (*gen.EventLogEntry, error) {
	var attributes any
	var resourceAttributes any
	if err := json.Unmarshal([]byte(row.Attributes), &attributes); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse event attributes")
	}
	if err := json.Unmarshal([]byte(row.ResourceAttributes), &resourceAttributes); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse event resource attributes")
	}

	return &gen.EventLogEntry{
		TimeUnixNano:       strconv.FormatInt(row.TimeUnixNano, 10),
		Kind:               row.Kind,
		Source:             row.Source,
		Name:               row.Name,
		BodyPreview:        row.BodyPreview,
		TraceID:            row.TraceID,
		SpanID:             row.SpanID,
		ProjectID:          row.ProjectID,
		Attributes:         attributes,
		ResourceAttributes: resourceAttributes,
	}, nil
}

// GetEventVolume returns the zero-filled logs-vs-spans volume timeseries for
// the event feed chart. Bucket width adapts to the range like the telemetry
// service's timeseries endpoints.
func (s *Service) GetEventVolume(ctx context.Context, payload *gen.GetEventVolumePayload) (*gen.GetEventVolumeResult, error) {
	orgID, timeStart, timeEnd, err := s.authorizeOrgEventRead(ctx, payload.From, payload.To)
	if err != nil {
		return nil, err
	}
	if err := validateEventKinds(payload.Kinds); err != nil {
		return nil, err
	}

	interval := calculateEventInterval(timeStart, timeEnd)

	rows, err := s.chRepo.GetEventVolume(ctx, chrepo.GetEventVolumeParams{
		OrganizationID:  orgID,
		TimeStart:       timeStart,
		TimeEnd:         timeEnd,
		Kinds:           payload.Kinds,
		Sources:         payload.Sources,
		Names:           payload.Names,
		Search:          conv.PtrValOr(payload.Search, ""),
		IntervalSeconds: interval,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error querying event volume").LogError(ctx, s.logger)
	}

	starts := eventBucketStarts(timeStart, timeEnd, interval)
	bucketIndex := make(map[int64]int, len(starts))
	buckets := make([]*gen.EventVolumeBucket, len(starts))
	for i, start := range starts {
		bucketIndex[start] = i
		buckets[i] = &gen.EventVolumeBucket{
			BucketTimeUnixNano: strconv.FormatInt(start, 10),
			LogCount:           0,
			SpanCount:          0,
		}
	}

	for _, row := range rows {
		i, ok := bucketIndex[row.BucketUnixNano]
		if !ok {
			continue
		}
		switch row.Kind {
		case chrepo.EventKindLog:
			buckets[i].LogCount = int64(row.EventCount) //nolint:gosec // bounded count
		case chrepo.EventKindSpan:
			buckets[i].SpanCount = int64(row.EventCount) //nolint:gosec // bounded count
		}
	}

	return &gen.GetEventVolumeResult{
		IntervalSeconds: interval,
		Buckets:         buckets,
	}, nil
}

// GetEventFacets returns the distinct sources and event/span names observed
// in the range, powering the event feed's filter dropdowns. The kind list is
// static (log/span), so it is not returned here.
func (s *Service) GetEventFacets(ctx context.Context, payload *gen.GetEventFacetsPayload) (*gen.GetEventFacetsResult, error) {
	orgID, timeStart, timeEnd, err := s.authorizeOrgEventRead(ctx, payload.From, payload.To)
	if err != nil {
		return nil, err
	}
	if err := validateEventKinds(payload.Kinds); err != nil {
		return nil, err
	}

	facets, err := s.chRepo.GetEventFacets(ctx, chrepo.EventLogFilters{
		OrganizationID: orgID,
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		Kinds:          payload.Kinds,
		Sources:        nil,
		Names:          nil,
		Search:         "",
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error querying event facets").LogError(ctx, s.logger)
	}

	return &gen.GetEventFacetsResult{
		Sources: facets.Sources,
		Names:   facets.Names,
	}, nil
}

// parseEventTimeRange parses the ISO 8601 window into Unix nanoseconds,
// mirroring the telemetry service's parseTimeRange.
func parseEventTimeRange(from, to string) (timeStart, timeEnd int64, err error) {
	fromTime, parseErr := time.Parse(time.RFC3339, from)
	if parseErr != nil {
		return 0, 0, oops.E(oops.CodeBadRequest, parseErr, "invalid 'from' time format, expected ISO 8601 (e.g., '2025-12-19T10:00:00Z')")
	}
	toTime, parseErr := time.Parse(time.RFC3339, to)
	if parseErr != nil {
		return 0, 0, oops.E(oops.CodeBadRequest, parseErr, "invalid 'to' time format, expected ISO 8601 (e.g., '2025-12-19T11:00:00Z')")
	}

	timeStart = fromTime.UnixNano()
	timeEnd = toTime.UnixNano()

	// Validate that from < to to prevent unsigned integer overflow in ClickHouse queries
	if timeStart >= timeEnd {
		return 0, 0, oops.E(oops.CodeBadRequest, nil, "'from' time must be before 'to' time")
	}

	return timeStart, timeEnd, nil
}

// calculateEventInterval determines the bucket width for a time range,
// mirroring the telemetry service's calculateInterval.
func calculateEventInterval(timeStart, timeEnd int64) int64 {
	durationHours := (timeEnd - timeStart) / int64(time.Hour)

	switch {
	case durationHours <= 1:
		return 60 // 1 minute buckets
	case durationHours <= 24:
		return 900 // 15 minute buckets
	case durationHours <= 168: // 7 days
		return 3600 // 1 hour buckets
	case durationHours <= 720: // 30 days
		return 21600 // 6 hour buckets
	default:
		return 86400 // 1 day buckets for 90+ days
	}
}

// eventBucketStarts returns the aligned bucket start times (unix nanoseconds)
// that span [timeStart, timeEnd] at the given interval, matching the SQL
// toStartOfInterval bucketing.
func eventBucketStarts(timeStart, timeEnd, intervalSeconds int64) []int64 {
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
