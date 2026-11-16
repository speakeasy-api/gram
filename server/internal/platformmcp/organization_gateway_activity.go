package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/authz"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// ErrProjectGatewayActivityProjectInvalid and
// ErrProjectGatewayActivityProjectNotFound refuse a project selection that is
// malformed or names no live project of the organization.
var (
	ErrProjectGatewayActivityProjectInvalid  = errors.New("platform mcp project gateway activity project invalid")
	ErrProjectGatewayActivityProjectNotFound = errors.New("platform mcp project gateway activity project not found")
)

// ErrOrganizationGatewayActivityUnavailable marks a deployment that cannot
// serve the read, kept apart from authorization and budget refusals.
var ErrOrganizationGatewayActivityUnavailable = errors.New("platform mcp organization gateway activity unavailable")

const organizationGatewayActivityFeature = "organization_gateway_activity"

// OrganizationGatewayActivityReader is the ClickHouse read model the activity
// read needs: the overview summary and the gateway's discovery funnel and
// per-server breakdown, scoped to a set of projects.
type OrganizationGatewayActivityReader interface {
	GetOverviewSummary(ctx context.Context, arg telemetryrepo.GetOverviewSummaryParams) (*telemetryrepo.OverviewSummary, error)
	GetMetaMCPServerUsage(ctx context.Context, arg telemetryrepo.GetMetaMCPServerUsageParams) (telemetryrepo.MetaMCPUsage, error)
	GetTelemetryWatermark(ctx context.Context, arg telemetryrepo.GetTelemetryWatermarkParams) (int64, error)
}

// OrganizationGatewayActivityService reports calls dispatched through the
// organization gateway across every project in the organization, as the
// dashboard's Organization Gateway Activity tab does.
type OrganizationGatewayActivityService struct {
	db         *pgxpool.Pool
	telemetry  OrganizationGatewayActivityReader
	authorizer deviceCoverageAuthorizer
	budget     OperationBudget
	now        func() time.Time
}

func NewOrganizationGatewayActivityService(db *pgxpool.Pool, telemetry OrganizationGatewayActivityReader, authorizer deviceCoverageAuthorizer, budget OperationBudget) *OrganizationGatewayActivityService {
	if db == nil || telemetry == nil || authorizer == nil || !budget.valid() {
		return nil
	}
	return &OrganizationGatewayActivityService{db: db, telemetry: telemetry, authorizer: authorizer, budget: budget, now: time.Now}
}

func (s *OrganizationGatewayActivityService) valid() bool {
	return s != nil && s.db != nil && s.telemetry != nil && s.authorizer != nil && s.budget.valid()
}

type GetOrganizationGatewayActivityInput struct {
	Window string `json:"window,omitempty" jsonschema:"observation window: 1h, 24h (default), 7d, or 30d"`
	// ProjectID selects one project's Project Gateway instead of the whole
	// Organization Gateway.
	ProjectID string `json:"project_id,omitempty" jsonschema:"exact project UUID to report that project's Project Gateway; omit for the Organization Gateway"`
}

// OrganizationGatewayDiscovery counts the discovery steps agents took before
// dispatching a call.
type OrganizationGatewayDiscovery struct {
	ListServers    int64 `json:"list_servers"`
	DescribeServer int64 `json:"describe_server"`
	DescribeTools  int64 `json:"describe_tools"`
}

// OrganizationGatewayServer is one MCP server's share of the dispatched calls.
type OrganizationGatewayServer struct {
	Name       string `json:"name"`
	MCPID      string `json:"mcp_id"`
	ProjectID  string `json:"project_id"`
	ToolCalls  int64  `json:"tool_calls"`
	ErrorCount int64  `json:"error_count"`
}

// GetOrganizationGatewayActivityOutput carries no users, clients, or tokens:
// it answers how much the gateway is used and where it fails.
type GetOrganizationGatewayActivityOutput struct {
	Envelope        DataEnvelope                 `json:"data"`
	DispatchedCalls int64                        `json:"dispatched_calls"`
	FailedCalls     int64                        `json:"failed_calls"`
	Discovery       OrganizationGatewayDiscovery `json:"discovery"`
	TopServers      []OrganizationGatewayServer  `json:"top_servers"`
}

