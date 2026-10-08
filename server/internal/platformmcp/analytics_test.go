package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
)

// stubAnalyticsEngine answers from the default catalog and records what it
// was asked, so a test can see the request reach the engine unchanged.
type stubAnalyticsEngine struct {
	result        *analytics.QueryResult
	values        []analytics.DimensionValue
	err           error
	tenant        analytics.Tenant
	request       analytics.Request
	valuesRequest analytics.ValuesRequest
	calls         int
}

func (s *stubAnalyticsEngine) Catalog() *analytics.Catalog { return analytics.Default }

func (s *stubAnalyticsEngine) Query(_ context.Context, tenant analytics.Tenant, req analytics.Request) (*analytics.QueryResult, error) {
	s.calls++
	s.tenant = tenant
	s.request = req
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func (s *stubAnalyticsEngine) Values(_ context.Context, tenant analytics.Tenant, req analytics.ValuesRequest) ([]analytics.DimensionValue, error) {
	s.calls++
	s.tenant = tenant
	s.valuesRequest = req
	if s.err != nil {
		return nil, s.err
	}
	return s.values, nil
}

// stubAnalyticsProjects resolves one project for every caller, or refuses
// every caller, and records what it was asked to resolve.
type stubAnalyticsProjects struct {
	project ResolvedProject
	err     error
	inputs  []FindMCPInput
}

func (s *stubAnalyticsProjects) ResolveProjectRead(_ context.Context, _ Principal, input FindMCPInput) (ResolvedProject, error) {
	s.inputs = append(s.inputs, input)
	if s.err != nil {
		return ResolvedProject{}, s.err
	}
	return s.project, nil
}

type analyticsTestHarness struct {
	reg      *Registrar
	engine   *stubAnalyticsEngine
	projects *stubAnalyticsProjects
	flags    *riskMutationFlagProvider
	project  ResolvedProject
}

var (
	allowAnalyticsBudget = OperationBudget{Connection: allowOperationLimiter{}, Organization: allowOperationLimiter{}}
	analyticsToolNames   = []string{describeAnalyticsCatalogToolName, listAnalyticsDimensionValuesToolName, runAnalyticsQueryToolName}
	analyticsTestWindow  = `"from":"2026-09-01T00:00:00Z","to":"2026-09-08T00:00:00Z"`
	analyticsTestFrom    = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	analyticsTestTo      = time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
)

func newAnalyticsTestHarness(t *testing.T, evaluation feature.Evaluation, budget OperationBudget) *analyticsTestHarness {
	t.Helper()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	engine := &stubAnalyticsEngine{}
	projects := &stubAnalyticsProjects{project: project, err: nil, inputs: nil}
	flags := &riskMutationFlagProvider{evaluation: evaluation, err: nil, flag: "", groups: nil}
	service := NewAnalyticsService(engine, flags, riskMutationOrganizationResolver{slug: "org", err: nil}, projects, budget)
	require.NotNil(t, service)

	reg := newRegistrar(newTestMCPServer())
	registerAnalyticsTools(reg, service)
	return &analyticsTestHarness{reg: reg, engine: engine, projects: projects, flags: flags, project: project}
}

func (h *analyticsTestHarness) invoke(t *testing.T, tool, arguments string) (any, error) {
	t.Helper()
	return descriptorByName(t, h.reg, tool).Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(arguments))
}

func (h *analyticsTestHarness) projectArguments() string {
	return `{"project_id":"` + h.project.ID.String() + `"}`
}

func (h *analyticsTestHarness) valuesArguments(extra string) string {
	return `{"project_id":"` + h.project.ID.String() + `","dataset":"tool_calls","dimension":"tool_name",` + analyticsTestWindow + extra + `}`
}

func (h *analyticsTestHarness) queryArguments(extra string) string {
	return `{"project_id":"` + h.project.ID.String() + `","dataset":"tool_calls",` + analyticsTestWindow + extra + `}`
}

// validArguments is one well-formed call per tool.
func (h *analyticsTestHarness) validArguments() map[string]string {
	return map[string]string{
		describeAnalyticsCatalogToolName:     h.projectArguments(),
		listAnalyticsDimensionValuesToolName: h.valuesArguments(""),
		runAnalyticsQueryToolName:            h.queryArguments(`,"measures":[{"op":"count"}]`),
	}
}

