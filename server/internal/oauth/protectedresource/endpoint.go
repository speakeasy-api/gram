package protectedresource

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
)

// ErrEndpointResourceInvalid means the endpoint did not establish a valid
// same-origin authorization resource. Such metadata must not select a token.
var ErrEndpointResourceInvalid = errors.New("invalid endpoint authorization resource")

// EndpointResource separates the HTTP endpoint from its authorization resource.
type EndpointResource struct {
	// Resource is the exact identifier in validated protected-resource metadata.
	Resource string `json:"resource"`

	// AuthorizationServers lists the issuers that metadata permits for Resource.
	AuthorizationServers []string `json:"authorization_servers"`

	// Discovered distinguishes verified metadata from an endpoint with no metadata.
	Discovered bool `json:"discovered"`
}

// DiscoverEndpointResource resolves an HTTP transport's authorization resource.
// An origin fallback may describe a parent resource instead of the transport
// path. Its identifier and canonical metadata location are verified together;
// the strict Record boundary for an already-known resource remains unchanged.
func DiscoverEndpointResource(ctx context.Context, policy *guardian.Policy, endpoint string) (EndpointResource, error) {
	var none EndpointResource
	transport, err := url.Parse(endpoint)
	if err != nil || transport.Scheme != "https" || transport.Hostname() == "" || transport.User != nil || transport.Fragment != "" {
		return none, ErrEndpointResourceInvalid
	}
	doc, _, err := wellknown.DiscoverProtectedResourceMetadata(ctx, policy, endpoint)
	if err != nil {
		// Only metadata absent at every probe means "no metadata"; a final
		// 404 after an earlier probe failed is still a discovery failure.
		if failure, ok := errors.AsType[*wellknown.ProtectedResourceDiscoveryError](err); ok && failure.Absent() {
			return EndpointResource{Resource: endpoint, AuthorizationServers: nil, Discovered: false}, nil
		}
		return none, fmt.Errorf("discover endpoint resource: %w", err)
	}
	resource, resourceErr := url.Parse(doc.Resource)
	if resourceErr != nil || resource == nil ||
		resource.User != nil || resource.Fragment != "" ||
		!sameHTTPSOrigin(resource, transport) {
		return none, ErrEndpointResourceInvalid
	}
	if !doc.ValidForResource(doc.Resource) {
		// A transport-specific document may name a broader resource. Confirm
		// it at that resource's canonical location before trusting the alias.
		canonical, _, err := wellknown.DiscoverProtectedResourceMetadata(ctx, policy, doc.Resource)
		if err != nil {
			if failure, ok := errors.AsType[*wellknown.ProtectedResourceDiscoveryError](err); ok && failure.Code() == "not_found" {
				return none, ErrEndpointResourceInvalid
			}
			return none, fmt.Errorf("confirm endpoint resource: %w", err)
		}
		if !canonical.ValidForResource(doc.Resource) {
			return none, ErrEndpointResourceInvalid
		}
		doc = canonical
	}
	if len(doc.AuthorizationServers) == 0 {
		return none, ErrEndpointResourceInvalid
	}
	return EndpointResource{Resource: doc.Resource, AuthorizationServers: doc.AuthorizationServers, Discovered: true}, nil
}

// HTTPS's default port does not create a distinct origin.
func sameHTTPSOrigin(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if u.Port() == "" {
			return "443"
		}
		return u.Port()
	}
	return strings.EqualFold(a.Scheme, "https") && strings.EqualFold(b.Scheme, "https") && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}
