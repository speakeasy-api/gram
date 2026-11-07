package telemetry

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// maxMCPServerSeriesPoints bounds the series one health read may return. The
// widest window the admin service asks for is 90 days in daily buckets, plus
// the partial bucket at each end.
const maxMCPServerSeriesPoints = 100

// MCPServerTelemetryTarget identifies one MCP server in telemetry. It is built
// from the admin service's own rows, not from Platform MCP's resolver.
type MCPServerTelemetryTarget struct {
	ProjectID string
	// MCPServerID is empty for toolset-only servers.
	MCPServerID string
	// ToolsetSlug is empty when the server has no toolset or several live
	// servers share it, because the slug would then count their calls too.
	ToolsetSlug string
	// URLSlug is the <slug> in /mcp/<slug>, matched against hook-observed calls.
	URLSlug string
}

// MCPServerOutcomes counts one server's tool calls by outcome class.
type MCPServerOutcomes struct {
	Success      int64
	Unauthorized int64
	ClientError  int64
	ServerError  int64
	Blocked      int64
	Failed       int64
	Unknown      int64
	// Watermark is the newest event observed in the project. Zero when the
	// project holds no telemetry.
	Watermark time.Time
}

// MCPServerSeriesPoint is one bucket of a server's tool call series.
type MCPServerSeriesPoint struct {
	BucketStart time.Time
	Total       int64
	Failed      int64
}

// maxMCPServerUsageTools bounds the tools one usage read returns.
const maxMCPServerUsageTools = 25

// maxMCPServerUsageUsers bounds the distinct callers one usage read counts.
const maxMCPServerUsageUsers = 1000

// MCPServerToolUsage is one tool's aggregate calls on a server.
type MCPServerToolUsage struct {
	Name      string
	Calls     int64
	Successes int64
}

// MCPServerUsage aggregates how a server was used, with no caller identity.
type MCPServerUsage struct {
	// DistinctUsers counts callers, capped at maxMCPServerUsageUsers.
	DistinctUsers int64
	// Clients lists the client names that called the server.
	Clients []string
	// Tools lists the most-called tools.
	Tools []MCPServerToolUsage
}

// MCPServerHealth reads one server's tool call telemetry for the admin health
// report. Like SupportCoverage, it is constructed apart from Service so the
// admin binary carries only the ClickHouse repository.
type MCPServerHealth struct {
	chRepo *repo.Queries
}

// NewMCPServerHealth builds a reader over ClickHouse. db is accepted for
// parity with NewSupportCoverage; the reader holds no Postgres state.
func NewMCPServerHealth(_ *pgxpool.Pool, chConn clickhouse.Conn) *MCPServerHealth {
	return &MCPServerHealth{chRepo: repo.New(chConn)}
}

// Outcomes counts the server's calls in [from, to] across both lanes: calls
// that reached Speakeasy directly and calls an agent hook observed.
func (h *MCPServerHealth) Outcomes(ctx context.Context, target MCPServerTelemetryTarget, from, to time.Time) (*MCPServerOutcomes, error) {
	if target.ProjectID == "" {
		return nil, fmt.Errorf("mcp server health: project id is required")
	}

	params := repo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs:       []string{target.ProjectID},
		ToolsetSlugs:         nonEmpty(target.ToolsetSlug),
		MCPServerURLSuffixes: nil,
		MCPServerIDs:         nonEmpty(target.MCPServerID),
		ToolSources:          nil,
		CanonicalIdentityOrg: "",
		TimeStart:            from.UnixNano(),
		TimeEnd:              to.UnixNano(),
		Limit:                0,
	}
	if target.URLSlug != "" {
		params.MCPServerURLSuffixes = []string{"/mcp/" + target.URLSlug}
	}
	// With no identity the breakdown falls back to every server in the
	// project, which would attribute other servers' calls to this one.
	if len(params.ToolsetSlugs) == 0 && len(params.MCPServerIDs) == 0 && len(params.MCPServerURLSuffixes) == 0 {
		return nil, fmt.Errorf("mcp server health: target names no server identity")
	}

	rows, err := h.chRepo.GetMCPOutcomeBreakdown(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("read mcp outcome breakdown: %w", err)
	}

	out := &MCPServerOutcomes{
		Success:      0,
		Unauthorized: 0,
		ClientError:  0,
		ServerError:  0,
		Blocked:      0,
		Failed:       0,
		Unknown:      0,
		Watermark:    time.Time{},
	}
	for _, row := range rows {
		count, err := toCount(row.CallCount)
		if err != nil {
			return nil, err
		}
		var slot *int64
		switch row.Outcome {
		case repo.MCPOutcomeSuccess:
			slot = &out.Success
		case repo.MCPOutcomeUnauthorized:
			slot = &out.Unauthorized
		case repo.MCPOutcomeClientError:
			slot = &out.ClientError
		case repo.MCPOutcomeServerError:
			slot = &out.ServerError
		case repo.MCPOutcomeBlocked:
			slot = &out.Blocked
		case repo.MCPOutcomeFailed:
			slot = &out.Failed
		case repo.MCPOutcomeUnknown:
			slot = &out.Unknown
		default:
			return nil, fmt.Errorf("mcp server health: unrecognized outcome class %q", row.Outcome)
		}
		if *slot > math.MaxInt64-count {
			return nil, fmt.Errorf("mcp server health: outcome count overflows")
		}
		*slot += count
	}

	watermark, err := h.chRepo.GetTelemetryWatermark(ctx, repo.GetTelemetryWatermarkParams{GramProjectIDs: []string{target.ProjectID}})
	if err != nil {
		return nil, fmt.Errorf("read telemetry watermark: %w", err)
	}
	if watermark > 0 {
		out.Watermark = time.Unix(0, watermark).UTC()
	}

	return out, nil
}

