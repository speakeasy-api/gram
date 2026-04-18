package risk_analysis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// FetchUnanalyzed retrieves a batch of chat message IDs that have not yet been
// analyzed for a given risk policy at its current version.
type FetchUnanalyzed struct {
	logger *slog.Logger
	repo   *repo.Queries
}

func NewFetchUnanalyzed(logger *slog.Logger, db *pgxpool.Pool) *FetchUnanalyzed {
	return &FetchUnanalyzed{
		logger: logger,
		repo:   repo.New(db),
	}
}

type FetchUnanalyzedArgs struct {
	ProjectID    uuid.UUID
	RiskPolicyID uuid.UUID
	BatchLimit   int32
}

type FetchUnanalyzedResult struct {
	MessageIDs    []uuid.UUID
	PolicyVersion int64
	Sources       []string
}

func (a *FetchUnanalyzed) Do(ctx context.Context, args FetchUnanalyzedArgs) (*FetchUnanalyzedResult, error) {
	start := time.Now()

	// Always read the current policy to get the latest version and sources.
	policy, err := a.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        args.RiskPolicyID,
		ProjectID: args.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("get risk policy: %w", err)
	}

	if !policy.Enabled {
		// Policy is disabled — clear all existing results.
		if err := a.repo.DeleteStaleRiskResults(ctx, repo.DeleteStaleRiskResultsParams{
			RiskPolicyID:  args.RiskPolicyID,
			ProjectID:     args.ProjectID,
			PolicyVersion: policy.Version + 1, // version+1 deletes everything including current
		}); err != nil {
			return nil, fmt.Errorf("delete results for disabled policy: %w", err)
		}

		return &FetchUnanalyzedResult{
			MessageIDs:    nil,
			PolicyVersion: policy.Version,
			Sources:       policy.Sources,
		}, nil
	}

	queryStart := time.Now()
	ids, err := a.repo.FetchUnanalyzedMessageIDs(ctx, repo.FetchUnanalyzedMessageIDsParams{
		ProjectID:     uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		RiskPolicyID:  args.RiskPolicyID,
		PolicyVersion: policy.Version,
		BatchLimit:    args.BatchLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch unanalyzed message IDs: %w", err)
	}

	a.logger.InfoContext(ctx, "fetched unanalyzed message IDs",
		slog.Int(logKeyCount, len(ids)),
		slog.Int64(logKeyPolicyVersion, policy.Version),
		slog.Duration(logKeyQueryDur, time.Since(queryStart)),
		slog.Duration(logKeyTotalDur, time.Since(start)),
	)

	return &FetchUnanalyzedResult{
		MessageIDs:    ids,
		PolicyVersion: policy.Version,
		Sources:       policy.Sources,
	}, nil
}
