package activities

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/encryption"
)

type GetAIIntegrationsCandidates struct {
	integrations *aiintegrations.Store
}

func NewGetAIIntegrationsCandidates(logger *slog.Logger, db *pgxpool.Pool, encryptionClient *encryption.Client) *GetAIIntegrationsCandidates {
	return &GetAIIntegrationsCandidates{
		integrations: aiintegrations.NewStore(logger, db, encryptionClient),
	}
}

type GetAIIntegrationsCandidatesInput struct {
	PollDueBefore time.Time
	Limit         int32
}

func (c *GetAIIntegrationsCandidates) Do(ctx context.Context, input GetAIIntegrationsCandidatesInput) ([]aiintegrations.UsagePollCandidate, error) {
	candidates, err := c.integrations.ListUsagePollCandidates(ctx, input.PollDueBefore, input.Limit)
	if err != nil {
		return nil, fmt.Errorf("get ai integrations candidates: %w", err)
	}
	return candidates, nil
}
