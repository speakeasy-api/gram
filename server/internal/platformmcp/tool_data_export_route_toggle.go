//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	pauseDataExportToolName  = operationPauseDataExport
	resumeDataExportToolName = operationResumeDataExport

	dataExportToggleScopeNote = "Only the route's on/off state changes: its data source and destination stay exactly as they are, and destinations, headers, and routes cannot be edited or deleted from here. A route already in the requested state is reported as unchanged rather than refused. The result reports the route's committed state; Speakeasy does not record when a route last delivered, so the result says that rather than giving a time. "
	dataExportToggleDataNote  = "While a route is paused, the data it would have exported is dropped, not buffered: resuming does not send anything produced in between. "
)

// registerDataExportRouteToggleTools keeps the live and unavailable manifests
// identical — same names, schemas, annotations, audiences, and authorization —
// so the tools never appear on and disappear from the catalogue as a
// deployment composes or fails to compose the service behind them.
func registerDataExportRouteToggleTools(reg *Registrar, service *DataExportRouteToggleService) {
	pause := unavailableDataExportToggleHandler[ToggleDataExportRouteInput]()
	resume := unavailableDataExportToggleHandler[ResumeDataExportRouteInput]()
	if service.valid() {
		pause = func(ctx context.Context, _ *mcp.CallToolRequest, input ToggleDataExportRouteInput) (*mcp.CallToolResult, ToggleDataExportRouteOutput, error) {
			return principalToolCall(ctx, dataExportToggleToolResult, func(principal Principal) (ToggleDataExportRouteOutput, error) {
				return service.Pause(ctx, principal, input)
			})
		}
		resume = func(ctx context.Context, _ *mcp.CallToolRequest, input ResumeDataExportRouteInput) (*mcp.CallToolResult, ToggleDataExportRouteOutput, error) {
			return principalToolCall(ctx, dataExportToggleToolResult, func(principal Principal) (ToggleDataExportRouteOutput, error) {
				return service.Resume(ctx, principal, input)
			})
		}
	}

	// Organization admin is what the dashboard's route update requires, and
	// it is this package's convention for external writes. External only, like
	// the other data export tools: export is egress of project data.
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}
	addTool(reg, &mcp.Tool{
		Name:  pauseDataExportToolName,
		Title: "Pause a Data Export",
		Description: "Stop one data export route from sending data to its destination, leaving the route and its destination configured so it can be resumed later. " +
			"Supply the project ID and the exact route ID from list_data_exports, an idempotency key, and confirmed: true only after the user confirms the exact project and route. " +
			dataExportToggleScopeNote + dataExportToggleDataNote +
			"The pause reaches the export relays within about a minute, so data can keep flowing briefly; for a destination that includes sensitive fields it stops on the next batch.",
		// Pausing discards the data produced while the route is paused, which
		// cannot be recovered by resuming, so the hint says destructive.
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true)},
	}, meta, pause)
	addTool(reg, &mcp.Tool{
		Name:  resumeDataExportToolName,
		Title: "Resume a Data Export",
		Description: "Start a paused data export route sending data to its destination again. " +
			"Supply the project ID and the exact route ID from list_data_exports, the route's data_source and destination_id from that same read as expected_data_source and expected_destination_id, an idempotency key, and confirmed: true only after the user confirms the exact project, route, data source, and destination. " +
			"If the route's destination or data source changed after that read, the resume is refused as route_changed and nothing is changed; read the data exports again and confirm what the route now points at. " +
			dataExportToggleScopeNote + dataExportToggleDataNote +
			"The resume reaches the export relays within about a minute for every destination, so the first batches after it can still be dropped. " +
			"A route cannot be resumed, and nothing is changed, when it has no destination, when its destination was deleted, or when its destination's stored configuration (endpoint, sensitive-data setting, or headers) can no longer be used; this applies even if the route is already on. The refusal names which, and the destination has to be repaired in the dashboard.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false)},
	}, meta, resume)
}

func unavailableDataExportToggleHandler[In any]() mcp.ToolHandlerFor[In, ToggleDataExportRouteOutput] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ In) (*mcp.CallToolResult, ToggleDataExportRouteOutput, error) {
		payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: dataExportToggleFeature, Message: "Pausing or resuming data exports is not available on this server."})
		if err != nil {
			return nil, ToggleDataExportRouteOutput{}, fmt.Errorf("encode unavailable data export toggle result: %w", err)
		}
		return nil, ToggleDataExportRouteOutput{}, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
	}
}

type dataExportToggleRefusal struct {
	Code    string `json:"code"`
	Feature string `json:"feature"`
	Message string `json:"message"`
}

func dataExportToggleToolResult(err error) (*mcp.CallToolResult, bool) {
	if refusal, ok := externalAuthorizationToolResult(err); ok {
		return refusal, true
	}
	result := dataExportToggleRefusal{Feature: dataExportToggleFeature}
	var toggle *DataExportToggleError
	switch {
	case errors.As(err, &toggle):
		result.Code, result.Message = toggle.Code, toggle.Message
	case errors.Is(err, ErrUnavailable):
		result.Code = unavailableCode
		result.Message = "Pausing or resuming data exports is temporarily unavailable."
	default:
		return nil, false
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
}
