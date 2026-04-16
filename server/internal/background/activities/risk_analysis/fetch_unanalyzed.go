package risk_analysis

import (
	"context"
	"fmt"
	"log/slog"

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
	// Always read the current policy to get the latest version and sources.
	policy, err := a.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        args.RiskPolicyID,
		ProjectID: args.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("get risk policy: %w", err)
	}

	if !policy.Enabled {
		return &FetchUnanalyzedResult{
			MessageIDs:    nil,
			PolicyVersion: policy.Version,
			Sources:       policy.Sources,
		}, nil
	}

	// Clean up results from older policy versions.
	if err := a.repo.DeleteStaleRiskResults(ctx, repo.DeleteStaleRiskResultsParams{
		RiskPolicyID:  args.RiskPolicyID,
		ProjectID:     args.ProjectID,
		PolicyVersion: policy.Version,
	}); err != nil {
		return nil, fmt.Errorf("delete stale risk results: %w", err)
	}

	ids, err := a.repo.FetchUnanalyzedMessageIDs(ctx, repo.FetchUnanalyzedMessageIDsParams{
		ProjectID:     uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		RiskPolicyID:  args.RiskPolicyID,
		PolicyVersion: policy.Version,
		BatchLimit:    args.BatchLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch unanalyzed message IDs: %w", err)
	}

	return &FetchUnanalyzedResult{
		MessageIDs:    ids,
		PolicyVersion: policy.Version,
		Sources:       policy.Sources,
	}, nil
}
