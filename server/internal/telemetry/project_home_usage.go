package telemetry

import (
	"cmp"
	"context"
	"errors"
	"math/bits"
	"slices"
	"strings"
	"sync"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// projectHomeUsage is the usage section of the project home page, returned
// with the overview so the page loads with one request.
type projectHomeUsage struct {
	byUser   *telem_gen.QueryResult
	byAgent  *telem_gen.QueryResult
	mcpUsage *telem_gen.ProjectMcpUsage
}

// fetchProjectHomeUsage runs the project home usage reads in process.
//
// The per-user and per-agent reads run at the same time. When the per-user
// read shows no agent usage (or the caller lacks org:read), the page falls back
// to the MCP view, which reads the external end user and tool summary.
//
// It never fails: an error drops the affected section, so the overview tiles
// still render.
func (s *Service) fetchProjectHomeUsage(ctx context.Context, from, to, projectID string, timeStart, timeEnd int64) projectHomeUsage {
	var (
		usage      projectHomeUsage
		byUserErr  error
		byAgentErr error
		wg         sync.WaitGroup
	)
	wg.Go(func() {
		usage.byUser, byUserErr = s.queryProjectUsage(ctx, from, to, projectID, "email", 100)
	})
	wg.Go(func() {
		usage.byAgent, byAgentErr = s.queryProjectUsage(ctx, from, to, projectID, "hook_source", 10)
	})
	wg.Wait()

	s.logUsageError(ctx, byUserErr, "error querying project usage by user")
	s.logUsageError(ctx, byAgentErr, "error querying project usage by agent")

	if hasAgentUsage(usage.byUser) {
		return usage
	}

	// No agent usage: the page shows the MCP view, which never reads the
	// per-agent breakdown.
	usage.byAgent = nil
	mcp, err := s.chRepo.GetProjectMcpUsage(ctx, repo.GetProjectMcpUsageParams{
		GramProjectID: projectID,
		TimeStart:     timeStart,
		TimeEnd:       timeEnd,
	})
	s.logUsageError(ctx, err, "error reading project MCP usage")
	// Non-nil even on error, so the client knows the section has settled.
	usage.mcpUsage = buildProjectMcpUsage(mcp)
	return usage
}

func (s *Service) queryProjectUsage(ctx context.Context, from, to, projectID, groupBy string, topN int) (*telem_gen.QueryResult, error) {
	res, err := s.query(ctx, &telem_gen.QueryPayload{
		SessionToken:           nil,
		From:                   from,
		To:                     to,
		GroupBy:                &groupBy,
		Filters:                []*telem_gen.QueryFilter{{Dimension: "project_id", Values: []string{projectID}}},
		GranularitySeconds:     nil,
		IncludeDimensionValues: false,
		TopN:                   topN,
		SortBy:                 "llm_tokens",
	}, false)
	if err != nil {
		return nil, err
	}
	// buildQueryResult still gap-fills a zero series per group; the home page
	// reads only the table.
	res.Timeseries = []*telem_gen.QuerySeries{}
	return res, nil
}

// projectMcpTopToolsLimit is how many tools each MCP view tool list returns.
// The project home cards show the top 5.
const projectMcpTopToolsLimit = 5

// buildProjectMcpUsage ranks and trims the tool lists. Most used tools rank by
// calls. Failing tools keep only tools with a failure and rank by failure rate,
// then by failures, so a high-volume failing tool outranks a one-off failure
// at the same rate.
func buildProjectMcpUsage(usage repo.ProjectMcpUsage) *telem_gen.ProjectMcpUsage {
	endUsers := make([]*telem_gen.ProjectMcpEndUser, 0, len(usage.TopEndUsers))
	for _, u := range usage.TopEndUsers {
		endUsers = append(endUsers, &telem_gen.ProjectMcpEndUser{
			UserID:    u.UserID,
			ToolCalls: int64(u.ToolCalls), //nolint:gosec // Bounded count
		})
	}

	tools := usage.Tools
	slices.SortFunc(tools, func(a, b repo.ProjectMcpToolRow) int {
		return cmp.Or(cmp.Compare(b.Calls, a.Calls), strings.Compare(a.ToolURN, b.ToolURN))
	})

	failing := slices.DeleteFunc(slices.Clone(tools), func(t repo.ProjectMcpToolRow) bool { return t.Failures == 0 })
	slices.SortFunc(failing, func(a, b repo.ProjectMcpToolRow) int {
		// Compare failures/calls by cross-multiplying in 128 bits, which stays
		// exact without overflow. Every failure is also a call, so calls is
		// never zero here.
		return cmp.Or(
			compareMul128(b.Failures, a.Calls, a.Failures, b.Calls),
			cmp.Compare(b.Failures, a.Failures),
			strings.Compare(a.ToolURN, b.ToolURN),
		)
	})

	return &telem_gen.ProjectMcpUsage{
		EndUsersCount:   int64(usage.EndUsersCount), //nolint:gosec // Bounded count
		TopEndUsers:     endUsers,
		TopTools:        toProjectMcpToolUsage(tools),
		TopFailingTools: toProjectMcpToolUsage(failing),
	}
}

func toProjectMcpToolUsage(rows []repo.ProjectMcpToolRow) []*telem_gen.ProjectMcpToolUsage {
	rows = rows[:min(len(rows), projectMcpTopToolsLimit)]
	out := make([]*telem_gen.ProjectMcpToolUsage, 0, len(rows))
	for _, t := range rows {
		out = append(out, &telem_gen.ProjectMcpToolUsage{
			Urn:      t.ToolURN,
			Calls:    int64(t.Calls),    //nolint:gosec // Bounded count
			Failures: int64(t.Failures), //nolint:gosec // Bounded count
		})
	}
	return out
}

// logUsageError skips client faults and authorization denials: a caller
// without org:read gets the MCP view by design. ShareableError does not
// classify itself as a client fault, so its auth codes are checked here.
func (s *Service) logUsageError(ctx context.Context, err error, msg string) {
	if err == nil || oops.IsClientFault(err) {
		return
	}
	var shareable *oops.ShareableError
	if errors.As(err, &shareable) && (shareable.Code == oops.CodeForbidden || shareable.Code == oops.CodeUnauthorized) {
		return
	}
	s.logger.ErrorContext(ctx, msg, attr.SlogError(err))
}

// hasAgentUsage reports whether any row has nonzero usage.
// attribute_metrics_summaries only admits agent-surface rows, so any usage
// means the project has hook data.
func hasAgentUsage(res *telem_gen.QueryResult) bool {
	if res == nil {
		return false
	}
	for _, row := range res.Table {
		m := row.Measures
		if m != nil && (m.TotalTokens > 0 || m.TotalCost > 0 || m.TotalChats > 0 || m.TotalToolCalls > 0) {
			return true
		}
	}
	return false
}

// compareMul128 compares a*b with c*d using full 128-bit products.
func compareMul128(a, b, c, d uint64) int {
	hi1, lo1 := bits.Mul64(a, b)
	hi2, lo2 := bits.Mul64(c, d)
	return cmp.Or(cmp.Compare(hi1, hi2), cmp.Compare(lo1, lo2))
}
