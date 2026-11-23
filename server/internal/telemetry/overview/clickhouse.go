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
	GetUnifiedActiveServerCount(context.Context, repo.GetTopServersParams) (uint64, error)
	GetTopServers(context.Context, repo.GetTopServersParams) ([]repo.TopServer, error)
}

// Params bounds one project overview read: the project and the time window.
type Params struct {
	ProjectID         string
	TimeStart         int64
	TimeEnd           int64
	HostedMCPMatchers []repo.HostedMCPMatcher
	MCPServerMatchers []repo.MCPServerMatcher
	MetaMCPMatchers   []repo.MetaMCPMatcher
}

// Result is the ClickHouse half of a project overview.
type Result struct {
	ToolMetrics        *repo.OverviewSummary
	ActiveServersCount uint64
	TopServers         []repo.TopServer
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
			// Org-scope read: Speakeasy-hosted sources stay counted, matching the
			// summaries fast path.
			ExcludedHookSources: nil,
			GramProjectIDs:      []string{params.ProjectID},
			TimeStart:           params.TimeStart,
			TimeEnd:             params.TimeEnd,
			User:                repo.UserIdentity{UserIDs: nil, Emails: nil},
			CanonicalUser:       repo.CanonicalUserIdentity{OrgID: "", UserID: "", EmailLower: ""},
			ExternalUserID:      "",
			APIKeyID:            "",
			ToolsetSlug:         "",
			ToolsetSlugServerID: "",
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
		result.ActiveServersCount, queryErr = reader.GetUnifiedActiveServerCount(egCtx, repo.GetTopServersParams{
			GramProjectID:     params.ProjectID,
			TimeStart:         params.TimeStart,
			TimeEnd:           params.TimeEnd,
			HostedMCPMatchers: params.HostedMCPMatchers,
			MCPServerMatchers: params.MCPServerMatchers,
			MetaMCPMatchers:   params.MetaMCPMatchers,
			Limit:             0,
		})
		if queryErr != nil {
			return oops.E(oops.CodeUnexpected, queryErr, "error retrieving active server count")
		}
		return nil
	})

	eg.Go(func() error {
		var queryErr error
		result.TopServers, queryErr = reader.GetTopServers(egCtx, repo.GetTopServersParams{
			GramProjectID:     params.ProjectID,
			TimeStart:         params.TimeStart,
			TimeEnd:           params.TimeEnd,
			HostedMCPMatchers: params.HostedMCPMatchers,
			MCPServerMatchers: params.MCPServerMatchers,
			MetaMCPMatchers:   params.MetaMCPMatchers,
			// Fetch extra rows so alternate reported names can fold before the
			// API trims to ten, while keeping the backend read bounded.
			Limit: 50,
		})
		if queryErr != nil {
			return oops.E(oops.CodeUnexpected, queryErr, "error retrieving top servers")
		}
		return nil
	})

	if err := eg.Wait(); err != nil {
		return Result{}, fmt.Errorf("fetch project overview ClickHouse data: %w", err)
	}

	return result, nil
}
