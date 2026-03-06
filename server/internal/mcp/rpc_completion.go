package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

type completionResult struct {
	Completion completionValues `json:"completion"`
}

type completionValues struct {
	Values  []string `json:"values"`
	HasMore bool     `json:"hasMore"`
	Total   int      `json:"total"`
}

func handleCompletionComplete(ctx context.Context, logger *slog.Logger, id msgID) (json.RawMessage, error) {
	bs, err := json.Marshal(&result[completionResult]{
		ID: id,
		Result: completionResult{
			Completion: completionValues{
				Values:  []string{},
				HasMore: false,
				Total:   0,
			},
		},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize completion/complete response").Log(ctx, logger)
	}

	return bs, nil
}
