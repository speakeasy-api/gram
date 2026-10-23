package mcp

import "context"

// ResolveCachedBuiltinForTest exercises the shared cached-reference loader.
func (s *Service) ResolveCachedBuiltinForTest(ctx context.Context, ref EndpointRef) (*ResolvedMcpEndpoint, error) {
	return s.loadResolvedMcpEndpointByRef(ctx, ref)
}

// ResolveSharedBuiltinForTest exercises the same RFC 8707 resolver token grants use.
func (s *Service) ResolveSharedBuiltinForTest(ctx context.Context, endpoint *ResolvedMcpEndpoint, resource string) (*ResolvedMcpEndpoint, string, error) {
	resolved, rejection, err := s.resolveSharedResource(ctx, s.logger, endpoint.sharedAuthorizationServer, resource)
	return resolved, string(rejection), err
}
