package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// stubDiagnosticsTelemetry satisfies the overview read model with a fixed
// watermark; the search tests only need the envelope it feeds.
type stubDiagnosticsTelemetry struct {
	watermark int64
}

func (s stubDiagnosticsTelemetry) GetMCPOutcomeBreakdown(context.Context, telemetryrepo.GetMCPOutcomeBreakdownParams) ([]telemetryrepo.MCPOutcomeBreakdownRow, error) {
	return nil, nil
}

func (s stubDiagnosticsTelemetry) GetTelemetryWatermark(context.Context, telemetryrepo.GetTelemetryWatermarkParams) (int64, error) {
	return s.watermark, nil
}

func (s stubDiagnosticsTelemetry) GetOverviewSummary(context.Context, telemetryrepo.GetOverviewSummaryParams) (*telemetryrepo.OverviewSummary, error) {
	return nil, nil
}

func (s stubDiagnosticsTelemetry) GetActiveCounts(context.Context, telemetryrepo.GetActiveCountsParams) (*telemetryrepo.ActiveCounts, error) {
	return nil, nil
}

func (s stubDiagnosticsTelemetry) GetTopServers(context.Context, telemetryrepo.GetTopServersParams) ([]telemetryrepo.TopServer, error) {
	return nil, nil
}

func (s stubDiagnosticsTelemetry) GetUnifiedActiveServerCount(context.Context, telemetryrepo.GetTopServersParams) (uint64, error) {
	return 0, nil
}

func (s stubDiagnosticsTelemetry) GetSkillsSummary(context.Context, telemetryrepo.GetSkillsSummaryParams) ([]telemetryrepo.SkillSummaryRow, error) {
	return nil, nil
}

func (s stubDiagnosticsTelemetry) GetSkillBreakdown(context.Context, telemetryrepo.GetSkillBreakdownParams) ([]telemetryrepo.SkillBreakdownRow, error) {
	return nil, nil
}

// recordingToolCallSearchReader captures the repository parameters a search
// built and serves canned rows.
type recordingToolCallSearchReader struct {
	traceParams telemetryrepo.ListToolUsageTracesParams
	rows        []telemetryrepo.ToolUsageTraceSummary
	// paged, when set, serves rows the way the repository does: newest first,
	// strictly after the cursor position, at most Limit of them.
	paged     bool
	keyParams telemetryrepo.ListAttributeKeysParams
	keys      []string
	calls     int
}

func (r *recordingToolCallSearchReader) ListToolUsageTraces(_ context.Context, arg telemetryrepo.ListToolUsageTracesParams) ([]telemetryrepo.ToolUsageTraceSummary, error) {
	r.traceParams = arg
	r.calls++
	if !r.paged {
		return r.rows, nil
	}
	page := make([]telemetryrepo.ToolUsageTraceSummary, 0, arg.Limit)
	for _, row := range r.rows {
		// The window is honoured the way the repository honours it, so a test
		// about window drift observes which rows are reachable rather than only
		// which bounds were asked for.
		if row.StartTimeUnixNano < arg.TimeStart || row.StartTimeUnixNano > arg.TimeEnd {
			continue
		}
		if arg.CursorID != "" && (row.StartTimeUnixNano > arg.CursorTimeUnixNano || (row.StartTimeUnixNano == arg.CursorTimeUnixNano && row.ID >= arg.CursorID)) {
			continue
		}
		if len(page) == arg.Limit {
			break
		}
		page = append(page, row)
	}
	return page, nil
}

func (r *recordingToolCallSearchReader) ListAttributeKeys(_ context.Context, arg telemetryrepo.ListAttributeKeysParams) ([]string, error) {
	r.keyParams = arg
	return r.keys, nil
}

type recordingDrilldownAuditor struct {
	attributions []string
}

func (a *recordingDrilldownAuditor) RecordUserMCPStatusRead(context.Context, Principal, string, string, string, string) error {
	return nil
}

func (a *recordingDrilldownAuditor) RecordUsageAttributionRead(_ context.Context, _ Principal, projectID, targetKind, target, maskedIdentity, window string) error {
	a.attributions = append(a.attributions, strings.Join([]string{projectID, targetKind, target, maskedIdentity, window}, "|"))
	return nil
}

// failingDrilldownAuditor stands for an audit sink that cannot record, which
// must refuse the read rather than let it proceed unrecorded.
type failingDrilldownAuditor struct {
	calls int
}

var errAuditSinkUnavailable = errors.New("audit sink unavailable")

func (a *failingDrilldownAuditor) RecordUserMCPStatusRead(context.Context, Principal, string, string, string, string) error {
	return errAuditSinkUnavailable
}

func (a *failingDrilldownAuditor) RecordUsageAttributionRead(context.Context, Principal, string, string, string, string, string) error {
	a.calls++
	return errAuditSinkUnavailable
}

// stubCanonicalIdentityGate puts one organization in the identity fold, the way
// the telemetry service's rollout flag does.
type stubCanonicalIdentityGate struct {
	orgID string
}

func (g stubCanonicalIdentityGate) CanonicalOrgFor(_ context.Context, orgID string) string {
	if orgID == g.orgID {
		return orgID
	}
	return ""
}

var toolCallSearchTestNow = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

const toolCallSearchTestProject = "00000000-0000-0000-0000-000000000001"

// newToolCallSearchService composes the search without Postgres or ClickHouse:
// every dependency the search reaches is a fake, and the mcp_id attribution
// path, which needs Postgres, is the one path these tests do not exercise.
func newToolCallSearchService(t *testing.T, search ToolCallSearchReader, auditor DrilldownAuditor) *DiagnosticsService {
	t.Helper()

	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_tool_call_search_unit")
	require.NoError(t, err)
	codec := newSubjectReferenceCodec("tool-call-search-test-key")
	return &DiagnosticsService{
		db:              conn,
		telemetry:       stubDiagnosticsTelemetry{watermark: toolCallSearchTestNow.Add(-time.Minute).UnixNano()},
		drilldown:       nil,
		search:          search,
		references:      codec,
		sensitiveBudget: allowBudget(),
		volume:          DrilldownVolumeBudget{Rows: allowOperationLimiter{}, MetricQueries: allowOperationLimiter{}},
		auditor:         auditor,
		sessions:        nil,
		sessionCapture:  nil,
		reader:          diagnosticsProjectReader{output: ListProjectsOutput{}},
		readiness:       nil,
		budget:          allowBudget(),
		identityGate:    literalIdentityGate{},
		now:             func() time.Time { return toolCallSearchTestNow },
	}
}

func toolCallSearchRow(id string, occurredAt time.Time, userKind, userKey string, status int32) telemetryrepo.ToolUsageTraceSummary {
	client := "claude-code"
	return telemetryrepo.ToolUsageTraceSummary{
		ID:                id,
		TraceID:           "trace-" + id,
		LogGroupKind:      "trace_id",
		LogGroupValue:     "trace-" + id,
		StartTimeUnixNano: occurredAt.UnixNano(),
		LogCount:          2,
		GramURN:           "tools:http:payments:refund",
		ToolName:          "refund",
		TargetType:        telemetryrepo.ToolUsageTargetTypeHostedMCP,
		TargetKind:        "server",
		TargetID:          "payments",
		TargetLabel:       "Payments",
		UserKey:           userKey,
		UserLabel:         userKey,
		UserKind:          userKind,
		HookSource:        &client,
		EventSource:       "tool_call",
		HTTPStatusCode:    &status,
		HookStatus:        nil,
		BlockReason:       nil,
		AccountType:       nil,
		MetaMCPServerID:   "",
		ClientKey:         "claude-code",
		ClientLabel:       "Claude Code",
		ClientVersion:     nil,
	}
}

