package telemetry

import (
	"context"

	"github.com/google/uuid"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// organizationGatewayScope is the telemetry an organization's gateway
// reports: rows stamped with its derived gateway id in any of the
// organization's projects. Discovery rows carry no project, so the nil
// project is included; the gateway id keeps them to this organization.
func (s *Service) organizationGatewayScope(ctx context.Context, from, to string) (orgQueryScope, string, error) {
	scope, err := s.resolveOrgQueryScope(ctx, from, to, nil)
	if err != nil {
		return scope, "", err
	}
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	scope.projectIDs = append(scope.projectIDs, uuid.Nil.String())
	return scope, orggateway.GatewayID(authCtx.ActiveOrganizationID).String(), nil
}

// GetOrganizationGatewayOverview is GetObservabilityOverview for calls
// dispatched through the caller's organization gateway.
func (s *Service) GetOrganizationGatewayOverview(ctx context.Context, payload *telem_gen.GetOrganizationGatewayOverviewPayload) (*telem_gen.GetObservabilityOverviewResult, error) {
	scope, gatewayID, err := s.organizationGatewayScope(ctx, payload.From, payload.To)
	if err != nil {
		return nil, err
	}

	return s.observabilityOverview(ctx, repo.GetOverviewSummaryParams{
		ExcludedHookSources: nil,
		GramProjectIDs:      scope.projectIDs,
		TimeStart:           scope.timeStart,
		TimeEnd:             scope.timeEnd,
		User:                repo.UserIdentity{UserIDs: nil, Emails: nil},
		CanonicalUser:       repo.CanonicalUserIdentity{OrgID: "", UserID: "", EmailLower: ""},
		ExternalUserID:      "",
		APIKeyID:            "",
		ToolsetSlug:         "",
		ToolsetSlugServerID: "",
		RemoteMCPServerID:   "",
		MCPServerID:         "",
		MetaMCPServerID:     gatewayID,
		EventSource:         "",
		HookSource:          "",
		AccountType:         "",
		ExternalOrgID:       "",
	}, true)
}

// GetOrganizationGatewayUsage is GetMetaMcpServerUsage for the caller's
// organization gateway. Its servers are every live server in the
// organization, named here because they span projects.
func (s *Service) GetOrganizationGatewayUsage(ctx context.Context, payload *telem_gen.GetOrganizationGatewayUsagePayload) (*telem_gen.GetMetaMcpServerUsageResult, error) {
	scope, gatewayID, err := s.organizationGatewayScope(ctx, payload.From, payload.To)
	if err != nil {
		return nil, err
	}
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	usage, err := s.chRepo.GetMetaMCPServerUsage(ctx, repo.GetMetaMCPServerUsageParams{
		GramProjectIDs:  scope.projectIDs,
		MetaMCPServerID: gatewayID,
		TimeStart:       scope.timeStart,
		TimeEnd:         scope.timeEnd,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error fetching organization gateway usage")
	}

	servers, err := mcpserversRepo.New(s.db).ListMCPServersByOrganizationID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing organization mcp servers")
	}
	names := make(map[string]string, len(servers))
	for _, server := range servers {
		names[server.ID.String()] = conv.Default(server.Name.String, server.Slug.String)
	}

	return metaMCPUsageResult(usage, names), nil
}
