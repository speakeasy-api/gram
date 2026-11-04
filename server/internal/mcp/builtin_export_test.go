package mcp

import (
	"context"
	"fmt"
)

// ResolveCachedBuiltinForTest exercises the shared cached-reference loader.
func (s *Service) ResolveCachedBuiltinForTest(ctx context.Context, ref EndpointRef) (*ResolvedMcpEndpoint, error) {
	return s.loadResolvedMcpEndpointByRef(ctx, ref)
}

// ResolveSharedBuiltinForTest exercises the same RFC 8707 resolver token grants use.
func (s *Service) ResolveSharedBuiltinForTest(ctx context.Context, endpoint *ResolvedMcpEndpoint, resource string) (*ResolvedMcpEndpoint, string, error) {
	resolved, rejection, err := s.resolveSharedResource(ctx, s.logger, endpoint.sharedAuthorizationServer, resource)
	return resolved, string(rejection), err
}

// InvalidateSessionPolicyForTest simulates expiry of the ordinary HTTP policy cache.
func (s *Service) InvalidateSessionPolicyForTest(ctx context.Context, issuerID, jti string) error {
	if err := s.sessionPolicyCache.DeleteByKey(ctx, sessionPolicyCacheKey(issuerID, jti)); err != nil {
		return fmt.Errorf("invalidate session policy: %w", err)
	}
	return nil
}