func (s *OrganizationGatewayActivityService) Get(ctx context.Context, principal Principal, input GetOrganizationGatewayActivityInput) (GetOrganizationGatewayActivityOutput, error) {
	if !s.valid() || principal.OrganizationID == "" {
		return GetOrganizationGatewayActivityOutput{}, ErrOrganizationGatewayActivityUnavailable
	}
	var project *projectsrepo.Project
	if input.ProjectID != "" {
		id, err := uuid.Parse(input.ProjectID)
		if err != nil {
			return GetOrganizationGatewayActivityOutput{}, ErrProjectGatewayActivityProjectInvalid
		}
		// A project's gateway reads like the dashboard's Project Gateway tab,
		// which needs access to that project.
		if err := s.authorizer.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: id.String(), Dimensions: nil}); err != nil {
			return GetOrganizationGatewayActivityOutput{}, fmt.Errorf("require project read for project gateway activity: %w", err)
		}
		row, err := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: id, OrganizationID: principal.OrganizationID})
		if errors.Is(err, pgx.ErrNoRows) {
			return GetOrganizationGatewayActivityOutput{}, ErrProjectGatewayActivityProjectNotFound
		}
		if err != nil {
			return GetOrganizationGatewayActivityOutput{}, fmt.Errorf("read project gateway project: %w", err)
		}
		project = &row
	} else if err := s.authorizer.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: principal.OrganizationID, Dimensions: nil}); err != nil {
		return GetOrganizationGatewayActivityOutput{}, fmt.Errorf("require org read for organization gateway activity: %w", err)
	}
	now := s.now()
	window, err := resolveWindow(input.Window, now, overviewWindowSpec)
	if err != nil {
		return GetOrganizationGatewayActivityOutput{}, err
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return GetOrganizationGatewayActivityOutput{}, err
	}

	var projectIDs []string
	var gatewayID string
	if project != nil {
		projectIDs = []string{project.ID.String()}
		gatewayID = orggateway.ProjectGatewayID(principal.OrganizationID, project.ID).String()
	} else {
		projects, err := projectsrepo.New(s.db).ListProjectsByOrganization(ctx, principal.OrganizationID)
		if err != nil {
			return GetOrganizationGatewayActivityOutput{}, fmt.Errorf("list organization projects: %w", err)
		}
		projectIDs = make([]string, 0, len(projects)+1)
		for _, row := range projects {
			projectIDs = append(projectIDs, row.ID.String())
		}
		gatewayID = orggateway.GatewayID(principal.OrganizationID).String()
	}
	// Discovery rows carry no project; the gateway id keeps them to this
	// organization or project.
	projectIDs = append(projectIDs, uuid.Nil.String())
	start, end := window.start.UnixNano(), window.end.UnixNano()

	summary, err := s.telemetry.GetOverviewSummary(ctx, telemetryrepo.GetOverviewSummaryParams{
		ExcludedHookSources: nil,
		GramProjectIDs:      projectIDs,
		TimeStart:           start,
		TimeEnd:             end,
		User:                telemetryrepo.UserIdentity{UserIDs: nil, Emails: nil},
		CanonicalUser:       telemetryrepo.CanonicalUserIdentity{OrgID: "", UserID: "", EmailLower: ""},
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
	})
	if err != nil {
		return GetOrganizationGatewayActivityOutput{}, fmt.Errorf("read organization gateway summary: %w", err)
	}
	usage, err := s.telemetry.GetMetaMCPServerUsage(ctx, telemetryrepo.GetMetaMCPServerUsageParams{
		GramProjectIDs:  projectIDs,
		MetaMCPServerID: gatewayID,
		TimeStart:       start,
		TimeEnd:         end,
	})
	if err != nil {
		return GetOrganizationGatewayActivityOutput{}, fmt.Errorf("read organization gateway usage: %w", err)
	}
	servers, err := mcpserversrepo.New(s.db).ListMCPServersByOrganizationID(ctx, principal.OrganizationID)
	if err != nil {
		return GetOrganizationGatewayActivityOutput{}, fmt.Errorf("list organization mcp servers: %w", err)
	}
	watermark, err := s.telemetry.GetTelemetryWatermark(ctx, telemetryrepo.GetTelemetryWatermarkParams{GramProjectIDs: projectIDs})
	if err != nil {
		return GetOrganizationGatewayActivityOutput{}, fmt.Errorf("read organization gateway watermark: %w", err)
	}

	known := make(map[string]mcpserversrepo.McpServer, len(servers))
	for _, server := range servers {
		if project != nil && server.ProjectID != project.ID {
			continue
		}
		known[server.ID.String()] = server
	}
	// Usage is ordered most active first; servers no longer in the
	// organization still count toward the totals but are not named.
	top := make([]OrganizationGatewayServer, 0, maxOverviewServers)
	for _, row := range usage.Members {
		server, ok := known[row.McpServerID]
		if !ok {
			continue
		}
		if len(top) == maxOverviewServers {
			break
		}
		name := server.Name.String
		if name == "" {
			name = server.Slug.String
		}
		top = append(top, OrganizationGatewayServer{
			Name:       name,
			MCPID:      row.McpServerID,
			ProjectID:  server.ProjectID.String(),
			ToolCalls:  boundedCount(row.ToolCalls),
			ErrorCount: boundedCount(row.ErrorCount),
		})
	}

	discovery := OrganizationGatewayDiscovery{
		ListServers:    boundedCount(usage.Funnel.ListServers),
		DescribeServer: boundedCount(usage.Funnel.DescribeServer),
		DescribeTools:  boundedCount(usage.Funnel.DescribeTools),
	}
	var dispatched, failed int64
	if summary != nil {
		dispatched = boundedCount(summary.TotalToolCalls)
		failed = boundedCount(summary.FailedToolCalls)
	}
	observed := dispatched > 0 || discovery.ListServers > 0 || discovery.DescribeServer > 0 || discovery.DescribeTools > 0
	return GetOrganizationGatewayActivityOutput{
		Envelope:        newDataEnvelope(now, watermarkTime(watermark), window, observed),
		DispatchedCalls: dispatched,
		FailedCalls:     failed,
		Discovery:       discovery,
		TopServers:      top,
	}, nil
}
