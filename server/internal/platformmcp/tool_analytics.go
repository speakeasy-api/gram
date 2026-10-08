//nolint:exhaustruct // MCP SDK manifests and JSON schemas intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
)

const (
	describeAnalyticsCatalogToolName     = "describe_analytics_catalog"
	listAnalyticsDimensionValuesToolName = "list_analytics_dimension_values"
	runAnalyticsQueryToolName            = "run_analytics_query"

	// analyticsFeature names the capability in refusals.
	analyticsFeature = "analytics"
	// analyticsNotEnabledCode is the refusal when Explore is off for the
	// organization, as opposed to unavailableCode, which asks for a retry.
	analyticsNotEnabledCode = "feature_not_enabled"
)

// analyticsRefusal is what a tool says when it will not run; an invalid
// request also names the field and value to correct.
type analyticsRefusal struct {
	Code    string `json:"code"`
	Feature string `json:"feature"`
	Reason  string `json:"reason,omitempty"`
	Field   string `json:"field,omitempty"`
	Value   string `json:"value,omitempty"`
	Message string `json:"message"`
}

// registerAnalyticsTools registers the three tools; without a live service
// each is served as a stub of the same shape.
func registerAnalyticsTools(reg *Registrar, service *AnalyticsService) {
	meta := ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}
	live := service.valid()

	describe := &mcp.Tool{
		Name:  describeAnalyticsCatalogToolName,
		Title: "Describe the Analytics Catalog",
		Description: "Read the catalog a project's agent session data is queried through: every dataset (what one row is, and how many days back it can be asked about), its dimensions with the filter operators and the one aggregation each admits, its measures with their units and aggregations, the time grains, and the limits every query is held to. " +
			"Call this before run_analytics_query or list_analytics_dimension_values: the names it returns are the only vocabulary they accept, and nothing here is a table or SQL. A field's description says which producers fill it; a field's lookup names the per-project map it reads through, so grouping and filtering on it speak the mapped names. " +
			"Requires Explore to be switched on for the organization.",
		Annotations: readOnlyAnnotations(),
		InputSchema: analyticsProjectSchema(nil, []string{"project_id"}),
	}
	values := &mcp.Tool{
		Name:  listAnalyticsDimensionValuesToolName,
		Title: "List a Dimension's Values",
		Description: "List the values one dimension of a dataset actually holds inside a time window, most frequent first with how many rows at the dataset's grain carry each, for choosing filter values before run_analytics_query. " +
			"Every value returned is one a filter will match: a dimension that reads through a lookup shows the mapped names, and the empty value is never offered. " +
			"At most 200 values, 50 by default; the half-open window [from, to) is bounded by the dataset's max_time_range_days. Requires Explore to be switched on for the organization.",
		Annotations: readOnlyAnnotations(),
		InputSchema: analyticsProjectSchema(map[string]*jsonschema.Schema{
			"dataset":   stringSchema("Dataset to look in, by name from describe_analytics_catalog.", 1, 64),
			"dimension": stringSchema("Dimension to list the values of, by name from describe_analytics_catalog.", 1, 64),
			"from":      dateTimeSchema("Start of the half-open window [from, to), RFC 3339."),
			"to":        dateTimeSchema("End of the half-open window [from, to), RFC 3339."),
			"limit":     {Type: "integer", Minimum: new(float64(1)), Maximum: new(float64(analytics.MaxValuesLimit)), Description: fmt.Sprintf("Maximum values to return; defaults to %d and cannot exceed %d.", analytics.DefaultValuesLimit, analytics.MaxValuesLimit)},
		}, []string{"project_id", "dataset", "dimension", "from", "to"}),
	}
	query := &mcp.Tool{
		Name:  runAnalyticsQueryToolName,
		Title: "Run an Analytics Query",
		Description: "Run one query against a dataset from describe_analytics_catalog, in catalog vocabulary, never SQL. " +
			fmt.Sprintf("Grouped, the default: group by up to %d dimensions, optionally bucketed by a time grain, with one or more measures (count alone, count_distinct over a dimension that admits it, or an aggregation over a measure field that admits it), filters ANDed with at most %d values each, an order by measure alias, and a limit of at most %d rows (%d by default), ordered by the first measure descending unless order_by says otherwise. ", analytics.MaxDimensions, analytics.MaxFilterValues, analytics.MaxLimit, analytics.DefaultLimit) +
			"Ungrouped: rows at the dataset's grain, newest first, projecting the named dimensions, with no measures and no grain. " +
			"Row keys are the dimension names plus each measure's alias; time_bucket is present when a grain is set and time on ungrouped rows, both RFC 3339 in UTC, and columns says which key is which. " +
			"The half-open window [from, to) is bounded by the dataset's max_time_range_days. An invalid request is refused naming the field to correct; a result holds at most limit rows and does not say whether more exist. Requires Explore to be switched on for the organization.",
		Annotations: readOnlyAnnotations(),
		InputSchema: runAnalyticsQuerySchema(),
	}

	if !live {
		for _, tool := range []*mcp.Tool{describe, values, query} {
			tool.Description += " Analytics queries are unavailable in this deployment."
			addTool(reg, tool, meta, unavailableTool(analyticsFeature))
		}
		return
	}

	addTool(reg, describe, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input DescribeAnalyticsCatalogInput) (*mcp.CallToolResult, DescribeAnalyticsCatalogOutput, error) {
		return analyticsToolCall(ctx, service.logger, describe.Name, func(principal Principal) (DescribeAnalyticsCatalogOutput, error) {
			return service.Describe(ctx, principal, input)
		})
	})
	addTool(reg, values, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input ListAnalyticsDimensionValuesInput) (*mcp.CallToolResult, ListAnalyticsDimensionValuesOutput, error) {
		return analyticsToolCall(ctx, service.logger, values.Name, func(principal Principal) (ListAnalyticsDimensionValuesOutput, error) {
			return service.Values(ctx, principal, input)
		})
	})
	addTool(reg, query, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input RunAnalyticsQueryInput) (*mcp.CallToolResult, RunAnalyticsQueryOutput, error) {
		return analyticsToolCall(ctx, service.logger, query.Name, func(principal Principal) (RunAnalyticsQueryOutput, error) {
			return service.Query(ctx, principal, input)
		})
	})
}

