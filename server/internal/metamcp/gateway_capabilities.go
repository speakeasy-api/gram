package metamcp

import (
	"context"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// Populate capabilities only on authorized API responses, after transaction
// completion. Audit snapshots keep the persisted configuration alone.
func (s *Service) withGatewayCapabilities(ctx context.Context, view *types.MetaMcpServer) *types.MetaMcpServer {
	enabled := s.productFeatures.PlatformFeatureCheck(ctx, view.OrganizationID, string(productfeatures.FeatureGatewayDiscoveryModes))
	view.DiscoveryModesEnabled = &enabled
	return view
}
