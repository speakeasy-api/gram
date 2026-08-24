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
)

// DrilldownTelemetryReader is the additional telemetry the bounded drill-down
// tools read. It stays separate from DiagnosticsTelemetryReader so the
// overview-first entry points cannot accidentally acquire row-level reads.
type DrilldownTelemetryReader interface {
	GetMCPToolOutcomeBreakdown(ctx context.Context, arg telemetryrepo.GetMCPToolOutcomeBreakdownParams) ([]telemetryrepo.MCPToolOutcomeBreakdownRow, error)
	ListMCPTraceReferences(ctx context.Context, arg telemetryrepo.ListMCPTraceReferencesParams) ([]telemetryrepo.MCPTraceReferenceRow, error)
}

// drilldownTarget is one resolved MCP plus the window to read it over. Every
// drill-down begins by resolving these together, because each of them is an
// authorization decision: the MCP must be one this principal can already see,
// and the window must be one of the named ones.
type drilldownTarget struct {
	toolsetSlugs []string
	urlSuffixes  []string
	projectID    string
	mcpServerID  string
	window       ResolvedWindow
	now          time.Time
}

// hostedToolsetSlug is the toolset a hosted MCP's traffic is recorded under, or
// empty for a remote, tunneled, or unproxied server.
//
// Emptiness matters: the summary reads treat an empty slug as "no filter", so a
// caller that passes it through unchecked gets the whole project's numbers back
// under one MCP's name. Every use of this value must handle the empty case
// rather than forwarding it.
func (t drilldownTarget) hostedToolsetSlug() string {
	if len(t.toolsetSlugs) == 0 {
		return ""
	}
	return t.toolsetSlugs[0]
}