// TestSearchToolCallsOutput_ProjectsOnlyAllowlistedFields pins the serialized
// shape. The projection is positive: a field that is not listed here is not
// served, so an addition has to be made deliberately here first.
func TestSearchToolCallsOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	window, err := resolveWindow("7d", toolCallSearchTestNow, toolCallSearchWindowSpec)
	require.NoError(t, err)
	output := SearchToolCallsOutput{
		ProjectID: toolCallSearchTestProject,
		MCPID:     "00000000-0000-0000-0000-000000000002",
		Envelope:  newDataEnvelope(toolCallSearchTestNow, toolCallSearchTestNow.Add(-time.Minute), window, true),
		Calls: []ToolCallMatch{{
			OccurredAt:     toolCallSearchTestNow.Format(time.RFC3339Nano),
			ToolName:       "refund",
			TargetType:     telemetryrepo.ToolUsageTargetTypeHostedMCP,
			TargetKind:     "server",
			Target:         "Payments",
			Outcome:        ToolCallOutcomeFailure,
			Client:         "claude-code",
			MaskedIdentity: "p***@e***",
			UserReference:  "opaque",
		}},
		NextCursor:             "opaque-cursor",
		AttributionUnavailable: true,
	}

	require.ElementsMatch(t, []string{
		"project_id", "mcp_id",
		"data", "queried_at", "data_through", "freshness", "no_observations", "resolved_window", "window", "from", "to",
		"calls", "occurred_at", "tool_name", "target_type", "target_kind", "target", "outcome", "client", "masked_identity", "user_reference",
		"next_cursor", "attribution_unavailable",
	}, decodeKeys(t, output))
}

func TestListAttributeKeysOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	window, err := resolveWindow("", toolCallSearchTestNow, attributeKeysWindowSpec)
	require.NoError(t, err)
	require.Equal(t, DiagnosticWindowLastWeek, window.Window)
	output := ListAttributeKeysOutput{
		ProjectID:  toolCallSearchTestProject,
		Envelope:   newDataEnvelope(toolCallSearchTestNow, toolCallSearchTestNow, window, true),
		CustomKeys: []string{"@region"},
		SystemKeys: []string{"gram.hook.source"},
		Truncated:  false,
	}

	require.ElementsMatch(t, []string{
		"project_id",
		"data", "queried_at", "data_through", "freshness", "no_observations", "resolved_window", "window", "from", "to",
		"custom_keys", "system_keys", "truncated",
	}, decodeKeys(t, output))
}

func TestNormalizeToolCallSearch_RefusesInvalidInput(t *testing.T) {
	t.Parallel()

	longText := strings.Repeat("x", maxToolCallSearchTextLength+1)
	tooManyFilters := make([]ToolCallAttributeFilter, maxToolCallAttributeFilters+1)
	for i := range tooManyFilters {
		tooManyFilters[i] = ToolCallAttributeFilter{Key: "@region", Op: "eq", Values: []string{"eu"}}
	}
	for name, input := range map[string]SearchToolCallsInput{
		"missing project":           {ProjectID: " "},
		"unknown outcome":           {ProjectID: toolCallSearchTestProject, Outcome: "timeout"},
		"oversized tool name":       {ProjectID: toolCallSearchTestProject, ToolNameContains: longText},
		"oversized error text":      {ProjectID: toolCallSearchTestProject, ErrorContains: longText},
		"too many attributes":       {ProjectID: toolCallSearchTestProject, Attributes: tooManyFilters},
		"malformed key":             {ProjectID: toolCallSearchTestProject, Attributes: []ToolCallAttributeFilter{{Key: "region; DROP", Op: "eq", Values: []string{"eu"}}}},
		"content-bearing key":       {ProjectID: toolCallSearchTestProject, Attributes: []ToolCallAttributeFilter{{Key: "gen_ai.tool.call.arguments", Op: "eq", Values: []string{"x"}}}},
		"contains on system key":    {ProjectID: toolCallSearchTestProject, Attributes: []ToolCallAttributeFilter{{Key: "gram.hook.source", Op: "contains", Values: []string{"cl"}}}},
		"unknown op":                {ProjectID: toolCallSearchTestProject, Attributes: []ToolCallAttributeFilter{{Key: "@region", Op: "like", Values: []string{"eu"}}}},
		"eq without value":          {ProjectID: toolCallSearchTestProject, Attributes: []ToolCallAttributeFilter{{Key: "@region", Op: "eq", Values: nil}}},
		"exists with value":         {ProjectID: toolCallSearchTestProject, Attributes: []ToolCallAttributeFilter{{Key: "@region", Op: "exists", Values: []string{"eu"}}}},
		"in without values":         {ProjectID: toolCallSearchTestProject, Attributes: []ToolCallAttributeFilter{{Key: "@region", Op: "in", Values: nil}}},
		"oversized attribute value": {ProjectID: toolCallSearchTestProject, Attributes: []ToolCallAttributeFilter{{Key: "@region", Op: "eq", Values: []string{strings.Repeat("v", maxToolCallAttributeValLen+1)}}}},
	} {
		_, err := normalizeToolCallSearch(input)
		require.ErrorIs(t, err, ErrToolCallSearchInvalid, name)
	}
}

func TestNormalizeToolCallSearch_MapsTextFiltersOntoExistingPredicates(t *testing.T) {
	t.Parallel()

	search, err := normalizeToolCallSearch(SearchToolCallsInput{
		ProjectID:        toolCallSearchTestProject,
		ToolNameContains: " refund ",
		ErrorContains:    "timed out",
		Outcome:          "Failure",
		Attributes: []ToolCallAttributeFilter{
			{Key: "@region", Op: "", Values: []string{"eu"}},
			{Key: "@team", Op: "contains", Values: []string{"pay"}},
			{Key: "gram.hook.source", Op: "in", Values: []string{"claude-code", "cursor"}},
			{Key: "http.response.status_code", Op: "exists", Values: nil},
		},
		Limit: 500,
	})
	require.NoError(t, err)
	require.Equal(t, ToolCallOutcomeFailure, search.outcome)
	require.Equal(t, maxToolCallSearchLimit, search.limit)
	require.Equal(t, []telemetryrepo.AttributeFilter{
		{Path: "gram.tool.name", Op: "contains", Values: []string{"refund"}},
		{Path: "gram.hook.error", Op: "contains", Values: []string{"timed out"}},
		{Path: "@region", Op: "eq", Values: []string{"eu"}},
		{Path: "@team", Op: "contains", Values: []string{"pay"}},
		{Path: "gram.hook.source", Op: "in", Values: []string{"claude-code", "cursor"}},
		{Path: "http.response.status_code", Op: "exists", Values: []string{}},
	}, search.repoFilters())

	defaulted, err := normalizeToolCallSearch(SearchToolCallsInput{ProjectID: toolCallSearchTestProject})
	require.NoError(t, err)
	require.Equal(t, defaultToolCallSearchLimit, defaulted.limit)
	require.Empty(t, defaulted.repoFilters())
}

