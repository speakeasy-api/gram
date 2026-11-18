package telemetry

import (
	"context"

	"github.com/google/uuid"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/telemetryerrs"
)

// projectGatewayScope is the telemetry the selected project's gateway
// reports: rows stamped with its derived gateway id. Calls carry the project;
// discovery rows carry no project, so the nil project is included and the
// gateway id, unique to this project, keeps them to it.
type projectGatewayScope struct {
	projectID  uuid.UUID
	projectIDs []string
	gatewayID  string
	timeStart  int64
	timeEnd    int64
}

func (s *Service) resolveProjectGatewayScope(ctx context.Context, from, to string) (projectGatewayScope, error) {
	var scope projectGatewayScope
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil || authCtx.ActiveOrganizationID == "" {
		return scope, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return scope, err
	}
	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return scope, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}
	if !logsEnabled {
		return scope, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}
	scope.timeStart, scope.timeEnd, err = parseTimeRange(&from, &to)
	if err != nil {
		return scope, err
	}
	scope.projectID = *authCtx.ProjectID
	scope.projectIDs = []string{authCtx.ProjectID.String(), uuid.Nil.String()}
	scope.gatewayID = orggateway.ProjectGatewayID(authCtx.ActiveOrganizationID, *authCtx.ProjectID).String()
	return scope, nil
}

// GetProjectGatewayOverview is GetObservabilityOverview for calls dispatched
// through the selected project's gateway.
func (s *Service) GetProjectGatewayOverview(ctx context.Context, payload *telem_gen.GetProjectGatewayOverviewPayload) (*telem_gen.GetObservabilityOverviewResult, error) {
	scope, err := s.resolveProjectGatewayScope(ctx, payload.From, payload.To)
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
		MetaMCPServerID:     scope.gatewayID,
		EventSource:         "",
		HookSource:          "",
		AccountType:         "",
		ExternalOrgID:       "",
	}, true)
}

// GetProjectGatewayUsage is GetMetaMcpServerUsage for the selected project's
// gateway. Its servers are every live server in the project, named here
// because a derived gateway has no member rows.
func (s *Service) GetProjectGatewayUsage(ctx context.Context, payload *telem_gen.GetProjectGatewayUsagePayload) (*telem_gen.GetMetaMcpServerUsageResult, error) {
	scope, err := s.resolveProjectGatewayScope(ctx, payload.From, payload.To)
	if err != nil {
		return nil, err
	}

	usage, err := s.chRepo.GetMetaMCPServerUsage(ctx, repo.GetMetaMCPServerUsageParams{
		GramProjectIDs:  scope.projectIDs,
		MetaMCPServerID: scope.gatewayID,
		TimeStart:       scope.timeStart,
		TimeEnd:         scope.timeEnd,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error fetching project gateway usage")
	}

	servers, err := mcpserversRepo.New(s.db).ListMCPServersByProjectID(ctx, mcpserversRepo.ListMCPServersByProjectIDParams{
		ProjectID:            scope.projectID,
		RemoteMcpServerID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UnproxiedMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing project mcp servers")
	}
	names := make(map[string]string, len(servers))
	for _, server := range servers {
		names[server.ID.String()] = conv.Default(server.Name.String, server.Slug.String)
	}

	return metaMCPUsageResult(usage, names), nil
}
