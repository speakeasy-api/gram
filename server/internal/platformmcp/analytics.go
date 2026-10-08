package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
)

// AnalyticsEngine is the query path the analytics tools answer through: the
// same catalog, compiler and runner as the analytics RPC.
type AnalyticsEngine interface {
	Catalog() *analytics.Catalog
	Query(ctx context.Context, tenant analytics.Tenant, req analytics.Request) (*analytics.QueryResult, error)
	Values(ctx context.Context, tenant analytics.Tenant, req analytics.ValuesRequest) ([]analytics.DimensionValue, error)
}

var (
	// ErrAnalyticsNotEnabled marks a read for an organization that has
	// Explore switched off.
	ErrAnalyticsNotEnabled = errors.New("platform mcp analytics not enabled")
	// ErrAnalyticsInvalid marks a read that does not name one exact project.
	ErrAnalyticsInvalid = errors.New("invalid platform mcp analytics read")
)

// AnalyticsService serves the analytics catalog, values and queries to the
// Platform MCP.
type AnalyticsService struct {
	logger        *slog.Logger
	engine        AnalyticsEngine
	flags         feature.Provider
	organizations OrganizationSlugResolver
	// projects holds the caller to project:read on the exact project named.
	projects ProjectReadResolver
	budget   OperationBudget
}

// NewAnalyticsService returns nil without a logger, an engine, an
// organization resolver or a project reader, so the tools are served as stubs.
func NewAnalyticsService(logger *slog.Logger, engine AnalyticsEngine, flags feature.Provider, organizations OrganizationSlugResolver, projects ProjectReadResolver, budget OperationBudget) *AnalyticsService {
	if logger == nil || engine == nil || organizations == nil || projects == nil {
		return nil
	}
	return &AnalyticsService{logger: logger, engine: engine, flags: flags, organizations: organizations, projects: projects, budget: budget}
}

func (s *AnalyticsService) valid() bool {
	return s != nil && s.logger != nil && s.engine != nil && s.organizations != nil && s.projects != nil && s.budget.valid()
}

// AnalyticsProject is the project a result was read for.
type AnalyticsProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func analyticsProject(project ResolvedProject) AnalyticsProject {
	return AnalyticsProject{ID: project.ID.String(), Name: project.Name, Slug: project.Slug}
}

func analyticsTenant(principal Principal, project ResolvedProject) analytics.Tenant {
	return analytics.Tenant{OrganizationID: principal.OrganizationID, ProjectID: project.ID.String()}
}

// DescribeAnalyticsCatalogInput names the project the rollout flag and
// project:read are checked against; the catalog is the same for every project.
type DescribeAnalyticsCatalogInput struct {
	ProjectID string `json:"project_id"`
}

// AnalyticsLookup is a per-project map a dimension reads through at query time.
type AnalyticsLookup struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AnalyticsField is one queryable field of a dataset.
type AnalyticsField struct {
	Name string `json:"name"`
	// Type is string, int64 or float64.
	Type string `json:"type"`
	// Role is dimension or measure.
	Role string `json:"role"`
	// Default marks a field of the query the dataset opens on.
	Default bool `json:"default"`
	// Unit of a measure, when it has one.
	Unit string `json:"unit,omitempty"`
	// Operators a dimension admits in a filter.
	Operators []string `json:"operators,omitempty"`
	// Aggregations the field admits: ops over a measure, count_distinct over
	// a dimension.
	Aggregations []string `json:"aggregations,omitempty"`
	// Description is set when the catalog has more to say than the name.
	Description string `json:"description,omitempty"`
	// Lookup is the map this dimension reads through, if any.
	Lookup *AnalyticsLookup `json:"lookup,omitempty"`
}

// AnalyticsDataset is a dataset and the fields it exposes.
type AnalyticsDataset struct {
	Name string `json:"name"`
	// Kind is event or metric.
	Kind string `json:"kind"`
	// Grain is what one row represents, as a noun.
	Grain       string `json:"grain"`
	Description string `json:"description"`
	// MaxTimeRangeDays is the longest window a query may span.
	MaxTimeRangeDays int              `json:"max_time_range_days"`
	Fields           []AnalyticsField `json:"fields"`
}