// Usage aggregates tools, clients, and distinct callers in [from, to] across
// both lanes. It returns counts and names only, never who the callers are.
func (h *MCPServerHealth) Usage(ctx context.Context, target MCPServerTelemetryTarget, from, to time.Time) (*MCPServerUsage, error) {
	if target.ProjectID == "" {
		return nil, fmt.Errorf("mcp server usage: project id is required")
	}
	params := repo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs:       []string{target.ProjectID},
		ToolsetSlugs:         nonEmpty(target.ToolsetSlug),
		MCPServerURLSuffixes: nil,
		MCPServerIDs:         nonEmpty(target.MCPServerID),
		ToolSources:          nil,
		CanonicalIdentityOrg: "",
		TimeStart:            from.UnixNano(),
		TimeEnd:              to.UnixNano(),
		Limit:                0,
	}
	if target.URLSlug != "" {
		params.MCPServerURLSuffixes = []string{"/mcp/" + target.URLSlug}
	}
	if len(params.ToolsetSlugs) == 0 && len(params.MCPServerIDs) == 0 && len(params.MCPServerURLSuffixes) == 0 {
		return nil, fmt.Errorf("mcp server usage: target names no server identity")
	}

	toolRows, err := h.chRepo.GetMCPToolOutcomeBreakdown(ctx, repo.GetMCPToolOutcomeBreakdownParams{GetMCPOutcomeBreakdownParams: params})
	if err != nil {
		return nil, fmt.Errorf("read mcp server tools: %w", err)
	}
	usage := &MCPServerUsage{DistinctUsers: 0, Clients: []string{}, Tools: aggregateToolUsage(toolRows)}

	params.Limit = maxMCPServerUsageUsers
	users, err := h.chRepo.ListMCPUsageUsers(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("read mcp server users: %w", err)
	}
	usage.DistinctUsers = int64(len(users))

	params.Limit = 0
	breakdown, err := h.chRepo.GetMCPOutcomeBreakdown(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("read mcp server clients: %w", err)
	}
	seen := make(map[string]bool, len(breakdown))
	for _, row := range breakdown {
		if row.Client != "" && row.Client != repo.MCPClientUnattributed && !seen[row.Client] {
			seen[row.Client] = true
			usage.Clients = append(usage.Clients, row.Client)
		}
	}
	slices.Sort(usage.Clients)
	return usage, nil
}

// Series buckets the server's tool calls in [from, to] from the direct lane,
// matching a row by the server id or the toolset slug as the outcome breakdown
// does. The time series filters on one identity at a time, so when the target
// has both the buckets are counted by inclusion-exclusion: calls by id, plus
// calls by slug, minus calls carrying both. The slug read excludes rows
// stamped with another server id, keeping shared-toolset member calls separate.
func (h *MCPServerHealth) Series(ctx context.Context, target MCPServerTelemetryTarget, from, to time.Time, bucket time.Duration) ([]MCPServerSeriesPoint, error) {
	if target.ProjectID == "" {
		return nil, fmt.Errorf("mcp server health: project id is required")
	}
	intervalSeconds := int64(bucket / time.Second)
	if intervalSeconds <= 0 {
		return nil, fmt.Errorf("mcp server health: bucket must be at least one second")
	}

	switch {
	case target.MCPServerID != "" && target.ToolsetSlug != "":
		byID, err := h.seriesBuckets(ctx, target.ProjectID, target.MCPServerID, "", target.MCPServerID, from, to, intervalSeconds)
		if err != nil {
			return nil, err
		}
		bySlug, err := h.seriesBuckets(ctx, target.ProjectID, "", target.ToolsetSlug, target.MCPServerID, from, to, intervalSeconds)
		if err != nil {
			return nil, err
		}
		byBoth, err := h.seriesBuckets(ctx, target.ProjectID, target.MCPServerID, target.ToolsetSlug, target.MCPServerID, from, to, intervalSeconds)
		if err != nil {
			return nil, err
		}
		return mergeSeries(byID, bySlug, byBoth)
	case target.MCPServerID != "":
		return h.seriesBuckets(ctx, target.ProjectID, target.MCPServerID, "", target.MCPServerID, from, to, intervalSeconds)
	case target.ToolsetSlug != "":
		return h.seriesBuckets(ctx, target.ProjectID, "", target.ToolsetSlug, target.MCPServerID, from, to, intervalSeconds)
	default:
		return nil, fmt.Errorf("mcp server health: target names no direct-lane identity")
	}
}

