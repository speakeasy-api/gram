//nolint:exhaustruct // Diagnostic projections intentionally omit documented optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	DiagnosticsConnectionLimitName   = "platform-mcp-diagnostics-connection"
	DiagnosticsOrganizationLimitName = "platform-mcp-diagnostics-organization"
)

// maxDiagnosticClients bounds the per-client evidence list. Client evidence is
// self-reported and exists to point at a suspect, not to enumerate a fleet.
const maxDiagnosticClients = 10

// maxOverviewServers bounds the top-server list on a project overview.
const maxOverviewServers = 10

// maxOverviewProjects bounds the organization-wide scope comparison. When an
// organization has more projects than this, the comparison covers a subset and
// the diagnosis says so rather than asserting a scope from partial coverage.
const maxOverviewProjects = 100

var ErrDiagnosticsTargetNotFound = errors.New("platform mcp diagnostics target not found")

// FeatureChecker answers whether an organization has a product feature enabled.
// It mirrors the telemetry service's checker so both surfaces resolve the
// organization's metrics mode from the same source.
type FeatureChecker func(ctx context.Context, organizationID string) (bool, error)

// ProjectOverviewSessionReader is the PostgreSQL side of a session-mode
// overview. In session mode the active-user count is a count of chat
// participants, which ClickHouse's tool-call lane cannot answer.
type ProjectOverviewSessionReader interface {
	GetActiveUserCountByMessages(ctx context.Context, arg chatrepo.GetActiveUserCountByMessagesParams) (int64, error)
}

// DiagnosticsTelemetryReader is the Gram-owned telemetry this surface reads.
// It is deliberately two bounded aggregate queries: there is no query grammar
// here and no way to reach a raw row.
type DiagnosticsTelemetryReader interface {
	GetMCPOutcomeBreakdown(ctx context.Context, arg telemetryrepo.GetMCPOutcomeBreakdownParams) ([]telemetryrepo.MCPOutcomeBreakdownRow, error)
	GetTelemetryWatermark(ctx context.Context, arg telemetryrepo.GetTelemetryWatermarkParams) (int64, error)
	GetOverviewSummary(ctx context.Context, arg telemetryrepo.GetOverviewSummaryParams) (*telemetryrepo.OverviewSummary, error)
	GetActiveCounts(ctx context.Context, arg telemetryrepo.GetActiveCountsParams) (*telemetryrepo.ActiveCounts, error)
	GetTopServers(ctx context.Context, arg telemetryrepo.GetTopServersParams) ([]telemetryrepo.TopServer, error)
}

// DiagnosticsService answers the two overview-first questions: what is this
// project doing, and why is this one MCP not working.
type DiagnosticsService struct {
	db             *pgxpool.Pool
	telemetry      DiagnosticsTelemetryReader
	sessions       ProjectOverviewSessionReader
	sessionCapture FeatureChecker
	reader         Reader
	readiness      *ReadinessService
	budget         OperationBudget
	now            func() time.Time
}

func NewDiagnosticsService(db *pgxpool.Pool, telemetry DiagnosticsTelemetryReader, sessionCapture FeatureChecker, reader Reader, readiness *ReadinessService, budget OperationBudget) *DiagnosticsService {
	var sessions ProjectOverviewSessionReader
	if db != nil {
		sessions = chatrepo.New(db)
	}
	return &DiagnosticsService{
		db:             db,
		telemetry:      telemetry,
		sessions:       sessions,
		sessionCapture: sessionCapture,
		reader:         reader,
		readiness:      readiness,
		budget:         budget,
		now:            time.Now,
	}
}

func (s *DiagnosticsService) valid() bool {
	return s != nil && s.db != nil && s.telemetry != nil && s.sessions != nil && s.sessionCapture != nil && s.reader != nil && s.budget.valid()
}