// AnalyticsLimits restates the compiler's guardrails so a caller can stay
// inside them.
type AnalyticsLimits struct {
	MaxDimensions      int `json:"max_dimensions"`
	MaxFilterValues    int `json:"max_filter_values"`
	DefaultLimit       int `json:"default_limit"`
	MaxLimit           int `json:"max_limit"`
	MaxRowsLimit       int `json:"max_rows_limit"`
	DefaultValuesLimit int `json:"default_values_limit"`
	MaxValuesLimit     int `json:"max_values_limit"`
}

// DescribeAnalyticsCatalogOutput is the catalog every query is written in.
type DescribeAnalyticsCatalogOutput struct {
	Project  AnalyticsProject   `json:"project"`
	Datasets []AnalyticsDataset `json:"datasets"`
	// Grains are the time bucket widths a grouped query may use.
	Grains []string        `json:"grains"`
	Limits AnalyticsLimits `json:"limits"`
}

// Describe serves the catalog to a caller who may read the named project.
func (s *AnalyticsService) Describe(ctx context.Context, principal Principal, input DescribeAnalyticsCatalogInput) (DescribeAnalyticsCatalogOutput, error) {
	project, err := s.authorize(ctx, principal, input.ProjectID)
	if err != nil {
		return DescribeAnalyticsCatalogOutput{}, err
	}
	return describeAnalyticsCatalog(project, s.engine.Catalog()), nil
}

func describeAnalyticsCatalog(project ResolvedProject, catalog *analytics.Catalog) DescribeAnalyticsCatalogOutput {
	datasets := catalog.Datasets()
	out := DescribeAnalyticsCatalogOutput{
		Project:  analyticsProject(project),
		Datasets: make([]AnalyticsDataset, 0, len(datasets)),
		Grains:   analyticsNames(analytics.TimeGrains),
		Limits: AnalyticsLimits{
			MaxDimensions:      analytics.MaxDimensions,
			MaxFilterValues:    analytics.MaxFilterValues,
			DefaultLimit:       analytics.DefaultLimit,
			MaxLimit:           analytics.MaxLimit,
			MaxRowsLimit:       analytics.MaxRowsLimit,
			DefaultValuesLimit: analytics.DefaultValuesLimit,
			MaxValuesLimit:     analytics.MaxValuesLimit,
		},
	}
	for _, ds := range datasets {
		fields := make([]AnalyticsField, 0, len(ds.Fields))
		for _, f := range ds.Fields {
			field := AnalyticsField{
				Name:         f.Name,
				Type:         string(f.Type),
				Role:         string(f.Role),
				Default:      f.Default,
				Unit:         f.Unit,
				Operators:    analyticsNames(f.Operators),
				Aggregations: analyticsNames(f.Aggregations),
				Description:  f.Description,
				Lookup:       nil,
			}
			if lookup, ok := catalog.Lookup(f.Lookup); ok {
				field.Lookup = &AnalyticsLookup{Name: lookup.Name, Description: lookup.Description}
			}
			fields = append(fields, field)
		}
		out.Datasets = append(out.Datasets, AnalyticsDataset{
			Name:             ds.Name,
			Kind:             string(ds.Kind),
			Grain:            ds.Grain,
			Description:      ds.Description,
			MaxTimeRangeDays: ds.MaxTimeRangeDays(),
			Fields:           fields,
		})
	}
	return out
}

// analyticsNames renders a catalog enum as strings, nil when empty so it is
// omitted.
func analyticsNames[T ~string](values []T) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = string(value)
	}
	return out
}

// ListAnalyticsDimensionValuesInput asks what one dimension holds in a window.
type ListAnalyticsDimensionValuesInput struct {
	ProjectID string `json:"project_id"`
	Dataset   string `json:"dataset"`
	Dimension string `json:"dimension"`
	From      string `json:"from"`
	To        string `json:"to"`
	Limit     int    `json:"limit,omitempty"`
}

