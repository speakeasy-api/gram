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

// consentGatewayPolicy combines a gateway connection's discovery mode and
// tool selection into its versioned policy. Non-gateway selections keep the
// legacy document, so this returns nil for them.
func (s *Service) consentGatewayPolicy(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, selectedAgentID, modeValue string, selection *toolfilter.SessionSelection) (*toolfilter.SessionPolicy, error) {
	if !endpoint.MetaMcpServerID.Valid {
		selection = nil
	}
	if modeValue == "" && selection == nil {
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
		Selection: selection,
		Gateway:   &toolfilter.GatewayOptions{DiscoveryMode: discoveryMode},
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
