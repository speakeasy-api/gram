package riskfindingscols

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// UpdateRow is one per-finding column update ready for the ClickHouse mutation
// sink: the target row id, the finding's created_at (bounds the mutation for
// partition pruning), and the two new column values.
type UpdateRow struct {
	ID               uuid.UUID
	CreatedAt        time.Time
	MessageCreatedAt time.Time
	AssistantID      string
}

// Transformer maps a SourceRow to an UpdateRow. The mapping is a near
// pass-through — the source query already resolves both values — and the stage
// exists for harness symmetry with the riskfindings migration. Its one piece
// of logic is a defensive guard mirroring the source's SQL COALESCE: a zero
// MessageCreatedAt falls back to the finding's created_at, the same value the
// ClickHouse column DEFAULT computes, so a malformed input can never write a
// zero timestamp into ClickHouse.
type Transformer struct{}

// NewTransformer builds the pass-through transformer.
func NewTransformer() *Transformer {
	return &Transformer{}
}

// Transform implements pipeline.Transformer.
func (t *Transformer) Transform(_ context.Context, in SourceRow) ([]UpdateRow, error) {
	messageCreatedAt := in.MessageCreatedAt
	if messageCreatedAt.IsZero() {
		messageCreatedAt = in.CreatedAt
	}

	return []UpdateRow{{
		ID:               in.ID,
		CreatedAt:        in.CreatedAt,
		MessageCreatedAt: messageCreatedAt,
		AssistantID:      in.AssistantID,
	}}, nil
}