// TestSearchToolCalls_WindowIsBoundedAtAMonth pins the window policy: the
// default is a day, a month is the most the search looks back, and anything
// outside the closed set is refused rather than clamped.
func TestSearchToolCalls_WindowIsBoundedAtAMonth(t *testing.T) {
	t.Parallel()

	reader := &recordingToolCallSearchReader{}
	service := newToolCallSearchService(t, reader, &recordingDrilldownAuditor{})

	output, err := service.SearchToolCalls(t.Context(), testPrincipal(), SearchToolCallsInput{ProjectID: toolCallSearchTestProject})
	require.NoError(t, err)
	require.Equal(t, DiagnosticWindowLastDay, output.Envelope.ResolvedWindow.Window)
	require.Equal(t, toolCallSearchTestNow.Add(-24*time.Hour).UnixNano(), reader.traceParams.TimeStart)
	require.Equal(t, toolCallSearchTestNow.UnixNano(), reader.traceParams.TimeEnd)
	require.Equal(t, defaultToolCallSearchLimit+1, reader.traceParams.Limit)
	require.Equal(t, "desc", reader.traceParams.SortOrder)
	require.Empty(t, reader.traceParams.Query)
	require.True(t, output.Envelope.NoObservations)
	require.Equal(t, FreshnessCurrent, output.Envelope.Freshness)

	output, err = service.SearchToolCalls(t.Context(), testPrincipal(), SearchToolCallsInput{ProjectID: toolCallSearchTestProject, Window: "30d"})
	require.NoError(t, err)
	require.Equal(t, DiagnosticWindowLastMonth, output.Envelope.ResolvedWindow.Window)
	require.Equal(t, toolCallSearchTestNow.Add(-30*24*time.Hour).UnixNano(), reader.traceParams.TimeStart)

	_, err = service.SearchToolCalls(t.Context(), testPrincipal(), SearchToolCallsInput{ProjectID: toolCallSearchTestProject, Window: "90d"})
	require.ErrorIs(t, err, ErrDiagnosticWindowInvalid)
}

func TestSearchToolCalls_ForwardsFiltersToTheToolLogsQuery(t *testing.T) {
	t.Parallel()

	reader := &recordingToolCallSearchReader{}
	service := newToolCallSearchService(t, reader, &recordingDrilldownAuditor{})

	_, err := service.SearchToolCalls(t.Context(), testPrincipal(), SearchToolCallsInput{
		ProjectID:        toolCallSearchTestProject,
		ToolNameContains: "refund",
		ErrorContains:    "timeout",
		Outcome:          ToolCallOutcomeBlocked,
		Attributes:       []ToolCallAttributeFilter{{Key: "@region", Op: "eq", Values: []string{"eu"}}},
		Limit:            5,
	})
	require.NoError(t, err)
	require.Equal(t, toolCallSearchTestProject, reader.traceParams.GramProjectID)
	require.Equal(t, []string{ToolCallOutcomeBlocked}, reader.traceParams.Statuses)
	require.Equal(t, 6, reader.traceParams.Limit)
	require.Equal(t, []telemetryrepo.AttributeFilter{
		{Path: "gram.tool.name", Op: "contains", Values: []string{"refund"}},
		{Path: "gram.hook.error", Op: "contains", Values: []string{"timeout"}},
		{Path: "@region", Op: "eq", Values: []string{"eu"}},
	}, reader.traceParams.Filters)
	// This search names no MCP, so it pins only that a project-wide search
	// leaves every target selector unset rather than accidentally scoping
	// itself. What an mcp_id search selects is pinned against a real
	// configured server by
	// TestSearchToolCallsNarrowsToOneServerWithoutReportedNames.
	require.Empty(t, reader.traceParams.HostedToolsetSlugs)
	require.Empty(t, reader.traceParams.MCPServerTargetIDs)
	require.Empty(t, reader.traceParams.ShadowServerNames)
	require.Empty(t, reader.traceParams.UserFilters)
	require.Zero(t, reader.traceParams.CursorTimeUnixNano)
	require.Empty(t, reader.traceParams.CursorID)
}

// TestSearchToolCalls_MasksIdentitiesAndMintsReferences pins that a page names
// nobody: the identity is masked, the raw key never serializes, and the
// reference it carries resolves back to the same person only through this
// search in this project.
func TestSearchToolCalls_MasksIdentitiesAndMintsReferences(t *testing.T) {
	t.Parallel()

	auditor := &recordingDrilldownAuditor{}
	reader := &recordingToolCallSearchReader{rows: []telemetryrepo.ToolUsageTraceSummary{
		toolCallSearchRow("row-1", toolCallSearchTestNow.Add(-time.Minute), "email", "person@example.test", 500),
		toolCallSearchRow("row-2", toolCallSearchTestNow.Add(-2*time.Minute), "agent_id", "agent-42", 200),
		toolCallSearchRow("row-3", toolCallSearchTestNow.Add(-3*time.Minute), "unknown", "Unknown", 200),
	}}
	service := newToolCallSearchService(t, reader, auditor)
	principal := testPrincipal()

	output, err := service.SearchToolCalls(t.Context(), principal, SearchToolCallsInput{ProjectID: toolCallSearchTestProject})
	require.NoError(t, err)
	require.Len(t, output.Calls, 3)
	require.Empty(t, output.NextCursor)
	require.False(t, output.Envelope.NoObservations)

	person := output.Calls[0]
	require.Equal(t, ToolCallOutcomeFailure, person.Outcome)
	require.Equal(t, "p***@e***", person.MaskedIdentity)
	require.Equal(t, "Payments", person.Target)
	require.Equal(t, "claude-code", person.Client)
	require.NotEmpty(t, person.UserReference)
	subject, err := service.references.DecodeScoped(person.UserReference, principal, subjectKindUser, toolCallSearchUserScope(toolCallSearchTestProject), toolCallSearchTestNow)
	require.NoError(t, err)
	require.Equal(t, "email:person@example.test", subject)

	agent := output.Calls[1]
	require.Equal(t, ToolCallOutcomeSuccess, agent.Outcome)
	require.Equal(t, "a***", agent.MaskedIdentity)
	require.Empty(t, agent.UserReference, "agents have no referenceable identity column")
	require.Empty(t, output.Calls[2].MaskedIdentity)

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "person@example.test")
	require.NotContains(t, string(encoded), "agent-42")
	require.NotContains(t, string(encoded), "trace-row")
	require.Empty(t, auditor.attributions, "a page without a person filter is not an attribution read")

	// Narrowing to the referenced person filters the right column and is
	// recorded as an attribution read before the query runs.
	_, err = service.SearchToolCalls(t.Context(), principal, SearchToolCallsInput{ProjectID: toolCallSearchTestProject, UserReference: person.UserReference})
	require.NoError(t, err)
	require.Equal(t, []telemetryrepo.ToolUsageUserFilter{{Kind: "email", Key: "person@example.test"}}, reader.traceParams.UserFilters)
	require.Equal(t, []string{toolCallSearchTestProject + "|project|" + toolCallSearchTestProject + "|p***@e***|24h"}, auditor.attributions)

	// The same reference does not resolve for another project or another session.
	_, err = service.SearchToolCalls(t.Context(), principal, SearchToolCallsInput{ProjectID: "00000000-0000-0000-0000-000000000009", UserReference: person.UserReference})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	other := principal
	other.Generation = "generation-2"
	_, err = service.SearchToolCalls(t.Context(), other, SearchToolCallsInput{ProjectID: toolCallSearchTestProject, UserReference: person.UserReference})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
}

