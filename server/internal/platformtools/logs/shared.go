package logs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TelemetryService is the subset of the telemetry management service that the
// search_logs platform tool calls. The concrete telemetry service satisfies
// it; the tool passes nil auth tokens because the runtime supplies the
// project and auth context out of band.
type TelemetryService interface {
	SearchLogs(ctx context.Context, payload *telemetry.SearchLogsPayload) (*telemetry.SearchLogsResult, error)
}

// writeLogsDisabledResponse encodes a structured response the model can read
// and surface to the user, complete with the navigation hint to flip logging
// on in the dashboard. Telemetry read methods share the ErrLogsDisabled
// sentinel, so the log search tool routes through this on that error.
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

// isForbidden reports whether a telemetry read was refused by authorization.
func isForbidden(err error) bool {
	var shareable *oops.ShareableError
	return errors.As(err, &shareable) && shareable.Code == oops.CodeForbidden
}

// writeLogReadRequiredResponse encodes a structured refusal for the tools that
// return individual tool calls or raw log records. Those need log:read on the
// project, which no system role holds, so the model is told how the user can
// get it instead of surfacing a bare permission error.
func writeLogReadRequiredResponse(wr io.Writer) error {
	body := map[string]string{
		"error":   "log_read_required",
		"message": "Viewing this project's individual tool calls and logs requires the log:read permission on the project, which this caller does not hold.",
		"hint":    "Tell the user an organization admin can grant log:read for this project through a custom role on the Access page. Aggregate usage tools still work without it.",
	}
	if err := json.NewEncoder(wr).Encode(body); err != nil {
		return fmt.Errorf("encode log read required response: %w", err)
	}
	return nil
}
