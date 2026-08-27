package mcp

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// BuildResolvedMcpEndpointByRefForTest exposes the challenge-resumption
// resolver so external tests can pin its dispatch and fail-closed arms
// without driving a full OAuth flow.
func (s *Service) BuildResolvedMcpEndpointByRefForTest(ctx context.Context, ref EndpointRef) (*ResolvedMcpEndpoint, error) {
	return s.buildResolvedMcpEndpointByRef(ctx, ref)
}

// ErrToolsetEndpointMismatchForTest exposes the re-point fail-closed sentinel
// for assertions.
var ErrToolsetEndpointMismatchForTest = errToolsetEndpointMismatch

// RouteUpstreamTokenForTest exposes the real proxied-backend credential
// selector, so tests drive it with persisted grants rather than restating its
// selection rules.
func RouteUpstreamTokenForTest(ctx context.Context, logger *slog.Logger, tokens map[uuid.UUID]remotesessions.UpstreamToken, upstreamResource string) (string, error) {
	return routeUpstreamToken(ctx, logger, tokens, upstreamResource)
}
