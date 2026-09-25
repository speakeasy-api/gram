package mcp

import (
	"context"
	"encoding/json"

	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// gatewayDiscoveryOptionsEnabled gates new settings only. Runtime policy
// parsing and enforcement do not depend on this product feature.
func (s *Service) gatewayDiscoveryOptionsEnabled(ctx context.Context, endpoint *ResolvedMcpEndpoint) bool {
	return s.gatewayConnectionOptionsEnabled(ctx, endpoint, feature.FlagGatewayDiscoveryModes)
}

func (s *Service) gatewayConnectionOptionsEnabled(ctx context.Context, endpoint *ResolvedMcpEndpoint, flag feature.Flag) bool {
	if !endpoint.MetaMcpServerID.Valid {
		return false
	}
	return s.platformFeatureChecker != nil && s.platformFeatureChecker(ctx, endpoint.OrganizationID, string(productfeatures.FeatureGatewayDiscoveryModes))
}

func (s *Service) consentGatewayPolicy(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, selectedAgentID, modeValue string, selection *toolfilter.SessionSelection) (*toolfilter.SessionPolicy, error) {
	if modeValue == "" {
		return nil, nil
	}
	mode := metamcp.DiscoveryMode(modeValue)
	if state.FirstParty || selectedAgentID != "" || !mode.Valid() || !s.gatewayDiscoveryOptionsEnabled(ctx, endpoint) {
		return nil, oops.E(oops.CodeBadRequest, nil, "discovery mode is not available for this connection")
	}
	policy := &toolfilter.SessionPolicy{
		Resource:  endpointToolSelectionResource(endpoint),
		Selection: selection,
		Gateway:   &toolfilter.GatewayOptions{Frozen: nil, DiscoveryMode: &mode},
	}
	if _, err := json.Marshal(policy); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid gateway connection options")
	}
	return policy, nil
}
