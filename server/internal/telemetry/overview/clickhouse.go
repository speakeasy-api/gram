// Package overview is the neutral read path for project overview analytics.
//
// It exists so the management telemetry handler and Platform MCP compose the
// same ClickHouse reads and projection rather than growing a second
// implementation each: a summary the dashboard shows and a summary an external
// diagnostic tool reports must not be able to disagree.
package overview

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"golang.org/x/sync/errgroup"
)

type ClickHouseReader interface {
	GetOverviewSummary(context.Context, repo.GetOverviewSummaryParams) (*repo.OverviewSummary, error)
	GetActiveCounts(context.Context, repo.GetActiveCountsParams) (*repo.ActiveCounts, error)
	GetTopServers(context.Context, repo.GetTopServersParams) ([]repo.TopServer, error)
	GetTopUsers(context.Context, repo.GetTopUsersParams) ([]repo.TopUser, error)
	GetLLMClientBreakdown(context.Context, repo.GetLLMClientBreakdownParams) ([]repo.LLMClientUsage, error)
}

// Params bounds one project overview read: the project, the current window,
// the equal-length comparison window preceding it, and whether session capture
// is the organization's metrics mode.
type Params struct {
	ProjectID       string
	TimeStart       int64
	TimeEnd         int64
	ComparisonStart int64
	ComparisonEnd   int64
	SessionMode     bool
}

// Result is the ClickHouse half of a project overview. TopUsers and LLMClients
// stay empty in session mode, where those two lists come from PostgreSQL
// instead; a caller that needs them reads Params.SessionMode rather than
// treating empty as "no activity".
type Result struct {
	ToolMetrics           *repo.OverviewSummary
	ToolMetricsComparison *repo.OverviewSummary
	ActiveCounts          *repo.ActiveCounts
	TopServers            []repo.TopServer
	TopUsers              []repo.TopUser
	LLMClients            []repo.LLMClientUsage
}

// FetchClickHouse runs a project overview's ClickHouse lanes concurrently and
// returns them together. It performs no authorization and no gating; a caller
// establishes both before reading.
func FetchClickHouse(
	ctx context.Context,
	reader ClickHouseReader,
	params Params,
) (Result, error) {
	var result Result
	// clickhouse.Conn is a connection pool: concurrent queries acquire separate
	// transports and remain bounded by the pool's MaxOpenConns setting.
	eg, egCtx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		var queryErr error
		result.ToolMetrics, queryErr = reader.GetOverviewSummary(egCtx, repo.GetOverviewSummaryParams{
			// Org-scope read: Gram-hosted sources stay counted, matching the
			// summaries fast path.
			ExcludedHookSources: nil,
			GramProjectID:       params.ProjectID,
			TimeStart:           params.TimeStart,
			TimeEnd:             params.TimeEnd,
			User:                repo.UserIdentity{UserIDs: nil, Emails: nil},
			CanonicalUser:       repo.CanonicalUserIdentity{OrgID: "", UserID: "", EmailLower: ""},
			ExternalUserID:      "",
			APIKeyID:            "",
			ToolsetSlug:         "",
			RemoteMCPServerID:   "",
			MCPServerID:         "",
			MetaMCPServerID:     "",
			EventSource:         "",
			HookSource:          "",
			AccountType:         "",
			ExternalOrgID:       "",
		})
		if queryErr != nil {
			return oops.E(oops.CodeUnexpected, queryErr, "error retrieving tool call metrics")
		}
		return nil
	})

	eg.Go(func() error {
		var queryErr error
		result.ToolMetricsComparison, queryErr = reader.GetOverviewSummary(egCtx, repo.GetOverviewSummaryParams{
			// Org-scope read: Gram-hosted sources stay counted, matching the
			// summaries fast path.
			ExcludedHookSources: nil,
			GramProjectID:       params.ProjectID,
			TimeStart:           params.ComparisonStart,
			TimeEnd:             params.ComparisonEnd,
			User:                repo.UserIdentity{UserIDs: nil, Emails: nil},
			CanonicalUser:       repo.CanonicalUserIdentity{OrgID: "", UserID: "", EmailLower: ""},
			ExternalUserID:      "",
			APIKeyID:            "",
			ToolsetSlug:         "",
			RemoteMCPServerID:   "",
			MCPServerID:         "",
			MetaMCPServerID:     "",
			EventSource:         "",
			HookSource:          "",
			AccountType:         "",
			ExternalOrgID:       "",
		})
		if queryErr != nil {
			return oops.E(oops.CodeUnexpected, queryErr, "error retrieving comparison tool call metrics")
		}
		return nil
	})

	eg.Go(func() error {
		var queryErr error
		result.ActiveCounts, queryErr = reader.GetActiveCounts(egCtx, repo.GetActiveCountsParams{
			GramProjectID:  params.ProjectID,
			TimeStart:      params.TimeStart,
			TimeEnd:        params.TimeEnd,
			ExternalUserID: "",
			APIKeyID:       "",
			ToolsetSlug:    "",
			SessionMode:    params.SessionMode,
		})
		if queryErr != nil {
			return oops.E(oops.CodeUnexpected, queryErr, "error retrieving active server counts")
		}
		return nil
	})

	eg.Go(func() error {
		var queryErr error
		result.TopServers, queryErr = reader.GetTopServers(egCtx, repo.GetTopServersParams{
			GramProjectID:  params.ProjectID,
			TimeStart:      params.TimeStart,
			TimeEnd:        params.TimeEnd,
			ExternalUserID: "",
			APIKeyID:       "",
			ToolsetSlug:    "",
			Limit:          10,
		})
		if queryErr != nil {
			return oops.E(oops.CodeUnexpected, queryErr, "error retrieving top servers")
		}
		return nil
	})

	if !params.SessionMode {
		eg.Go(func() error {
			var queryErr error
			result.TopUsers, queryErr = reader.GetTopUsers(egCtx, repo.GetTopUsersParams{
				GramProjectID:  params.ProjectID,
				TimeStart:      params.TimeStart,
				TimeEnd:        params.TimeEnd,
				ExternalUserID: "",
				APIKeyID:       "",
				ToolsetSlug:    "",
				Limit:          10,
				SessionMode:    false,
			})
			if queryErr != nil {
				return oops.E(oops.CodeUnexpected, queryErr, "error retrieving top users from CH")
			}
			return nil
		})

		eg.Go(func() error {
			var queryErr error
			result.LLMClients, queryErr = reader.GetLLMClientBreakdown(egCtx, repo.GetLLMClientBreakdownParams{
				GramProjectID:  params.ProjectID,
				TimeStart:      params.TimeStart,
				TimeEnd:        params.TimeEnd,
				ExternalUserID: "",
				APIKeyID:       "",
				ToolsetSlug:    "",
				SessionMode:    false,
			})
			if queryErr != nil {
				return oops.E(oops.CodeUnexpected, queryErr, "error retrieving LLM client breakdown from CH")
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return Result{}, fmt.Errorf("fetch project overview ClickHouse data: %w", err)
	}

	return result, nil
}
