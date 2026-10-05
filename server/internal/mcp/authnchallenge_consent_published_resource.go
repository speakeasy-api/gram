// Published resource preference for the connect leg (GRW-256): a provider may
// match the RFC 8707 resource exactly against the resource its RFC 9728
// metadata publishes, so https://host and https://host/ are not
// interchangeable. Whichever spelling an MCP server was registered with, the
// grant records the upstream's own.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

// publishedResourceTimeout bounds the metadata read a Connect click waits on.
const publishedResourceTimeout = 3 * time.Second

// protectedResourceFetcher reads the RFC 9728 metadata document published for
// resourceURL.
type protectedResourceFetcher func(ctx context.Context, resourceURL string) (wellknown.OAuthProtectedResourceMetadata, error)

// publishedResource returns the resource the upstream's metadata publishes for
// registered when a grant recorded with it routes to registered — the two
// differ only in trailing slashes — and registered otherwise, including when
// the metadata cannot be read. A document naming any other resource is
// ignored: Gram never sends a resource for a different upstream.
func (s *Service) publishedResource(ctx context.Context, logger *slog.Logger, registered string) string {
	if !urls.IsAbsoluteHTTPSOrLoopback(registered) {
		return registered
	}

	fetch := s.protectedResourceFetcher
	if fetch == nil {
		fetch = s.fetchProtectedResource
	}
	fetchCtx, cancel := context.WithTimeout(ctx, publishedResourceTimeout)
	defer cancel()
	doc, err := fetch(fetchCtx, registered)
	switch {
	case err != nil:
		logger.WarnContext(ctx, "read upstream protected resource metadata; sending the registered resource", attr.SlogURLFull(registered), attr.SlogError(err))
		return registered
	case !grantRoutesToUpstream(doc.Resource, registered, false):
		return registered
	}
	return doc.Resource
}

func (s *Service) fetchProtectedResource(ctx context.Context, resourceURL string) (wellknown.OAuthProtectedResourceMetadata, error) {
	if s.guardianPolicy == nil {
		return wellknown.OAuthProtectedResourceMetadata{}, errors.New("no outbound http policy")
	}
	doc, _, err := wellknown.DiscoverProtectedResourceMetadata(ctx, s.guardianPolicy, resourceURL)
	if err != nil {
		return wellknown.OAuthProtectedResourceMetadata{}, fmt.Errorf("discover protected resource metadata: %w", err)
	}
	return doc, nil
}
