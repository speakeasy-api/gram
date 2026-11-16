//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/dataexports"
)

const sendDataExportTestEventToolName = operationSendDataExportTestEvent

// registerDataExportTestEventTool keeps one manifest whether or not the
// service composed, so the tool never drops out of the catalogue.
func registerDataExportTestEventTool(reg *Registrar, service *DataExportTestEventService) {
	handler := func(_ context.Context, _ *mcp.CallToolRequest, _ SendDataExportTestEventInput) (*mcp.CallToolResult, SendDataExportTestEventOutput, error) {
		payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: dataExportTestFeature, Message: "Sending a data export test event is not available on this server."})
		if err != nil {
			return nil, SendDataExportTestEventOutput{}, fmt.Errorf("encode unavailable data export test event result: %w", err)
		}
		return nil, SendDataExportTestEventOutput{}, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
	}
	if service.valid() {
		handler = func(ctx context.Context, _ *mcp.CallToolRequest, input SendDataExportTestEventInput) (*mcp.CallToolResult, SendDataExportTestEventOutput, error) {
			return principalToolCall(ctx, dataExportTestEventToolResult, func(principal Principal) (SendDataExportTestEventOutput, error) {
				return service.Send(ctx, principal, input)
			})
		}
	}

	// Org admin, like the dashboard. External only, like the other data export tools.
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}
	addTool(reg, &mcp.Tool{
		Name:  sendDataExportTestEventToolName,
		Title: "Send a Data Export Test Event",
		Description: "Send a test event straight to the destination of a data export route and report what it answered, to check the endpoint and API key. " +
			dataExportTestSignalsDescription() +
			"Supply the project's slug, the exact route ID, and the route's data_source and destination_id from list_data_exports as expected_data_source and expected_destination_id. Call first with confirmed: false: nothing is sent, and the confirmation_required refusal says what to show the user. Once they confirm the exact project, route, data source, and destination, call again with the same input and confirmed: true. " +
			"Every test log, span and metric data point, and every request's resource, carries speakeasy.test_event=true. The test log and span also carry speakeasy.test_event.id, one ID per call returned as test_event_id; the metric does not, so tests add no metric time series. Every test log is named speakeasy.test_event and its message starts with \"Speakeasy test event:\", but real data can use that name and text too. To filter tests out, tell the user to filter on speakeasy.test_event=true: Speakeasy drops it from all data it receives, so only tests carry it. A test contains no real connection, finding, tool call, person, or server. " +
			"Outcomes: accepted (2xx), refused (401 or 403: check the API key, and for Datadog that the endpoint matches the site), not_found (404: check the URL; Speakeasy adds /v1/logs, /v1/traces or /v1/metrics), unreachable (timeout, no connection, or an address Speakeasy may not send to), error (any other answer). " +
			"The top-level outcome is accepted only when every signal was accepted; otherwise it is the worst signal's. " +
			"Show the user the returned message, each signal's message when there is more than one, and speakeasy.test_event.id=<test_event_id> so they can find the test log and trace in their destination. Each route, and each organization across all its routes, can send only a few tests a minute; past that the call is refused as rate_limited, and when Speakeasy cannot check that limit it is refused as unavailable and nothing is sent. Nothing about the route or destination changes.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: false, DestructiveHint: new(false), OpenWorldHint: new(true)},
	}, meta, handler)
}

func dataExportTestEventToolResult(err error) (*mcp.CallToolResult, bool) {
	if refusal, ok := externalAuthorizationToolResult(err); ok {
		return refusal, true
	}
	refusal, ok := errors.AsType[*DataExportRefusalError](err)
	if !ok {
		return nil, false
	}
	payload, marshalErr := json.Marshal(dataExportRefusal{Code: refusal.Code, Feature: dataExportTestFeature, Message: refusal.Message})
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
}

// dataExportTestSignalsDescription says what each data source's test sends, from the source registry.
func dataExportTestSignalsDescription() string {
	signalNames := map[dataexports.Signal]string{
		dataexports.SignalLogs:    "one test log",
		dataexports.SignalTraces:  "one trace",
		dataexports.SignalMetrics: "one metric",
	}
	var others []string
	for _, source := range dataexports.Sources() {
		if slices.Equal(source.Signals, []dataexports.Signal{dataexports.SignalLogs}) {
			continue
		}
		names := make([]string, len(source.Signals))
		for i, signal := range source.Signals {
			names[i] = signalNames[signal]
		}
		list := names[len(names)-1]
		if len(names) > 1 {
			list = strings.Join(names[:len(names)-1], ", ") + " and " + list
		}
		others = append(others, source.Name+", which sends "+list)
	}
	if len(others) == 0 {
		return "Every data source sends one test log. "
	}
	return "Every data source sends one test log, except " + strings.Join(others, "; and ") + ", and gets one result for each. "
}
