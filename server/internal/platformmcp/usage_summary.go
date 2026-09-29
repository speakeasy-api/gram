//nolint:exhaustruct // Diagnostic projections intentionally omit documented optional fields.
package platformmcp

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/servernames"
	telemetrysvc "github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// usageSummaryTargetRowLimit bounds how many per-target rows the usage summary
// aggregates before folding them onto configured servers and target types. One
// configured server can be classified under several target ids, so more rows
// are aggregated than are ever returned; the fold is what the per-type caps
// apply to.
//
// The read asks for one row beyond this so an omission can be told from a
// result that merely fills the cap exactly; that sentinel is dropped before
// aggregation, so no bucket ever counts it.
const usageSummaryTargetRowLimit = 500

// maxUsageSummaryTargets bounds the per-target list inside each target type.
const maxUsageSummaryTargets = 5

// usageTargetTypes is the closed set of target types the usage summary
// reports, in the order it reports them. Every type is emitted on every
// result, zero included, so a caller comparing two of them never has to
// infer that an absent bucket means nothing was observed.
var usageTargetTypes = []string{
	telemetryrepo.ToolUsageTargetTypeHostedMCP,
	telemetryrepo.ToolUsageTargetTypeTunneledMCP,
	telemetryrepo.ToolUsageTargetTypeMetaMCP,
	telemetryrepo.ToolUsageTargetTypeShadowMCP,
	telemetryrepo.ToolUsageTargetTypeLocalTool,
	telemetryrepo.ToolUsageTargetTypeSkill,
}

// ToolUsageBreakdownReader is the target-aware tool usage read the summary
// answers from. It is the same pipeline the dashboard Insights board and the
// managed platform_get_tool_usage_summary tool read, so every surface reports
// one number for one window. It stays separate from DiagnosticsTelemetryReader
// so a deployment can withhold it independently.
type ToolUsageBreakdownReader interface {
	GetToolUsageTotals(ctx context.Context, arg telemetryrepo.GetToolUsageSummaryParams) (telemetryrepo.ToolUsageTotalsRow, error)
	GetToolUsageTargets(ctx context.Context, arg telemetryrepo.GetToolUsageSummaryParams) ([]telemetryrepo.ToolUsageTargetSummaryRow, error)
}

// WithToolUsageBreakdown attaches the per-target-type usage summary. A nil
// reader keeps get_tool_usage_summary registered as its unavailable stub.
func (s *DiagnosticsService) WithToolUsageBreakdown(reader ToolUsageBreakdownReader) *DiagnosticsService {
	if s != nil {
		s.toolUsage = reader
	}
	return s
}

func (s *DiagnosticsService) toolUsageValid() bool {
	return s.valid() && s.toolUsage != nil
}

// GetToolUsageSummaryInput asks for one project's usage mix. It carries no
// target filter: the summary answers "how much of our usage is X" for every
// target type at once, and narrowing to one server is what the diagnostics
// and drill-down tools are for.
type GetToolUsageSummaryInput struct {
	ProjectID string `json:"project_id" jsonschema:"project ID to summarize"`
	Window    string `json:"window,omitempty" jsonschema:"observation window: 1h, 24h, 7d (default), or 30d"`
}

// ToolUsageTarget is one server, gateway, or skill's share of a target type's
// tool calls.
type ToolUsageTarget struct {
	// Name is the configured MCP server's name when the target resolved to
	// one, and otherwise the configured label the usage pipeline classified
	// the calls under: a gateway's name, a skill's name, or "Local Tools".
	Name string `json:"name"`

	// MCPID is the configured MCP server the calls were attributed to, usable
	// with get_mcp and the diagnostics tools. Empty when the target is not a
	// configured MCP server.
	MCPID string `json:"mcp_id,omitempty"`

	ToolCalls       int64 `json:"tool_calls"`
	FailedToolCalls int64 `json:"failed_tool_calls"`
}

