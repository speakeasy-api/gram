package businessmemory

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const (
	// pplx-embed-v1-0.6b is available through OpenRouter today and under an
	// MIT license for a future self-hosted deployment.
	embeddingModel      = "perplexity/pplx-embed-v1-0.6b"
	embeddingDimensions = 1024
)

func enableFilteredVectorScan(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "SET LOCAL hnsw.iterative_scan = strict_order"); err != nil {
		return fmt.Errorf("enable filtered vector scan: %w", err)
	}
	return nil
}