// TestSearchToolCalls_CursorResumesOnlyTheQueryThatMintedIt pins the paging
// contract: a full page hands back a cursor, the cursor resumes from the last
// row served, and it is refused for a different filter set or window.
func TestSearchToolCalls_CursorResumesOnlyTheQueryThatMintedIt(t *testing.T) {
	t.Parallel()

	rows := make([]telemetryrepo.ToolUsageTraceSummary, 0, 3)
	for i := range 3 {
		rows = append(rows, toolCallSearchRow("row-"+string(rune('a'+i)), toolCallSearchTestNow.Add(-time.Duration(i+1)*time.Minute), "email", "person@example.test", 200))
	}
	reader := &recordingToolCallSearchReader{rows: rows}
	service := newToolCallSearchService(t, reader, &recordingDrilldownAuditor{})
	principal := testPrincipal()
	first := SearchToolCallsInput{ProjectID: toolCallSearchTestProject, Outcome: ToolCallOutcomeSuccess, Limit: 2}

	output, err := service.SearchToolCalls(t.Context(), principal, first)
	require.NoError(t, err)
	require.Len(t, output.Calls, 2)
	require.NotEmpty(t, output.NextCursor)

	next := first
	next.Cursor = output.NextCursor
	_, err = service.SearchToolCalls(t.Context(), principal, next)
	require.NoError(t, err)
	require.Equal(t, rows[1].StartTimeUnixNano, reader.traceParams.CursorTimeUnixNano)
	require.Equal(t, "row-b", reader.traceParams.CursorID)

	replayed := next
	replayed.Outcome = ToolCallOutcomeFailure
	_, err = service.SearchToolCalls(t.Context(), principal, replayed)
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)

	widened := next
	widened.Window = "7d"
	_, err = service.SearchToolCalls(t.Context(), principal, widened)
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)

	garbage := next
	garbage.Cursor = "not-a-cursor"
	_, err = service.SearchToolCalls(t.Context(), principal, garbage)
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
}

func TestToolCallCursor_CarriesThePageKeyTraversalAndWindow(t *testing.T) {
	t.Parallel()

	window, err := resolveWindow("24h", toolCallSearchTestNow, toolCallSearchWindowSpec)
	require.NoError(t, err)

	position, err := parseToolCallCursor(formatToolCallCursor(1_700_000_000_000_000_000, "summary-1", 40, window))
	require.NoError(t, err)
	require.Equal(t, int64(1_700_000_000_000_000_000), position.timeUnixNano)
	require.Equal(t, "summary-1", position.id)
	require.Equal(t, 40, position.traversed)
	require.Equal(t, window.start, position.windowStart)
	require.Equal(t, window.end, position.windowEnd)

	for _, value := range []string{
		"",
		"t:1:0:x",
		"s2:",
		"s2:abc",
		"s2:-1:0:1:2:x",
		"s2:0:0:1:2:x",
		"s2:1700000000",
		"s2:1700000000:x",
		"s2:1700000000:0:1:2:",
		"s2:1700000000:-1:1:2:x",
		// Window bounds that are absent, unparseable, non-positive, or not an
		// interval leave nothing to anchor to.
		"s2:1700000000:0:x",
		"s2:1700000000:0:1:x",
		"s2:1700000000:0:abc:2:x",
		"s2:1700000000:0:1:abc:x",
		"s2:1700000000:0:0:2:x",
		"s2:1700000000:0:2:2:x",
		"s2:1700000000:0:3:2:x",
		// A cursor minted before the window was anchored carries no interval, and
		// is refused rather than resumed against a window recomputed from now.
		// The second is the case the prefix itself has to catch: a pre-anchor
		// payload whose summary id happens to contain colons has the arity of an
		// anchored one, so only the shape tag tells them apart.
		"s:1700000000000000000:0:summary-1",
		"s:1700000000000000000:0:1:2:summary-1",
		formatToolCallCursor(1, "x", maxToolCallSearchTraversal+1, window),
	} {
		_, err := parseToolCallCursor(value)
		require.ErrorIs(t, err, ErrSubjectReferenceNotFound, value)
	}
}

func TestListAttributeKeys_SplitsCustomFromFilterableSystemKeys(t *testing.T) {
	t.Parallel()

	reader := &recordingToolCallSearchReader{keys: []string{
		"gram.hook.source",
		"app.region",
		"gen_ai.tool.call.arguments",
		"gen_ai.tool.call.result",
		"app.team",
		"http.response.status_code",
		"gen_ai.output.messages",
	}}
	service := newToolCallSearchService(t, reader, &recordingDrilldownAuditor{})

	output, err := service.ListAttributeKeys(t.Context(), testPrincipal(), ListAttributeKeysInput{ProjectID: toolCallSearchTestProject})
	require.NoError(t, err)
	require.Equal(t, []string{"@region", "@team"}, output.CustomKeys)
	require.Equal(t, []string{"gram.hook.source", "http.response.status_code"}, output.SystemKeys)
	require.False(t, output.Truncated)
	require.False(t, output.Envelope.NoObservations)
	require.Equal(t, DiagnosticWindowLastWeek, output.Envelope.ResolvedWindow.Window)
	require.Equal(t, toolCallSearchTestNow.Add(-7*24*time.Hour).UnixNano(), reader.keyParams.TimeStart)
	require.Equal(t, toolCallSearchTestProject, reader.keyParams.GramProjectID)

	_, err = service.ListAttributeKeys(t.Context(), testPrincipal(), ListAttributeKeysInput{ProjectID: ""})
	require.ErrorIs(t, err, ErrToolCallSearchInvalid)
}

// TestListAttributeKeys_CapsEachKindAndReportsTheCut pins the bound the tool
// description cites. Each kind is cut to maxAttributeKeysPerKind and truncated
// says so, which is the only signal that a key's absence from the inventory
// does not mean the project never recorded it — the inference an agent would
// otherwise draw from a list it believes is complete.
//
// The cases walk the boundary in each kind independently, because that is where
// a cut-detection bug lives: the flag is an OR over the two kinds, so moving
// both of them together cannot tell an OR from an AND. Exactly at the cap
// nothing is cut and nothing is claimed to be, which is what keeps truncated a
// positive signal rather than always-on noise.
func TestListAttributeKeys_CapsEachKindAndReportsTheCut(t *testing.T) {
	t.Parallel()

	keysOfKind := func(prefix string, count int) []string {
		keys := make([]string, 0, count)
		for i := range count {
			keys = append(keys, fmt.Sprintf("%s_%04d", prefix, i))
		}
		return keys
	}

	for _, test := range []struct {
		name          string
		custom        int
		system        int
		wantCustom    int
		wantSystem    int
		wantTruncated bool
	}{
		{name: "well under the cap in both kinds", custom: 3, system: 4, wantCustom: 3, wantSystem: 4, wantTruncated: false},
		{name: "exactly at the cap in both kinds", custom: maxAttributeKeysPerKind, system: maxAttributeKeysPerKind, wantCustom: maxAttributeKeysPerKind, wantSystem: maxAttributeKeysPerKind, wantTruncated: false},
		{name: "one past the cap in both kinds", custom: maxAttributeKeysPerKind + 1, system: maxAttributeKeysPerKind + 1, wantCustom: maxAttributeKeysPerKind, wantSystem: maxAttributeKeysPerKind, wantTruncated: true},
		{name: "one past the cap in custom only", custom: maxAttributeKeysPerKind + 1, system: 3, wantCustom: maxAttributeKeysPerKind, wantSystem: 3, wantTruncated: true},
		{name: "one past the cap in system only", custom: 3, system: maxAttributeKeysPerKind + 1, wantCustom: 3, wantSystem: maxAttributeKeysPerKind, wantTruncated: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			keys := append(keysOfKind("app.custom", test.custom), keysOfKind("gram.system", test.system)...)
			reader := &recordingToolCallSearchReader{keys: keys}
			service := newToolCallSearchService(t, reader, &recordingDrilldownAuditor{})

			output, err := service.ListAttributeKeys(t.Context(), testPrincipal(), ListAttributeKeysInput{ProjectID: toolCallSearchTestProject})
			require.NoError(t, err)
			require.Len(t, output.CustomKeys, test.wantCustom)
			require.Len(t, output.SystemKeys, test.wantSystem)
			require.Equal(t, test.wantTruncated, output.Truncated,
				"a cut inventory must say so and an uncut one must not, or truncated stops distinguishing absence from non-existence")
		})
	}
}

