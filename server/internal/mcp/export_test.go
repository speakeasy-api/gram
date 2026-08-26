package mcp

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// RouteUpstreamTokenForTest exposes the real proxied-backend credential
// selector, so tests drive it with persisted grants rather than restating its
// selection rules.
func RouteUpstreamTokenForTest(ctx context.Context, logger *slog.Logger, tokens map[uuid.UUID]remotesessions.UpstreamToken, upstreamResource string) (string, error) {
	return routeUpstreamToken(ctx, logger, tokens, upstreamResource)
}
