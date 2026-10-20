//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
)

const getMyOrganizationGatewayName = "get_my_organization_gateway"

const getMyOrganizationGatewayDescription = "Get the one MCP URL that gives you every MCP server in your organization you are allowed to reach, across all projects, signed in with your organization account. " +
	"Returns available=true with the url to add to an MCP client, or available=false with unavailable_reason and the next action. It names no servers: once connected, the gateway's list_servers tool lists the servers your access reaches by slug, prefixed with the project only when two projects share a slug. A connection that chooses Direct at sign-in lists each tool as server--tool instead. " +
	"No token, credential, or other member's access is returned."

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
}

// NewOrganizationGatewayService returns a service that builds gateway URLs
// under serverURL.
func NewOrganizationGatewayService(db *pgxpool.Pool, serverURL *url.URL) *OrganizationGatewayService {
	return &OrganizationGatewayService{db: db, serverURL: serverURL}
}

func (s *OrganizationGatewayService) valid() bool {
	return s != nil && s.db != nil && s.serverURL != nil
}

// Get returns the principal's organization gateway.
func (s *OrganizationGatewayService) Get(ctx context.Context, principal Principal) (GetMyOrganizationGatewayOutput, error) {
	organization, err := repo.New(s.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return GetMyOrganizationGatewayOutput{}, ErrUnavailable
	case err != nil:
		return GetMyOrganizationGatewayOutput{}, fmt.Errorf("load organization: %w", err)
	}

	_, unavailable, err := orggateway.Availability(ctx, s.db, organization)
	if err != nil {
		return GetMyOrganizationGatewayOutput{}, fmt.Errorf("resolve organization gateway: %w", err)
	}
	switch unavailable {
	case orggateway.UnavailabilityNone:
		return GetMyOrganizationGatewayOutput{
			Available:         true,
			URL:               orggateway.URL(platformBaseURL(ctx, s.serverURL), organization.Slug),
			UnavailableReason: "",
			NextAction:        "Add url to your MCP client as a remote server and sign in with your organization account when prompted. Then call list_servers through it.",
		}, nil
	case orggateway.UnavailabilityNoOrganizationIssuer:
		return unavailableOrganizationGateway(unavailable, "Ask an organization administrator to set up an organization-wide authorization server."), nil
	case orggateway.UnavailabilityMultipleOrganizationIssuers:
		return unavailableOrganizationGateway(unavailable, "Ask an organization administrator to consolidate the organization-wide authorization servers into one."), nil
	case orggateway.UnavailabilityTurnedOff:
		return unavailableOrganizationGateway(unavailable, "An organization administrator turned the gateway off. Ask them to turn it on in organization settings under Gateway."), nil
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