// seriesBuckets reads one filtered time series. Every call over the same
// window and interval returns the same gap-filled bucket starts.
func (h *MCPServerHealth) seriesBuckets(ctx context.Context, projectID, mcpServerID, toolsetSlug, toolsetSlugServerID string, from, to time.Time, intervalSeconds int64) ([]MCPServerSeriesPoint, error) {
	buckets, err := h.chRepo.GetTimeSeriesMetrics(ctx, repo.GetTimeSeriesMetricsParams{
		ExcludedHookSources: nil,
		GramProjectIDs:      []string{projectID},
		TimeStart:           from.UnixNano(),
		TimeEnd:             to.UnixNano(),
		IntervalSeconds:     intervalSeconds,
		User:                repo.UserIdentity{UserIDs: nil, Emails: nil},
		CanonicalUser:       repo.CanonicalUserIdentity{OrgID: "", UserID: "", EmailLower: ""},
		ExternalUserID:      "",
		APIKeyID:            "",
		ToolsetSlug:         toolsetSlug,
		ToolsetSlugServerID: toolsetSlugServerID,
		RemoteMCPServerID:   "",
		MCPServerID:         mcpServerID,
		MetaMCPServerID:     "",
		EventSource:         "",
		HookSource:          "",
		AccountType:         "",
		ExternalOrgID:       "",
	})
	if err != nil {
		return nil, fmt.Errorf("read mcp server time series: %w", err)
	}
	if len(buckets) > maxMCPServerSeriesPoints {
		return nil, fmt.Errorf("mcp server health: series has %d buckets, more than %d", len(buckets), maxMCPServerSeriesPoints)
	}

	points := make([]MCPServerSeriesPoint, 0, len(buckets))
	for _, b := range buckets {
		total, err := toCount(b.TotalToolCalls)
		if err != nil {
			return nil, err
		}
		failed, err := toCount(b.FailedToolCalls)
		if err != nil {
			return nil, err
		}
		if failed > total {
			return nil, fmt.Errorf("mcp server health: bucket has more failed calls than calls")
		}
		points = append(points, MCPServerSeriesPoint{
			BucketStart: time.Unix(0, b.BucketTimeUnixNano).UTC(),
			Total:       total,
			Failed:      failed,
		})
	}
	return points, nil
}

// mergeSeries returns byID + bySlug - byBoth per bucket. The three reads share
// a window and interval, so their buckets must line up; anything else is a
// malformed read rather than something to reconcile.
func mergeSeries(byID, bySlug, byBoth []MCPServerSeriesPoint) ([]MCPServerSeriesPoint, error) {
	if len(bySlug) != len(byID) || len(byBoth) != len(byID) {
		return nil, fmt.Errorf("mcp server health: series reads returned %d, %d and %d buckets", len(byID), len(bySlug), len(byBoth))
	}
	merged := make([]MCPServerSeriesPoint, len(byID))
	for i := range byID {
		if !bySlug[i].BucketStart.Equal(byID[i].BucketStart) || !byBoth[i].BucketStart.Equal(byID[i].BucketStart) {
			return nil, fmt.Errorf("mcp server health: series reads disagree on bucket %d", i)
		}
		total := byID[i].Total + bySlug[i].Total - byBoth[i].Total
		failed := byID[i].Failed + bySlug[i].Failed - byBoth[i].Failed
		if total < 0 || failed < 0 || failed > total {
			return nil, fmt.Errorf("mcp server health: series bucket %d does not reconcile", i)
		}
		merged[i] = MCPServerSeriesPoint{BucketStart: byID[i].BucketStart, Total: total, Failed: failed}
	}
	return merged, nil
}

func nonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

func toCount(value uint64) (int64, error) {
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("mcp server health: count %d overflows", value)
	}
	return int64(value), nil
}

// aggregateToolUsage folds per-outcome rows into the most-called tools.
func aggregateToolUsage(rows []repo.MCPToolOutcomeBreakdownRow) []MCPServerToolUsage {
	byName := make(map[string]*MCPServerToolUsage, len(rows))
	for _, row := range rows {
		if row.ToolName == "" {
			continue
		}
		tool := byName[row.ToolName]
		if tool == nil {
			tool = &MCPServerToolUsage{Name: row.ToolName, Calls: 0, Successes: 0}
			byName[row.ToolName] = tool
		}
		tool.Calls += clampInt64(row.CallCount)
		if row.Outcome == repo.MCPOutcomeSuccess {
			tool.Successes += clampInt64(row.CallCount)
		}
	}
	tools := make([]MCPServerToolUsage, 0, len(byName))
	for _, tool := range byName {
		tools = append(tools, *tool)
	}
	slices.SortFunc(tools, func(a, b MCPServerToolUsage) int {
		if a.Calls != b.Calls {
			return cmp.Compare(b.Calls, a.Calls)
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return tools[:min(len(tools), maxMCPServerUsageTools)]
}

func clampInt64(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}
