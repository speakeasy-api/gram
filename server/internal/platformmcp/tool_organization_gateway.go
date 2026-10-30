//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// Administrator assignment review already has list_access_members,
// get_member_mcp_access, get_mcp_access, and the access-role mutation tools.
// Activation stays in the dashboard's review flow: there is no durable review
// proposal for an MCP mutation to confirm. This discovery tool shares the
// live opt-in gate; organization_gateway_integration_test proves its refusal.
const getMyOrganizationGatewayName = "get_my_organization_gateway"

const getMyOrganizationGatewayDescription = "Get the one MCP URL that gives you every MCP server in your organization you are allowed to reach, across all projects, signed in with your organization account. " +
	"Returns available=true with the url to add to an MCP client, or available=false with unavailable_reason and the next action. It names no servers: once connected, the tool discovery mode chosen at sign-in decides how the gateway exposes the servers your access reaches. " +
	"Progressive offers list_servers, which lists those servers by slug, prefixed with the project only when two projects share a slug, then describe_server, describe_tools and execute_tool. Direct lists each tool in tools/list as server--tool. Code Mode offers a single execute tool that runs Python to search, describe and call those tools. " +
	"For a connection limited to one project, use get_my_project_gateway instead. No token, credential, or other member's access is returned."

// organizationGatewayNextAction gives each tool discovery mode its own first
// step: only Progressive exposes list_servers.
const organizationGatewayNextAction = "Add url to your MCP client as a remote server and sign in with your organization account when prompted, choosing a tool discovery mode. " +
	"Then follow the mode you chose. Progressive: call list_servers, then describe_server, describe_tools and execute_tool. Direct: call the server--tool tools listed by tools/list. Code Mode: run Python with the execute tool."

// getMyOrganizationGatewayMeta is shared by the live and unavailable
// registrations. The gateway is the caller's own connection target, so only
// a person's external client asks for it; it is organization-wide, so no
// project is named.
var getMyOrganizationGatewayMeta = ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeNone}

// GetMyOrganizationGatewayOutput is the caller's organization gateway, or why
// there is none.
type GetMyOrganizationGatewayOutput struct {
	// Available reports whether the organization is served a gateway.
	Available bool `json:"available"`

	// URL is the gateway's MCP URL, set only when Available.
	URL string `json:"url,omitempty"`

	// UnavailableReason is no_organization_issuer,
	// multiple_organization_issuers, custom_domain_only, turned_off, or
	// organization_disabled, set only when not Available.
	UnavailableReason string `json:"unavailable_reason,omitempty"`

	// NextAction tells the caller what to do with the answer.
	NextAction string `json:"next_action"`
}

// OrganizationGatewayService answers get_my_organization_gateway.
type OrganizationGatewayService struct {
	db        *pgxpool.Pool
	serverURL *url.URL

	// orgHosts resolves the platform host stored as an organization's default
	// host. Nil URLs follow the request's platform origin.
	orgHosts *orghost.Resolver
}

// NewOrganizationGatewayService returns a service that builds gateway URLs
// under serverURL.
func NewOrganizationGatewayService(db *pgxpool.Pool, serverURL *url.URL) *OrganizationGatewayService {
	return &OrganizationGatewayService{db: db, serverURL: serverURL, orgHosts: nil}
}

// WithOrganizationHosts makes gateway URLs use the platform host stored as the
// organization's default host, the host the dashboard serves it on.
func (s *OrganizationGatewayService) WithOrganizationHosts(resolver *orghost.Resolver) *OrganizationGatewayService {
	if s != nil {
		s.orgHosts = resolver
	}
	return s
}

func (s *OrganizationGatewayService) valid() bool {
	return s != nil && s.db != nil && s.serverURL != nil
}

// Get returns the principal's organization gateway.
func (s *OrganizationGatewayService) Get(ctx context.Context, principal Principal) (GetMyOrganizationGatewayOutput, error) {
	return s.get(ctx, principal, orggateway.Availability)
}

// getForPlatformMCP is Get as Platform MCP's own use of the gateway sees it:
// on unless an administrator turned it off explicitly.
func (s *OrganizationGatewayService) getForPlatformMCP(ctx context.Context, principal Principal) (GetMyOrganizationGatewayOutput, error) {
	return s.get(ctx, principal, orggateway.PlatformAvailability)
}

func (s *OrganizationGatewayService) get(ctx context.Context, principal Principal, availability func(context.Context, usersessionsrepo.DBTX, repo.OrganizationMetadatum) (uuid.UUID, orggateway.Unavailability, error)) (GetMyOrganizationGatewayOutput, error) {
	organization, err := repo.New(s.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return GetMyOrganizationGatewayOutput{}, ErrUnavailable
	case err != nil:
		return GetMyOrganizationGatewayOutput{}, fmt.Errorf("load organization: %w", err)
	}

	_, unavailable, err := availability(ctx, s.db, organization)
	if err != nil {
		return GetMyOrganizationGatewayOutput{}, fmt.Errorf("resolve organization gateway: %w", err)
	}
	switch unavailable {
	case orggateway.UnavailabilityNone:
		baseURL, _ := organizationBaseURLs(ctx, s.orgHosts, organization.DefaultHost, s.serverURL, nil)
		return GetMyOrganizationGatewayOutput{
			Available:         true,
			URL:               orggateway.URL(baseURL, organization.Slug),
			UnavailableReason: "",
			NextAction:        organizationGatewayNextAction,
		}, nil
	case orggateway.UnavailabilityNoOrganizationIssuer:
		return unavailableOrganizationGateway(unavailable, "Ask an organization administrator to set up an organization-wide authorization server."), nil
	case orggateway.UnavailabilityMultipleOrganizationIssuers:
		return unavailableOrganizationGateway(unavailable, "Ask an organization administrator to consolidate the organization-wide authorization servers into one."), nil
	case orggateway.UnavailabilityTurnedOff:
		return unavailableOrganizationGateway(unavailable, "Organization Gateway is off by default. Ask an organization administrator to open the Organization Gateway card on the MCP Servers page, review MCP Assignments, and enable it."), nil
	case orggateway.UnavailabilityCustomDomainOnly:
		return unavailableOrganizationGateway(unavailable, "Your organization only allows its MCP servers to be reached through its own custom domain, where the gateway is not offered. Connect to each MCP server's address on that domain."), nil
	default:
		return unavailableOrganizationGateway(unavailable, "This organization is disabled. Contact your organization administrator."), nil
	}
}