// AnalyticsDimensionValue is one value and how many rows carry it.
type AnalyticsDimensionValue struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// ListAnalyticsDimensionValuesOutput lists a dimension's values, most
// frequent first.
type ListAnalyticsDimensionValuesOutput struct {
	Project   AnalyticsProject          `json:"project"`
	Dataset   string                    `json:"dataset"`
	Dimension string                    `json:"dimension"`
	Values    []AnalyticsDimensionValue `json:"values"`
}

// Values lists what a dimension holds, read the way a filter will match it.
func (s *AnalyticsService) Values(ctx context.Context, principal Principal, input ListAnalyticsDimensionValuesInput) (ListAnalyticsDimensionValuesOutput, error) {
	var zero ListAnalyticsDimensionValuesOutput
	project, err := s.authorize(ctx, principal, input.ProjectID)
	if err != nil {
		return zero, err
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return zero, err
	}
	from, to, err := analytics.ParseWindow(input.From, input.To)
	if err != nil {
		return zero, fmt.Errorf("read dimension values window: %w", err)
	}
	values, err := s.engine.Values(ctx, analyticsTenant(principal, project), analytics.ValuesRequest{
		Dataset:      input.Dataset,
		Dimension:    input.Dimension,
		FromUnixNano: from,
		ToUnixNano:   to,
		Limit:        input.Limit,
	})
	if err != nil {
		return zero, fmt.Errorf("list dimension values: %w", err)
	}
	out := ListAnalyticsDimensionValuesOutput{
		Project:   analyticsProject(project),
		Dataset:   input.Dataset,
		Dimension: input.Dimension,
		Values:    make([]AnalyticsDimensionValue, 0, len(values)),
	}
	for _, value := range values {
		out.Values = append(out.Values, AnalyticsDimensionValue{Value: value.Value, Count: value.Count})
	}
	return out, nil
}

// AnalyticsMeasure is an op over a field, or count alone.
type AnalyticsMeasure struct {
	Op    string `json:"op"`
	Field string `json:"field,omitempty"`
	Alias string `json:"alias,omitempty"`
}

// AnalyticsFilter is a predicate on a dimension. All filters are ANDed.
type AnalyticsFilter struct {
	Field    string   `json:"field"`
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
}

// AnalyticsOrderBy sorts a grouped result by one of its measures.
type AnalyticsOrderBy struct {
	Measure   string `json:"measure"`
	Direction string `json:"direction,omitempty"`
}

// RunAnalyticsQueryInput is a query against one dataset, mirroring the
// analytics RPC's payload.
type RunAnalyticsQueryInput struct {
	ProjectID  string             `json:"project_id"`
	Dataset    string             `json:"dataset"`
	From       string             `json:"from"`
	To         string             `json:"to"`
	Grain      string             `json:"grain,omitempty"`
	Dimensions []string           `json:"dimensions,omitempty"`
	Measures   []AnalyticsMeasure `json:"measures,omitempty"`
	Filters    []AnalyticsFilter  `json:"filters,omitempty"`
	OrderBy    []AnalyticsOrderBy `json:"order_by,omitempty"`
	Limit      int                `json:"limit,omitempty"`
	Ungrouped  bool               `json:"ungrouped,omitempty"`
}

// AnalyticsColumn is one result column.
type AnalyticsColumn struct {
	Name string `json:"name"`
	// Kind is time, dimension or measure.
	Kind string `json:"kind"`
}

// RunAnalyticsQueryOutput serves both modes: row keys are the dimensions and
// measure aliases, plus time_bucket under a grain or time when ungrouped.
type RunAnalyticsQueryOutput struct {
	Project AnalyticsProject `json:"project"`
	Dataset string           `json:"dataset"`
	// Plan is a stable diagnostic identifier, not a table name.
	Plan    string            `json:"plan"`
	Columns []AnalyticsColumn `json:"columns"`
	Rows    []map[string]any  `json:"rows"`
}