// TestSearchToolCalls_RefusesIdentityAttributesBeforeAnyRead pins that
// user_reference is the only way to narrow to one person, because it is the only
// way that records the attribution read. An identity attribute would reach the
// same rows through an ordinary predicate, so it is refused before the search
// runs — and refused for the same request the audited path would refuse, rather
// than succeeding where that path fails closed.
func TestSearchToolCalls_RefusesIdentityAttributesBeforeAnyRead(t *testing.T) {
	t.Parallel()

	reader := &recordingToolCallSearchReader{rows: []telemetryrepo.ToolUsageTraceSummary{
		toolCallSearchRow("row-1", toolCallSearchTestNow.Add(-time.Minute), "email", "person@example.test", 200),
	}}
	auditor := &failingDrilldownAuditor{}
	service := newToolCallSearchService(t, reader, auditor)
	principal := testPrincipal()

	// The platform's own identity columns, the paths that reach them without
	// being materialized, and both operators that can name a value.
	for _, filter := range []ToolCallAttributeFilter{
		{Key: "user.email", Op: "eq", Values: []string{"person@example.test"}},
		{Key: "user.email", Op: "in", Values: []string{"person@example.test", "other@example.test"}},
		{Key: "user.email", Op: "not_eq", Values: []string{"person@example.test"}},
		{Key: "user.email", Op: "exists"},
		{Key: "user.id", Op: "eq", Values: []string{"user-1"}},
		{Key: "gram.external_user.id", Op: "eq", Values: []string{"ext-1"}},
		{Key: "gram.litellm.user_email", Op: "eq", Values: []string{"person@example.test"}},
		{Key: "gram.actor.username", Op: "eq", Values: []string{"person"}},
	} {
		_, err := service.SearchToolCalls(t.Context(), principal, SearchToolCallsInput{
			ProjectID:  toolCallSearchTestProject,
			Attributes: []ToolCallAttributeFilter{filter},
		})
		require.ErrorIs(t, err, ErrToolCallSearchInvalid, filter.Key+" "+filter.Op)
		require.ErrorContains(t, err, "user_reference", filter.Key+" "+filter.Op)
	}
	require.Zero(t, reader.calls, "a refused identity filter must not reach the tool-call read")
	require.Zero(t, auditor.calls, "the refusal happens before anything is recorded")

	// The audited door, for the same person, with the same auditor: it refuses
	// too, and reads nothing. This is the comparison the attribute path was
	// bypassing.
	reference, err := service.references.EncodeScoped(principal, subjectKindUser, toolCallSearchUserScope(toolCallSearchTestProject), FormatSubjectIdentity(SubjectIdentityEmail, "person@example.test"), toolCallSearchTestNow)
	require.NoError(t, err)
	_, err = service.SearchToolCalls(t.Context(), principal, SearchToolCallsInput{ProjectID: toolCallSearchTestProject, UserReference: reference})
	require.ErrorIs(t, err, errAuditSinkUnavailable)
	require.Equal(t, 1, auditor.calls)
	require.Zero(t, reader.calls, "an attribution read that cannot be recorded must not be served")

	// A project's own custom attribute keeps every operator: it is the project's
	// data, not the platform's identity model.
	_, err = service.SearchToolCalls(t.Context(), principal, SearchToolCallsInput{
		ProjectID:  toolCallSearchTestProject,
		Attributes: []ToolCallAttributeFilter{{Key: "@requester_email", Op: "eq", Values: []string{"person@example.test"}}},
	})
	require.NoError(t, err)
	require.Equal(t, 1, reader.calls)
}

// TestListAttributeKeys_WithholdsIdentityKeys pins that key discovery never
// offers a filter the search refuses, so an agent is not led to build an
// unaudited identity search and told it is malformed only afterwards.
func TestListAttributeKeys_WithholdsIdentityKeys(t *testing.T) {
	t.Parallel()

	reader := &recordingToolCallSearchReader{keys: []string{
		"user.email",
		"user.id",
		"gram.external_user.id",
		"gram.litellm.user_email",
		"gram.hook.source",
		"app.region",
	}}
	service := newToolCallSearchService(t, reader, &recordingDrilldownAuditor{})

	output, err := service.ListAttributeKeys(t.Context(), testPrincipal(), ListAttributeKeysInput{ProjectID: toolCallSearchTestProject})
	require.NoError(t, err)
	require.Equal(t, []string{"gram.hook.source"}, output.SystemKeys)
	require.Equal(t, []string{"@region"}, output.CustomKeys)
}

// TestToolCallSearch_RefusesHeaderAttributesAndWithholdsThemFromDiscovery pins
// both halves of the header boundary. Withholding a header value from the
// result does not protect it while a caller can still test a guess against it:
// eq or in over http.request.headers.Cookie answers whether the guess was right
// by which calls come back, which is the oracle the tool-content refusal exists
// to close. So a header filter is refused before any read, and key discovery
// never offers one either.
func TestToolCallSearch_RefusesHeaderAttributesAndWithholdsThemFromDiscovery(t *testing.T) {
	t.Parallel()

	headerKeys := []string{
		// The header maps, whose own keys are whatever the wire carried.
		"http.request.headers.Cookie",
		"http.request.headers.Authorization",
		"http.response.headers.Location",
		"http.response.headers.Set_Cookie",
		"http.request.headers",
		"http.response.headers",
		// The individual headers the server stamps.
		"http.request.header.user_agent",
		"http.request.header.origin",
		"http.response.header.www_authenticate",
		// A header map nested under another producer's namespace.
		"gram.mcp.request.headers.X_Api_Key",
	}

	reader := &recordingToolCallSearchReader{rows: []telemetryrepo.ToolUsageTraceSummary{
		toolCallSearchRow("row-1", toolCallSearchTestNow.Add(-time.Minute), "email", "person@example.test", 200),
	}}
	service := newToolCallSearchService(t, reader, &recordingDrilldownAuditor{})
	principal := testPrincipal()

	for _, key := range headerKeys {
		for _, op := range []string{"eq", "in", "not_eq", "exists", "not_exists"} {
			filter := ToolCallAttributeFilter{Key: key, Op: op, Values: []string{"guess"}}
			if op == "exists" || op == "not_exists" {
				filter.Values = nil
			}
			_, err := service.SearchToolCalls(t.Context(), principal, SearchToolCallsInput{
				ProjectID:  toolCallSearchTestProject,
				Attributes: []ToolCallAttributeFilter{filter},
			})
			require.ErrorIs(t, err, ErrToolCallSearchInvalid, key+" "+op)
			require.ErrorContains(t, err, "HTTP header", key+" "+op)
		}
	}
	require.Zero(t, reader.calls, "a refused header filter must not reach the tool-call read")

	// The http.* attributes that are not headers keep working, so the rule is a
	// header rule and not an http rule.
	for _, key := range []string{
		"http.request.method",
		"http.route",
		"http.response.status_code",
		"http.server.request.duration",
		"http.request.body",
	} {
		_, err := service.SearchToolCalls(t.Context(), principal, SearchToolCallsInput{
			ProjectID:  toolCallSearchTestProject,
			Attributes: []ToolCallAttributeFilter{{Key: key, Op: "exists"}},
		})
		require.NoError(t, err, key)
	}
	require.Equal(t, 5, reader.calls)

	// A custom key keeps every operator even when it is spelled like a header:
	// it is the project's own integration data, not a header the platform
	// recorded, exactly as the identity and content refusals already treat
	// custom keys. This is the boundary the input schema states, pinned here so
	// the schema cannot drift from what the server enforces.
	for _, filter := range []ToolCallAttributeFilter{
		{Key: "@headers.Cookie", Op: "eq", Values: []string{"guess"}},
		{Key: "@http.request.headers.Authorization", Op: "contains", Values: []string{"Bearer"}},
	} {
		_, err := service.SearchToolCalls(t.Context(), principal, SearchToolCallsInput{
			ProjectID:  toolCallSearchTestProject,
			Attributes: []ToolCallAttributeFilter{filter},
		})
		require.NoError(t, err, filter.Key)
	}
	require.Equal(t, 7, reader.calls)

	// The other half: discovery never names a key the search refuses, so an
	// agent is not led to build a header predicate and told it is malformed
	// only afterwards.
	keysReader := &recordingToolCallSearchReader{keys: append(append([]string{}, headerKeys...), "gram.hook.source", "http.response.status_code", "app.region")}
	keysService := newToolCallSearchService(t, keysReader, &recordingDrilldownAuditor{})

	output, err := keysService.ListAttributeKeys(t.Context(), principal, ListAttributeKeysInput{ProjectID: toolCallSearchTestProject})
	require.NoError(t, err)
	require.Equal(t, []string{"gram.hook.source", "http.response.status_code"}, output.SystemKeys)
	require.Equal(t, []string{"@region"}, output.CustomKeys)
}