// GetProjectOverviewInput asks for one project's activity. It carries no
// filters: the overview is the entry point, and narrowing happens through the
// drill-down tools once it has identified something to look at.
type GetProjectOverviewInput struct {
	ProjectID string `json:"project_id" jsonschema:"AICP project ID to summarize"`
	Window    string `json:"window,omitempty" jsonschema:"observation window: 1h, 24h (default), 7d, or 30d"`
}

// ProjectOverviewServer is one MCP server's share of the project's tool calls.
type ProjectOverviewServer struct {
	Name      string `json:"name"`
	ToolCalls int64  `json:"tool_calls"`
}

// GetProjectOverviewOutput is the Platform subset of the project overview: the
// activity and failure shape of a project, aggregated server-side.
//
// It carries no users, no clients, no tokens, and no cost. Those are either
// personal data this surface does not project or a billing concern that does
// not belong in a diagnostic.
type GetProjectOverviewOutput struct {
	ProjectID string       `json:"project_id"`
	Envelope  DataEnvelope `json:"data"`
	// MetricsMode is the organization's metrics mode, resolved server-side. It
	// is reported because it changes what ActiveUsers counts: chat participants
	// under "session", tool-call actors under "tool_call".
	MetricsMode     string                  `json:"metrics_mode"`
	ToolCalls       int64                   `json:"tool_calls"`
	FailedToolCalls int64                   `json:"failed_tool_calls"`
	ActiveServers   int64                   `json:"active_servers"`
	ActiveUsers     SubjectCount            `json:"active_users"`
	TopServers      []ProjectOverviewServer `json:"top_servers"`
}

func (s *DiagnosticsService) GetProjectOverview(ctx context.Context, principal Principal, input GetProjectOverviewInput) (GetProjectOverviewOutput, error) {
	if !s.valid() {
		return GetProjectOverviewOutput{}, ErrUnavailable
	}
	if input.ProjectID == "" {
		return GetProjectOverviewOutput{}, fmt.Errorf("project_id is required")
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return GetProjectOverviewOutput{}, err
	}
	now := s.now()
	window, err := resolveWindow(input.Window, now)
	if err != nil {
		return GetProjectOverviewOutput{}, err
	}
	projectUUID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		return GetProjectOverviewOutput{}, fmt.Errorf("parse project id: %w", err)
	}
	// Reading the MCP inventory for the project is what establishes that this
	// principal may see the project at all; the telemetry queries below are
	// project-scoped and must not run before it.
	if _, err := s.reader.FindMCP(ctx, principal, FindMCPInput{ProjectID: input.ProjectID, Limit: 1}); err != nil {
		return GetProjectOverviewOutput{}, fmt.Errorf("resolve project overview project: %w", err)
	}

	sessionMode, err := s.sessionCapture(ctx, principal.OrganizationID)
	if err != nil {
		return GetProjectOverviewOutput{}, fmt.Errorf("resolve organization metrics mode: %w", err)
	}

	projectIDs := []string{input.ProjectID}
	start, end := window.start.UnixNano(), window.end.UnixNano()

	summary, err := s.telemetry.GetOverviewSummary(ctx, telemetryrepo.GetOverviewSummaryParams{
		GramProjectID: input.ProjectID,
		TimeStart:     start,
		TimeEnd:       end,
	})
	if err != nil {
		return GetProjectOverviewOutput{}, fmt.Errorf("read project overview summary: %w", err)
	}
	counts, err := s.telemetry.GetActiveCounts(ctx, telemetryrepo.GetActiveCountsParams{
		GramProjectID: input.ProjectID,
		TimeStart:     start,
		TimeEnd:       end,
		// SessionMode is deliberately left false. It switches only the
		// active-user expression, and under session capture that value is
		// replaced below by the PostgreSQL chat-participant count; the
		// active-server count it does not affect at all.
		SessionMode: false,
	})
	if err != nil {
		return GetProjectOverviewOutput{}, fmt.Errorf("read project overview active counts: %w", err)
	}
	servers, err := s.telemetry.GetTopServers(ctx, telemetryrepo.GetTopServersParams{
		GramProjectID: input.ProjectID,
		TimeStart:     start,
		TimeEnd:       end,
		Limit:         maxOverviewServers,
	})
	if err != nil {
		return GetProjectOverviewOutput{}, fmt.Errorf("read project overview top servers: %w", err)
	}
	watermark, err := s.telemetry.GetTelemetryWatermark(ctx, telemetryrepo.GetTelemetryWatermarkParams{GramProjectIDs: projectIDs})
	if err != nil {
		return GetProjectOverviewOutput{}, fmt.Errorf("read project overview watermark: %w", err)
	}

	output := GetProjectOverviewOutput{
		ProjectID:   input.ProjectID,
		Envelope:    newDataEnvelope(now, watermarkTime(watermark), window),
		MetricsMode: metricsMode(sessionMode),
		TopServers:  make([]ProjectOverviewServer, 0, len(servers)),
	}
	if summary != nil {
		output.ToolCalls = boundedCount(summary.TotalToolCalls)
		output.FailedToolCalls = boundedCount(summary.FailedToolCalls)
	}
	if counts != nil {
		output.ActiveServers = boundedCount(counts.ActiveServersCount)
		output.ActiveUsers = NewSubjectCount(boundedCount(counts.ActiveUsersCount))
	}
	if sessionMode {
		// Under session capture the active-user count is a count of chat
		// participants held in PostgreSQL. Reporting ClickHouse's tool-call
		// actors here would answer a different question than metrics_mode says
		// this number answers.
		activeUsers, err := s.sessions.GetActiveUserCountByMessages(ctx, chatrepo.GetActiveUserCountByMessagesParams{
			ProjectID: projectUUID,
			TimeStart: conv.ToPGTimestamptz(window.start),
			TimeEnd:   conv.ToPGTimestamptz(window.end),
		})
		if err != nil {
			return GetProjectOverviewOutput{}, fmt.Errorf("read project overview active users: %w", err)
		}
		output.ActiveUsers = NewSubjectCount(activeUsers)
	}
	for _, server := range servers {
		output.TopServers = append(output.TopServers, ProjectOverviewServer{
			Name:      server.ServerName,
			ToolCalls: boundedCount(server.ToolCallCount),
		})
	}
	return output, nil
}

