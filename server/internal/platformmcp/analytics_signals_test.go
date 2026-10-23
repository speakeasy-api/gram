package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
	signalquery "github.com/speakeasy-api/gram/server/internal/telemetry/analytics/signals"
)

type signalAnalyticsFeatures bool

func (s signalAnalyticsFeatures) IsFeatureEnabled(context.Context, string, productfeatures.Feature) (bool, error) {
	return bool(s), nil
}

func TestSignalAnalyticsWorksIndependentlyOfExplore(t *testing.T) {
	t.Parallel()
	h := newAnalyticsTestHarness(t, feature.EvaluationDisabled, allowAnalyticsBudget)
	h.service.signalFeatures = signalAnalyticsFeatures(true)
	sensor, signal := uuid.NewString(), uuid.NewString()
	h.engine.result = &analytics.QueryResult{Dataset: "signals", Plan: "signals.canonical_final", Rows: []analytics.Row{{"matched_messages": int64(2)}}, SignalResult: &analytics.SignalResult{Metadata: analytics.SignalMetadata{Exact: true, Unit: "original_message", Coverage: "unknown", SeriesComplete: true}, Summary: analytics.Row{"answered": int64(4)}}}
	args := fmt.Sprintf(`{"project_id":%q,"dataset":"signals",%s,"grain":"auto","signal_query":{"selection":{"sensor_id":%q,"signal_id":%q,"threshold":0.5},"group_by":[{"attribute":"department"}]}}`, h.project.ID.String(), analyticsTestWindow, sensor, signal)
	raw, err := h.invoke(t, runAnalyticsQueryToolName, args)
	require.NoError(t, err)
	out, ok := raw.(RunAnalyticsQueryOutput)
	require.True(t, ok)
	require.True(t, out.SignalResult.Metadata.Exact)
	require.Equal(t, sensor, h.engine.request.SignalQuery.Selection.SensorID)
	require.Equal(t, []signalquery.Dimension{{Attribute: "department"}}, h.engine.request.SignalQuery.GroupBy)
	raw, err = h.invoke(t, describeAnalyticsCatalogToolName, h.projectArguments())
	require.NoError(t, err)
	catalog, ok := raw.(DescribeAnalyticsCatalogOutput)
	require.True(t, ok)
	require.Empty(t, catalog.Datasets)
	require.Equal(t, "signals", catalog.SignalMetrics.Dataset)
}

func TestExploreDoesNotGrantSignalEntitlement(t *testing.T) {
	t.Parallel()
	h := newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	args := fmt.Sprintf(`{"project_id":%q,"dataset":"signals",%s}`, h.project.ID.String(), analyticsTestWindow)
	_, err := h.invoke(t, runAnalyticsQueryToolName, args)
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, "feature_not_enabled")
	require.Zero(t, h.engine.calls)
}

func TestSignalEntitlementDoesNotGrantRiskDatasetAccess(t *testing.T) {
	t.Parallel()
	h := newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	h.service.signalFeatures = signalAnalyticsFeatures(true)
	member := authz.NewGrant(authz.ScopeProjectRead, h.project.ID.String())
	raw, err := h.invokeWithGrants(t, describeAnalyticsCatalogToolName, h.projectArguments(), member)
	require.NoError(t, err)
	catalog, ok := raw.(DescribeAnalyticsCatalogOutput)
	require.True(t, ok)
	require.NotNil(t, catalog.SignalMetrics)
	require.Equal(t, "signals", catalog.SignalMetrics.Dataset)
	for _, dataset := range catalog.Datasets {
		require.NotEqual(t, "risk_findings", dataset.Name)
	}
	for tool, arguments := range map[string]string{
		runAnalyticsQueryToolName:            `{"project_id":"` + h.project.ID.String() + `","dataset":"risk_findings",` + analyticsTestWindow + `,"measures":[{"op":"count"}]}`,
		listAnalyticsDimensionValuesToolName: `{"project_id":"` + h.project.ID.String() + `","dataset":"risk_findings","dimension":"category",` + analyticsTestWindow + `}`,
	} {
		_, err := h.invokeWithGrants(t, tool, arguments, member)
		require.Equal(t, "forbidden", requireAnalyticsRefusal(t, err).Code, tool)
	}
	require.Zero(t, h.engine.calls, "signal entitlement must not authorize risk data reads")
}

