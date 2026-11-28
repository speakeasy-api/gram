package agentmanagement

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/feature"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

const (
	runtimeGatewayProjectRequired = "project_assignment_required"
	runtimeGatewayMachineDisabled = "machine_access_disabled"
)

// WithRuntimeGateway configures the server-side dependencies used to resolve
// an agent's runtime endpoint. Without both dependencies the lookup fails
// closed and never returns a URL.
func (s *Service) WithRuntimeGateway(flags feature.Provider, hosts *orghost.Resolver) *Service {
	s.runtimeGatewayFlags = flags
	s.runtimeGatewayHosts = hosts
	return s
}

// GetRuntimeGateway resolves the only endpoint an agent runtime may use. The
// authorization is deliberately the credential-management predicate used by
// key issuance, not project:read: the project is selected from the authorized
// agent record and cannot be supplied by the caller.
func (s *Service) GetRuntimeGateway(ctx context.Context, payload *gen.GetRuntimeGatewayPayload) (*gen.AgentRuntimeGateway, error) {
	agentID, err := parseAgentID(payload.AgentID)
	if err != nil {
		return nil, err
	}
	human, agent, err := s.authorizer.RequireAgent(ctx, s.db, agentID, OwnedAgentAuthorize)
	if err != nil {
		return nil, s.serviceError(ctx, err, "resolve agent runtime gateway")
	}
	if !agent.ProjectID.Valid {
		return unavailableRuntimeGateway(runtimeGatewayProjectRequired), nil
	}
	if s.runtimeGatewayFlags == nil || s.runtimeGatewayHosts == nil {
		return unavailableRuntimeGateway(runtimeGatewayMachineDisabled), nil
	}
	machineAccess, err := s.runtimeGatewayFlags.IsFlagEnabledLocal(ctx, feature.FlagProjectGatewayMachineAccess, agent.OrganizationID, nil, nil)
	if err != nil {
		return nil, s.serviceError(ctx, fmt.Errorf("evaluate project gateway machine access: %w", err), "resolve agent runtime gateway")
	}
	if !machineAccess {
		return unavailableRuntimeGateway(runtimeGatewayMachineDisabled), nil
	}

	project, err := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
		ID: agent.ProjectID.UUID, OrganizationID: human.Auth.ActiveOrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return unavailableRuntimeGateway(runtimeGatewayProjectRequired), nil
	}
	if err != nil {
		return nil, s.serviceError(ctx, err, "read agent project")
	}
	organization, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, human.Auth.ActiveOrganizationID)
	if err != nil {
		return nil, s.serviceError(ctx, err, "read agent organization")
	}
	lockdown, err := customdomains.OrganizationLockdown(ctx, s.db, organization.ID)
	if err != nil {
		return nil, s.serviceError(ctx, err, "resolve project gateway host")
	}
	_, unavailable, err := orggateway.Availability(ctx, s.db, organization)
	if err != nil {
		return nil, s.serviceError(ctx, err, "resolve project gateway availability")
	}
	if unavailable != orggateway.UnavailabilityNone {
		servedOnCustomDomain := unavailable == orggateway.UnavailabilityCustomDomainOnly && lockdown.ServingHost != ""
		if !servedOnCustomDomain {
			return unavailableRuntimeGateway(string(unavailable)), nil
		}
	}

	baseURL := s.runtimeGatewayHosts.ServerURL(organization.DefaultHost)
	if lockdown.ServingHost != "" {
		baseURL = &url.URL{Scheme: "https", Host: lockdown.ServingHost}
	}
	endpoint := orggateway.ProjectURL(baseURL, organization.Slug, project.Slug)
	return &gen.AgentRuntimeGateway{Available: true, URL: &endpoint, UnavailableReason: nil}, nil
}

func unavailableRuntimeGateway(reason string) *gen.AgentRuntimeGateway {
	return &gen.AgentRuntimeGateway{Available: false, URL: nil, UnavailableReason: &reason}
}