// TestSearchToolCalls_CursorAnchorsTheWindowAcrossPages pins that paging reads
// the interval the first page read, not one recomputed from the clock. The
// clock advances between pages here, which is what a real investigation does
// while it reads: without the anchor, a relative window slides forward and the
// oldest calls in it silently stop being reachable part-way through a walk.
//
// Each hop advances the clock by less than SubjectReferenceTTL, so the cursor
// is still live and the only thing under test is the window it resumes against.
func TestSearchToolCalls_CursorAnchorsTheWindowAcrossPages(t *testing.T) {
	t.Parallel()

	const hop = 9 * time.Minute

	// Newest first. The oldest row sits near the start of the 1h window, so a
	// window recomputed one hop later no longer contains it: resolving from a
	// clock at +9m reads from -51m, and this row is at -55m.
	rows := []telemetryrepo.ToolUsageTraceSummary{
		toolCallSearchRow("row-new", toolCallSearchTestNow.Add(-time.Minute), "email", "person@example.test", 200),
		toolCallSearchRow("row-old", toolCallSearchTestNow.Add(-55*time.Minute), "email", "other@example.test", 200),
	}
	reader := &recordingToolCallSearchReader{rows: rows, paged: true}
	service := newToolCallSearchService(t, reader, &recordingDrilldownAuditor{})
	clock := toolCallSearchTestNow
	service.now = func() time.Time { return clock }
	principal := testPrincipal()
	input := SearchToolCallsInput{ProjectID: toolCallSearchTestProject, Window: "1h", Limit: 1}

	first, err := service.SearchToolCalls(t.Context(), principal, input)
	require.NoError(t, err)
	require.Len(t, first.Calls, 1)
	require.NotEmpty(t, first.NextCursor)
	firstStart, firstEnd := reader.traceParams.TimeStart, reader.traceParams.TimeEnd

	// Time passes between the two requests.
	clock = toolCallSearchTestNow.Add(hop)

	next := input
	next.Cursor = first.NextCursor
	second, err := service.SearchToolCalls(t.Context(), principal, next)
	require.NoError(t, err)
	require.Equal(t, firstStart, reader.traceParams.TimeStart, "the second page must read the window the first page read")
	require.Equal(t, firstEnd, reader.traceParams.TimeEnd, "the second page must read the window the first page read")
	require.Equal(t, first.Envelope.ResolvedWindow.From, second.Envelope.ResolvedWindow.From)
	require.Equal(t, first.Envelope.ResolvedWindow.To, second.Envelope.ResolvedWindow.To)

	// The call near the original window start is still reachable, which is the
	// result the caller actually cares about.
	require.Len(t, second.Calls, 1)
	require.Equal(t, time.Unix(0, rows[1].StartTimeUnixNano).UTC().Format(time.RFC3339Nano), second.Calls[0].OccurredAt)
	require.Empty(t, second.NextCursor, "the exhausted fixture yields no further cursor")

	// Three pages, two hops: the anchor has to be carried forward by the second
	// page's own cursor, not re-minted from the clock, or the drift reappears
	// one page later.
	mid := toolCallSearchRow("row-mid", toolCallSearchTestNow.Add(-30*time.Minute), "email", "third@example.test", 200)
	reader.rows = []telemetryrepo.ToolUsageTraceSummary{rows[0], mid, rows[1]}
	clock = toolCallSearchTestNow
	first, err = service.SearchToolCalls(t.Context(), principal, input)
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)

	clock = toolCallSearchTestNow.Add(hop)
	next.Cursor = first.NextCursor
	second, err = service.SearchToolCalls(t.Context(), principal, next)
	require.NoError(t, err)
	require.NotEmpty(t, second.NextCursor)

	clock = toolCallSearchTestNow.Add(2 * hop)
	third := input
	third.Cursor = second.NextCursor
	page, err := service.SearchToolCalls(t.Context(), principal, third)
	require.NoError(t, err)
	require.Equal(t, firstStart, reader.traceParams.TimeStart, "the anchor must survive every hop, not just the first")
	require.Equal(t, firstEnd, reader.traceParams.TimeEnd, "the anchor must survive every hop, not just the first")
	require.Len(t, page.Calls, 1)
	require.Equal(t, time.Unix(0, rows[1].StartTimeUnixNano).UTC().Format(time.RFC3339Nano), page.Calls[0].OccurredAt)
}

