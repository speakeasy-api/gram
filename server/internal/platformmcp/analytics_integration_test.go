package platformmcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// platformAnalyticsToolCall is one terminal tool call observation.
func platformAnalyticsToolCall(organizationID, projectID, recordID, sessionID, user, tool string, at time.Time) chrepo.AgentEventRow {
	return chrepo.AgentEventRow{
		OrganizationID:     organizationID,
		ProjectID:          projectID,
		OccurredAtUnixNano: at.UnixNano(),
		ObservedAtUnixNano: at.UnixNano(),
		RecordID:           recordID,
		SessionID:          sessionID,
		TurnID:             "t1",
		EventID:            recordID,
		EventType:          "tool_call_result",
		RawEventName:       "tool_call_result",
		Source:             "claude-code",
		Provider:           "anthropic",
		Surface:            "claude-code",
		UserID:             "",
		UserEmail:          user,
		ExternalUserID:     "",
		AccountType:        "",
		BillingMode:        "",
		ExternalOrgID:      "",
		DeviceID:           "",
		DepartmentName:     "",
		DivisionName:       "",
		JobTitle:           "",
		EmployeeType:       "",
		CostCenterName:     "",
		Roles:              nil,
		Groups:             nil,
		Model:              "claude-sonnet-4",
		QuerySource:        "",
		SkillName:          "",
		AgentName:          "",
		MCPServerName:      "",
		MCPToolName:        "",
		ToolName:           tool,
		Text:               "",
		Outcome:            "ok",
		OutcomeMessage:     "",
		DurationNano:       5_000_000,
		InputContent:       "",
		OutputContent:      "",
		InputTokens:        0,
		OutputTokens:       0,
		CacheReadTokens:    0,
		CacheWriteTokens:   0,
		CostUSD:            0,
		Attributes:         "{}",
		ResourceAttributes: "{}",
		ScopeAttributes:    "{}",
	}
}