// ToolUsageByTargetType is every tool call in the window that reached one
// kind of target.
type ToolUsageByTargetType struct {
	TargetType      string `json:"target_type"`
	ToolCalls       int64  `json:"tool_calls"`
	FailedToolCalls int64  `json:"failed_tool_calls"`
	// SharePercent is this type's share of tool_calls on the result, rounded
	// server-side to one decimal place. Shares sum to less than 100 when
	// targets_truncated is set.
	SharePercent float64 `json:"share_percent"`
	// Targets counts the distinct servers, gateways, or skills the calls
	// reached after folding onto configured servers.
	Targets int `json:"targets"`
	// TopTargets lists the busiest targets of this type. Shadow MCP servers
	// are known only by the name the calling app used for them, so the shadow
	// bucket reports its count and leaves this empty; list_shadow_mcp_inventory
	// is the reviewed view of which servers those are.
	TopTargets []ToolUsageTarget `json:"top_targets"`
}

// GetToolUsageSummaryOutput is the Platform subset of the tool usage summary:
// how a project's tool calls divide between hosted MCP, tunneled MCP,
// gateways, shadow MCP, local tools, and skills. It carries no users, no
// clients, and no cost.
type GetToolUsageSummaryOutput struct {
	ProjectID        string       `json:"project_id"`
	Envelope         DataEnvelope `json:"data"`
	ToolCalls        int64        `json:"tool_calls"`
	FailedToolCalls  int64        `json:"failed_tool_calls"`
	BlockedToolCalls int64        `json:"blocked_tool_calls"`
	UniqueTools      int64        `json:"unique_tools"`
	// UsageByTarget holds one entry per target type, always in the same order
	// and always complete.
	UsageByTarget []ToolUsageByTargetType `json:"usage_by_target"`
	// TargetsTruncated reports that the window held more distinct targets than
	// the per-target read returns, so the buckets sum to less than tool_calls.
	TargetsTruncated bool `json:"targets_truncated"`
}

func (s *DiagnosticsService) GetToolUsageSummary(ctx context.Context, principal Principal, input GetToolUsageSummaryInput) (GetToolUsageSummaryOutput, error) {
	if !s.toolUsageValid() {
		return GetToolUsageSummaryOutput{}, ErrUnavailable
	}
	if input.ProjectID == "" {
		return GetToolUsageSummaryOutput{}, fmt.Errorf("project_id is required")
	}
	now := s.now()
	window, err := resolveWindow(input.Window, now, usageSummaryWindowSpec)
	if err != nil {
		return GetToolUsageSummaryOutput{}, err
	}
	projectUUID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		return GetToolUsageSummaryOutput{}, fmt.Errorf("parse project id: %w", err)
	}
	if _, err := s.reader.ResolveProjectRead(ctx, principal, FindMCPInput{ProjectID: input.ProjectID}); err != nil {
		return GetToolUsageSummaryOutput{}, fmt.Errorf("authorize tool usage summary: %w", err)
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return GetToolUsageSummaryOutput{}, err
	}

	// The same matchers the dashboard and the managed tool classify with, so
	// a call lands in the same target type on every surface.
	hostedMatchers, serverMatchers, err := telemetrysvc.LoadToolUsageMatchers(ctx, s.db, projectUUID)
	if err != nil {
		return GetToolUsageSummaryOutput{}, fmt.Errorf("load tool usage matchers: %w", err)
	}
	metaMatchers, err := telemetrysvc.LoadMetaMCPMatchers(ctx, s.db, projectUUID)
	if err != nil {
		return GetToolUsageSummaryOutput{}, fmt.Errorf("load tool usage gateway matchers: %w", err)
	}
	params := toolUsageSummaryParams(input.ProjectID, window, hostedMatchers, serverMatchers, metaMatchers)

	totals, err := s.toolUsage.GetToolUsageTotals(ctx, params)
	if err != nil {
		return GetToolUsageSummaryOutput{}, fmt.Errorf("read tool usage totals: %w", err)
	}
	rows, err := s.toolUsage.GetToolUsageTargets(ctx, params)
	if err != nil {
		return GetToolUsageSummaryOutput{}, fmt.Errorf("read tool usage targets: %w", err)
	}
	// The pipeline classifies a hook-observed call it could not tie to a URL
	// or a source id as shadow MCP under the name the agent used. Folding
	// those names onto configured servers is what keeps a hosted server that
	// Claude Code reached through a plugin prefix out of the shadow bucket.
	resolver, err := s.serverNameResolver(ctx, principal.OrganizationID, input.ProjectID)
	if err != nil {
		return GetToolUsageSummaryOutput{}, fmt.Errorf("resolve tool usage server names: %w", err)
	}
	watermark, err := s.telemetry.GetTelemetryWatermark(ctx, telemetryrepo.GetTelemetryWatermarkParams{GramProjectIDs: []string{input.ProjectID}})
	if err != nil {
		return GetToolUsageSummaryOutput{}, fmt.Errorf("read tool usage watermark: %w", err)
	}

	// The read asked for one row past the cap. Its presence is what proves a
	// target was left out, and it is dropped here rather than aggregated: a
	// sentinel counted into a bucket would report calls the result then claims
	// are missing.
	rows, truncated := boundedRows(rows, usageSummaryTargetRowLimit)

	toolCalls := boundedCount(totals.EventCount)
	buckets := attributeUsageByTarget(rows, resolver, configuredServerTargetTypes(resolver, serverMatchers), toolCalls, maxUsageSummaryTargets)
	return GetToolUsageSummaryOutput{
		ProjectID:        input.ProjectID,
		Envelope:         newDataEnvelope(now, watermarkTime(watermark), window, usageObserved(totals, buckets)),
		ToolCalls:        toolCalls,
		FailedToolCalls:  boundedCount(totals.FailureCount),
		BlockedToolCalls: boundedCount(totals.BlockedCount),
		UniqueTools:      boundedCount(totals.UniqueTools),
		UsageByTarget:    buckets,
		TargetsTruncated: truncated,
	}, nil
}