// GetMCPDiagnosticsInput names one configured MCP, using the same identity
// find_mcp and get_mcp return.
type GetMCPDiagnosticsInput struct {
	ProjectID string `json:"project_id" jsonschema:"AICP project ID that owns the MCP"`
	MCPID     string `json:"mcp_id" jsonschema:"configured MCP ID as returned by find_mcp or get_mcp"`
	Window    string `json:"window,omitempty" jsonschema:"observation window: 1h, 24h (default), 7d, or 30d"`
}

// MCPOutcomeSummary is a server's calls in the window, already summed per
// outcome class. There is no per-bucket series: an external MCP client has no
// scratch compute to add one up with.
type MCPOutcomeSummary struct {
	Total        int64 `json:"total"`
	Success      int64 `json:"success"`
	Unauthorized int64 `json:"unauthorized"`
	ClientError  int64 `json:"client_error"`
	ServerError  int64 `json:"server_error"`
	Failed       int64 `json:"failed"`
	Unknown      int64 `json:"unknown"`
}

// MCPClientEvidence is what one client reported about its own calls. The name
// is self-reported by that client and is evidence, not a dimension: nothing in
// this surface lets a caller filter or group by it.
type MCPClientEvidence struct {
	Client   string `json:"client"`
	Calls    int64  `json:"calls"`
	Failures int64  `json:"failures"`
}

// MCPDiagnosticsReadiness is the latest server-side readiness result, with the
// freshness that decides whether it can exonerate anything.
type MCPDiagnosticsReadiness struct {
	State     string `json:"state"`
	Freshness string `json:"freshness"`
	CheckedAt string `json:"checked_at,omitempty"`
}