func unavailableOrganizationGateway(reason orggateway.Unavailability, nextAction string) GetMyOrganizationGatewayOutput {
	return GetMyOrganizationGatewayOutput{Available: false, URL: "", UnavailableReason: string(reason), NextAction: nextAction}
}

func registerOrganizationGatewayTool(reg *Registrar, service *OrganizationGatewayService) {
	registerProjectGatewayTool(reg, service)
	if !service.valid() {
		addTool(reg, &mcp.Tool{
			Name:        getMyOrganizationGatewayName,
			Title:       "Get My Organization Gateway",
			Description: "Get your organization's MCP gateway URL. This is not switched on for your organization yet.",
			Annotations: readOnlyAnnotations(),
		}, getMyOrganizationGatewayMeta, unavailableTool("organization_gateway"))
		return
	}
	addTool(reg, &mcp.Tool{
		Name:        getMyOrganizationGatewayName,
		Title:       "Get My Organization Gateway",
		Description: getMyOrganizationGatewayDescription,
		Annotations: readOnlyAnnotations(),
	}, getMyOrganizationGatewayMeta, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, GetMyOrganizationGatewayOutput, error) {
		return principalToolCall(ctx, func(error) (*mcp.CallToolResult, bool) { return nil, false }, func(principal Principal) (GetMyOrganizationGatewayOutput, error) {
			return service.Get(ctx, principal)
		})
	})
}

// GetMyProjectGatewayInput selects one exact project in the caller's organization.
type GetMyProjectGatewayInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Exact slug of a project in your organization; required, with no default project fallback"`
}

// GetProject returns the same organization-authorized connection limited to one
// live project. Missing, deleted, and foreign projects have the same refusal.
func (s *OrganizationGatewayService) GetProject(ctx context.Context, principal Principal, input GetMyProjectGatewayInput) (GetMyOrganizationGatewayOutput, error) {
	if input.ProjectSlug == "" {
		return GetMyOrganizationGatewayOutput{}, &ToolRefusalError{Code: "invalid_request", Payload: `{"code":"invalid_request","message":"project_slug is required"}`}
	}
	grants, ok := authz.GrantsFromContext(ctx)
	if !ok {
		return GetMyOrganizationGatewayOutput{}, ErrUnavailable
	}
	project, err := projectsrepo.New(s.db).GetProjectBySlug(ctx, projectsrepo.GetProjectBySlugParams{
		Slug: input.ProjectSlug, OrganizationID: principal.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return GetMyOrganizationGatewayOutput{}, ErrUnavailable
	}
	if err != nil {
		return GetMyOrganizationGatewayOutput{}, fmt.Errorf("load project: %w", err)
	}
	// Match ResolveProjectRead's exact project:read check using the live grants
	// loaded by external admission. Refuse hidden projects like absent projects.
	allowed, err := authz.GrantsAuthorize(grants, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: project.ID.String(), Dimensions: nil})
	if err != nil {
		return GetMyOrganizationGatewayOutput{}, fmt.Errorf("authorize project gateway discovery: %w", err)
	}
	if !allowed {
		return GetMyOrganizationGatewayOutput{}, ErrUnavailable
	}
	output, err := s.Get(ctx, principal)
	if err != nil || !output.Available {
		return output, err
	}
	gatewayURL, err := url.Parse(output.URL)
	if err != nil {
		return GetMyOrganizationGatewayOutput{}, fmt.Errorf("parse gateway URL: %w", err)
	}
	output.URL = gatewayURL.JoinPath("projects", project.Slug).String()
	return output, nil
}

func registerProjectGatewayTool(reg *Registrar, service *OrganizationGatewayService) {
	// A separate explicit-project tool preserves the organization tool's no-input
	// behavior without pretending that an omitted selector means Default project.
	addTool(reg, &mcp.Tool{
		Name:        "get_my_project_gateway",
		Title:       "Get My Project Gateway",
		Description: "Get one MCP URL for the servers you are allowed to reach in an exact project. Requires project_slug. Uses the organization's gateway availability and organization sign-in, but lists and calls servers only in that project. Add the returned url to your MCP client and sign in with your organization account. Returns available=false and next_action when the organization gateway is unavailable. No tokens, credentials, server names, or other members' access are returned. For all projects, use get_my_organization_gateway.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMyProjectGatewayInput) (*mcp.CallToolResult, GetMyOrganizationGatewayOutput, error) {
		return principalToolCall(ctx, func(error) (*mcp.CallToolResult, bool) { return nil, false }, func(principal Principal) (GetMyOrganizationGatewayOutput, error) {
			if !service.valid() {
				payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: "organization_gateway", Message: "This is not switched on for your organization yet."})
				if err != nil {
					return GetMyOrganizationGatewayOutput{}, fmt.Errorf("encode gateway unavailable result: %w", err)
				}
				return GetMyOrganizationGatewayOutput{}, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
			}
			return service.GetProject(ctx, principal, input)
		})
	})
}
