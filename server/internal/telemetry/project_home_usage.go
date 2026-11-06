package telemetry

import (
	"context"
	"sync"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// projectHomeUsage is the usage section of the project home page, returned
// with the overview so the page loads with one request.
type projectHomeUsage struct {
	byUser        *telem_gen.QueryResult
	byAgent       *telem_gen.QueryResult
	externalUsers []*telem_gen.UserSummary
}

// fetchProjectHomeUsage runs the project home usage reads in process.
//
// The per-user and per-agent reads run at the same time. When the per-user
// read shows no agent usage (or the caller lacks org:read), the page falls back
// to the MCP view, which needs every external end user.
//
// It never fails: an error drops the affected section, so the overview tiles
// still render.
func (s *Service) fetchProjectHomeUsage(ctx context.Context, from, to, projectID string) projectHomeUsage {
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
	users, err := s.listAllExternalUsers(ctx, from, to)
	s.logUsageError(ctx, err, "error listing project external users")
	// Non-nil even on error, so the client knows the section has settled.
	usage.externalUsers = users
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

// listAllExternalUsers follows the searchUsers cursor to the end.
func (s *Service) listAllExternalUsers(ctx context.Context, from, to string) ([]*telem_gen.UserSummary, error) {
	users := []*telem_gen.UserSummary{}
	var cursor *string
	for {
		res, err := s.SearchUsers(ctx, &telem_gen.SearchUsersPayload{
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Filter: &telem_gen.SearchUsersFilter{
				From:          from,
				To:            to,
				DeploymentID:  nil,
				UserIds:       nil,
				EventSource:   nil,
				HookSource:    nil,
				AccountType:   nil,
				ExternalOrgID: nil,
			},
			UserType: "external",
			GroupBy:  "employee",
			Cursor:   cursor,
			Sort:     "desc",
			Limit:    1000,
			Metrics:  "full",
			Source:   "logs",
		})
		if err != nil {
			return users, err
		}
		users = append(users, res.Users...)
		if res.NextCursor == nil || *res.NextCursor == "" {
			return users, nil
		}
		cursor = res.NextCursor
	}
}

// logUsageError skips client faults: a caller without org:read gets the MCP
// view by design.
func (s *Service) logUsageError(ctx context.Context, err error, msg string) {
	if err == nil || oops.IsClientFault(err) {
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