// toolUsageSummaryParams narrows the target-aware read to one project and
// window with no filter, so the totals and the per-target rows describe the
// same population of calls.
func toolUsageSummaryParams(projectID string, window ResolvedWindow, hosted []telemetryrepo.HostedMCPMatcher, servers []telemetryrepo.MCPServerMatcher, meta []telemetryrepo.MetaMCPMatcher) telemetryrepo.GetToolUsageSummaryParams {
	return telemetryrepo.GetToolUsageSummaryParams{
		GramProjectID:     projectID,
		TimeStart:         window.start.UnixNano(),
		TimeEnd:           window.end.UnixNano(),
		HostedMCPMatchers: hosted,
		MCPServerMatchers: servers,
		MetaMCPMatchers:   meta,
		// One past the aggregation cap: the extra row is the sentinel that
		// tells an omission from a result that exactly fills the cap.
		TargetLimit: usageSummaryTargetRowLimit + 1,
	}
}

// configuredServerTargetTypes maps each configured server the usage pipeline
// knows a source id for to the target type it classifies that server's calls
// under, so a call folded onto the server from the shadow bucket lands in the
// same type its directly attributed calls did. A configured server with no
// matcher is hosted: that is the type the pipeline gives every server it
// cannot tell apart from a hosted one.
func configuredServerTargetTypes(resolver *servernames.Resolver, servers []telemetryrepo.MCPServerMatcher) map[string]string {
	types := map[string]string{}
	for _, matcher := range servers {
		id, ok := resolver.Resolve(matcher.TargetID)
		if !ok {
			continue
		}
		if _, seen := types[id]; !seen {
			types[id] = matcher.TargetType
		}
	}
	return types
}

// usageTargetKey identifies one folded target within a target type.
type usageTargetKey struct {
	targetType string
	key        string
}

