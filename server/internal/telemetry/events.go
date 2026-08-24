package telemetry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	"golang.org/x/sync/errgroup"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/telemetryerrs"
)

const (
	defaultEventLogLimit = 50
	maxEventLogLimit     = 200
)

// eventLogCursor is the keyset position of the last event on a page,
// serialized as base64 JSON into the opaque cursor string.
type eventLogCursor struct {
	TimeUnixNano int64  `json:"time_unix_nano"`
	RecordID     string `json:"record_id"`
}

func encodeEventLogCursor(timeUnixNano int64, recordID string) string {
	payload, err := json.Marshal(eventLogCursor{
		TimeUnixNano: timeUnixNano,
		RecordID:     recordID,
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
	if payload.RecordID == "" {
		return eventLogCursor{}, fmt.Errorf("missing record_id")
	}
	return payload, nil
}

// authorizeOrgEventRead authorizes the caller for org-wide event feed reads
// (session + org read scope + logs enabled) and parses the time window. The
// new signal tables carry organization_id as a first-class column, so unlike
// resolveOrgQueryScope no project resolution is needed.
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

	timeStart, timeEnd, err = parseTimeRange(&from, &to)
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
		if k != repo.EventKindLog && k != repo.EventKindSpan {
			return oops.E(oops.CodeBadRequest, nil, "invalid event kind %q", k)
		}
	}
	return nil
}

// ListEventLog returns one page of the org's merged OpenTelemetry event feed
// (logs and spans, newest first) plus a capped total count for the
// "n of m events" display.
func (s *Service) ListEventLog(ctx context.Context, payload *telem_gen.ListEventLogPayload) (*telem_gen.ListEventLogResult, error) {
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
	var cursorRecordID string
	if payload.Cursor != nil && *payload.Cursor != "" {
		cursor, err := decodeEventLogCursor(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor")
		}
		cursorTimeUnixNano = cursor.TimeUnixNano
		cursorRecordID = cursor.RecordID
	}

	filters := repo.EventLogFilters{
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
		items       []repo.EventLogRow
		totalCount  int64
		totalCapped bool
	)
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		var egErr error
		items, egErr = s.chRepo.ListEventLog(egCtx, repo.ListEventLogParams{
			EventLogFilters:    filters,
			CursorTimeUnixNano: cursorTimeUnixNano,
			CursorRecordID:     cursorRecordID,
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
		next := encodeEventLogCursor(last.TimeUnixNano, last.RecordID)
		nextCursor = &next
	}

	events := make([]*telem_gen.EventLogEntry, 0, len(items))
	for _, item := range items {
		entry, err := toEventLogEntry(item)
		if err != nil {
			return nil, err
		}
		events = append(events, entry)
	}

	return &telem_gen.ListEventLogResult{
		Events:           events,
		NextCursor:       nextCursor,
		TotalCount:       totalCount,
		TotalCountCapped: totalCapped,
	}, nil
}

// toEventLogEntry converts a ClickHouse feed row to the API type, parsing the
// JSON-encoded attribute payloads into objects.
func toEventLogEntry(row repo.EventLogRow) (*telem_gen.EventLogEntry, error) {
	var attributes any
	var resourceAttributes any
	if err := json.Unmarshal([]byte(row.Attributes), &attributes); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse event attributes")
	}
	if err := json.Unmarshal([]byte(row.ResourceAttributes), &resourceAttributes); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse event resource attributes")
	}

	return &telem_gen.EventLogEntry{
		RecordID:           row.RecordID,
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
// the event feed chart. Bucket width adapts to the range like the other
// telemetry timeseries endpoints.
func (s *Service) GetEventVolume(ctx context.Context, payload *telem_gen.GetEventVolumePayload) (*telem_gen.GetEventVolumeResult, error) {
	orgID, timeStart, timeEnd, err := s.authorizeOrgEventRead(ctx, payload.From, payload.To)
	if err != nil {
		return nil, err
	}
	if err := validateEventKinds(payload.Kinds); err != nil {
		return nil, err
	}

	interval := calculateInterval(timeStart, timeEnd)

	rows, err := s.chRepo.GetEventVolume(ctx, repo.GetEventVolumeParams{
		EventLogFilters: repo.EventLogFilters{
			OrganizationID: orgID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			Kinds:          payload.Kinds,
			Sources:        payload.Sources,
			Names:          payload.Names,
			Search:         conv.PtrValOr(payload.Search, ""),
		},
		IntervalSeconds: interval,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error querying event volume").LogError(ctx, s.logger)
	}

	starts := bucketStarts(timeStart, timeEnd, interval)
	bucketIndex := make(map[int64]int, len(starts))
	buckets := make([]*telem_gen.EventVolumeBucket, len(starts))
	for i, start := range starts {
		bucketIndex[start] = i
		buckets[i] = &telem_gen.EventVolumeBucket{
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
		case repo.EventKindLog:
			buckets[i].LogCount = int64(row.EventCount) //nolint:gosec // bounded count
		case repo.EventKindSpan:
			buckets[i].SpanCount = int64(row.EventCount) //nolint:gosec // bounded count
		}
	}

	return &telem_gen.GetEventVolumeResult{
		IntervalSeconds: interval,
		Buckets:         buckets,
	}, nil
}

// GetEventFacets returns the distinct sources and event/span names observed
// in the range, powering the event feed's filter dropdowns. The kind list is
// static (log/span), so it is not returned here.
func (s *Service) GetEventFacets(ctx context.Context, payload *telem_gen.GetEventFacetsPayload) (*telem_gen.GetEventFacetsResult, error) {
	orgID, timeStart, timeEnd, err := s.authorizeOrgEventRead(ctx, payload.From, payload.To)
	if err != nil {
		return nil, err
	}
	if err := validateEventKinds(payload.Kinds); err != nil {
		return nil, err
	}

	facets, err := s.chRepo.GetEventFacets(ctx, repo.EventLogFilters{
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

	return &telem_gen.GetEventFacetsResult{
		Sources: facets.Sources,
		Names:   facets.Names,
	}, nil
}
