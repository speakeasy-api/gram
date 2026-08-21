//nolint:exhaustruct // Diagnostic projections intentionally omit documented optional fields.
package platformmcp

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	SensitiveDiagnosticsConnectionLimitName   = "platform-mcp-sensitive-diagnostics-connection"
	SensitiveDiagnosticsOrganizationLimitName = "platform-mcp-sensitive-diagnostics-organization"
)

const (
	// maxDrilldownTools bounds the per-tool event breakdown.
	maxDrilldownTools = 25
	// maxTraceReferences bounds one page of correlation references.
	maxTraceReferences = 20
	// maxUserStatusRows bounds one page of per-subject rows.
	maxUserStatusRows = 25
)

// DrilldownTelemetryReader is the additional telemetry the bounded drill-down
// tools read. It stays separate from DiagnosticsTelemetryReader so the
// overview-first entry points cannot accidentally acquire row-level reads.
type DrilldownTelemetryReader interface {
	GetMCPToolOutcomeBreakdown(ctx context.Context, arg telemetryrepo.GetMCPToolOutcomeBreakdownParams) ([]telemetryrepo.MCPToolOutcomeBreakdownRow, error)
	ListMCPTraceReferences(ctx context.Context, arg telemetryrepo.ListMCPTraceReferencesParams) ([]telemetryrepo.MCPTraceReferenceRow, error)
	GetTopUsers(ctx context.Context, arg telemetryrepo.GetTopUsersParams) ([]telemetryrepo.TopUser, error)
}

// drilldownTarget is one resolved MCP plus the window to read it over. Every
// drill-down begins by resolving these together, because each of them is an
// authorization decision: the MCP must be one this principal can already see,
// and the window must be one of the named ones.
type drilldownTarget struct {
	toolsetSlugs []string
	urlSuffixes  []string
	projectID    string
	window       ResolvedWindow
	now          time.Time
}

func (s *DiagnosticsService) resolveDrilldown(ctx context.Context, principal Principal, projectID, mcpID, window string, budget OperationBudget) (drilldownTarget, error) {
	if !s.valid() || s.drilldown == nil || !budget.valid() {
		return drilldownTarget{}, ErrUnavailable
	}
	if projectID == "" || mcpID == "" {
		return drilldownTarget{}, fmt.Errorf("project_id and mcp_id are required")
	}
	if err := budget.Allow(ctx, principal); err != nil {
		return drilldownTarget{}, err
	}
	now := s.now()
	resolved, err := resolveWindow(window, now)
	if err != nil {
		return drilldownTarget{}, err
	}
	// GetMCP is the authorization boundary, exactly as it is for diagnostics:
	// it fails closed for an MCP this principal cannot see, and every read below
	// is scoped to what it returned.
	if _, err := s.reader.GetMCP(ctx, principal, GetMCPInput{ProjectID: projectID, MCPID: mcpID}); err != nil {
		return drilldownTarget{}, fmt.Errorf("resolve drilldown mcp: %w", err)
	}
	target, err := s.diagnosticsTarget(ctx, principal.OrganizationID, projectID, mcpID)
	if err != nil {
		return drilldownTarget{}, err
	}
	return drilldownTarget{
		toolsetSlugs: nonEmpty(target.ToolsetSlug),
		urlSuffixes:  mcpURLSuffixes(target.McpSlug),
		projectID:    projectID,
		window:       resolved,
		now:          now,
	}, nil
}

func (t drilldownTarget) outcomeParams() telemetryrepo.GetMCPOutcomeBreakdownParams {
	return telemetryrepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs:       []string{t.projectID},
		ToolsetSlugs:         t.toolsetSlugs,
		MCPServerURLSuffixes: t.urlSuffixes,
		TimeStart:            t.window.start.UnixNano(),
		TimeEnd:              t.window.end.UnixNano(),
	}
}

func (s *DiagnosticsService) drilldownEnvelope(ctx context.Context, target drilldownTarget) (DataEnvelope, error) {
	watermark, err := s.telemetry.GetTelemetryWatermark(ctx, telemetryrepo.GetTelemetryWatermarkParams{
		GramProjectIDs: []string{target.projectID},
	})
	if err != nil {
		return DataEnvelope{}, fmt.Errorf("read drilldown watermark: %w", err)
	}
	return newDataEnvelope(target.now, watermarkTime(watermark), target.window), nil
}

