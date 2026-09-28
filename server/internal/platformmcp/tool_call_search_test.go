package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
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

var toolCallSearchTestNow = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

const toolCallSearchTestProject = "00000000-0000-0000-0000-000000000001"

// newToolCallSearchService composes the search without Postgres or ClickHouse:
// every dependency the search reaches is a fake, and the mcp_id attribution
// path, which needs Postgres, is the one path these tests do not exercise.
func newToolCallSearchService(t *testing.T, search ToolCallSearchReader, auditor DrilldownAuditor) *DiagnosticsService {
	t.Helper()

	codec, err := newSubjectReferenceCodec("tool-call-search-test-key")
	require.NoError(t, err)
	return &DiagnosticsService{
		db:              nil,
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
		identityGate:    nil,
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
	require.Empty(t, reader.traceParams.HostedToolsetSlugs)
	require.Empty(t, reader.traceParams.MCPServerTargetIDs)
	// No search ever selects a shadow row by the name a calling app reported.
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

func TestToolCallCursor_CarriesThePageKeyAndTraversal(t *testing.T) {
	t.Parallel()

	position, id, traversed, err := parseToolCallCursor(formatToolCallCursor(1_700_000_000_000_000_000, "summary-1", 40))
	require.NoError(t, err)
	require.Equal(t, int64(1_700_000_000_000_000_000), position)
	require.Equal(t, "summary-1", id)
	require.Equal(t, 40, traversed)

	for _, value := range []string{"", "t:1:0:x", "s:", "s:abc", "s:-1:0:x", "s:0:0:x", "s:1700000000", "s:1700000000:x", "s:1700000000:0:", "s:1700000000:-1:x", formatToolCallCursor(1, "x", maxToolCallSearchTraversal+1)} {
		_, _, _, err := parseToolCallCursor(value)
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

// TestServerIdentitiesForToolCallSearch_RemoteServerWithoutToolsetSlug pins that
// a configured remote server, which has no toolset slug, is still matched by
// the slug and id its matcher stamps as the target id under the hosted or
// tunneled type.
func TestServerIdentitiesForToolCallSearch_RemoteServerWithoutToolsetSlug(t *testing.T) {
	t.Parallel()

	serverID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	identities := serverIdentitiesForToolCallSearch(platformrepo.GetPlatformMCPDiagnosticsTargetRow{
		McpServerID:     serverID,
		ProjectID:       uuid.MustParse(toolCallSearchTestProject),
		McpSlug:         "billing",
		ToolsetSlug:     "",
		ToolsetMcpCount: 0,
	})

	require.False(t, identities.empty())
	require.Equal(t, []string{"billing", serverID.String()}, identities.targetIDs)
	require.Empty(t, identities.toolsetSlugs)

	// A server with no slug is still matched by its id.
	unnamed := serverIdentitiesForToolCallSearch(platformrepo.GetPlatformMCPDiagnosticsTargetRow{
		McpServerID:     serverID,
		ProjectID:       uuid.MustParse(toolCallSearchTestProject),
		McpSlug:         "",
		ToolsetSlug:     "",
		ToolsetMcpCount: 0,
	})
	require.Equal(t, []string{serverID.String()}, unnamed.targetIDs)
}

// TestServerIdentitiesForToolCallSearch_OmitsClientReportedNames pins the rule
// that keeps one configured server's history its own: a shadow row is named by
// the calling app, so a personal server someone happens to call "billing" must
// not surface in the corporate "billing" server's history. The identities
// therefore carry no shadow selector at all, and the only spellings they do
// carry are matched under target types a client cannot choose.
func TestServerIdentitiesForToolCallSearch_OmitsClientReportedNames(t *testing.T) {
	t.Parallel()

	serverID := uuid.MustParse("00000000-0000-0000-0000-000000000004")
	identities := serverIdentitiesForToolCallSearch(platformrepo.GetPlatformMCPDiagnosticsTargetRow{
		McpServerID:     serverID,
		ProjectID:       uuid.MustParse(toolCallSearchTestProject),
		McpSlug:         "billing",
		ToolsetSlug:     "billing-toolset",
		ToolsetMcpCount: 1,
	})

	// Every selector the search can build from a configured server, so a new
	// one cannot be added without this assertion being revisited.
	params := telemetryrepo.ListToolUsageTracesParams{
		HostedToolsetSlugs: identities.toolsetSlugs,
		MCPServerTargetIDs: identities.targetIDs,
		ShadowServerNames:  nil,
	}
	require.Equal(t, []string{"billing-toolset"}, params.HostedToolsetSlugs)
	require.Equal(t, []string{"billing", serverID.String()}, params.MCPServerTargetIDs)
	require.Empty(t, params.ShadowServerNames,
		"a configured server must never select shadow rows by a client-reported name")

	// A server known only by a name a client reported has no reliable identity
	// at all, so the search reports attribution unavailable rather than handing
	// back whatever else answers to that name.
	nameOnly := serverIdentitiesForToolCallSearch(platformrepo.GetPlatformMCPDiagnosticsTargetRow{
		McpServerID:     uuid.Nil,
		ProjectID:       uuid.MustParse(toolCallSearchTestProject),
		McpSlug:         "",
		ToolsetSlug:     "",
		ToolsetMcpCount: 0,
	})
	require.True(t, nameOnly.empty())
}

// TestServerIdentitiesForToolCallSearch_HostedToolsetSlugIsHostedOnly pins that
// a hosted server's toolset slug is matched under the hosted type only, and is
// dropped when several configured wrappers share the toolset.
func TestServerIdentitiesForToolCallSearch_HostedToolsetSlugIsHostedOnly(t *testing.T) {
	t.Parallel()

	serverID := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	row := platformrepo.GetPlatformMCPDiagnosticsTargetRow{
		McpServerID:     serverID,
		ProjectID:       uuid.MustParse(toolCallSearchTestProject),
		McpSlug:         "payments",
		ToolsetSlug:     "payments-toolset",
		ToolsetMcpCount: 1,
	}
	identities := serverIdentitiesForToolCallSearch(row)
	require.Equal(t, []string{"payments-toolset"}, identities.toolsetSlugs)
	require.Equal(t, []string{"payments", serverID.String()}, identities.targetIDs)

	row.ToolsetMcpCount = 2
	shared := serverIdentitiesForToolCallSearch(row)
	require.Empty(t, shared.toolsetSlugs, "a toolset shared by several wrappers cannot be attributed to one")
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
	mintCursor := func(traversed int) string {
		cursor, err := service.references.EncodeScoped(principal, subjectKindCursor, scope, formatToolCallCursor(toolCallSearchTestNow.UnixNano(), "row-000", traversed), toolCallSearchTestNow)
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

// TestToolCallSearchTools_RequireTheDrilldownComposition pins that the pair is
// withheld, not served unbound, when any dependency a page relies on is absent.
func TestToolCallSearchTools_RequireTheDrilldownComposition(t *testing.T) {
	t.Parallel()

	reader := &recordingToolCallSearchReader{}
	require.False(t, (*DiagnosticsService)(nil).toolCallSearchValid())
	require.True(t, newToolCallSearchService(t, reader, &recordingDrilldownAuditor{}).toolCallSearchValid())

	withoutReferences := newToolCallSearchService(t, reader, &recordingDrilldownAuditor{})
	withoutReferences.references = nil
	require.False(t, withoutReferences.toolCallSearchValid())
	_, err := withoutReferences.SearchToolCalls(t.Context(), testPrincipal(), SearchToolCallsInput{ProjectID: toolCallSearchTestProject})
	require.ErrorIs(t, err, ErrUnavailable)

	withoutAuditor := newToolCallSearchService(t, reader, nil)
	require.False(t, withoutAuditor.toolCallSearchValid())

	withoutSearch := newToolCallSearchService(t, nil, &recordingDrilldownAuditor{})
	require.False(t, withoutSearch.toolCallSearchValid())
	require.Same(t, withoutSearch, withoutSearch.WithToolCallSearch(nil))
	require.True(t, withoutSearch.WithToolCallSearch(reader).toolCallSearchValid())

	// mcp_id needs Postgres to resolve the server's telemetry identities.
	_, err = newToolCallSearchService(t, reader, &recordingDrilldownAuditor{}).SearchToolCalls(t.Context(), testPrincipal(), SearchToolCallsInput{ProjectID: toolCallSearchTestProject, MCPID: "00000000-0000-0000-0000-000000000002"})
	require.ErrorIs(t, err, ErrUnavailable)
}

// TestToolCallSearchTools_AreRegisteredForBothAudiences pins the manifest: both
// tools serve the external endpoint and the assistant, are read-only, name their
// project explicitly, and the row-level search is admin-gated while key
// discovery follows the project-read reads it exists to prepare.
func TestToolCallSearchTools_AreRegisteredForBothAudiences(t *testing.T) {
	t.Parallel()

	live := newRegistrar(newTestMCPServer())
	registerToolCallSearchTools(live, nil)
	unavailable := newRegistrar(newTestMCPServer())
	registerUnavailableToolCallSearchTools(unavailable)

	for _, registrar := range []*Registrar{live, unavailable} {
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
}