type GetMCPDiagnosticsOutput struct {
	ProjectID string       `json:"project_id"`
	MCPID     string       `json:"mcp_id"`
	Envelope  DataEnvelope `json:"data"`

	Readiness MCPDiagnosticsReadiness `json:"readiness"`
	Outcomes  MCPOutcomeSummary       `json:"outcomes"`
	// OrganizationOutcomes is the same summary across the organization's
	// projects. It is what makes the scope check answerable server-side.
	OrganizationOutcomes MCPOutcomeSummary `json:"organization_outcomes"`
	// OrganizationOutcomesPartial reports that the comparison covered only the
	// first maxOverviewProjects projects. When it is true the attribution's
	// scope is forced to unknown rather than asserted from partial coverage.
	OrganizationOutcomesPartial bool                `json:"organization_outcomes_partial"`
	Clients                     []MCPClientEvidence `json:"clients"`
	ClientsTruncated            bool                `json:"clients_truncated"`
	Attribution                 FaultAttribution    `json:"attribution"`
}

func (s *DiagnosticsService) GetMCPDiagnostics(ctx context.Context, principal Principal, input GetMCPDiagnosticsInput) (GetMCPDiagnosticsOutput, error) {
	if !s.valid() {
		return GetMCPDiagnosticsOutput{}, ErrUnavailable
	}
	if input.ProjectID == "" || input.MCPID == "" {
		return GetMCPDiagnosticsOutput{}, fmt.Errorf("project_id and mcp_id are required")
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return GetMCPDiagnosticsOutput{}, err
	}
	now := s.now()
	window, err := resolveWindow(input.Window, now)
	if err != nil {
		return GetMCPDiagnosticsOutput{}, err
	}

	// GetMCP is the authorization boundary: it fails closed for an MCP this
	// principal cannot see, and everything below reads only what it returned.
	mcp, err := s.reader.GetMCP(ctx, principal, GetMCPInput{ProjectID: input.ProjectID, MCPID: input.MCPID})
	if err != nil {
		return GetMCPDiagnosticsOutput{}, fmt.Errorf("resolve diagnostics mcp: %w", err)
	}
	target, err := s.diagnosticsTarget(ctx, principal.OrganizationID, input.ProjectID, input.MCPID)
	if err != nil {
		return GetMCPDiagnosticsOutput{}, err
	}

	projectIDs, projectsTruncated, err := s.organizationProjectIDs(ctx, principal)
	if err != nil {
		return GetMCPDiagnosticsOutput{}, err
	}
	start, end := window.start.UnixNano(), window.end.UnixNano()

	serverRows, err := s.telemetry.GetMCPOutcomeBreakdown(ctx, telemetryrepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs:       []string{input.ProjectID},
		ToolsetSlugs:         nonEmpty(target.ToolsetSlug),
		MCPServerURLSuffixes: mcpURLSuffixes(target.McpSlug),
		TimeStart:            start,
		TimeEnd:              end,
	})
	if err != nil {
		return GetMCPDiagnosticsOutput{}, fmt.Errorf("read mcp outcome breakdown: %w", err)
	}
	organizationRows, err := s.telemetry.GetMCPOutcomeBreakdown(ctx, telemetryrepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs: projectIDs,
		TimeStart:      start,
		TimeEnd:        end,
	})
	if err != nil {
		return GetMCPDiagnosticsOutput{}, fmt.Errorf("read organization outcome breakdown: %w", err)
	}
	// Scoped to the diagnosed project, not to the organization the comparison
	// spans. An organization-wide watermark reports the freshest observation
	// anywhere, so a busy sibling project would stamp this result fresh while
	// the diagnosed project's own observations had stopped arriving — exactly
	// the reading that turns an absence of evidence into evidence of health.
	watermark, err := s.telemetry.GetTelemetryWatermark(ctx, telemetryrepo.GetTelemetryWatermarkParams{GramProjectIDs: []string{input.ProjectID}})
	if err != nil {
		return GetMCPDiagnosticsOutput{}, fmt.Errorf("read diagnostics watermark: %w", err)
	}

	serverTotals := totalsFromRows(serverRows)
	organizationTotals := totalsFromRows(organizationRows)
	readiness, readinessFound := s.currentReadiness(ctx, principal, mcp)
	clients, truncated := clientEvidence(serverRows)

	attribution := attributeFault(readiness, readinessFound, serverTotals, organizationTotals)
	if projectsTruncated {
		// The organization comparison covered a subset, so it cannot support a
		// claim either way about where the pattern lives.
		attribution.Scope = FaultScopeUnknown
	}

	return GetMCPDiagnosticsOutput{
		ProjectID: input.ProjectID,
		MCPID:     input.MCPID,
		Envelope:  newDataEnvelope(now, watermarkTime(watermark), window),
		Readiness: MCPDiagnosticsReadiness{
			State:     string(normalizedReadiness(readiness, readinessFound).State),
			Freshness: readinessFreshness(readiness, readinessFound),
			CheckedAt: readinessTimestamp(readiness.CheckedAt),
		},
		Outcomes:                    summaryFromTotals(serverTotals),
		OrganizationOutcomes:        summaryFromTotals(organizationTotals),
		OrganizationOutcomesPartial: projectsTruncated,
		Clients:                     clients,
		ClientsTruncated:            truncated,
		Attribution:                 attribution,
	}, nil
}