// attributeUsageByTarget folds the per-target rows onto configured servers and
// sums them into one bucket per target type. A hosted or tunneled row whose
// target id names a configured server carries that server's id; a shadow row
// whose reported name resolves to a configured server moves into that
// server's type, because it is that server's traffic reported under a name
// the pipeline did not recognise; every other row keeps the type and label the
// pipeline gave it. Every target type is present in the result, in
// usageTargetTypes order, with its share of total.
func attributeUsageByTarget(rows []telemetryrepo.ToolUsageTargetSummaryRow, resolver *servernames.Resolver, serverTypes map[string]string, total int64, limit int) []ToolUsageByTargetType {
	folded := map[usageTargetKey]*ToolUsageTarget{}
	order := []usageTargetKey{}
	for _, row := range rows {
		targetType := row.TargetType
		key := usageTargetKey{targetType: targetType, key: "target:" + row.TargetID}
		target := ToolUsageTarget{Name: row.TargetLabel}
		switch targetType {
		case telemetryrepo.ToolUsageTargetTypeHostedMCP, telemetryrepo.ToolUsageTargetTypeTunneledMCP, telemetryrepo.ToolUsageTargetTypeShadowMCP:
			id, ok := resolver.Resolve(row.TargetID)
			if !ok {
				break
			}
			if targetType == telemetryrepo.ToolUsageTargetTypeShadowMCP {
				targetType = telemetryrepo.ToolUsageTargetTypeHostedMCP
				if configured, known := serverTypes[id]; known {
					targetType = configured
				}
			}
			key = usageTargetKey{targetType: targetType, key: "mcp:" + id}
			target = ToolUsageTarget{Name: resolver.Name(id), MCPID: id}
		}
		if !knownUsageTargetType(targetType) {
			continue
		}
		entry, seen := folded[key]
		if !seen {
			entry = &target
			folded[key] = entry
			order = append(order, key)
		}
		entry.ToolCalls += boundedCount(row.EventCount)
		entry.FailedToolCalls += boundedCount(row.FailureCount)
	}

	byType := map[string]*ToolUsageByTargetType{}
	buckets := make([]ToolUsageByTargetType, 0, len(usageTargetTypes))
	for _, targetType := range usageTargetTypes {
		buckets = append(buckets, ToolUsageByTargetType{TargetType: targetType, TopTargets: []ToolUsageTarget{}})
		byType[targetType] = &buckets[len(buckets)-1]
	}
	for _, key := range order {
		bucket := byType[key.targetType]
		entry := folded[key]
		bucket.ToolCalls += entry.ToolCalls
		bucket.FailedToolCalls += entry.FailedToolCalls
		bucket.Targets++
		if key.targetType == telemetryrepo.ToolUsageTargetTypeShadowMCP {
			// A shadow server's only identity is the name the calling app used
			// for it, which this surface does not repeat.
			continue
		}
		bucket.TopTargets = append(bucket.TopTargets, *entry)
	}
	for i := range buckets {
		bucket := &buckets[i]
		bucket.SharePercent = sharePercent(bucket.ToolCalls, total)
		sort.SliceStable(bucket.TopTargets, func(a, b int) bool {
			if bucket.TopTargets[a].ToolCalls != bucket.TopTargets[b].ToolCalls {
				return bucket.TopTargets[a].ToolCalls > bucket.TopTargets[b].ToolCalls
			}
			return bucket.TopTargets[a].Name < bucket.TopTargets[b].Name
		})
		if len(bucket.TopTargets) > limit {
			bucket.TopTargets = bucket.TopTargets[:limit]
		}
	}
	return buckets
}

func knownUsageTargetType(targetType string) bool {
	return slices.Contains(usageTargetTypes, targetType)
}

// sharePercent is part's share of total as a percentage rounded server-side
// to one decimal place, so the caller is never handed a ratio it would have
// to format itself.
func sharePercent(part, total int64) float64 {
	if total <= 0 || part <= 0 {
		return 0
	}
	return float64(int64(float64(part)/float64(total)*1000+0.5)) / 10
}

// usageObserved reports whether the window holds any observation the summary
// goes on to report. The totals lead; the buckets are checked as well because
// they are what a caller reads, and no_observations is never asserted beside a
// nonzero metric.
func usageObserved(totals telemetryrepo.ToolUsageTotalsRow, buckets []ToolUsageByTargetType) bool {
	if totals.EventCount > 0 || totals.FailureCount > 0 || totals.BlockedCount > 0 || totals.UniqueTools > 0 || totals.UniqueTargets > 0 {
		return true
	}
	for _, bucket := range buckets {
		if bucket.ToolCalls > 0 || bucket.FailedToolCalls > 0 || bucket.Targets > 0 {
			return true
		}
	}
	return false
}
