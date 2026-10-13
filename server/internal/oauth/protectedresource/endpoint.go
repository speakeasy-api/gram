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
	doc, _, err := wellknown.DiscoverProtectedResourceMetadata(ctx, policy, endpoint)
	if err != nil {
		if failure, ok := errors.AsType[*wellknown.ProtectedResourceDiscoveryError](err); ok && failure.Code() == "not_found" {
			return EndpointResource{Resource: endpoint, AuthorizationServers: nil, Discovered: false}, nil
		}
		return none, fmt.Errorf("discover endpoint resource: %w", err)
	}
	transport, transportErr := url.Parse(endpoint)
	resource, resourceErr := url.Parse(doc.Resource)
	if transportErr != nil || resourceErr != nil || resource == nil || transport == nil ||
		resource.User != nil || resource.Fragment != "" ||
		!strings.EqualFold(resource.Scheme, transport.Scheme) || !strings.EqualFold(resource.Host, transport.Host) {
		return none, ErrEndpointResourceInvalid
	}
	if !doc.ValidForResource(doc.Resource) {
		// A transport-specific document may name a broader resource. Confirm
		// it at that resource's canonical location before trusting the alias.
		canonical, _, err := wellknown.DiscoverProtectedResourceMetadata(ctx, policy, doc.Resource)
		if err != nil {
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