func TestAnalyticsToolsAnswerFromClickHouse(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_analytics")
	require.NoError(t, err)
	chConn, err := platformMCPInfra.NewClickhouseClient(t)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	engine, err := analytics.NewEngine(conn, chConn)
	require.NoError(t, err)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagExplore, principal.OrganizationID, true)
	authzEngine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	reader := NewPostgresReader(testenv.NewLogger(t), conn).WithAuthorization(authzEngine)
	service := NewAnalyticsService(testenv.NewLogger(t), engine, flags, NewPostgresOrganizationSlugResolver(conn), reader, OperationBudget{Connection: allowOperationLimiter{}, Organization: allowOperationLimiter{}})
	require.NotNil(t, service)
	reg := newRegistrar(newTestMCPServer())
	registerAnalyticsTools(reg, service)

	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	call := func(recordID, sessionID, user, tool string, at time.Time) chrepo.AgentEventRow {
		return platformAnalyticsToolCall(principal.OrganizationID, project.ID.String(), recordID, sessionID, user, tool, at)
	}
	require.NoError(t, chrepo.New(chConn).InsertAgentEvents(ctx, []chrepo.AgentEventRow{
		call("c1", "s1", "ann@example.com", "Bash", base),
		call("c2", "s1", "ann@example.com", "Bash", base.Add(time.Minute)),
		call("c3", "s2", "bob@example.com", "Read", base.Add(2*time.Minute)),
		// Another project of the same organization: never in this project's answer.
		platformAnalyticsToolCall(principal.OrganizationID, "other-project", "c4", "s9", "eve@example.com", "Write", base.Add(3*time.Minute)),
	}))

	actor := contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	actor = contextvalues.SetActingSurface(actor, contextvalues.ActingSurfacePlatformMCP)
	granted := ContextWithPrincipal(authz.GrantsToContext(actor, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, project.ID.String())}), principal)
	window := `"from":"` + base.Add(-time.Hour).Format(time.RFC3339) + `","to":"` + base.Add(time.Hour).Format(time.RFC3339) + `"`
	projectID := `"project_id":"` + project.ID.String() + `"`

	t.Run("describe serves the catalog for the project", func(t *testing.T) {
		t.Parallel()
		raw, err := descriptorByName(t, reg, describeAnalyticsCatalogToolName).Invoke(granted, json.RawMessage(`{`+projectID+`}`))
		require.NoError(t, err)
		output, ok := raw.(DescribeAnalyticsCatalogOutput)
		require.True(t, ok)
		require.Equal(t, AnalyticsProject{ID: project.ID.String(), Name: project.Name, Slug: project.Slug}, output.Project)
		names := make([]string, 0, len(output.Datasets))
		for _, ds := range output.Datasets {
			names = append(names, ds.Name)
		}
		require.Contains(t, names, "sessions")
		require.Contains(t, names, "tool_calls")
	})

	t.Run("values lists what the dimension holds, most frequent first", func(t *testing.T) {
		t.Parallel()
		raw, err := descriptorByName(t, reg, listAnalyticsDimensionValuesToolName).Invoke(granted, json.RawMessage(`{`+projectID+`,"dataset":"tool_calls","dimension":"tool_name",`+window+`}`))
		require.NoError(t, err)
		output, ok := raw.(ListAnalyticsDimensionValuesOutput)
		require.True(t, ok)
		require.Equal(t, "tool_calls", output.Dataset)
		require.Equal(t, "tool_name", output.Dimension)
		require.Equal(t, []AnalyticsDimensionValue{{Value: "Bash", Count: 2}, {Value: "Read", Count: 1}}, output.Values, "the other project's Write is not a value here")
	})

	t.Run("a grouped query counts calls by tool", func(t *testing.T) {
		t.Parallel()
		raw, err := descriptorByName(t, reg, runAnalyticsQueryToolName).Invoke(granted, json.RawMessage(`{`+projectID+`,"dataset":"tool_calls",`+window+`,"dimensions":["tool_name"],"measures":[{"op":"count"},{"op":"count_distinct","field":"user","alias":"people"}]}`))
		require.NoError(t, err)
		output, ok := raw.(RunAnalyticsQueryOutput)
		require.True(t, ok)
		require.Equal(t, "tool_calls", output.Dataset)
		require.Equal(t, "tool_calls.agent_events", output.Plan)
		require.Equal(t, []AnalyticsColumn{{Name: "tool_name", Kind: "dimension"}, {Name: "count", Kind: "measure"}, {Name: "people", Kind: "measure"}}, output.Columns)
		require.Len(t, output.Rows, 2)
		require.Equal(t, "Bash", output.Rows[0]["tool_name"])
		require.EqualValues(t, 2, output.Rows[0]["count"])
		require.EqualValues(t, 1, output.Rows[0]["people"])
		require.Equal(t, "Read", output.Rows[1]["tool_name"])
		require.EqualValues(t, 1, output.Rows[1]["count"])
	})

	t.Run("an ungrouped query returns rows newest first", func(t *testing.T) {
		t.Parallel()
		raw, err := descriptorByName(t, reg, runAnalyticsQueryToolName).Invoke(granted, json.RawMessage(`{`+projectID+`,"dataset":"tool_calls",`+window+`,"ungrouped":true,"dimensions":["tool_call","user"],"limit":2}`))
		require.NoError(t, err)
		output, ok := raw.(RunAnalyticsQueryOutput)
		require.True(t, ok)
		require.Equal(t, []AnalyticsColumn{{Name: "time", Kind: "time"}, {Name: "tool_call", Kind: "dimension"}, {Name: "user", Kind: "dimension"}}, output.Columns)
		require.Len(t, output.Rows, 2, "the limit holds")
		require.Equal(t, "c3", output.Rows[0]["tool_call"])
		require.Equal(t, "bob@example.com", output.Rows[0]["user"])
		require.Equal(t, base.Add(2*time.Minute).Format(time.RFC3339Nano), output.Rows[0]["time"])
		require.Equal(t, "c2", output.Rows[1]["tool_call"])
	})

	t.Run("an invalid request is refused naming the field", func(t *testing.T) {
		t.Parallel()
		_, err := descriptorByName(t, reg, runAnalyticsQueryToolName).Invoke(granted, json.RawMessage(`{`+projectID+`,"dataset":"tool_calls",`+window+`,"dimensions":["department"],"measures":[{"op":"count"}]}`))
		refusal := requireAnalyticsRefusal(t, err)
		require.Equal(t, "invalid_request", refusal.Code)
		require.Equal(t, "unknown_field", refusal.Reason)
		require.Equal(t, "dimensions[0]", refusal.Field)
		require.Equal(t, "department", refusal.Value)
	})

	t.Run("a caller without project read is refused before any read", func(t *testing.T) {
		t.Parallel()
		denied := ContextWithPrincipal(authz.GrantsToContext(actor, nil), principal)
		for _, call := range []struct{ tool, arguments string }{
			{describeAnalyticsCatalogToolName, `{` + projectID + `}`},
			{listAnalyticsDimensionValuesToolName, `{` + projectID + `,"dataset":"tool_calls","dimension":"tool_name",` + window + `}`},
			{runAnalyticsQueryToolName, `{` + projectID + `,"dataset":"tool_calls",` + window + `,"measures":[{"op":"count"}]}`},
		} {
			_, err := descriptorByName(t, reg, call.tool).Invoke(denied, json.RawMessage(call.arguments))
			refusal := requireAnalyticsRefusal(t, err)
			require.Equal(t, "forbidden", refusal.Code, call.tool)
		}
	})
}

