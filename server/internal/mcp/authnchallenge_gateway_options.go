package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
)

// gatewayDiscoveryOptionsOffered reports whether a connection to endpoint can
// choose its own discovery mode: stored gateways and the organization gateway.
// Agent gateways are ungated and never reach a consent page.
func gatewayDiscoveryOptionsOffered(endpoint *ResolvedMcpEndpoint) bool {
	return endpoint.MetaMcpServerID.Valid
}

// consentGatewayPolicy records the discovery mode a gateway connection is
// approved with. "Gateway default" (an empty mode) records the gateway's
// current default, so a later change to that default reaches the connection
// only when it reconnects: clients cache tools/list and the gateway advertises
// no listChanged. First-party and agent approvals carry no options. Browser
// approval attaches the captured catalog after publication.
func (s *Service) consentGatewayPolicy(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, selectedAgentID, modeValue string) (*toolfilter.SessionPolicy, error) {
	offered := !state.FirstParty && selectedAgentID == "" && gatewayDiscoveryOptionsOffered(endpoint)
	var mode metamcp.DiscoveryMode
	if modeValue == "" {
		if !offered {
			return nil, nil
		}
		// A Code Mode default is recorded even while Code Mode is unavailable;
		// serving falls back to Progressive until it returns.
		resolved, err := s.gatewayDefaultDiscoveryMode(ctx, endpoint)
		if err != nil {
			return nil, oops.E(oops.CodeUnavailable, err, "could not read the gateway's discovery mode; retry shortly").LogError(ctx, s.logger)
		}
		mode = resolved
	} else {
		mode = metamcp.DiscoveryMode(modeValue)
		if !offered || !mode.Valid() {
			return nil, oops.E(oops.CodeBadRequest, nil, "discovery mode is not available for this connection")
		}
		if mode == metamcp.DiscoveryModeCode && !s.codeExecutor.Available() {
			return nil, oops.E(oops.CodeUnavailable, nil, "gateway code mode is temporarily unavailable; retry shortly")
		}
		if mode == metamcp.DiscoveryModeCode && !s.CodeModeAvailable(ctx, endpoint.OrganizationID) {
			return nil, oops.E(oops.CodeForbidden, nil, "gateway code mode is not available")
		}
	}
	policy := &toolfilter.SessionPolicy{
		Resource:  endpointToolSelectionResource(endpoint),
		Selection: nil,
		Gateway:   &toolfilter.GatewayOptions{DiscoveryMode: &mode},
		Catalog:   nil,
	}
	if _, err := json.Marshal(policy); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid gateway connection options")
	}
	return policy, nil
}

// gatewayDefaultDiscoveryMode returns the discovery mode a gateway offers
// connections that do not choose one: a stored gateway's own default, or the
// organization gateway's default from its organization.
func (s *Service) gatewayDefaultDiscoveryMode(ctx context.Context, endpoint *ResolvedMcpEndpoint) (metamcp.DiscoveryMode, error) {
	if endpoint.IsOrganizationGateway() {
		organization, err := s.orgsRepo.GetOrganizationMetadata(ctx, endpoint.OrganizationID)
		if err != nil {
			return "", fmt.Errorf("read organization gateway discovery mode: %w", err)
		}
		return orggateway.DiscoveryMode(organization), nil
	}
	metaServer, err := metamcprepo.New(s.db).GetMetaMCPServer(ctx, metamcprepo.GetMetaMCPServerParams{
		ID: endpoint.MetaMcpServerID.UUID, ProjectID: endpoint.ProjectID, OrganizationID: endpoint.OrganizationID,
	})
	if err != nil {
		return "", fmt.Errorf("read gateway discovery mode: %w", err)
	}
	return metamcp.ResolveDiscoveryMode(metaServer.DiscoveryMode), nil
}

// legacyToolSelection keeps a selection on the grant only when no gateway
// policy carries it, so a code never holds two policies.
func legacyToolSelection(selection *toolfilter.SessionSelection, policy *toolfilter.SessionPolicy) *toolfilter.SessionSelection {
	if policy != nil {
		return nil
	}
	return selection
}
