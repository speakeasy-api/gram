// Package catalogauth projects public OAuth discovery into bounded catalogue capabilities.
package catalogauth

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// discoveryTimeout bounds all public metadata requests for one staff review.
const discoveryTimeout = 8 * time.Second

// Discovery records advertised capabilities, not a verified OAuth connection.
// It deliberately excludes issuer URLs, upstream text, credentials and tenant data.
type Discovery struct {
	// SupportsDCR reports an advertised registration endpoint.
	SupportsDCR bool `json:"supports_dcr"`
	// SupportsCIMD reports metadata-document support compatible with public clients.
	SupportsCIMD bool `json:"supports_cimd"`
	// AuthSetup is none, dcr, cimd, manual, or unknown.
	AuthSetup string `json:"auth_setup"`
	// Status distinguishes complete metadata from partial or unavailable discovery and explicit no-auth selection.
	Status string `json:"status"`
}

// Unknown makes absence of evidence explicit, never a manual-setup conclusion.
func Unknown() Discovery {
	return Discovery{SupportsDCR: false, SupportsCIMD: false, AuthSetup: "unknown", Status: "unavailable"}
}

// Discover reads only public resource and issuer metadata through the outbound policy.
// No OAuth clients are registered and no authentication is sent.
func Discover(ctx context.Context, policy *guardian.Policy, endpoint, authMode string) Discovery {
	if authMode == "none" {
		return Discovery{SupportsDCR: false, SupportsCIMD: false, AuthSetup: "none", Status: "not_required"}
	}
	result := Unknown()
	if authMode != "oauth" || policy == nil {
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	resource, warnings, err := wellknown.DiscoverProtectedResourceMetadata(ctx, policy, endpoint)
	if err != nil {
		return result
	}
	// Multiple issuers do not establish which registration strategy an installer will select.
	if len(warnings) != 0 || len(resource.AuthorizationServers) != 1 {
		result.Status = "partial"
		return result
	}
	issuer := resource.AuthorizationServers[0]
	metadata, err := remotesessions.DiscoverIssuerMetadata(ctx, policy, issuer)
	if err != nil || metadata.UnreadableURL != "" || metadata.Issuer != issuer || metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" {
		result.Status = "partial"
		return result
	}
	result.SupportsDCR = metadata.RegistrationEndpoint != ""
	result.SupportsCIMD = remotesessions.SupportsClientIDMetadataDocument(metadata.ClientIDMetadataDocumentSupported, metadata.TokenEndpointAuthMethodsSupported)
	result.Status = "complete"
	result.AuthSetup = "manual"
	if result.SupportsDCR {
		result.AuthSetup = "dcr"
	} else if result.SupportsCIMD {
		result.AuthSetup = "cimd"
	}
	return result
}

// Valid rejects unbounded or contradictory capability projections.
func (d Discovery) Valid() bool {
	switch d.Status {
	case "not_required":
		return d.AuthSetup == "none" && !d.SupportsDCR && !d.SupportsCIMD
	case "partial", "unavailable":
		return d.AuthSetup == "unknown" && !d.SupportsDCR && !d.SupportsCIMD
	case "complete":
		if d.SupportsDCR {
			return d.AuthSetup == "dcr"
		}
		if d.SupportsCIMD {
			return d.AuthSetup == "cimd"
		}
		return d.AuthSetup == "manual"
	default:
		return false
	}
}
