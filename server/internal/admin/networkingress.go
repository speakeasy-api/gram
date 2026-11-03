package admin

import (
	"context"
	"errors"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// SetNetworkIngressGatewayMigrator enables the staff tools that move an
// organization's private network ingress onto the tailnet gateway. Without
// it those endpoints report the feature as unavailable.
func (s *Service) SetNetworkIngressGatewayMigrator(migrator *networkingress.GatewayMigrator) {
	s.networkIngressGateway = migrator
}

func (s *Service) GetOrganizationNetworkIngress(ctx context.Context, payload *gen.GetOrganizationNetworkIngressPayload) (*gen.AdminOrganizationNetworkIngress, error) {
	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	if s.networkIngressGateway == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "tailnet gateway migration is not configured on this admin server")
	}

	state, err := s.networkIngressGateway.State(ctx, organizationID)
	if errors.Is(err, networkingress.ErrGatewayIngressNotFound) {
		return &gen.AdminOrganizationNetworkIngress{OrganizationID: organizationID, GatewayTag: s.networkIngressGateway.Tag(), Ingress: nil}, nil
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load network ingress").LogError(ctx, s.logger, attr.SlogOrganizationID(organizationID))
	}

	return s.networkIngressResult(organizationID, state), nil
}

func (s *Service) MoveOrganizationNetworkIngressToGateway(ctx context.Context, payload *gen.MoveOrganizationNetworkIngressToGatewayPayload) (*gen.AdminOrganizationNetworkIngress, error) {
	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	if s.networkIngressGateway == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "tailnet gateway migration is not configured on this admin server")
	}

	actor, displayName, _ := adminActor(ctx)
	state, err := s.networkIngressGateway.MoveToGateway(ctx, organizationID, actor, displayName)
	switch {
	case errors.Is(err, networkingress.ErrGatewayIngressNotFound):
		return nil, oops.E(oops.CodeNotFound, err, "this organization has no private network ingress to move")
	case errors.Is(err, networkingress.ErrGatewayCredentialsRejected):
		return nil, oops.E(oops.CodeBadRequest, err, "Tailscale refused to mint a %s key with the saved OAuth client. Ask the customer to give the client that tag and the Auth Keys, Devices Core and Services write scopes, then rotate it in their dashboard.", s.networkIngressGateway.Tag()).LogWarn(ctx, s.logger)
	case errors.Is(err, networkingress.ErrGatewayCredentialsMissing):
		return nil, oops.E(oops.CodeBadRequest, err, "the saved Tailscale credentials are missing or unreadable; ask the customer to rotate them").LogWarn(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "move network ingress to the tailnet gateway").LogError(ctx, s.logger, attr.SlogOrganizationID(organizationID))
	}

	return s.networkIngressResult(organizationID, state), nil
}

func (s *Service) networkIngressResult(organizationID string, state networkingress.GatewayIngressState) *gen.AdminOrganizationNetworkIngress {
	ingress := state.Ingress
	nodes := make([]*gen.AdminNetworkIngressGatewayNode, 0, len(state.Nodes))
	for _, node := range state.Nodes {
		nodes = append(nodes, &gen.AdminNetworkIngressGatewayNode{
			Slot:           node.Slot,
			Status:         node.Status,
			PodName:        conv.PtrEmpty(node.PodName),
			LastError:      conv.PtrEmpty(node.LastError),
			LastSeenAt:     formatOptionalTime(node.LastSeenAt),
			ConnectedSince: formatOptionalTime(node.ConnectedSince),
			Retired:        node.Retired,
		})
	}

	return &gen.AdminOrganizationNetworkIngress{
		OrganizationID: organizationID,
		GatewayTag:     s.networkIngressGateway.Tag(),
		Ingress: &gen.AdminNetworkIngress{
			ID:                    ingress.ID.String(),
			Provider:              ingress.Provider,
			Hostname:              ingress.Hostname,
			DNSName:               conv.FromPGText[string](ingress.DnsName),
			Runtime:               state.Runtime,
			Status:                ingress.Status,
			LastError:             conv.FromPGText[string](ingress.LastError),
			Enabled:               ingress.Enabled,
			CredentialsConfigured: ingress.CredentialsEncrypted.Valid && ingress.CredentialsEncrypted.String != "",
			HealthCheckedAt:       formatOptionalTime(ingress.HealthCheckedAt.Time),
			ConnectedSince:        formatOptionalTime(ingress.ConnectedSince.Time),
			CreatedAt:             ingress.CreatedAt.Time.UTC().Format(time.RFC3339),
			GatewayNodes:          nodes,
		},
	}
}

func formatOptionalTime(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	return new(value.UTC().Format(time.RFC3339))
}