// TestSearchToolCalls_FoldsThePersonReferenceLikeItsProducer pins that a person
// filter asks for the same identity folding the lists that mint a reference
// apply. list_mcp_usage_users can report a canonical directory address for calls
// stored under a linked alias, so without the fold the advertised reference
// would select rows whose user_key never carries that address — none of them, or
// only the part of the person's history spelled that one way.
func TestSearchToolCalls_FoldsThePersonReferenceLikeItsProducer(t *testing.T) {
	t.Parallel()

	reader := &recordingToolCallSearchReader{}
	service := newToolCallSearchService(t, reader, &recordingDrilldownAuditor{})
	principal := testPrincipal()
	service.identityGate = stubCanonicalIdentityGate{orgID: principal.OrganizationID}

	// A canonical address minted the way list_mcp_usage_users mints one, for a
	// person whose calls are recorded under an alias.
	reference, err := service.references.EncodeScoped(principal, subjectKindUser, toolCallSearchUserScope(toolCallSearchTestProject), FormatSubjectIdentity(SubjectIdentityEmail, "work@example.test"), toolCallSearchTestNow)
	require.NoError(t, err)

	_, err = service.SearchToolCalls(t.Context(), principal, SearchToolCallsInput{ProjectID: toolCallSearchTestProject, UserReference: reference})
	require.NoError(t, err)
	require.Equal(t, []telemetryrepo.ToolUsageUserFilter{{Kind: "email", Key: "work@example.test"}}, reader.traceParams.UserFilters)
	require.Equal(t, principal.OrganizationID, reader.traceParams.CanonicalIdentityOrg,
		"the trace filter must fold through the same identity map the reference's producer folded through")

	// An organization outside the rollout keeps the literal comparison, exactly
	// as its producer does.
	outside := newToolCallSearchService(t, reader, &recordingDrilldownAuditor{})
	outside.identityGate = stubCanonicalIdentityGate{orgID: "another-organization"}
	outsideReference, err := outside.references.EncodeScoped(principal, subjectKindUser, toolCallSearchUserScope(toolCallSearchTestProject), FormatSubjectIdentity(SubjectIdentityEmail, "work@example.test"), toolCallSearchTestNow)
	require.NoError(t, err)
	_, err = outside.SearchToolCalls(t.Context(), principal, SearchToolCallsInput{ProjectID: toolCallSearchTestProject, UserReference: outsideReference})
	require.NoError(t, err)
	require.Empty(t, reader.traceParams.CanonicalIdentityOrg)
}

// TestToolLogsTargets_RemoteServerWithoutToolsetSlug pins that a configured
// remote server, which has no toolset slug, is still matched by the slug and
// id its matcher stamps as the target id under the hosted or tunneled type.
func TestToolLogsTargets_RemoteServerWithoutToolsetSlug(t *testing.T) {
	t.Parallel()

	serverID := uuid.MustParse("00000000-0000-0000-0000-000000000002").String()
	targets := serverIdentity{
		mcpServerID: serverID,
		mcpSlug:     "billing",
		toolsetSlug: "",
		urlSuffixes: []string{"/mcp/billing"},
		toolSources: nil,
	}.toolLogsTargets()

	require.False(t, targets.empty())
	require.Equal(t, []string{"billing", serverID}, targets.mcpServerTargetIDs)
	require.Empty(t, targets.hostedToolsetSlugs)

	// A server with no slug is still matched by its id.
	unnamed := serverIdentity{
		mcpServerID: serverID,
		mcpSlug:     "",
		toolsetSlug: "",
		urlSuffixes: nil,
		toolSources: nil,
	}.toolLogsTargets()
	require.Equal(t, []string{serverID}, unnamed.mcpServerTargetIDs)
}

// TestToolLogsTargets_OmitsClientReportedNames pins the rule that keeps one
// configured server's history its own: a shadow row is named by the calling
// app, so a personal server someone happens to call "billing" must not surface
// in the corporate "billing" server's history.
//
// The identity deliberately carries a fully populated toolSources, the way
// serverIdentity resolves it for the summary reads, so this asserts that a
// trace-level read drops those names rather than merely never having had them.
func TestToolLogsTargets_OmitsClientReportedNames(t *testing.T) {
	t.Parallel()

	serverID := uuid.MustParse("00000000-0000-0000-0000-000000000004").String()
	identity := serverIdentity{
		mcpServerID: serverID,
		mcpSlug:     "billing",
		toolsetSlug: "billing-toolset",
		urlSuffixes: []string{"/mcp/billing"},
		toolSources: []string{"billing", "Billing", "plugin_finance_Billing", serverID},
	}
	targets := identity.toolLogsTargets()

	// The platform-stamped spellings are carried, asserted positively so that
	// dropping the slug is a failure too: it is what the matcher stamps on a
	// proxied call, and the query admits it only under target types a client
	// cannot choose.
	require.Equal(t, []string{"billing", serverID}, targets.mcpServerTargetIDs)
	require.Equal(t, []string{"billing-toolset"}, targets.hostedToolsetSlugs)

	// Every spelling only an agent vouched for reaches no selector, under any
	// target type. The slug and the id are excepted because the platform
	// stamps those itself; both are asserted positively above.
	for _, reported := range identity.toolSources {
		if reported == "billing" || reported == serverID {
			continue
		}
		require.NotContains(t, targets.mcpServerTargetIDs, reported)
		require.NotContains(t, targets.hostedToolsetSlugs, reported)
	}

	// There is deliberately no shadow selector on toolLogsTargets for the
	// search to pass on, which is a compile-time guarantee rather than
	// something to assert here. That the search really leaves
	// ShadowServerNames unset is pinned against a real configured server by
	// TestSearchToolCallsNarrowsToOneServerWithoutReportedNames.

	// An identity with nothing the platform stamped is empty, so the search
	// reports attribution unavailable rather than dropping the target filter
	// and reading the whole project under this server's name.
	nameOnly := serverIdentity{
		mcpServerID: "",
		mcpSlug:     "",
		toolsetSlug: "",
		urlSuffixes: nil,
		toolSources: []string{"billing", "Billing"},
	}.toolLogsTargets()
	require.True(t, nameOnly.empty())
}

// TestToolLogsTargets_HostedToolsetSlugIsHostedOnly pins that a hosted server's
// toolset slug is carried as the hosted selector, and that when serverIdentity
// blanked it because several configured wrappers share the toolset, the server
// is still matched by the target ids alone.
func TestToolLogsTargets_HostedToolsetSlugIsHostedOnly(t *testing.T) {
	t.Parallel()

	serverID := uuid.MustParse("00000000-0000-0000-0000-000000000003").String()
	identity := serverIdentity{
		mcpServerID: serverID,
		mcpSlug:     "payments",
		toolsetSlug: "payments-toolset",
		urlSuffixes: []string{"/mcp/payments"},
		toolSources: nil,
	}
	targets := identity.toolLogsTargets()
	require.Equal(t, []string{"payments-toolset"}, targets.hostedToolsetSlugs)
	require.Equal(t, []string{"payments", serverID}, targets.mcpServerTargetIDs)

	identity.toolsetSlug = ""
	shared := identity.toolLogsTargets()
	require.Empty(t, shared.hostedToolsetSlugs, "a toolset shared by several wrappers cannot be attributed to one")
	require.False(t, shared.empty())
}