func (s *DiagnosticsService) diagnosticsTarget(ctx context.Context, organizationID, projectID, mcpID string) (platformrepo.GetPlatformMCPDiagnosticsTargetRow, error) {
	parsedProject, err := uuid.Parse(projectID)
	if err != nil {
		return platformrepo.GetPlatformMCPDiagnosticsTargetRow{}, fmt.Errorf("parse project id: %w", err)
	}
	parsedMCP, err := uuid.Parse(mcpID)
	if err != nil {
		return platformrepo.GetPlatformMCPDiagnosticsTargetRow{}, fmt.Errorf("parse mcp id: %w", err)
	}
	row, err := platformrepo.New(s.db).GetPlatformMCPDiagnosticsTarget(ctx, platformrepo.GetPlatformMCPDiagnosticsTargetParams{
		OrganizationID: organizationID,
		McpServerID:    parsedMCP,
		ProjectID:      parsedProject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.GetPlatformMCPDiagnosticsTargetRow{}, ErrDiagnosticsTargetNotFound
	}
	if err != nil {
		return platformrepo.GetPlatformMCPDiagnosticsTargetRow{}, fmt.Errorf("resolve diagnostics target: %w", err)
	}
	return row, nil
}

// organizationProjectIDs collects the projects the scope comparison spans, and
// reports whether that listing was cut short. It reuses the same listing the
// caller can already read, so the comparison never covers a project the
// principal cannot see.
//
// The truncation flag is returned rather than swallowed: a comparison over an
// unstated subset of an organization reads as a comparison over all of it, and
// would let a partial view assert a scope it cannot support.
func (s *DiagnosticsService) organizationProjectIDs(ctx context.Context, principal Principal) ([]string, bool, error) {
	projects, err := s.reader.ListProjects(ctx, principal, ListProjectsInput{Limit: maxOverviewProjects})
	if err != nil {
		return nil, false, fmt.Errorf("list organization projects: %w", err)
	}
	ids := make([]string, 0, len(projects.Projects))
	for _, project := range projects.Projects {
		ids = append(ids, project.ID)
	}
	return ids, projects.Truncated, nil
}

// currentReadiness loads the persisted readiness result without probing the
// provider or spending the repair budget. A missing registration or an
// unavailable readiness service is reported as "not found", which
// attributeFault treats as no evidence rather than as a healthy result.
func (s *DiagnosticsService) currentReadiness(ctx context.Context, principal Principal, mcp MCP) (Readiness, bool) {
	if s.readiness == nil || mcp.Registration == nil || mcp.Registration.ID == "" || mcp.ProjectSlug == "" {
		return Readiness{}, false
	}
	_, readiness, found, err := s.readiness.CurrentReadiness(ctx, principal, mcp.ProjectSlug, mcp.Registration.ID)
	if err != nil {
		return Readiness{}, false
	}
	return readiness, found
}

func totalsFromRows(rows []telemetryrepo.MCPOutcomeBreakdownRow) outcomeTotals {
	var totals outcomeTotals
	for _, row := range rows {
		count := boundedCount(row.CallCount)
		totals.Total += count
		switch row.Outcome {
		case telemetryrepo.MCPOutcomeSuccess:
			totals.Success += count
		case telemetryrepo.MCPOutcomeUnauthorized:
			totals.Unauthorized += count
		case telemetryrepo.MCPOutcomeClientError:
			totals.ClientError += count
		case telemetryrepo.MCPOutcomeServerError:
			totals.ServerError += count
		case telemetryrepo.MCPOutcomeFailed:
			totals.Failed += count
		default:
			totals.Unknown += count
		}
	}
	return totals
}

// summaryFromTotals converts the internal tally into the serialized summary.
// The two types are field-identical on purpose: outcomeTotals is what fault
// attribution reasons over, MCPOutcomeSummary is what a caller receives, and
// keeping them separate stops a JSON tag change from silently altering either.
func summaryFromTotals(totals outcomeTotals) MCPOutcomeSummary {
	return MCPOutcomeSummary(totals)
}

// clientEvidence folds the breakdown to one row per client, ordered by failures
// then calls so the suspect leads. The list is capped and says when it was cut,
// because a silently truncated list reads as complete coverage.
func clientEvidence(rows []telemetryrepo.MCPOutcomeBreakdownRow) ([]MCPClientEvidence, bool) {
	byClient := map[string]*MCPClientEvidence{}
	for _, row := range rows {
		evidence, ok := byClient[row.Client]
		if !ok {
			evidence = &MCPClientEvidence{Client: row.Client}
			byClient[row.Client] = evidence
		}
		count := boundedCount(row.CallCount)
		evidence.Calls += count
		if row.Outcome != telemetryrepo.MCPOutcomeSuccess && row.Outcome != telemetryrepo.MCPOutcomeUnknown {
			evidence.Failures += count
		}
	}
	clients := make([]MCPClientEvidence, 0, len(byClient))
	for _, evidence := range byClient {
		clients = append(clients, *evidence)
	}
	sort.Slice(clients, func(i, j int) bool {
		if clients[i].Failures != clients[j].Failures {
			return clients[i].Failures > clients[j].Failures
		}
		if clients[i].Calls != clients[j].Calls {
			return clients[i].Calls > clients[j].Calls
		}
		return clients[i].Client < clients[j].Client
	})
	if len(clients) > maxDiagnosticClients {
		return clients[:maxDiagnosticClients], true
	}
	return clients, false
}

// metricsMode names what the overview's counts measure.
func metricsMode(sessionMode bool) string {
	if sessionMode {
		return "session"
	}
	return "tool_call"
}

func nonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

// mcpURLSuffixes is how a hosted MCP appears in the URL a hook-observed client
// called.
func mcpURLSuffixes(mcpSlug string) []string {
	if mcpSlug == "" {
		return nil
	}
	return []string{"/mcp/" + mcpSlug}
}

// boundedCount narrows a ClickHouse unsigned count. Counts of observed events
// never approach the int64 ceiling, and a negative result would be a worse
// answer than a saturated one.
func boundedCount(value uint64) int64 {
	const maxInt64 = uint64(1)<<63 - 1
	if value > maxInt64 {
		return int64(maxInt64)
	}
	return int64(value)
}
