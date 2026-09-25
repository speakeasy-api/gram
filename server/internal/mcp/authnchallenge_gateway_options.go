package mcp

import (
	"context"
	"encoding/json"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// gatewayDiscoveryOptionsEnabled gates new settings only. Runtime policy
// parsing and enforcement do not depend on this rollout flag.
func (s *Service) gatewayDiscoveryOptionsEnabled(ctx context.Context, endpoint *ResolvedMcpEndpoint) bool {
	if !endpoint.MetaMcpServerID.Valid {
		return false
	}
	enabled, err := s.features.IsFlagEnabled(ctx, feature.FlagGatewayDiscoveryModes, endpoint.OrganizationID, feature.OrgProjectGroups(endpoint.OrganizationID, endpoint.ProjectID.String()))
	return err == nil && enabled
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
		Gateway:   &toolfilter.GatewayOptions{DiscoveryMode: &mode},
	}
	if _, err := json.Marshal(policy); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid gateway connection options")
	}
	return policy, nil
}