// QueryMCPEventsInput drills into one MCP's calls by tool. It has no free-text
// filter and no attribute selector: the only axis is the MCP the overview
// already named.
type QueryMCPEventsInput struct {
	ProjectID string `json:"project_id" jsonschema:"AICP project ID that owns the MCP"`
	MCPID     string `json:"mcp_id" jsonschema:"configured MCP ID as returned by find_mcp or get_mcp"`
	Window    string `json:"window,omitempty" jsonschema:"observation window: 1h, 24h (default), 7d, or 30d"`
}

// MCPToolEvents is one tool's calls in the window, already summed per outcome.
type MCPToolEvents struct {
	ToolName string            `json:"tool_name"`
	Outcomes MCPOutcomeSummary `json:"outcomes"`
}

type QueryMCPEventsOutput struct {
	ProjectID string          `json:"project_id"`
	MCPID     string          `json:"mcp_id"`
	Envelope  DataEnvelope    `json:"data"`
	Tools     []MCPToolEvents `json:"tools"`
	Truncated bool            `json:"truncated"`
}

func (s *DiagnosticsService) QueryMCPEvents(ctx context.Context, principal Principal, input QueryMCPEventsInput) (QueryMCPEventsOutput, error) {
	target, err := s.resolveDrilldown(ctx, principal, input.ProjectID, input.MCPID, input.Window, s.budget)
	if err != nil {
		return QueryMCPEventsOutput{}, err
	}
	rows, err := s.drilldown.GetMCPToolOutcomeBreakdown(ctx, telemetryrepo.GetMCPToolOutcomeBreakdownParams{
		GetMCPOutcomeBreakdownParams: target.outcomeParams(),
	})
	if err != nil {
		return QueryMCPEventsOutput{}, fmt.Errorf("read mcp tool outcome breakdown: %w", err)
	}
	envelope, err := s.drilldownEnvelope(ctx, target)
	if err != nil {
		return QueryMCPEventsOutput{}, err
	}
	tools, truncated := toolEvents(rows)
	return QueryMCPEventsOutput{
		ProjectID: input.ProjectID,
		MCPID:     input.MCPID,
		Envelope:  envelope,
		Tools:     tools,
		Truncated: truncated,
	}, nil
}

// toolEvents folds the breakdown to one row per tool, ordered by failures then
// calls so the broken tool leads, and caps the list with an explicit flag.
func toolEvents(rows []telemetryrepo.MCPToolOutcomeBreakdownRow) ([]MCPToolEvents, bool) {
	byTool := map[string]*outcomeTotals{}
	order := []string{}
	for _, row := range rows {
		totals, ok := byTool[row.ToolName]
		if !ok {
			totals = &outcomeTotals{}
			byTool[row.ToolName] = totals
			order = append(order, row.ToolName)
		}
		addOutcome(totals, row.Outcome, boundedCount(row.CallCount))
	}
	events := make([]MCPToolEvents, 0, len(order))
	for _, name := range order {
		events = append(events, MCPToolEvents{ToolName: name, Outcomes: summaryFromTotals(*byTool[name])})
	}
	sortToolEvents(events)
	if len(events) > maxDrilldownTools {
		return events[:maxDrilldownTools], true
	}
	return events, false
}

// QueryMCPTracesInput asks for individual occurrences of a failure class the
// overview or diagnostics already identified.
type QueryMCPTracesInput struct {
	ProjectID string `json:"project_id" jsonschema:"AICP project ID that owns the MCP"`
	MCPID     string `json:"mcp_id" jsonschema:"configured MCP ID as returned by find_mcp or get_mcp"`
	Window    string `json:"window,omitempty" jsonschema:"observation window: 1h, 24h (default), 7d, or 30d"`
	Outcome   string `json:"outcome,omitempty" jsonschema:"optional outcome class to narrow to: success, unauthorized, client_error, server_error, failed, or unknown"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"opaque cursor returned by a previous query_mcp_traces result"`
}

// MCPTraceReference is one occurrence, reduced to what an investigation needs:
// when it happened, which tool, how it ended, and an opaque handle to quote.
// It carries no arguments, results, bodies, headers, URLs, or identities.
type MCPTraceReference struct {
	Reference  string `json:"reference"`
	OccurredAt string `json:"occurred_at"`
	ToolName   string `json:"tool_name,omitempty"`
	Outcome    string `json:"outcome"`
	Client     string `json:"client"`
}