func (s *DiagnosticsService) resolveDrilldown(ctx context.Context, principal Principal, projectID, mcpID, window string, budget OperationBudget, spec windowSpec) (drilldownTarget, error) {
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
	resolved, err := resolveWindow(window, now, spec)
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
		mcpServerID:  mcpID,
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

func (s *DiagnosticsService) drilldownEnvelope(ctx context.Context, target drilldownTarget, observed bool) (DataEnvelope, error) {
	watermark, err := s.telemetry.GetTelemetryWatermark(ctx, telemetryrepo.GetTelemetryWatermarkParams{
		GramProjectIDs: []string{target.projectID},
	})
	if err != nil {
		return DataEnvelope{}, fmt.Errorf("read drilldown watermark: %w", err)
	}
	return newDataEnvelope(target.now, watermarkTime(watermark), target.window, observed), nil
}

// QueryMCPEventsInput drills into one MCP's calls by tool. It has no free-text
// filter and no attribute selector: the only axis is the MCP the overview
// already named.
type QueryMCPEventsInput struct {
	ProjectID string `json:"project_id" jsonschema:"AICP project ID that owns the MCP"`
	MCPID     string `json:"mcp_id" jsonschema:"configured MCP ID as returned by find_mcp or get_mcp"`
	Window    string `json:"window,omitempty" jsonschema:"observation window: 1h or 24h (default); this tool looks back at most 24h"`
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
	target, err := s.resolveDrilldown(ctx, principal, input.ProjectID, input.MCPID, input.Window, s.budget, drilldownWindowSpec)
	if err != nil {
		return QueryMCPEventsOutput{}, err
	}
	rows, err := s.drilldown.GetMCPToolOutcomeBreakdown(ctx, telemetryrepo.GetMCPToolOutcomeBreakdownParams{
		GetMCPOutcomeBreakdownParams: target.outcomeParams(),
	})
	if err != nil {
		return QueryMCPEventsOutput{}, fmt.Errorf("read mcp tool outcome breakdown: %w", err)
	}
	tools, truncated := toolEvents(rows)
	envelope, err := s.drilldownEnvelope(ctx, target, len(rows) > 0)
	if err != nil {
		return QueryMCPEventsOutput{}, err
	}
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
	Window    string `json:"window,omitempty" jsonschema:"observation window: 1h or 24h (default); this tool looks back at most 24h"`
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
	target, err := s.resolveDrilldown(ctx, principal, input.ProjectID, input.MCPID, input.Window, s.budget, drilldownWindowSpec)
	if err != nil {
		return QueryMCPTracesOutput{}, err
	}

	before, beforeTraceID, err := s.decodeTraceCursor(input.Cursor, principal, target.now)
	if err != nil {
		return QueryMCPTracesOutput{}, err
	}

	params := telemetryrepo.ListMCPTraceReferencesParams{
		GetMCPOutcomeBreakdownParams: target.outcomeParams(),
		BeforeUnixNano:               before,
		BeforeTraceID:                beforeTraceID,
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
	envelope, err := s.drilldownEnvelope(ctx, target, len(rows) > 0)
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
		last := rows[len(rows)-1]
		cursor, err := s.references.Encode(principal, subjectKindTrace, formatCursorPosition(last.OccurredAt, last.TraceID), target.now)
		if err != nil {
			return QueryMCPTracesOutput{}, fmt.Errorf("mint trace cursor: %w", err)
		}
		output.NextCursor = cursor
	}
	return output, nil
}

// summaryIdentityParams scopes the summary read to exactly one identity filter.
//
// The two are recorded on different traffic: hosted calls carry
// gram.toolset.slug and no mcp_server id, so ANDing both matches nothing and
// silently reports zero latency. The mcp_server id is what scopes the models
// that carry no slug, where an empty slug would otherwise read as "no filter"
// and return the whole project under this MCP's name.
func summaryIdentityParams(target drilldownTarget, start, end int64) telemetryrepo.GetOverviewSummaryParams {
	params := telemetryrepo.GetOverviewSummaryParams{
		GramProjectID: target.projectID,
		TimeStart:     start,
		TimeEnd:       end,
	}
	if slug := target.hostedToolsetSlug(); slug != "" {
		params.ToolsetSlug = slug
		return params
	}
	params.MCPServerID = target.mcpServerID
	return params
}

// QueryMCPMetricsInput asks for one MCP's aggregate levels over a window.
type QueryMCPMetricsInput struct {
	ProjectID string `json:"project_id" jsonschema:"AICP project ID that owns the MCP"`
	MCPID     string `json:"mcp_id" jsonschema:"configured MCP ID as returned by find_mcp or get_mcp"`
	Window    string `json:"window,omitempty" jsonschema:"observation window: 1h, 24h (default), or 7d"`
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
	// ActiveUsersUnavailable reports that this MCP's model cannot be scoped by
	// the active-count read, so ActiveUsers is not an answer about this server.
	// Stated rather than left as a zero, which would read as "nobody".
	ActiveUsersUnavailable bool `json:"active_users_unavailable"`
}

func (s *DiagnosticsService) QueryMCPMetrics(ctx context.Context, principal Principal, input QueryMCPMetricsInput) (QueryMCPMetricsOutput, error) {
	target, err := s.resolveDrilldown(ctx, principal, input.ProjectID, input.MCPID, input.Window, s.budget, metricsWindowSpec)
	if err != nil {
		return QueryMCPMetricsOutput{}, err
	}
	toolsetSlug := target.hostedToolsetSlug()
	start, end := target.window.start.UnixNano(), target.window.end.UnixNano()

	// Call volume comes from the same correctly-scoped trace source the rest of
	// the drill-down reads, which matches on both the toolset slug and the
	// server URL. The summary read below can only narrow by toolset slug or
	// mcp_server_id, so it is used for latency alone.
	outcomeRows, err := s.telemetry.GetMCPOutcomeBreakdown(ctx, target.outcomeParams())
	if err != nil {
		return QueryMCPMetricsOutput{}, fmt.Errorf("read mcp metrics outcomes: %w", err)
	}
	summary, err := s.telemetry.GetOverviewSummary(ctx, summaryIdentityParams(target, start, end))
	if err != nil {
		return QueryMCPMetricsOutput{}, fmt.Errorf("read mcp metrics summary: %w", err)
	}
	totals := totalsFromRows(outcomeRows)
	envelope, err := s.drilldownEnvelope(ctx, target, totals.Total > 0)
	if err != nil {
		return QueryMCPMetricsOutput{}, err
	}
	output := QueryMCPMetricsOutput{
		ProjectID:       input.ProjectID,
		MCPID:           input.MCPID,
		Envelope:        envelope,
		ToolCalls:       totals.Total,
		FailedToolCalls: totals.failures(),
		FailureRate:     failureRate(totals.Total, totals.failures()),
	}
	if summary != nil {
		output.AvgLatencyMs = summary.AvgLatencyMs
	}
	// Active users are only answerable for a hosted server: the active-count
	// read narrows by toolset slug and by nothing else, so for any other model
	// the honest answer is that this metric is unavailable for this MCP — not
	// the project's user count wearing its name.
	if toolsetSlug == "" {
		output.ActiveUsersUnavailable = true
		return output, nil
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
	if counts != nil {
		output.ActiveUsers = NewSubjectCount(boundedCount(counts.ActiveUsersCount))
	}
	return output, nil
}

// GetUserMCPStatusInput names one subject and one MCP. The subject is given as
// an opaque reference minted by a summary tool, never as an email, account id,
// or name: the caller asks about someone it was shown, not about someone it
// can describe.
type GetUserMCPStatusInput struct {
	ProjectID string `json:"project_id" jsonschema:"AICP project ID that owns the MCP"`
	MCPID     string `json:"mcp_id" jsonschema:"configured MCP ID as returned by find_mcp or get_mcp"`
	// SubjectReference is an expiring, session-bound handle returned in the
	// optional rows of a summary tool. It cannot be searched, joined,
	// refreshed, or constructed.
	SubjectReference string `json:"subject_reference" jsonschema:"opaque subject reference returned by a summary tool; expires and is bound to this session"`
	Window           string `json:"window,omitempty" jsonschema:"observation window: 1h or 24h (default); this tool looks back at most 24h"`
}

// Subject state categories. Closed vocabulary, assigned server-side: a category
// is the whole answer, so there is no count to reconstruct an individual's
// activity pattern from.
const (
	SubjectStateActive         = "active"
	SubjectStateInactive       = "inactive"
	SubjectStateNoObservations = "no_observations"
)

type GetUserMCPStatusOutput struct {
	ProjectID string       `json:"project_id"`
	MCPID     string       `json:"mcp_id"`
	Envelope  DataEnvelope `json:"data"`

	// MaskedIdentity is enough to recognize a subject already known to the
	// administrator and not enough to learn one. It is never a raw identifier.
	MaskedIdentity string `json:"masked_identity"`
	// Activity is a state category rather than a count, so a caller cannot
	// assemble an activity profile from repeated calls.
	Activity string `json:"activity"`
	// Unavailable reports that this MCP's model cannot be scoped by the reads
	// behind this tool, so no statement is made about the subject at all.
	Unavailable bool `json:"unavailable"`
}

func (s *DiagnosticsService) GetUserMCPStatus(ctx context.Context, principal Principal, input GetUserMCPStatusInput) (GetUserMCPStatusOutput, error) {
	if s == nil || s.references == nil {
		return GetUserMCPStatusOutput{}, ErrUnavailable
	}
	if input.SubjectReference == "" {
		return GetUserMCPStatusOutput{}, fmt.Errorf("subject_reference is required")
	}
	// Metered on its own allowance: this is the one drill-down that reaches
	// personal data, so exhausting it must not be possible by spending the
	// ordinary diagnostic budget.
	target, err := s.resolveDrilldown(ctx, principal, input.ProjectID, input.MCPID, input.Window, s.sensitiveBudget, drilldownWindowSpec)
	if err != nil {
		return GetUserMCPStatusOutput{}, err
	}
	// Resolved only within the bound organization and connection generation.
	// An unknown, expired, cross-generation, or cross-organization reference is
	// a single not-found: distinguishing them would confirm that a reference
	// once existed, which is itself information about another organization.
	subject, err := s.references.Decode(input.SubjectReference, principal, subjectKindUser, target.now)
	if err != nil {
		return GetUserMCPStatusOutput{}, ErrSubjectReferenceNotFound
	}
	identityKind, identifier, err := parseSubjectIdentity(subject)
	if err != nil {
		return GetUserMCPStatusOutput{}, ErrSubjectReferenceNotFound
	}

	output := GetUserMCPStatusOutput{
		ProjectID:      input.ProjectID,
		MCPID:          input.MCPID,
		MaskedIdentity: maskSubject(identifier),
		Activity:       SubjectStateNoObservations,
	}

	toolsetSlug := target.hostedToolsetSlug()
	// A remote, tunneled, or unproxied server carries no toolset slug, and the
	// read below narrows by nothing else. Answering anyway would report the
	// project's activity as this MCP's.
	if toolsetSlug == "" {
		envelope, err := s.drilldownEnvelope(ctx, target, false)
		if err != nil {
			return GetUserMCPStatusOutput{}, err
		}
		output.Envelope = envelope
		output.Unavailable = true
		return output, nil
	}

	// Scoped to this one subject rather than filtered out of a truncated
	// top-user list: a subject outside that list would otherwise be reported as
	// having no observations, which on a personal-data question is a false
	// negative rather than a missing row.
	summaryParams := telemetryrepo.GetOverviewSummaryParams{
		GramProjectID: target.projectID,
		TimeStart:     target.window.start.UnixNano(),
		TimeEnd:       target.window.end.UnixNano(),
		ToolsetSlug:   toolsetSlug,
	}
	// Filtered on the column the identity actually lives in. Telemetry records
	// a person as an email, an external user id, or a Gram user id, and these
	// are separate columns: filtering the wrong one matches nothing and would
	// report an active person as inactive.
	switch identityKind {
	case SubjectIdentityEmail:
		summaryParams.User = telemetryrepo.UserIdentity{Emails: []string{identifier}}
	case SubjectIdentityExternal:
		summaryParams.ExternalUserID = identifier
	default:
		summaryParams.User = telemetryrepo.UserIdentity{UserIDs: []string{identifier}}
	}
	summary, err := s.telemetry.GetOverviewSummary(ctx, summaryParams)
	if err != nil {
		return GetUserMCPStatusOutput{}, fmt.Errorf("read mcp user activity: %w", err)
	}
	observed := summary != nil && summary.TotalToolCalls > 0
	envelope, err := s.drilldownEnvelope(ctx, target, observed)
	if err != nil {
		return GetUserMCPStatusOutput{}, err
	}
	output.Envelope = envelope
	if observed {
		output.Activity = SubjectStateActive
	} else {
		output.Activity = SubjectStateInactive
	}
	return output, nil
}

// maskSubject reduces an identifier to something an administrator who already
// knows the person can recognize, and someone who does not cannot learn.
//
// The local part and the domain are masked separately for an email so the shape
// stays readable; anything else keeps only its first character.
func maskSubject(subject string) string {
	local, domain, isEmail := strings.Cut(subject, "@")
	if !isEmail || local == "" || domain == "" {
		return maskToken(subject)
	}
	return maskToken(local) + "@" + maskToken(domain)
}

func maskToken(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	if len(runes) == 1 {
		return "*"
	}
	return string(runes[0]) + strings.Repeat("*", min(len(runes)-1, 3))
}

func (s *DiagnosticsService) decodeTraceCursor(cursor string, principal Principal, now time.Time) (int64, string, error) {
	if cursor == "" {
		return 0, "", nil
	}
	value, err := s.references.Decode(cursor, principal, subjectKindTrace, now)
	if err != nil {
		return 0, "", ErrSubjectReferenceNotFound
	}
	position, traceID, err := parseCursorPosition(value)
	if err != nil {
		return 0, "", ErrSubjectReferenceNotFound
	}
	return position, traceID, nil
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
func formatCursorPosition(unixNano int64, traceID string) string {
	return "t:" + strconv.FormatInt(unixNano, 10) + ":" + traceID
}

// parseCursorPosition recovers the composite page key. Both halves are
// required: the repo orders by (event_time_ns, trace_id), and a cursor carrying
// only the timestamp would skip every trace sharing the boundary nanosecond.
func parseCursorPosition(value string) (int64, string, error) {
	rest, ok := strings.CutPrefix(value, "t:")
	if !ok {
		return 0, "", ErrSubjectReferenceNotFound
	}
	timestamp, traceID, ok := strings.Cut(rest, ":")
	if !ok || traceID == "" {
		return 0, "", ErrSubjectReferenceNotFound
	}
	position, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || position <= 0 {
		return 0, "", ErrSubjectReferenceNotFound
	}
	return position, traceID, nil
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