// analyticsToolCall runs one read under the calling principal and turns
// refusals into readable error results. A failure the tool did not expect is
// logged and reads as unavailable, so the agent never sees its text.
func analyticsToolCall[Out any](ctx context.Context, logger *slog.Logger, tool string, call func(principal Principal) (Out, error)) (*mcp.CallToolResult, Out, error) {
	var zero Out
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return nil, zero, err
	}
	output, err := call(principal)
	if err == nil {
		return nil, output, nil
	}
	if result, ok := operationBudgetToolResult(err); ok {
		return result, zero, nil
	}
	refusal, named := analyticsRefusalFor(err)
	if !named {
		logger.ErrorContext(ctx, "platform mcp analytics read failed", attr.SlogError(err), attr.SlogToolName(tool), attr.SlogOrganizationID(principal.OrganizationID))
	}
	content, marshalErr := json.Marshal(refusal)
	if marshalErr != nil {
		return nil, zero, fmt.Errorf("encode analytics refusal: %w", marshalErr)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, zero, nil
}

// analyticsRefusalFor maps the errors a read can end in onto refusals; named
// is false for an error the tool did not expect, which reads as unavailable.
func analyticsRefusalFor(err error) (refusal analyticsRefusal, named bool) {
	refusal = analyticsRefusal{Feature: analyticsFeature}
	var invalid *analytics.Error
	switch {
	case errors.As(err, &invalid):
		refusal.Code = "invalid_request"
		refusal.Reason = string(invalid.Code)
		refusal.Field = invalid.Field
		refusal.Value = invalid.Value
		refusal.Message = invalid.Message + ". Correct that from describe_analytics_catalog rather than retrying the same request."
	case errors.Is(err, ErrAnalyticsInvalid):
		refusal.Code = "invalid_request"
		refusal.Message = "Name exactly one project by its ID, as list_projects returns it."
	case errors.Is(err, ErrAnalyticsNotEnabled):
		refusal.Code = analyticsNotEnabledCode
		refusal.Message = "Analytics queries are not switched on for your organization yet. They arrive with Explore; ask your Speakeasy contact to turn it on."
	case errors.Is(err, ErrForbidden):
		refusal.Code = "forbidden"
		refusal.Message = "That project is not one this caller can read."
	case errors.Is(err, ErrUnavailable):
		refusal.Code = unavailableCode
		refusal.Message = "Analytics queries are temporarily unavailable. Try again shortly."
	default:
		refusal.Code = unavailableCode
		refusal.Message = "Analytics queries are temporarily unavailable. Try again shortly."
		return refusal, false
	}
	return refusal, true
}

