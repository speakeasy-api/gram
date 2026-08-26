package explore

import (
	"fmt"
)

// minGranularitySeconds floors timeseries bucketing across all datasets.
const minGranularitySeconds int64 = 60

// Dataset categories organize the customer-facing dataset picker.
const (
	DatasetCategoryEvent = "event"
	DatasetCategoryUsage = "usage"
)

// Canonical strategies describe how an observation table becomes one logical
// row at the dataset grain.
const (
	CanonicalStrategyStandard = "standard"
	CanonicalStrategyTurn     = "turn"
)

// Field types decide the legal filter operators and UI input shape.
const (
	FieldTypeString = "string"
	FieldTypeFloat  = "float"
	FieldTypeInt    = "int"
)

// Field roles decide where a field can be used in the query grammar.
const (
	FieldRoleDimension = "dimension"
	FieldRoleMeasure   = "measure"
)

var (
	stringFilterOps  = []string{"in", "not_in", "contains", "exists"}
	numericFilterOps = []string{"eq", "neq", "gt", "gte", "lt", "lte"}
)

type fieldDefinition struct {
	Name        string
	Label       string
	Description string
}

var fieldDefinitions = []fieldDefinition{
	{Name: "project_id", Label: "Project", Description: "Gram project associated with the activity"},
	{Name: "granularity", Label: "Reporting interval", Description: "Provider reporting interval represented by the usage row"},
	{Name: "provider", Label: "Provider", Description: "AI provider, such as Anthropic"},
	{Name: "surface", Label: "Surface", Description: "Product that generated the activity, such as Claude Code or Claude Chat"},
	{Name: "account_type", Label: "Account type", Description: "Provider account type, such as team or personal"},
	{Name: "user_key", Label: "User", Description: "User identity associated with the activity"},
	{Name: "session_id", Label: "Session", Description: "Agent session or conversation"},
	{Name: "turn_id", Label: "Turn", Description: "Prompt or turn within a session"},
	{Name: "query_source", Label: "Query source", Description: "How the turn started, such as the main session or a subagent"},
	{Name: "request_model", Label: "Request model", Description: "Model requested from the AI provider"},
	{Name: "response_model", Label: "Response model", Description: "Model reported in the AI provider response"},
	{Name: "event_name", Label: "Event", Description: "Type of agent activity, such as a prompt, tool call, or API request"},
	{Name: "tool_name", Label: "Tool", Description: "Tool used for the activity"},
	{Name: "mcp_server", Label: "MCP server", Description: "MCP server that provided the tool"},
	{Name: "skill_name", Label: "Skill", Description: "Skill used for the activity"},
	{Name: "status", Label: "Status", Description: "Outcome of the activity"},
	{Name: "terminal", Label: "Terminal", Description: "Application that ran the session"},
}

// Field is one typed field exposed by a semantic dataset.
type Field struct {
	// Name is the stable query name.
	Name string

	// Label is the customer-facing display name.
	Label string

	// Type is string, float, or int.
	Type string

	// Role is dimension or measure.
	Role string

	// Unit is the measure's value unit and is empty for dimensions.
	Unit string

	// Description is the customer-facing explanation served by meta.
	Description string

	// Expr is the physical observation-table expression canonicalized into
	// c_<name>. Wide measures point directly at their typed columns.
	Expr string
}

func (f *Field) filterOps() []string {
	if f.Role == FieldRoleDimension {
		return stringFilterOps
	}
	return numericFilterOps
}

func (f *Field) canonicalExpr() string {
	if f.Name == "project_id" {
		return "toString(project_id)"
	}
	return canonicalColumn(f.Name)
}

func (f *Field) presenceExpr() string {
	return "isNotNull(" + f.canonicalExpr() + ")"
}

// Dataset is one semantic unit of analysis backed by an append-only
// observation table.
type Dataset struct {
	// Name is the stable dataset identifier.
	Name string

	// Label is the customer-facing display name.
	Label string

	// Category groups the dataset as event or usage.
	Category string

	// Table is the physical observation table.
	Table string

	// MeasurementName scopes a semantic usage dataset within a shared physical
	// measurements table. It is empty for datasets with a dedicated table.
	MeasurementName string

	// CanonicalStrategy selects the dataset's canonicalization pipeline.
	CanonicalStrategy string

	// Description explains the logical row population.
	Description string

	// Grain names what one canonical row represents.
	Grain string

	// TimeColumn is the physical DateTime64 column used for time windows.
	TimeColumn string

	// Fields are the typed customer-facing fields, in display order.
	Fields []Field
}

