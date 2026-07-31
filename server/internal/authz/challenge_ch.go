package authz

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ClickHouse/clickhouse-go/v2"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
)

// ChallengeCHWriter consumes ChallengeRow messages off Pub/Sub and inserts
// them into the ClickHouse authz_challenges table.
type ChallengeCHWriter struct {
	logger *slog.Logger
	repo   *authzrepo.Queries
}

// NewChallengeCHWriter builds a ChallengeCHWriter backed by the given
// ClickHouse connection.
func NewChallengeCHWriter(logger *slog.Logger, conn clickhouse.Conn) *ChallengeCHWriter {
	return &ChallengeCHWriter{
		logger: logger.With(attr.SlogComponent("authz-challenge-ch-writer")),
		repo:   authzrepo.New(conn),
	}
}

func (w *ChallengeCHWriter) HandleBatch(ctx context.Context, messages []*authzv1.ChallengeRow, _ []gcp.MessageMetadata) error {
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		row, err := challengeRowFromProto(msg)
		if err != nil {
			// Poison message: log and skip so the rest of the batch can proceed.
			// Returning an error would nack the whole batch, including good rows.
			w.logger.ErrorContext(ctx, "authz challenge has invalid payload, skipping",
				attr.SlogError(err),
				attr.SlogValueString(msg.GetId()),
			)
			continue
		}
		if err := w.repo.InsertChallenge(ctx, row); err != nil {
			// Return so the batch is nacked and retried (and eventually
			// dead-lettered). Unlike risk findings, the Pub/Sub message is the
			// sole path into ClickHouse — dropping on insert failure would lose
			// the challenge permanently.
			return fmt.Errorf("insert authz challenge %s: %w", row.ID, err)
		}
	}
	return nil
}