func requireAnalyticsRefusal(t *testing.T, err error) analyticsRefusal {
	t.Helper()
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	var decoded analyticsRefusal
	require.NoError(t, json.Unmarshal([]byte(refusal.Payload), &decoded))
	return decoded
}

func TestAnalyticsToolsDeclareTheirContract(t *testing.T) {
	t.Parallel()

	h := newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	for _, name := range analyticsToolNames {
		descriptor := descriptorByName(t, h.reg, name)
		require.True(t, descriptor.Annotations.ReadOnlyHint, name)
		require.Equal(t, ExternalAuthorizationMember, descriptor.Meta.Authorization, name)
		require.ElementsMatch(t, bothAudiences, descriptor.Meta.Audiences, name)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope, name)
		require.Equal(t, discoveryProjectRead, descriptor.Meta.DiscoveryScopes, name)
		require.Contains(t, descriptor.Description, "Explore", name)
		require.NotContains(t, descriptor.Description, "unavailable in this deployment", name)
		require.NotContains(t, descriptor.Description, "Gram", name)

		var schema struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(descriptor.InputSchema, &schema))
		require.Contains(t, schema.Required, "project_id", name)
		require.Contains(t, schema.Properties, "project_id", name)
		require.NotContains(t, schema.Properties, "project_slug", "an explicit-project tool names the project by ID: %s", name)
	}

	// The query schema restates the compiler's vocabulary and bounds, so a
	// model sees them before it is refused for crossing one.
	var query struct {
		Required   []string `json:"required"`
		Properties struct {
			Grain struct {
				Enum []string `json:"enum"`
			} `json:"grain"`
			Dimensions struct {
				MaxItems int `json:"maxItems"`
			} `json:"dimensions"`
			Limit struct {
				Maximum int `json:"maximum"`
			} `json:"limit"`
			Measures struct {
				Items struct {
					Properties struct {
						Op struct {
							Enum []string `json:"enum"`
						} `json:"op"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"measures"`
			Filters struct {
				Items struct {
					Properties struct {
						Operator struct {
							Enum []string `json:"enum"`
						} `json:"operator"`
						Values struct {
							MaxItems int `json:"maxItems"`
						} `json:"values"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"filters"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(descriptorByName(t, h.reg, runAnalyticsQueryToolName).InputSchema, &query))
	require.Equal(t, []string{"project_id", "dataset", "from", "to"}, query.Required)
	require.Equal(t, []string{"none", "hour", "day", "week", "month"}, query.Properties.Grain.Enum)
	require.Equal(t, analytics.MaxDimensions, query.Properties.Dimensions.MaxItems)
	require.Equal(t, analytics.MaxLimit, query.Properties.Limit.Maximum)
	require.Equal(t, []string{"count", "count_distinct", "sum", "avg", "min", "max", "p50", "p95", "p99"}, query.Properties.Measures.Items.Properties.Op.Enum)
	require.Equal(t, []string{"equals", "in"}, query.Properties.Filters.Items.Properties.Operator.Enum)
	require.Equal(t, analytics.MaxFilterValues, query.Properties.Filters.Items.Properties.Values.MaxItems)

	var values struct {
		Required   []string `json:"required"`
		Properties struct {
			Limit struct {
				Maximum int `json:"maximum"`
			} `json:"limit"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(descriptorByName(t, h.reg, listAnalyticsDimensionValuesToolName).InputSchema, &values))
	require.Equal(t, []string{"project_id", "dataset", "dimension", "from", "to"}, values.Required)
	require.Equal(t, analytics.MaxValuesLimit, values.Properties.Limit.Maximum)
}

// Without an engine the three tools keep their names, schemas, audiences and
// authorization and refuse readably, so the catalogue has one shape in every
// deployment.
func TestAnalyticsToolsStubWithoutAService(t *testing.T) {
	t.Parallel()

	reg := newRegistrar(newTestMCPServer())
	registerAnalyticsTools(reg, nil)
	live := newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	for name, arguments := range live.validArguments() {
		descriptor := descriptorByName(t, reg, name)
		require.Contains(t, descriptor.Description, "unavailable in this deployment", name)
		require.Equal(t, live.reg.descriptors[indexOfDescriptor(t, live.reg, name)].Meta, descriptor.Meta, name)
		require.JSONEq(t, string(descriptorByName(t, live.reg, name).InputSchema), string(descriptor.InputSchema), name)

		_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(arguments))
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal, name)
		require.Contains(t, refusal.Payload, `"code":"feature_unavailable"`, name)
		require.Contains(t, refusal.Payload, `"feature":"analytics"`, name)
	}

	require.Nil(t, NewAnalyticsService(nil, nil, riskMutationOrganizationResolver{slug: "org", err: nil}, &stubAnalyticsProjects{}, allowAnalyticsBudget))
	require.Nil(t, NewAnalyticsService(&stubAnalyticsEngine{}, nil, nil, &stubAnalyticsProjects{}, allowAnalyticsBudget))
	require.Nil(t, NewAnalyticsService(&stubAnalyticsEngine{}, nil, riskMutationOrganizationResolver{slug: "org", err: nil}, nil, allowAnalyticsBudget))
}

func indexOfDescriptor(t *testing.T, reg *Registrar, name string) int {
	t.Helper()
	for i, descriptor := range reg.Descriptors() {
		if descriptor.Name == name {
			return i
		}
	}
	t.Fatalf("tool %q was not registered", name)
	return -1
}

func TestDescribeAnalyticsCatalogServesTheCatalog(t *testing.T) {
	t.Parallel()

	h := newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	raw, err := h.invoke(t, describeAnalyticsCatalogToolName, h.projectArguments())
	require.NoError(t, err)
	output, ok := raw.(DescribeAnalyticsCatalogOutput)
	require.True(t, ok)

	require.Equal(t, AnalyticsProject{ID: h.project.ID.String(), Name: "Project", Slug: "project"}, output.Project)
	require.Equal(t, []string{"none", "hour", "day", "week", "month"}, output.Grains)
	require.Equal(t, AnalyticsLimits{MaxDimensions: 3, MaxFilterValues: 100, DefaultLimit: 100, MaxLimit: 1000, DefaultValuesLimit: 50, MaxValuesLimit: 200}, output.Limits)

	// Every dataset the catalog declares is served, in declaration order.
	declared := make([]string, 0, len(analytics.Default.Datasets()))
	for _, ds := range analytics.Default.Datasets() {
		declared = append(declared, ds.Name)
	}
	served := make([]string, 0, len(output.Datasets))
	byName := make(map[string]AnalyticsDataset, len(output.Datasets))
	for _, ds := range output.Datasets {
		served = append(served, ds.Name)
		byName[ds.Name] = ds
	}
	require.Equal(t, declared, served)

	sessions := byName["sessions"]
	require.Equal(t, "event", sessions.Kind)
	require.Equal(t, "session", sessions.Grain)
	require.Equal(t, analytics.MaxEventTimeRangeDays, sessions.MaxTimeRangeDays)
	fields := make(map[string]AnalyticsField, len(sessions.Fields))
	for _, f := range sessions.Fields {
		fields[f.Name] = f
	}
	require.Equal(t, AnalyticsField{Name: "user", Type: "string", Role: "dimension", Default: true, Unit: "", Operators: []string{"equals", "in"}, Aggregations: []string{"count_distinct"}, Description: "", Lookup: nil}, fields["user"])
	require.Equal(t, "measure", fields["duration_seconds"].Role)
	require.Equal(t, "s", fields["duration_seconds"].Unit)
	require.Equal(t, []string{"sum", "avg", "p95"}, fields["duration_seconds"].Aggregations)
	require.Empty(t, fields["duration_seconds"].Operators)

	toolFields := make(map[string]AnalyticsField, len(byName["tool_calls"].Fields))
	for _, f := range byName["tool_calls"].Fields {
		toolFields[f.Name] = f
	}
	require.NotNil(t, toolFields["mcp_server"].Lookup, "describe says which map mcp_server reads through")
	require.Equal(t, analytics.MCPServerDisplayNamesLookup, toolFields["mcp_server"].Lookup.Name)
	require.NotEmpty(t, toolFields["mcp_server"].Description)
	require.Nil(t, toolFields["tool_name"].Lookup)

	// The flag was evaluated for this organization and project, and the
	// project resolved exactly as named.
	require.Equal(t, feature.FlagExplore, h.flags.flag)
	require.Equal(t, map[string]string{"organization": "org", "slug": "org/project"}, h.flags.groups)
	require.Equal(t, []FindMCPInput{{ProjectID: h.project.ID.String(), ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""}}, h.projects.inputs)

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), `"unit":""`, "what the catalog left empty is omitted")
	require.NotContains(t, string(encoded), `"lookup":null`)
}

func TestAnalyticsToolsRefuseWhenExploreIsOff(t *testing.T) {
	t.Parallel()

	for _, evaluation := range []feature.Evaluation{feature.EvaluationDisabled, feature.EvaluationIndeterminate} {
		h := newAnalyticsTestHarness(t, evaluation, allowAnalyticsBudget)
		for name, arguments := range h.validArguments() {
			_, err := h.invoke(t, name, arguments)
			refusal := requireAnalyticsRefusal(t, err)
			require.Equal(t, unavailableCode, refusal.Code, name)
			require.Equal(t, analyticsFeature, refusal.Feature, name)
			require.Contains(t, refusal.Message, "not switched on", name)
			require.Contains(t, refusal.Message, "Explore", name)
		}
		require.Equal(t, feature.FlagExplore, h.flags.flag)
		require.Zero(t, h.engine.calls, "a refused caller never reaches the engine")
	}

	// A flag service that cannot answer is unavailable, not off.
	h := newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	h.flags.err = errors.New("flag service away")
	_, err := h.invoke(t, describeAnalyticsCatalogToolName, h.projectArguments())
	refusal := requireAnalyticsRefusal(t, err)
	require.Equal(t, unavailableCode, refusal.Code)
	require.Contains(t, refusal.Message, "temporarily unavailable")
	require.Zero(t, h.engine.calls)
}

// project:read on the exact project comes first: a caller who cannot read a
// project learns nothing about it, not even whether analytics is on.
func TestAnalyticsToolsRequireProjectReadBeforeAnythingElse(t *testing.T) {
	t.Parallel()

	h := newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	h.projects.err = ErrForbidden
	for name, arguments := range h.validArguments() {
		_, err := h.invoke(t, name, arguments)
		refusal := requireAnalyticsRefusal(t, err)
		require.Equal(t, "forbidden", refusal.Code, name)
		require.Equal(t, analyticsFeature, refusal.Feature, name)
	}
	require.Len(t, h.projects.inputs, len(analyticsToolNames))
	require.Empty(t, h.flags.flag, "the flag is evaluated only for a project the caller may read")
	require.Zero(t, h.engine.calls)

	// A project ID that is not an ID never reaches the resolver.
	h = newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	_, err := h.invoke(t, describeAnalyticsCatalogToolName, `{"project_id":"not-an-id"}`)
	refusal := requireAnalyticsRefusal(t, err)
	require.Equal(t, "invalid_request", refusal.Code)
	require.Contains(t, refusal.Message, "list_projects")
	require.Empty(t, h.projects.inputs)
}

func TestAnalyticsToolsChargeTheDiagnosticsBudget(t *testing.T) {
	t.Parallel()

	h := newAnalyticsTestHarness(t, feature.EvaluationEnabled, OperationBudget{Connection: denyOperationLimiter{}, Organization: allowOperationLimiter{}})
	_, err := h.invoke(t, describeAnalyticsCatalogToolName, h.projectArguments())
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, `"code":"rate_limited"`)
	require.Zero(t, h.engine.calls)
}

func TestRunAnalyticsQueryHandsTheRequestToTheEngineUnchanged(t *testing.T) {
	t.Parallel()

	h := newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	h.engine.result = &analytics.QueryResult{
		Dataset: "tool_calls",
		Plan:    "tool_calls.agent_events",
		Columns: []analytics.Column{{Name: "time_bucket", Kind: analytics.ColumnTime}, {Name: "tool_name", Kind: analytics.ColumnDimension}, {Name: "calls", Kind: analytics.ColumnMeasure}},
		Rows:    []analytics.Row{{"time_bucket": "2026-09-01T00:00:00Z", "tool_name": "Bash", "calls": int64(3)}},
	}

	raw, err := h.invoke(t, runAnalyticsQueryToolName, h.queryArguments(`,"grain":"day","dimensions":["tool_name"],"measures":[{"op":"count","alias":"calls"},{"op":"count_distinct","field":"user"}],"filters":[{"field":"status","operator":"in","values":["ok","error"]}],"order_by":[{"measure":"calls","direction":"asc"}],"limit":10`))
	require.NoError(t, err)
	output, ok := raw.(RunAnalyticsQueryOutput)
	require.True(t, ok)

	require.Equal(t, analytics.Tenant{OrganizationID: "<ORG_ID>", ProjectID: h.project.ID.String()}, h.engine.tenant, "the tenant is the caller's organization and the resolved project, never a field of the request")
	require.Equal(t, analytics.Request{
		Dataset:      "tool_calls",
		FromUnixNano: analyticsTestFrom.UnixNano(),
		ToUnixNano:   analyticsTestTo.UnixNano(),
		Grain:        analytics.TimeGrainDay,
		Dimensions:   []string{"tool_name"},
		Measures:     []analytics.Measure{{Op: "count", Field: "", Alias: "calls"}, {Op: "count_distinct", Field: "user", Alias: ""}},
		Filters:      []analytics.Filter{{Field: "status", Operator: "in", Values: []string{"ok", "error"}}},
		OrderBy:      []analytics.OrderBy{{Measure: "calls", Direction: "asc"}},
		Limit:        10,
		Ungrouped:    false,
	}, h.engine.request)

	require.Equal(t, AnalyticsProject{ID: h.project.ID.String(), Name: "Project", Slug: "project"}, output.Project)
	require.Equal(t, "tool_calls", output.Dataset)
	require.Equal(t, "tool_calls.agent_events", output.Plan)
	require.Equal(t, []AnalyticsColumn{{Name: "time_bucket", Kind: "time"}, {Name: "tool_name", Kind: "dimension"}, {Name: "calls", Kind: "measure"}}, output.Columns)
	require.Len(t, output.Rows, 1)
	require.Equal(t, "Bash", output.Rows[0]["tool_name"])
	require.EqualValues(t, 3, output.Rows[0]["calls"])

	// An ungrouped request passes its mode through and carries nothing the
	// mode has no use for; an omitted grain reaches the compiler as none.
	_, err = h.invoke(t, runAnalyticsQueryToolName, h.queryArguments(`,"ungrouped":true,"dimensions":["tool_call","session"]`))
	require.NoError(t, err)
	require.True(t, h.engine.request.Ungrouped)
	require.Empty(t, h.engine.request.Grain)
	require.Empty(t, h.engine.request.Measures)
	require.Equal(t, []string{"tool_call", "session"}, h.engine.request.Dimensions)
	require.Equal(t, 2, h.engine.calls)
}

func TestRunAnalyticsQueryRefusesWhatTheEngineRefusesByName(t *testing.T) {
	t.Parallel()

	h := newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	h.engine.err = &analytics.Error{Code: analytics.ErrUnknownField, Dataset: "tool_calls", Field: "dimensions[0]", Value: "department", Message: "dataset tool_calls has no dimension department"}
	_, err := h.invoke(t, runAnalyticsQueryToolName, h.queryArguments(`,"dimensions":["department"],"measures":[{"op":"count"}]`))
	require.Equal(t, analyticsRefusal{
		Code:    "invalid_request",
		Feature: analyticsFeature,
		Reason:  "unknown_field",
		Field:   "dimensions[0]",
		Value:   "department",
		Message: "dataset tool_calls has no dimension department. Correct that from describe_analytics_catalog rather than retrying the same request.",
	}, requireAnalyticsRefusal(t, err))
	require.Equal(t, 1, h.engine.calls)

	// A window the engine could never run is refused before it is asked.
	_, err = h.invoke(t, runAnalyticsQueryToolName, `{"project_id":"`+h.project.ID.String()+`","dataset":"tool_calls","from":"1500-01-01T00:00:00Z","to":"2026-09-08T00:00:00Z","measures":[{"op":"count"}]}`)
	refusal := requireAnalyticsRefusal(t, err)
	require.Equal(t, "invalid_request", refusal.Code)
	require.Equal(t, "invalid_time_range", refusal.Reason)
	require.Equal(t, "from", refusal.Field)
	require.Equal(t, 1, h.engine.calls)

	// Anything else the engine fails with is a failure of the call, not a
	// refusal the model should act on.
	h.engine.err = errors.New("the warehouse is away")
	_, err = h.invoke(t, runAnalyticsQueryToolName, h.queryArguments(`,"measures":[{"op":"count"}]`))
	require.ErrorContains(t, err, "the warehouse is away")
}

func TestListAnalyticsDimensionValuesListsWhatTheEngineReturns(t *testing.T) {
	t.Parallel()

	h := newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	h.engine.values = []analytics.DimensionValue{{Value: "Bash", Count: 3}, {Value: "Read", Count: 1}}
	raw, err := h.invoke(t, listAnalyticsDimensionValuesToolName, h.valuesArguments(`,"limit":5`))
	require.NoError(t, err)
	output, ok := raw.(ListAnalyticsDimensionValuesOutput)
	require.True(t, ok)

	require.Equal(t, analytics.Tenant{OrganizationID: "<ORG_ID>", ProjectID: h.project.ID.String()}, h.engine.tenant)
	require.Equal(t, analytics.ValuesRequest{Dataset: "tool_calls", Dimension: "tool_name", FromUnixNano: analyticsTestFrom.UnixNano(), ToUnixNano: analyticsTestTo.UnixNano(), Limit: 5}, h.engine.valuesRequest)
	require.Equal(t, ListAnalyticsDimensionValuesOutput{
		Project:   AnalyticsProject{ID: h.project.ID.String(), Name: "Project", Slug: "project"},
		Dataset:   "tool_calls",
		Dimension: "tool_name",
		Values:    []AnalyticsDimensionValue{{Value: "Bash", Count: 3}, {Value: "Read", Count: 1}},
	}, output)

	// A dimension that holds nothing in the window says so rather than null.
	h.engine.values = nil
	raw, err = h.invoke(t, listAnalyticsDimensionValuesToolName, h.valuesArguments(""))
	require.NoError(t, err)
	encoded, err := json.Marshal(raw)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"values":[]`)
	require.Zero(t, h.engine.valuesRequest.Limit, "an omitted limit reaches the engine as its default")
}

// The schema rejects what the compiler would refuse anyway, so a model is
// told before the call and the resolver and engine never see it.
func TestAnalyticsToolSchemasRejectWhatTheCompilerWouldRefuse(t *testing.T) {
	t.Parallel()

	h := newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	for name, call := range map[string]struct{ tool, arguments string }{
		"a missing project":                     {runAnalyticsQueryToolName, `{"dataset":"tool_calls",` + analyticsTestWindow + `}`},
		"too many dimensions":                   {runAnalyticsQueryToolName, h.queryArguments(`,"dimensions":["a","b","c","d"]`)},
		"a repeated dimension":                  {runAnalyticsQueryToolName, h.queryArguments(`,"dimensions":["user","user"]`)},
		"a limit past the maximum":              {runAnalyticsQueryToolName, h.queryArguments(`,"limit":1001`)},
		"an op the catalog does not know":       {runAnalyticsQueryToolName, h.queryArguments(`,"measures":[{"op":"median","field":"duration_ms"}]`)},
		"an operator the catalog does not know": {runAnalyticsQueryToolName, h.queryArguments(`,"filters":[{"field":"status","operator":"like","values":["ok"]}]`)},
		"a filter with no values":               {runAnalyticsQueryToolName, h.queryArguments(`,"filters":[{"field":"status","operator":"in","values":[]}]`)},
		"a grain the catalog does not know":     {runAnalyticsQueryToolName, h.queryArguments(`,"grain":"minute"`)},
		"an argument the tool does not take":    {runAnalyticsQueryToolName, h.queryArguments(`,"sql":"select 1"`)},
		"a values limit past the maximum":       {listAnalyticsDimensionValuesToolName, h.valuesArguments(`,"limit":201`)},
		"a values call without a dimension":     {listAnalyticsDimensionValuesToolName, `{"project_id":"` + h.project.ID.String() + `","dataset":"tool_calls",` + analyticsTestWindow + `}`},
	} {
		_, err := h.invoke(t, call.tool, call.arguments)
		require.ErrorContains(t, err, "arguments do not match the tool schema", name)
	}
	require.Zero(t, h.engine.calls)
	require.Empty(t, h.projects.inputs, "a request the schema rejects never reaches the project resolver")
}
