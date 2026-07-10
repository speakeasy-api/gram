package telemetry

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

type projectOverviewClickHouseReader interface {
	GetOverviewSummary(context.Context, repo.GetOverviewSummaryParams) (*repo.OverviewSummary, error)
	GetActiveCounts(context.Context, repo.GetActiveCountsParams) (*repo.ActiveCounts, error)
	GetTopServers(context.Context, repo.GetTopServersParams) ([]repo.TopServer, error)
	GetTopUsers(context.Context, repo.GetTopUsersParams) ([]repo.TopUser, error)
	GetLLMClientBreakdown(context.Context, repo.GetLLMClientBreakdownParams) ([]repo.LLMClientUsage, error)
}

type projectOverviewClickHouseParams struct {
	projectID       string
	timeStart       int64
	timeEnd         int64
	comparisonStart int64
	comparisonEnd   int64
	sessionMode     bool
}

type projectOverviewClickHouseResult struct {
	toolMetrics           *repo.OverviewSummary
	toolMetricsComparison *repo.OverviewSummary
	activeCounts          *repo.ActiveCounts
	topServers            []repo.TopServer
	topUsers              []repo.TopUser
	llmClients            []repo.LLMClientUsage
}

func fetchProjectOverviewClickHouse(
	ctx context.Context,
	tracer trace.Tracer,
	reader projectOverviewClickHouseReader,
	params projectOverviewClickHouseParams,
) (projectOverviewClickHouseResult, error) {
	var result projectOverviewClickHouseResult
	// clickhouse.Conn is a connection pool: concurrent queries acquire separate
	// transports and remain bounded by the pool's MaxOpenConns setting.
	eg, egCtx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return traceProjectOverviewQuery(egCtx, tracer, "telemetry.getProjectOverview.clickhouse.overview.current", func(queryCtx context.Context) error {
			var queryErr error
			result.toolMetrics, queryErr = reader.GetOverviewSummary(queryCtx, repo.GetOverviewSummaryParams{
				GramProjectID:     params.projectID,
				TimeStart:         params.timeStart,
				TimeEnd:           params.timeEnd,
				UserID:            "",
				ExternalUserID:    "",
				APIKeyID:          "",
				ToolsetSlug:       "",
				RemoteMCPServerID: "",
				MCPServerID:       "",
				EventSource:       "",
				HookSource:        "",
				AccountType:       "",
				ExternalOrgID:     "",
			})
			if queryErr != nil {
				return oops.E(oops.CodeUnexpected, queryErr, "error retrieving tool call metrics")
			}
			return nil
		})
	})

	eg.Go(func() error {
		return traceProjectOverviewQuery(egCtx, tracer, "telemetry.getProjectOverview.clickhouse.overview.comparison", func(queryCtx context.Context) error {
			var queryErr error
			result.toolMetricsComparison, queryErr = reader.GetOverviewSummary(queryCtx, repo.GetOverviewSummaryParams{
				GramProjectID:     params.projectID,
				TimeStart:         params.comparisonStart,
				TimeEnd:           params.comparisonEnd,
				UserID:            "",
				ExternalUserID:    "",
				APIKeyID:          "",
				ToolsetSlug:       "",
				RemoteMCPServerID: "",
				MCPServerID:       "",
				EventSource:       "",
				HookSource:        "",
				AccountType:       "",
				ExternalOrgID:     "",
			})
			if queryErr != nil {
				return oops.E(oops.CodeUnexpected, queryErr, "error retrieving comparison tool call metrics")
			}
			return nil
		})
	})

	eg.Go(func() error {
		return traceProjectOverviewQuery(egCtx, tracer, "telemetry.getProjectOverview.clickhouse.activeCounts", func(queryCtx context.Context) error {
			var queryErr error
			result.activeCounts, queryErr = reader.GetActiveCounts(queryCtx, repo.GetActiveCountsParams{
				GramProjectID:  params.projectID,
				TimeStart:      params.timeStart,
				TimeEnd:        params.timeEnd,
				ExternalUserID: "",
				APIKeyID:       "",
				ToolsetSlug:    "",
				SessionMode:    params.sessionMode,
			})
			if queryErr != nil {
				return oops.E(oops.CodeUnexpected, queryErr, "error retrieving active server counts")
			}
			return nil
		})
	})

	eg.Go(func() error {
		return traceProjectOverviewQuery(egCtx, tracer, "telemetry.getProjectOverview.clickhouse.topServers", func(queryCtx context.Context) error {
			var queryErr error
			result.topServers, queryErr = reader.GetTopServers(queryCtx, repo.GetTopServersParams{
				GramProjectID:  params.projectID,
				TimeStart:      params.timeStart,
				TimeEnd:        params.timeEnd,
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
	})

	if !params.sessionMode {
		eg.Go(func() error {
			return traceProjectOverviewQuery(egCtx, tracer, "telemetry.getProjectOverview.clickhouse.topUsers", func(queryCtx context.Context) error {
				var queryErr error
				result.topUsers, queryErr = reader.GetTopUsers(queryCtx, repo.GetTopUsersParams{
					GramProjectID:  params.projectID,
					TimeStart:      params.timeStart,
					TimeEnd:        params.timeEnd,
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
		})

		eg.Go(func() error {
			return traceProjectOverviewQuery(egCtx, tracer, "telemetry.getProjectOverview.clickhouse.llmClientBreakdown", func(queryCtx context.Context) error {
				var queryErr error
				result.llmClients, queryErr = reader.GetLLMClientBreakdown(queryCtx, repo.GetLLMClientBreakdownParams{
					GramProjectID:  params.projectID,
					TimeStart:      params.timeStart,
					TimeEnd:        params.timeEnd,
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
		})
	}

	if err := eg.Wait(); err != nil {
		return projectOverviewClickHouseResult{}, fmt.Errorf("fetch project overview ClickHouse data: %w", err)
	}

	return result, nil
}

func traceProjectOverviewQuery(ctx context.Context, tracer trace.Tracer, name string, query func(context.Context) error) error {
	queryCtx, span := tracer.Start(ctx, name)
	defer span.End()

	err := query(queryCtx)
	if err != nil {
		span.RecordError(err)
	}

	return err
}