type QueryMCPTracesOutput struct {
	ProjectID  string              `json:"project_id"`
	MCPID      string              `json:"mcp_id"`
	Envelope   DataEnvelope        `json:"data"`
	Traces     []MCPTraceReference `json:"traces"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

func (s *DiagnosticsService) QueryMCPTraces(ctx context.Context, principal Principal, input QueryMCPTracesInput) (QueryMCPTracesOutput, error) {
	if s == nil || s.references == nil {
		return QueryMCPTracesOutput{}, ErrUnavailable
	}
	if input.Outcome != "" && !validOutcomeClass(input.Outcome) {
		return QueryMCPTracesOutput{}, fmt.Errorf("outcome must be one of success, unauthorized, client_error, server_error, failed, unknown")
	}
	target, err := s.resolveDrilldown(ctx, principal, input.ProjectID, input.MCPID, input.Window, s.budget)
	if err != nil {
		return QueryMCPTracesOutput{}, err
	}

	before, err := s.decodeTraceCursor(input.Cursor, principal, target.now)
	if err != nil {
		return QueryMCPTracesOutput{}, err
	}

	params := telemetryrepo.ListMCPTraceReferencesParams{
		GetMCPOutcomeBreakdownParams: target.outcomeParams(),
		BeforeUnixNano:               before,
		// One extra row decides whether another page exists without a second
		// round trip, and is dropped before anything is projected.
		Limit: maxTraceReferences + 1,
	}
	if input.Outcome != "" {
		params.Outcomes = []string{input.Outcome}
	}
	rows, err := s.drilldown.ListMCPTraceReferences(ctx, params)
	if err != nil {
		return QueryMCPTracesOutput{}, fmt.Errorf("read mcp trace references: %w", err)
	}
	envelope, err := s.drilldownEnvelope(ctx, target)
	if err != nil {
		return QueryMCPTracesOutput{}, err
	}

	more := len(rows) > maxTraceReferences
	if more {
		rows = rows[:maxTraceReferences]
	}
	traces := make([]MCPTraceReference, 0, len(rows))
	for _, row := range rows {
		reference, err := s.references.Encode(principal, subjectKindTrace, row.TraceID, target.now)
		if err != nil {
			return QueryMCPTracesOutput{}, fmt.Errorf("mint trace reference: %w", err)
		}
		traces = append(traces, MCPTraceReference{
			Reference:  reference,
			OccurredAt: time.Unix(0, row.OccurredAt).UTC().Format(time.RFC3339),
			ToolName:   row.ToolName,
			Outcome:    row.Outcome,
			Client:     row.ClientName,
		})
	}

	output := QueryMCPTracesOutput{
		ProjectID: input.ProjectID,
		MCPID:     input.MCPID,
		Envelope:  envelope,
		Traces:    traces,
	}
	if more && len(rows) > 0 {
		cursor, err := s.references.Encode(principal, subjectKindTrace, formatCursorPosition(rows[len(rows)-1].OccurredAt), target.now)
		if err != nil {
			return QueryMCPTracesOutput{}, fmt.Errorf("mint trace cursor: %w", err)
		}
		output.NextCursor = cursor
	}
	return output, nil
}

// QueryMCPMetricsInput asks for one MCP's aggregate levels over a window.
type QueryMCPMetricsInput struct {
	ProjectID string `json:"project_id" jsonschema:"AICP project ID that owns the MCP"`
	MCPID     string `json:"mcp_id" jsonschema:"configured MCP ID as returned by find_mcp or get_mcp"`
	Window    string `json:"window,omitempty" jsonschema:"observation window: 1h, 24h (default), 7d, or 30d"`
}

// QueryMCPMetricsOutput is aggregated to the window it names. There is
// deliberately no per-bucket series: an external MCP client has no scratch
// compute, and handing it buckets to sum would be a contract failure.
type QueryMCPMetricsOutput struct {
	ProjectID string       `json:"project_id"`
	MCPID     string       `json:"mcp_id"`
	Envelope  DataEnvelope `json:"data"`

	ToolCalls       int64 `json:"tool_calls"`
	FailedToolCalls int64 `json:"failed_tool_calls"`
	// FailureRate is computed server-side and rounded to four decimal places.
	// Zero calls yields zero rather than an undefined ratio.
	FailureRate  float64      `json:"failure_rate"`
	AvgLatencyMs float64      `json:"avg_latency_ms"`
	ActiveUsers  SubjectCount `json:"active_users"`
}

func (s *DiagnosticsService) QueryMCPMetrics(ctx context.Context, principal Principal, input QueryMCPMetricsInput) (QueryMCPMetricsOutput, error) {
	target, err := s.resolveDrilldown(ctx, principal, input.ProjectID, input.MCPID, input.Window, s.budget)
	if err != nil {
		return QueryMCPMetricsOutput{}, err
	}
	toolsetSlug := ""
	if len(target.toolsetSlugs) > 0 {
		toolsetSlug = target.toolsetSlugs[0]
	}
	start, end := target.window.start.UnixNano(), target.window.end.UnixNano()

	summary, err := s.telemetry.GetOverviewSummary(ctx, telemetryrepo.GetOverviewSummaryParams{
		GramProjectID: target.projectID,
		TimeStart:     start,
		TimeEnd:       end,
		ToolsetSlug:   toolsetSlug,
	})
	if err != nil {
		return QueryMCPMetricsOutput{}, fmt.Errorf("read mcp metrics summary: %w", err)
	}
	counts, err := s.telemetry.GetActiveCounts(ctx, telemetryrepo.GetActiveCountsParams{
		GramProjectID: target.projectID,
		TimeStart:     start,
		TimeEnd:       end,
		ToolsetSlug:   toolsetSlug,
	})
	if err != nil {
		return QueryMCPMetricsOutput{}, fmt.Errorf("read mcp metrics active counts: %w", err)
	}
	envelope, err := s.drilldownEnvelope(ctx, target)
	if err != nil {
		return QueryMCPMetricsOutput{}, err
	}

	output := QueryMCPMetricsOutput{
		ProjectID: input.ProjectID,
		MCPID:     input.MCPID,
		Envelope:  envelope,
	}
	if summary != nil {
		output.ToolCalls = boundedCount(summary.TotalToolCalls)
		output.FailedToolCalls = boundedCount(summary.FailedToolCalls)
		output.FailureRate = failureRate(output.ToolCalls, output.FailedToolCalls)
		output.AvgLatencyMs = summary.AvgLatencyMs
	}
	if counts != nil {
		output.ActiveUsers = NewSubjectCount(boundedCount(counts.ActiveUsersCount))
	}
	return output, nil
}

// GetUserMCPStatusInput asks who is using one MCP and how it is going for them.
type GetUserMCPStatusInput struct {
	ProjectID string `json:"project_id" jsonschema:"AICP project ID that owns the MCP"`
	MCPID     string `json:"mcp_id" jsonschema:"configured MCP ID as returned by find_mcp or get_mcp"`
	Window    string `json:"window,omitempty" jsonschema:"observation window: 1h, 24h (default), 7d, or 30d"`
	// IncludeRows asks for per-subject rows rather than the aggregate alone.
	// It is refused when too few subjects are involved for a row to be
	// anything other than a person.
	IncludeRows bool `json:"include_rows,omitempty" jsonschema:"request per-subject rows; refused when fewer than five subjects are involved"`
}

// MCPUserStatus is one subject's activity against one MCP, addressed only by an
// expiring opaque reference. It carries no email, no external user id, and no
// name.
type MCPUserStatus struct {
	SubjectReference string `json:"subject_reference"`
	ToolCalls        int64  `json:"tool_calls"`
}

type GetUserMCPStatusOutput struct {
	ProjectID string       `json:"project_id"`
	MCPID     string       `json:"mcp_id"`
	Envelope  DataEnvelope `json:"data"`

	ActiveUsers SubjectCount `json:"active_users"`
	// RowsSuppressed reports that per-subject rows were withheld because too
	// few subjects were involved. It is stated rather than left to be inferred
	// from an empty list, which would read as "nobody used this".
	RowsSuppressed bool            `json:"rows_suppressed"`
	Users          []MCPUserStatus `json:"users"`
}

func (s *DiagnosticsService) GetUserMCPStatus(ctx context.Context, principal Principal, input GetUserMCPStatusInput) (GetUserMCPStatusOutput, error) {
	if s == nil || s.references == nil {
		return GetUserMCPStatusOutput{}, ErrUnavailable
	}
	// Metered on its own allowance: this is the one drill-down that reaches
	// personal data, so exhausting it must not be possible by spending the
	// ordinary diagnostic budget.
	target, err := s.resolveDrilldown(ctx, principal, input.ProjectID, input.MCPID, input.Window, s.sensitiveBudget)
	if err != nil {
		return GetUserMCPStatusOutput{}, err
	}
	toolsetSlug := ""
	if len(target.toolsetSlugs) > 0 {
		toolsetSlug = target.toolsetSlugs[0]
	}
	start, end := target.window.start.UnixNano(), target.window.end.UnixNano()

	counts, err := s.telemetry.GetActiveCounts(ctx, telemetryrepo.GetActiveCountsParams{
		GramProjectID: target.projectID,
		TimeStart:     start,
		TimeEnd:       end,
		ToolsetSlug:   toolsetSlug,
	})
	if err != nil {
		return GetUserMCPStatusOutput{}, fmt.Errorf("read mcp user active counts: %w", err)
	}
	envelope, err := s.drilldownEnvelope(ctx, target)
	if err != nil {
		return GetUserMCPStatusOutput{}, err
	}

	var activeUsers int64
	if counts != nil {
		activeUsers = boundedCount(counts.ActiveUsersCount)
	}
	output := GetUserMCPStatusOutput{
		ProjectID:   input.ProjectID,
		MCPID:       input.MCPID,
		Envelope:    envelope,
		ActiveUsers: NewSubjectCount(activeUsers),
		Users:       []MCPUserStatus{},
	}
	if !input.IncludeRows {
		return output, nil
	}
	// Refused rather than trimmed: with fewer than five subjects, a row is a
	// person however it is labelled, and the aggregate above already answers
	// the question rows were asked for.
	if activeUsers < SubjectSuppressionThreshold {
		output.RowsSuppressed = true
		return output, nil
	}

	users, err := s.drilldown.GetTopUsers(ctx, telemetryrepo.GetTopUsersParams{
		GramProjectID: target.projectID,
		TimeStart:     start,
		TimeEnd:       end,
		ToolsetSlug:   toolsetSlug,
		Limit:         maxUserStatusRows,
	})
	if err != nil {
		return GetUserMCPStatusOutput{}, fmt.Errorf("read mcp user activity: %w", err)
	}
	for _, user := range users {
		// user.UserID is whichever identity the telemetry fold resolved — an
		// email in many cases. It is minted into a reference here and never
		// projected, which is the whole reason references exist.
		reference, err := s.references.Encode(principal, subjectKindUser, user.UserID, target.now)
		if err != nil {
			return GetUserMCPStatusOutput{}, fmt.Errorf("mint subject reference: %w", err)
		}
		output.Users = append(output.Users, MCPUserStatus{
			SubjectReference: reference,
			ToolCalls:        boundedCount(user.ActivityCount),
		})
	}
	return output, nil
}

func (s *DiagnosticsService) decodeTraceCursor(cursor string, principal Principal, now time.Time) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	value, err := s.references.Decode(cursor, principal, subjectKindTrace, now)
	if err != nil {
		return 0, ErrSubjectReferenceInvalid
	}
	position, err := parseCursorPosition(value)
	if err != nil {
		return 0, ErrSubjectReferenceInvalid
	}
	return position, nil
}

// sortToolEvents puts the tool a caller should look at first at the top:
// most failures, then most calls, then name for a stable order.
func sortToolEvents(events []MCPToolEvents) {
	sort.Slice(events, func(i, j int) bool {
		left, right := events[i].Outcomes, events[j].Outcomes
		leftFailures := left.Unauthorized + left.ClientError + left.ServerError + left.Failed
		rightFailures := right.Unauthorized + right.ClientError + right.ServerError + right.Failed
		if leftFailures != rightFailures {
			return leftFailures > rightFailures
		}
		if left.Total != right.Total {
			return left.Total > right.Total
		}
		return events[i].ToolName < events[j].ToolName
	})
}

// A trace cursor carries only a position, minted through the same bound,
// expiring reference codec as everything else a caller holds between calls.
func formatCursorPosition(unixNano int64) string {
	return "t:" + strconv.FormatInt(unixNano, 10)
}

func parseCursorPosition(value string) (int64, error) {
	rest, ok := strings.CutPrefix(value, "t:")
	if !ok {
		return 0, ErrSubjectReferenceInvalid
	}
	position, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || position <= 0 {
		return 0, ErrSubjectReferenceInvalid
	}
	return position, nil
}

func validOutcomeClass(outcome string) bool {
	switch outcome {
	case telemetryrepo.MCPOutcomeSuccess,
		telemetryrepo.MCPOutcomeUnauthorized,
		telemetryrepo.MCPOutcomeClientError,
		telemetryrepo.MCPOutcomeServerError,
		telemetryrepo.MCPOutcomeFailed,
		telemetryrepo.MCPOutcomeUnknown:
		return true
	default:
		return false
	}
}

func failureRate(total, failed int64) float64 {
	if total <= 0 {
		return 0
	}
	rate := float64(failed) / float64(total)
	// Rounded server-side so the caller is never handed a ratio it would have
	// to format itself.
	return float64(int64(rate*10000+0.5)) / 10000
}