// Query runs one query for a caller who may read the named project.
func (s *AnalyticsService) Query(ctx context.Context, principal Principal, input RunAnalyticsQueryInput) (RunAnalyticsQueryOutput, error) {
	var zero RunAnalyticsQueryOutput
	project, err := s.authorize(ctx, principal, input.ProjectID)
	if err != nil {
		return zero, err
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return zero, err
	}
	from, to, err := analytics.ParseWindow(input.From, input.To)
	if err != nil {
		return zero, fmt.Errorf("read analytics query window: %w", err)
	}
	req := analytics.Request{
		Dataset:      input.Dataset,
		FromUnixNano: from,
		ToUnixNano:   to,
		Grain:        analytics.TimeGrain(input.Grain),
		Dimensions:   input.Dimensions,
		Measures:     make([]analytics.Measure, 0, len(input.Measures)),
		Filters:      make([]analytics.Filter, 0, len(input.Filters)),
		OrderBy:      make([]analytics.OrderBy, 0, len(input.OrderBy)),
		Limit:        input.Limit,
		Ungrouped:    input.Ungrouped,
	}
	for _, measure := range input.Measures {
		req.Measures = append(req.Measures, analytics.Measure{Op: measure.Op, Field: measure.Field, Alias: measure.Alias})
	}
	for _, filter := range input.Filters {
		req.Filters = append(req.Filters, analytics.Filter{Field: filter.Field, Operator: filter.Operator, Values: filter.Values})
	}
	for _, order := range input.OrderBy {
		req.OrderBy = append(req.OrderBy, analytics.OrderBy{Measure: order.Measure, Direction: order.Direction})
	}
	result, err := s.engine.Query(ctx, analyticsTenant(principal, project), req)
	if err != nil {
		return zero, fmt.Errorf("run analytics query: %w", err)
	}
	out := RunAnalyticsQueryOutput{
		Project: analyticsProject(project),
		Dataset: result.Dataset,
		Plan:    result.Plan,
		Columns: make([]AnalyticsColumn, 0, len(result.Columns)),
		Rows:    make([]map[string]any, 0, len(result.Rows)),
	}
	for _, column := range result.Columns {
		out.Columns = append(out.Columns, AnalyticsColumn{Name: column.Name, Kind: string(column.Kind)})
	}
	for _, row := range result.Rows {
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// authorize holds the caller to project:read on the exact project, then to
// the Explore flag. A project the caller cannot read reads as one that does
// not exist. Values and Query then charge the budget; Describe, which returns
// only the static catalog, is not charged.
func (s *AnalyticsService) authorize(ctx context.Context, principal Principal, projectID string) (ResolvedProject, error) {
	var zero ResolvedProject
	if !s.valid() {
		return zero, ErrUnavailable
	}
	if principal.OrganizationID == "" || projectID == "" {
		return zero, ErrAnalyticsInvalid
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return zero, ErrAnalyticsInvalid
	}
	project, err := s.projects.ResolveProjectRead(ctx, principal, FindMCPInput{ProjectID: projectID, ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""})
	if err != nil {
		return zero, fmt.Errorf("authorize analytics read: %w", err)
	}
	organizationSlug, err := s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil {
		return zero, fmt.Errorf("%w: resolve organization slug for analytics: %w", ErrUnavailable, err)
	}
	if organizationSlug == "" {
		return zero, fmt.Errorf("%w: organization slug is unavailable", ErrUnavailable)
	}
	// Disabled, missing and indeterminate all fail closed.
	evaluation, err := feature.EvaluateFlag(ctx, s.flags, feature.FlagExplore, principal.OrganizationID, feature.OrgProjectGroups(organizationSlug, project.Slug))
	if err != nil {
		return zero, fmt.Errorf("%w: evaluate analytics capability: %w", ErrUnavailable, err)
	}
	if evaluation != feature.EvaluationEnabled {
		return zero, ErrAnalyticsNotEnabled
	}
	return project, nil
}
