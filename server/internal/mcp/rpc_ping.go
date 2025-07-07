package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func handlePing(ctx context.Context, logger *slog.Logger, id msgID) (json.RawMessage, error) {
	bs, err := json.Marshal(&result[struct{}]{
		ID:     id,
		Result: struct{}{},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize ping response").Log(ctx, logger)
	}

	return bs, nil
}
