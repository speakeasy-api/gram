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
	listWidgetsToolName     = "list_widgets"
	getWidgetToolName       = "get_widget"
	saveWidgetToolName      = "save_widget"
	duplicateWidgetToolName = "duplicate_widget"
	deleteWidgetToolName    = "delete_widget"

	widgetWriteNote  = "Call with confirmed:false first: nothing changes, and the result shows what would change, the version to expect and a preview_token. Show that to the user, and only after they confirm call again with the identical input plus confirmed:true, expected_version, preview_token and a stable idempotency_key. If the widget changed after it was read the write is refused as version_changed and nothing changes; read it again and preview again. Retrying with the same idempotency_key returns the first result without writing twice. The result reports the committed widget from a fresh read; snapshot_scope verification_unavailable means the write committed but that read failed, so keep the retry inputs unchanged. "
	widgetAccessNote = "Requires read and write access to the project, and Explore to be switched on for the organization. External users only."
)

// registerWidgetTools registers the five widget tools. Without a live service
// each keeps its name, schema and metadata and refuses readably, so the
// catalogue does not change shape as a deployment composes the service.
func registerWidgetTools(reg *Registrar, service *WidgetService) {
	// External only: every write is audited under a user, and a managed
	// assistant has no user identity to act as.
	readMeta := ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}
	writeMeta := ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectWrite}

	list := &mcp.Tool{
		Name:  listWidgetsToolName,
		Title: "List Explore Widgets",
		Description: "List a project's Explore widgets — saved analytics questions, each kept with the chart that draws it — most recently updated first: name, description, dataset, chart type, who created it, when it was last updated and its version, whether it still works (invalid_reason names what a catalog change broke), and the dashboards it is on. " +
			"Use get_widget for a widget's full query and visualization. At most 200 widgets; truncated says there are more. Requires read access to the project and Explore to be switched on for the organization. External users only.",
		Annotations: readOnlyAnnotations(),
	}
	get := &mcp.Tool{
		Name:  getWidgetToolName,
		Title: "Read an Explore Widget",
		Description: "Read one widget's full definition: its query in the analytics catalog's vocabulary, its visualization, whether it still works (invalid_reason), the dashboards it is on, and its version, which a later save_widget, duplicate_widget or delete_widget names as expected_version. " +
			"Requires read access to the project and Explore to be switched on for the organization. External users only.",
		Annotations: readOnlyAnnotations(),
	}
	save := &mcp.Tool{
		Name:  saveWidgetToolName,
		Title: "Save an Explore Widget",
		Description: "Create a widget, or replace every field of an existing one when widget_id is given. The query is checked against the analytics catalog and the visualization against the query before anything is saved; an invalid widget is refused naming what is wrong. Build the query with describe_analytics_catalog, and try it with run_analytics_query first. " +
			"An update reaches every dashboard the widget is on; the preview names them. Only a widget's creator, or someone with write access to the project, can update it. " +
			widgetWriteNote + widgetAccessNote,
		// Replacing a widget overwrites the one every dashboard shows.
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true), OpenWorldHint: new(false)},
	}
	duplicate := &mcp.Tool{
		Name:  duplicateWidgetToolName,
		Title: "Duplicate an Explore Widget",
		Description: "Copy a widget into a new one the caller owns, named \"<name> (copy)\": the way to change a teammate's widget without redrawing theirs. A widget that no longer works cannot be duplicated until it is fixed. " +
			widgetWriteNote + widgetAccessNote,
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false), OpenWorldHint: new(false)},
	}
	remove := &mcp.Tool{
		Name:  deleteWidgetToolName,
		Title: "Delete an Explore Widget",
		Description: "Delete a widget. It comes off every dashboard it is on, and the dashboards stay; the preview names those dashboards and the result says which ones lost it. Only a widget's creator, or someone with write access to the project, can delete it. " +
			widgetWriteNote + widgetAccessNote,
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true), OpenWorldHint: new(false)},
	}

	if !service.valid() {
		addTool(reg, list, readMeta, unavailableWidgetHandler[ListWidgetsInput, ListWidgetsOutput]())
		addTool(reg, get, readMeta, unavailableWidgetHandler[GetWidgetInput, GetWidgetOutput]())
		addTool(reg, save, writeMeta, unavailableWidgetHandler[SaveWidgetInput, WidgetWriteOutput]())
		addTool(reg, duplicate, writeMeta, unavailableWidgetHandler[WidgetTargetInput, WidgetWriteOutput]())
		addTool(reg, remove, writeMeta, unavailableWidgetHandler[WidgetTargetInput, WidgetWriteOutput]())
		return
	}

	addTool(reg, list, readMeta, widgetHandler(service.List))
	addTool(reg, get, readMeta, widgetHandler(service.Get))
	addTool(reg, save, writeMeta, widgetHandler(service.Save))
	addTool(reg, duplicate, writeMeta, widgetHandler(service.Duplicate))
	addTool(reg, remove, writeMeta, widgetHandler(service.Delete))
}

func widgetHandler[In, Out any](call func(context.Context, Principal, In) (Out, error)) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		return principalToolCall(ctx, widgetToolResult, func(principal Principal) (Out, error) {
			return call(ctx, principal, input)
		})
	}
}

func unavailableWidgetHandler[In, Out any]() mcp.ToolHandlerFor[In, Out] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: widgetsFeature, Message: "Widgets are not available on this server."})
		if err != nil {
			return nil, zero, fmt.Errorf("encode unavailable widget result: %w", err)
		}
		return nil, zero, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
	}
}

// widgetRefusal is what a widget tool says when it will not act; field names
// the input to correct when there is one.
type widgetRefusal struct {
	Code    string `json:"code"`
	Feature string `json:"feature"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func widgetToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	refusal, ok := errors.AsType[*WidgetToolError](err)
	if !ok {
		return nil, false
	}
	payload, marshalErr := json.Marshal(widgetRefusal{Code: refusal.Code, Feature: widgetsFeature, Field: refusal.Field, Message: refusal.Message})
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
}