// analyticsProjectSchema is the tool's own fields beside the exact project ID
// every analytics tool takes.
func analyticsProjectSchema(common map[string]*jsonschema.Schema, required []string) *jsonschema.Schema {
	properties := make(map[string]*jsonschema.Schema, len(common)+1)
	maps.Copy(properties, common)
	properties["project_id"] = uuidSchema("Exact project ID to read, as list_projects returns it.")
	return closedObject(properties, required)
}

func dateTimeSchema(description string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "string", Format: "date-time", Description: description}
}

// described sets the description of a schema a helper built without one.
func described(schema *jsonschema.Schema, description string) *jsonschema.Schema {
	schema.Description = description
	return schema
}

// runAnalyticsQuerySchema mirrors the analytics RPC's payload and the
// compiler's bounds.
func runAnalyticsQuerySchema() *jsonschema.Schema {
	ops := []string{analytics.AggregationCount, string(analytics.AggregationCountDistinct), string(analytics.AggregationSum), string(analytics.AggregationAvg), string(analytics.AggregationMin), string(analytics.AggregationMax), string(analytics.AggregationP50), string(analytics.AggregationP95), string(analytics.AggregationP99)}
	measure := closedObject(map[string]*jsonschema.Schema{
		"op":    described(enumSchema(ops...), "Aggregation to apply. count takes no field; count_distinct takes a dimension that admits it; every other op needs a measure field that admits it, per describe_analytics_catalog."),
		"field": stringSchema("Field the op applies to: a dimension for count_distinct, a measure otherwise. Omit for count.", 1, 64),
		"alias": stringSchema("Result column name, a lowercase identifier. Defaults to the op, or op_field.", 1, 64),
	}, []string{"op"})
	filter := closedObject(map[string]*jsonschema.Schema{
		"field":    stringSchema("Dimension to filter on.", 1, 64),
		"operator": described(enumSchema(string(analytics.OperatorEquals), string(analytics.OperatorIn)), "Operator the dimension admits, per describe_analytics_catalog."),
		"values":   described(boundedArraySchema(&jsonschema.Schema{Type: "string"}, 1, analytics.MaxFilterValues, true), "equals takes exactly one value; in matches any of them."),
	}, []string{"field", "operator", "values"})
	order := closedObject(map[string]*jsonschema.Schema{
		"measure":   stringSchema("Alias of a requested measure.", 1, 64),
		"direction": described(enumSchema("asc", "desc"), "Defaults to desc."),
	}, []string{"measure"})
	return analyticsProjectSchema(map[string]*jsonschema.Schema{
		"dataset":    stringSchema("Dataset to query, by name from describe_analytics_catalog.", 1, 64),
		"from":       dateTimeSchema("Start of the half-open window [from, to), RFC 3339."),
		"to":         dateTimeSchema("End of the half-open window [from, to), RFC 3339."),
		"grain":      described(enumSchema(analyticsNames(analytics.TimeGrains)...), "Time bucket width for a grouped query. Omit for none."),
		"dimensions": described(boundedArraySchema(stringSchema("", 1, 64), 0, analytics.MaxDimensions, true), fmt.Sprintf("Group-by key when grouped; projected columns when ungrouped. At most %d.", analytics.MaxDimensions)),
		"measures":   described(arraySchema(measure, 0, false), "Composed measures. Required when grouped, forbidden when ungrouped."),
		"filters":    described(arraySchema(filter, 0, false), fmt.Sprintf("Filters, ANDed. At most %d values per filter.", analytics.MaxFilterValues)),
		"order_by":   described(arraySchema(order, 0, false), "Sort for a grouped result, by measure alias. Ungrouped rows are always newest first."),
		"limit":      {Type: "integer", Minimum: new(float64(1)), Maximum: new(float64(analytics.MaxLimit)), Description: fmt.Sprintf("Maximum rows; defaults to %d, at most %d for a grouped result and %d for ungrouped rows. Narrow the window or filter rather than raising it.", analytics.DefaultLimit, analytics.MaxLimit, analytics.MaxRowsLimit)},
		"ungrouped":  {Type: "boolean", Description: "Return rows at the dataset's grain instead of aggregating. Defaults to false."},
	}, []string{"project_id", "dataset", "from", "to"})
}