var datasets = []Dataset{
	{
		Name:              "events",
		Label:             "Events",
		Category:          DatasetCategoryEvent,
		Table:             "chat_events",
		MeasurementName:   "",
		CanonicalStrategy: CanonicalStrategyStandard,
		Description:       "Individual agent activity events.",
		Grain:             "event",
		TimeColumn:        "occurred_at",
		Fields: append(
			dimensionFields(
				"project_id",
				"provider",
				"surface",
				"account_type",
				"user_key",
				"session_id",
				"turn_id",
				"query_source",
				"request_model",
				"response_model",
				"event_name",
				"tool_name",
				"mcp_server",
				"skill_name",
				"status",
				"terminal",
			),
			measureField("duration_ms", "Duration", FieldTypeInt, "ms", "Reported activity duration in milliseconds"),
		),
	},
	{
		Name:              "turn_usage",
		Label:             "Turn usage",
		Category:          DatasetCategoryUsage,
		Table:             "chat_measurements",
		MeasurementName:   "turn_usage",
		CanonicalStrategy: CanonicalStrategyTurn,
		Description:       "Cost and token usage for each agent turn.",
		Grain:             "turn",
		TimeColumn:        "occurred_at",
		Fields: append(
			dimensionFields(
				"project_id",
				"provider",
				"surface",
				"account_type",
				"user_key",
				"session_id",
				"turn_id",
				"query_source",
				"request_model",
				"response_model",
			),
			usageMeasureFields()...,
		),
	},
	{
		Name:              "user_usage",
		Label:             "User usage",
		Category:          DatasetCategoryUsage,
		Table:             "chat_measurements",
		MeasurementName:   "user_usage",
		CanonicalStrategy: CanonicalStrategyStandard,
		Description:       "Cost and token usage for each user and reporting interval.",
		Grain:             "user and reporting interval",
		TimeColumn:        "occurred_at",
		Fields: append(
			dimensionFields(
				"project_id",
				"granularity",
				"provider",
				"surface",
				"account_type",
				"user_key",
				"request_model",
				"response_model",
			),
			usageMeasureFields()...,
		),
	},
}

func dimensionFields(names ...string) []Field {
	out := make([]Field, 0, len(names))
	for _, name := range names {
		definition := fieldDefinitionByName(name)
		out = append(out, Field{
			Name:        name,
			Label:       definition.Label,
			Type:        FieldTypeString,
			Role:        FieldRoleDimension,
			Unit:        "",
			Description: definition.Description,
			Expr:        name,
		})
	}
	return out
}

func measureField(name, label, fieldType, unit, description string) Field {
	return Field{
		Name:        name,
		Label:       label,
		Type:        fieldType,
		Role:        FieldRoleMeasure,
		Unit:        unit,
		Description: description,
		Expr:        name,
	}
}

func usageMeasureFields() []Field {
	return []Field{
		measureField("cost_usd", "Cost", FieldTypeFloat, "usd", "AI usage cost in USD"),
		measureField("input_tokens", "Input tokens", FieldTypeInt, "tokens", "Input or prompt tokens"),
		measureField("output_tokens", "Output tokens", FieldTypeInt, "tokens", "Output or completion tokens"),
		measureField("cache_read_tokens", "Cache-read tokens", FieldTypeInt, "tokens", "Input tokens read from a prompt cache"),
		measureField("cache_write_tokens", "Cache-write tokens", FieldTypeInt, "tokens", "Input tokens written to a prompt cache"),
	}
}

func fieldDefinitionByName(name string) fieldDefinition {
	for _, definition := range fieldDefinitions {
		if definition.Name == name {
			return definition
		}
	}
	return fieldDefinition{Name: name, Label: name, Description: ""}
}

func datasetByName(name string) (*Dataset, bool) {
	for i := range datasets {
		if datasets[i].Name == name {
			return &datasets[i], true
		}
	}
	return nil, false
}

func (d *Dataset) fieldByName(name string) (*Field, bool) {
	for i := range d.Fields {
		if d.Fields[i].Name == name {
			return &d.Fields[i], true
		}
	}
	return nil, false
}

func (d *Dataset) canonicalFields() []Field {
	out := make([]Field, 0, len(d.Fields))
	for i := range d.Fields {
		if d.Fields[i].Name != "project_id" {
			out = append(out, d.Fields[i])
		}
	}
	return out
}

func (d *Dataset) dimensionColumn(name string) (string, bool) {
	field, ok := d.fieldByName(name)
	if !ok || field.Role != FieldRoleDimension {
		return "", false
	}
	return field.canonicalExpr(), true
}

func (d *Dataset) measureNames() []string {
	out := make([]string, 0, len(d.Fields))
	for i := range d.Fields {
		if d.Fields[i].Role == FieldRoleMeasure {
			out = append(out, d.Fields[i].Name)
		}
	}
	return out
}

// UnknownMemberError reports a catalog member the query does not declare.
type UnknownMemberError struct {
	// Kind is the member kind.
	Kind string

	// Name echoes the requested name.
	Name string

	// Detail optionally narrows the failure.
	Detail string
}

func (e *UnknownMemberError) Error() string {
	msg := fmt.Sprintf("unknown %s %q", e.Kind, e.Name)
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// QueryValidationError reports a structurally invalid query.
type QueryValidationError struct {
	Msg string
}

func (e *QueryValidationError) Error() string {
	return e.Msg
}
