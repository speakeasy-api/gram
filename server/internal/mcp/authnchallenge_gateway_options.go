package mcp

import (
	"context"
	"encoding/json"

	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// gatewayDiscoveryOptionsOffered reports whether a connection to endpoint can
// choose its own discovery mode: stored gateways and the organization gateway.
// Agent gateways are ungated and never reach a consent page.
func gatewayDiscoveryOptionsOffered(endpoint *ResolvedMcpEndpoint) bool {
	return endpoint.MetaMcpServerID.Valid
}

// consentGatewayPolicy validates a gateway connection's discovery mode.
// Browser approval attaches the captured catalog after publication.
func (s *Service) consentGatewayPolicy(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, selectedAgentID, modeValue string) (*toolfilter.SessionPolicy, error) {
	if modeValue == "" {
		return nil, nil
	}
	var discoveryMode *metamcp.DiscoveryMode
	if modeValue != "" {
		mode := metamcp.DiscoveryMode(modeValue)
		if state.FirstParty || selectedAgentID != "" || !mode.Valid() || !gatewayDiscoveryOptionsOffered(endpoint) {
			return nil, oops.E(oops.CodeBadRequest, nil, "discovery mode is not available for this connection")
		}
		discoveryMode = &mode
	}
	policy := &toolfilter.SessionPolicy{
		Resource:  endpointToolSelectionResource(endpoint),
		Selection: nil,
		Gateway:   &toolfilter.GatewayOptions{DiscoveryMode: discoveryMode},
		Catalog:   nil,
	}
	if _, err := json.Marshal(policy); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid gateway connection options")
	}
	return policy, nil
}

// legacyToolSelection keeps a selection on the grant only when no gateway
// policy carries it, so a code never holds two policies.
func legacyToolSelection(selection *toolfilter.SessionSelection, policy *toolfilter.SessionPolicy) *toolfilter.SessionSelection {
	if policy != nil {
		return nil
	}
	return selection
}
