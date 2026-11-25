package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func handleServerDiscover(ctx context.Context, logger *slog.Logger, id mcpjsonrpc.ID, description serverDescription, supported []string) (json.RawMessage, error) {
	// Notifications receive only the transport's bodyless acknowledgement.
	if !id.IsSet() {
		return nil, nil
	}

	response := result[metamcp.DiscoverResult]{
		ID: id,
		Result: metamcp.DiscoverResult{
			SupportedVersions: slices.Clone(supported),
			Capabilities:      description.capabilities,
			Instructions:      description.instructions,
		},
		serverIdentity: description.identity,
		cacheHints:     description.cacheHints,
	}
	body, err := json.Marshal(response)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize server/discover response").LogError(ctx, logger)
	}
	return body, nil
}
