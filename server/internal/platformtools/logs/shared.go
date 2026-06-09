package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/speakeasy-api/gram/server/gen/telemetry"
)

// TelemetryService is the subset of the telemetry management service that the
// managed assistant's observability tools call. The concrete telemetry service
// satisfies it; tools pass nil auth tokens because the assistant runtime
// supplies the project and auth context out of band.
type TelemetryService interface {
	SearchLogs(ctx context.Context, payload *telemetry.SearchLogsPayload) (*telemetry.SearchLogsResult, error)
	SearchToolCalls(ctx context.Context, payload *telemetry.SearchToolCallsPayload) (*telemetry.SearchToolCallsResult, error)
	SearchChats(ctx context.Context, payload *telemetry.SearchChatsPayload) (*telemetry.SearchChatsResult, error)
	SearchUsers(ctx context.Context, payload *telemetry.SearchUsersPayload) (*telemetry.SearchUsersResult, error)
	GetProjectMetricsSummary(ctx context.Context, payload *telemetry.GetProjectMetricsSummaryPayload) (*telemetry.GetMetricsSummaryResult, error)
	GetUserMetricsSummary(ctx context.Context, payload *telemetry.GetUserMetricsSummaryPayload) (*telemetry.GetUserMetricsSummaryResult, error)
	GetObservabilityOverview(ctx context.Context, payload *telemetry.GetObservabilityOverviewPayload) (*telemetry.GetObservabilityOverviewResult, error)
	ListAttributeKeys(ctx context.Context, payload *telemetry.ListAttributeKeysPayload) (*telemetry.ListAttributeKeysResult, error)
}

// defaultTimeWindow fills an empty from/to range with a trailing 7-day window so
// a summary or overview tool can be called without the model always computing a
// window. A defaulted `from` is anchored to the window's end (an explicit `to`
// when given, otherwise now) so the result is always a forward 7-day window —
// never a backward one when the caller passes a past `to` but omits `from`.
// Times are RFC3339/ISO-8601 to match the telemetry payloads.
func defaultTimeWindow(from, to string) (string, string) {
	now := time.Now().UTC()

	windowEnd := now
	if to == "" {
		to = now.Format(time.RFC3339)
	} else if parsed, err := time.Parse(time.RFC3339, to); err == nil {
		windowEnd = parsed.UTC()
	}

	if from == "" {
		from = windowEnd.AddDate(0, 0, -7).Format(time.RFC3339)
	}
	return from, to
}

// writeLogsDisabledResponse encodes a structured response the model can read
// and surface to the user, complete with the navigation hint to flip logging
// on in the dashboard. Telemetry read methods share the ErrLogsDisabled
// sentinel, so every observability tool routes through this on that error.
func writeLogsDisabledResponse(wr io.Writer) error {
	body := map[string]string{
		"error":   "logging_disabled",
		"message": "Telemetry logging is not enabled for this organization, so this observability tool has no data to query.",
		"hint":    "Tell the user to enable logging from the dashboard: Observe → Logs, then toggle logging on. Once enabled, retry the question.",
	}
	if err := json.NewEncoder(wr).Encode(body); err != nil {
		return fmt.Errorf("encode logs disabled response: %w", err)
	}
	return nil
}
