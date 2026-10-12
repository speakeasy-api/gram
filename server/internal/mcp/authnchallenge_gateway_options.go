package mcp

import (
	"context"
	"encoding/json"

	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// gatewayDiscoveryOptionsOffered reports whether a connection to endpoint can
// choose its own discovery mode: stored gateways only.
func gatewayDiscoveryOptionsOffered(endpoint *ResolvedMcpEndpoint) bool {
	return endpoint.MetaMcpServerID.Valid && !endpoint.IsOrganizationGateway()
}

func (s *Service) consentGatewayPolicy(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, selectedAgentID, modeValue string, selection *toolfilter.SessionSelection) (*toolfilter.SessionPolicy, error) {
	if modeValue == "" {
		return nil, nil
	}
	mode := metamcp.DiscoveryMode(modeValue)
	if state.FirstParty || selectedAgentID != "" || !mode.Valid() || !gatewayDiscoveryOptionsOffered(endpoint) {
		return nil, oops.E(oops.CodeBadRequest, nil, "discovery mode is not available for this connection")
	}
	policy := &toolfilter.SessionPolicy{
		Resource:  endpointToolSelectionResource(endpoint),
		Selection: selection,
		Gateway:   &toolfilter.GatewayOptions{DiscoveryMode: &mode},
	}
	if _, err := json.Marshal(policy); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid gateway connection options")
	}
	return policy, nil
}