func TestAnalyticsToolsFollowTheExploreFlag(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_analytics_flag")
	require.NoError(t, err)
	chConn, err := platformMCPInfra.NewClickhouseClient(t)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	engine, err := analytics.NewEngine(conn, chConn)
	require.NoError(t, err)
	flags := &feature.InMemory{}
	authzEngine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	reader := NewPostgresReader(testenv.NewLogger(t), conn).WithAuthorization(authzEngine)
	reg := newRegistrar(newTestMCPServer())
	registerAnalyticsTools(reg, NewAnalyticsService(testenv.NewLogger(t), engine, flags, NewPostgresOrganizationSlugResolver(conn), reader, OperationBudget{Connection: allowOperationLimiter{}, Organization: allowOperationLimiter{}}))

	actor := contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	actor = contextvalues.SetActingSurface(actor, contextvalues.ActingSurfacePlatformMCP)
	granted := ContextWithPrincipal(authz.GrantsToContext(actor, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, project.ID.String())}), principal)
	arguments := json.RawMessage(`{"project_id":"` + project.ID.String() + `"}`)

	// Not set at all: indeterminate, and closed.
	_, err = descriptorByName(t, reg, describeAnalyticsCatalogToolName).Invoke(granted, arguments)
	refusal := requireAnalyticsRefusal(t, err)
	require.Equal(t, analyticsNotEnabledCode, refusal.Code)
	require.Contains(t, refusal.Message, "not switched on")

	flags.SetFlag(feature.FlagExplore, principal.OrganizationID, false)
	_, err = descriptorByName(t, reg, describeAnalyticsCatalogToolName).Invoke(granted, arguments)
	require.Equal(t, analyticsNotEnabledCode, requireAnalyticsRefusal(t, err).Code)

	flags.SetFlag(feature.FlagExplore, principal.OrganizationID, true)
	raw, err := descriptorByName(t, reg, describeAnalyticsCatalogToolName).Invoke(granted, arguments)
	require.NoError(t, err)
	output, ok := raw.(DescribeAnalyticsCatalogOutput)
	require.True(t, ok)
	require.Equal(t, project.ID.String(), output.Project.ID)
}