// TestSearchToolCalls_TraversalBudgetEndsPaging pins the cap on how many calls
// one search may walk: a cursor at the edge of the budget yields only what is
// left and no further cursor, and one at the cap yields nothing.
func TestSearchToolCalls_TraversalBudgetEndsPaging(t *testing.T) {
	t.Parallel()

	rows := make([]telemetryrepo.ToolUsageTraceSummary, 0, maxToolCallSearchLimit+1)
	for i := range maxToolCallSearchLimit + 1 {
		rows = append(rows, toolCallSearchRow(fmt.Sprintf("row-%03d", i), toolCallSearchTestNow.Add(-time.Duration(i+1)*time.Second), "email", "person@example.test", 200))
	}
	reader := &recordingToolCallSearchReader{rows: rows}
	service := newToolCallSearchService(t, reader, &recordingDrilldownAuditor{})
	principal := testPrincipal()
	input := SearchToolCallsInput{ProjectID: toolCallSearchTestProject, Limit: maxToolCallSearchLimit}
	scope := func() string {
		search, err := normalizeToolCallSearch(input)
		require.NoError(t, err)
		search.window, err = resolveWindow(input.Window, toolCallSearchTestNow, toolCallSearchWindowSpec)
		require.NoError(t, err)
		return search.cursorScope()
	}()
	window, err := resolveWindow(input.Window, toolCallSearchTestNow, toolCallSearchWindowSpec)
	require.NoError(t, err)
	mintCursor := func(traversed int) string {
		cursor, err := service.references.EncodeScoped(principal, subjectKindCursor, scope, formatToolCallCursor(toolCallSearchTestNow.UnixNano(), "row-000", traversed, window), toolCallSearchTestNow)
		require.NoError(t, err)
		return cursor
	}

	// A full page under the budget hands back a cursor.
	output, err := service.SearchToolCalls(t.Context(), principal, input)
	require.NoError(t, err)
	require.Len(t, output.Calls, maxToolCallSearchLimit)
	require.Equal(t, maxToolCallSearchLimit+1, reader.traceParams.Limit)
	require.NotEmpty(t, output.NextCursor)

	// Three calls short of the cap: only three are served, and no cursor.
	nearCap := input
	nearCap.Cursor = mintCursor(maxToolCallSearchTraversal - 3)
	output, err = service.SearchToolCalls(t.Context(), principal, nearCap)
	require.NoError(t, err)
	require.Len(t, output.Calls, 3)
	require.Empty(t, output.NextCursor)

	// At the cap: nothing is served, and no cursor.
	atCap := input
	atCap.Cursor = mintCursor(maxToolCallSearchTraversal)
	output, err = service.SearchToolCalls(t.Context(), principal, atCap)
	require.NoError(t, err)
	require.Empty(t, output.Calls)
	require.Empty(t, output.NextCursor)
	require.False(t, output.Envelope.NoObservations, "rows were observed even though none could be handed over")
}

// TestSearchToolCalls_CursorResumesAcrossThePageBoundary pins that following the
// cursor walks the fixture in order without skipping or repeating a row, and
// that the walk ends without a cursor once the fixture is exhausted.
func TestSearchToolCalls_CursorResumesAcrossThePageBoundary(t *testing.T) {
	t.Parallel()

	rows := make([]telemetryrepo.ToolUsageTraceSummary, 0, 5)
	for i := range 5 {
		rows = append(rows, toolCallSearchRow(fmt.Sprintf("row-%d", i), toolCallSearchTestNow.Add(-time.Duration(i+1)*time.Minute), "email", "person@example.test", 200))
	}
	reader := &recordingToolCallSearchReader{rows: rows, paged: true}
	service := newToolCallSearchService(t, reader, &recordingDrilldownAuditor{})
	principal := testPrincipal()
	input := SearchToolCallsInput{ProjectID: toolCallSearchTestProject, Limit: 2}

	seen := []string{}
	cursor := ""
	for page := range 3 {
		input.Cursor = cursor
		output, err := service.SearchToolCalls(t.Context(), principal, input)
		require.NoError(t, err)
		for _, call := range output.Calls {
			seen = append(seen, call.OccurredAt)
		}
		if page < 2 {
			require.Len(t, output.Calls, 2)
			require.NotEmpty(t, output.NextCursor)
		} else {
			require.Len(t, output.Calls, 1)
			require.Empty(t, output.NextCursor, "the exhausted fixture yields no further cursor")
		}
		cursor = output.NextCursor
	}
	expected := make([]string, 0, len(rows))
	for _, row := range rows {
		expected = append(expected, time.Unix(0, row.StartTimeUnixNano).UTC().Format(time.RFC3339Nano))
	}
	require.Equal(t, expected, seen)
	require.Equal(t, 3, reader.calls)
	require.Equal(t, rows[3].StartTimeUnixNano, reader.traceParams.CursorTimeUnixNano)
	require.Equal(t, "row-3", reader.traceParams.CursorID)
}

// TestSearchToolCalls_ResponseCapTrimsThePage pins the 256 KiB response cap: a
// page whose rows do not fit is cut to the leading rows that do, the cursor
// resumes from the last row actually handed over so the dropped suffix stays
// reachable, and a page where nothing fits carries no cursor at all.
func TestSearchToolCalls_ResponseCapTrimsThePage(t *testing.T) {
	t.Parallel()

	const rowCount = 10
	wide := strings.Repeat("x", maxDrilldownResponseBytes/(rowCount/2))
	rows := make([]telemetryrepo.ToolUsageTraceSummary, 0, rowCount+1)
	for i := range rowCount + 1 {
		row := toolCallSearchRow(fmt.Sprintf("row-%02d", i), toolCallSearchTestNow.Add(-time.Duration(i+1)*time.Minute), "email", "person@example.test", 200)
		row.ToolName = wide
		rows = append(rows, row)
	}
	reader := &recordingToolCallSearchReader{rows: rows, paged: true}
	service := newToolCallSearchService(t, reader, &recordingDrilldownAuditor{})
	principal := testPrincipal()
	input := SearchToolCallsInput{ProjectID: toolCallSearchTestProject, Limit: rowCount}

	output, err := service.SearchToolCalls(t.Context(), principal, input)
	require.NoError(t, err)
	require.NotEmpty(t, output.Calls)
	require.Less(t, len(output.Calls), rowCount, "the page must be cut to fit the response cap")
	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), maxDrilldownResponseBytes)
	require.NotEmpty(t, output.NextCursor, "a page cut to fit still has a next page")

	// The cursor resumes from the last row served, not from the last row read.
	next := input
	next.Cursor = output.NextCursor
	_, err = service.SearchToolCalls(t.Context(), principal, next)
	require.NoError(t, err)
	last := rows[len(output.Calls)-1]
	require.Equal(t, last.StartTimeUnixNano, reader.traceParams.CursorTimeUnixNano)
	require.Equal(t, last.ID, reader.traceParams.CursorID)

	// A single row larger than the cap leaves nothing to serve and no position
	// to resume from.
	huge := toolCallSearchRow("row-huge", toolCallSearchTestNow.Add(-time.Minute), "email", "person@example.test", 200)
	huge.ToolName = strings.Repeat("x", maxDrilldownResponseBytes+1)
	reader.rows = []telemetryrepo.ToolUsageTraceSummary{huge, rows[0]}
	output, err = service.SearchToolCalls(t.Context(), principal, input)
	require.NoError(t, err)
	require.Empty(t, output.Calls)
	require.Empty(t, output.NextCursor)
}

// TestToolCallSearchTools_AreRegisteredForBothAudiences pins the manifest: both
// tools serve the external endpoint and the assistant, are read-only, name their
// project explicitly, and the row-level search is admin-gated while key
// discovery follows the project-read reads it exists to prepare.
func TestToolCallSearchTools_AreRegisteredForBothAudiences(t *testing.T) {
	t.Parallel()

	registrar := newRegistrar(newTestMCPServer())
	registerToolCallSearchTools(registrar, newToolCallSearchService(t, &recordingToolCallSearchReader{}, &recordingDrilldownAuditor{}))

	search := descriptorByName(t, registrar, "search_tool_calls")
	require.Equal(t, ExternalAuthorizationOrgAdmin, search.Meta.Authorization)
	require.Equal(t, bothAudiences, search.Meta.Audiences)
	require.Equal(t, ProjectScopeExplicit, search.Meta.ProjectScope)
	require.True(t, search.Annotations.ReadOnlyHint)

	keys := descriptorByName(t, registrar, "list_attribute_keys")
	require.Equal(t, ExternalAuthorizationMember, keys.Meta.Authorization)
	require.Equal(t, bothAudiences, keys.Meta.Audiences)
	require.Equal(t, ProjectScopeExplicit, keys.Meta.ProjectScope)
	require.Equal(t, discoveryProjectRead, keys.Meta.DiscoveryScopes)
	require.True(t, keys.Annotations.ReadOnlyHint)
}
