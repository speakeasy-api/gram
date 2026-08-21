package mcp

import "context"

// BuildResolvedMcpEndpointByRefForTest exposes the challenge-resumption
// resolver so external tests can pin its dispatch and fail-closed arms
// without driving a full OAuth flow.
func (s *Service) BuildResolvedMcpEndpointByRefForTest(ctx context.Context, ref EndpointRef) (*ResolvedMcpEndpoint, error) {
	return s.buildResolvedMcpEndpointByRef(ctx, ref)
}

// ErrToolsetEndpointMismatchForTest exposes the re-point fail-closed sentinel
// for assertions.
var ErrToolsetEndpointMismatchForTest = errToolsetEndpointMismatch