func TestSignalDefinitionToolHasExactScopeAndUnavailableParity(t *testing.T) {
	t.Parallel()
	live := newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	reg := newRegistrar(aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())), newTestMCPServer())
	registerAnalyticsTools(reg, nil)
	a, b := descriptorByName(t, live.reg, getSignalDefinitionToolName), descriptorByName(t, reg, getSignalDefinitionToolName)
	require.Equal(t, a.Meta, b.Meta)
	require.Equal(t, bothAudiences, a.Meta.Audiences)
	require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
	var schema struct {
		Required []string `json:"required"`
	}
	require.NoError(t, json.Unmarshal(a.InputSchema, &schema))
	require.ElementsMatch(t, []string{"project_id", "sensor_id", "definition_hash"}, schema.Required)
	require.Contains(t, b.Description, "unavailable in this deployment")
	arguments := fmt.Sprintf(`{"project_id":%q,"sensor_id":%q,"definition_hash":%q}`, live.project.ID.String(), uuid.NewString(), strings.Repeat("a", 64))
	_, err := b.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(arguments))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, `"code":"feature_unavailable"`)
}

func TestSignalSessionFinderSharesEntitlementAndUnavailableContract(t *testing.T) {
	t.Parallel()
	h := newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	args := fmt.Sprintf(`{"project_id":%q,%s,"signal_query":{"selection":{"sensor_id":%q,"signal_id":%q,"threshold":0.5}},"compare_previous":true}`, h.project.ID.String(), analyticsTestWindow, uuid.NewString(), uuid.NewString())
	_, err := h.invoke(t, findSignalSessionsToolName, args)
	require.Equal(t, analyticsNotEnabledCode, requireAnalyticsRefusal(t, err).Code)
	require.Zero(t, h.engine.calls)
	h.service.signalFeatures = signalAnalyticsFeatures(true)
	raw, err := h.invoke(t, findSignalSessionsToolName, args)
	require.NoError(t, err)
	result, ok := raw.(FindSignalSessionsOutput)
	require.True(t, ok)
	require.InDelta(t, .5, result.Findings.Concentration["top_session_share"], .000001)
	reg := newRegistrar(aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())), newTestMCPServer())
	registerAnalyticsTools(reg, nil)
	live, unavailable := descriptorByName(t, h.reg, findSignalSessionsToolName), descriptorByName(t, reg, findSignalSessionsToolName)
	require.Equal(t, bothAudiences, live.Meta.Audiences)
	require.Equal(t, ProjectScopeExplicit, live.Meta.ProjectScope)
	require.Equal(t, live.Meta, unavailable.Meta)
	require.JSONEq(t, string(live.InputSchema), string(unavailable.InputSchema))
	var schema struct {
		Properties map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(live.InputSchema, &schema))
	for _, field := range []string{"group_by", "groups", "points"} {
		require.NotContains(t, schema.Properties["signal_query"].Properties, field)
	}
}

func TestSignalEvidenceRejectsMismatchedCallerContext(t *testing.T) {
	t.Parallel()
	h := newAnalyticsTestHarness(t, feature.EvaluationEnabled, allowAnalyticsBudget)
	h.service.signalFeatures = signalAnalyticsFeatures(true)
	principal := testRiskPrincipal("user")
	input := FindSignalSessionsInput{ProjectID: h.project.ID.String(), From: "2026-09-01T00:00:00Z", To: "2026-09-08T00:00:00Z", Evidence: true}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{UserID: "another-user", ActiveOrganizationID: principal.OrganizationID})
	_, err := h.service.FindSignalSessions(ctx, principal, input)
	require.ErrorIs(t, err, ErrForbidden)
	require.Zero(t, h.engine.calls)
	ctx = contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{UserID: principal.UserID, ActiveOrganizationID: principal.OrganizationID})
	_, err = h.service.FindSignalSessions(ctx, principal, input)
	require.NoError(t, err)
	require.Equal(t, 1, h.engine.calls)
}
