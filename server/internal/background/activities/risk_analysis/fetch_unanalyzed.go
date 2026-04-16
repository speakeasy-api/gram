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
// analyzed for a given risk policy at the specified version.
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
	ProjectID     uuid.UUID
	RiskPolicyID  uuid.UUID
	PolicyVersion int64
	BatchLimit    int32
}

type FetchUnanalyzedResult struct {
	MessageIDs []uuid.UUID
}

func (a *FetchUnanalyzed) Do(ctx context.Context, args FetchUnanalyzedArgs) (*FetchUnanalyzedResult, error) {
	ids, err := a.repo.FetchUnanalyzedMessageIDs(ctx, repo.FetchUnanalyzedMessageIDsParams{
		ProjectID:     uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		RiskPolicyID:  args.RiskPolicyID,
		PolicyVersion: args.PolicyVersion,
		BatchLimit:    args.BatchLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch unanalyzed message IDs: %w", err)
	}

	return &FetchUnanalyzedResult{MessageIDs: ids}, nil
}
