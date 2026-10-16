//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

const getMyChannelsAccessName = "get_my_channels_access"

const getMyChannelsAccessDescription = "Get the Channels endpoint for your organization and which Channels permissions you personally hold. " +
	"Channels is organization-scoped messaging between dashboard users and the agent instances of your organization's identities. " +
	"Returns the endpoint URL to add to an MCP client, whether you may connect an instance, administer Channels, and read every conversation, plus the next action. " +
	"It returns no messages, no conversations, no presence, no other member's permissions, and no credential: authorize against the returned endpoint to use Channels. " +
	"Your organization must also have Channels switched on, which this tool cannot see; the endpoint refuses every operation until it is."

// getMyChannelsAccessMeta mirrors get_my_organization_gateway: the answer is
// about the caller's own connection target, so only a person's external client
// asks for it, and Channels is organization-wide so no project is named.
//
// Discovery is open to every member. A member who holds no Channels
// permission is exactly who needs the no_channels_permission answer and its
// instruction to ask for access; the handler evaluates each permission itself.
var getMyChannelsAccessMeta = ToolMeta{
	Authorization: ExternalAuthorizationMember,
	Audiences:     externalOnly,
	ProjectScope:  ProjectScopeNone,
}

// GetMyChannelsAccessOutput is the caller's own Channels access.
type GetMyChannelsAccessOutput struct {
	// EndpointURL is the Channels MCP endpoint to authorize against. It is
	// returned even when the caller holds no permission, because the endpoint
	// is not a secret and naming it is how a caller asks an administrator for
	// the right thing.
	EndpointURL string `json:"endpoint_url"`

	// CanConnect reports whether the caller may connect a Channels instance
	// and take part in conversations.
	CanConnect bool `json:"can_connect"`

	// CanAdminister reports whether the caller may change Channels
	// availability and participation exceptions. It never implies posting into
	// another member's private conversation.
	CanAdminister bool `json:"can_administer"`

	// CanReadAllConversations reports whether the caller may observe private
	// conversations they do not take part in. Observation never grants posting.
	CanReadAllConversations bool `json:"can_read_all_conversations"`

	// UnavailableReason is no_channels_permission when the caller holds none
	// of the three, and empty otherwise.
	UnavailableReason string `json:"unavailable_reason,omitempty"`

	// NextAction tells the caller what to do with the answer.
	NextAction string `json:"next_action"`
}

// ChannelsAccessService answers get_my_channels_access.
type ChannelsAccessService struct {
	engine    *authz.Engine
	serverURL *url.URL
}

// NewChannelsAccessService returns a service that reports Channels access
// against engine and names the endpoint under serverURL.
func NewChannelsAccessService(engine *authz.Engine, serverURL *url.URL) *ChannelsAccessService {
	return &ChannelsAccessService{engine: engine, serverURL: serverURL}
}

func (s *ChannelsAccessService) valid() bool {
	return s != nil && s.engine != nil && s.serverURL != nil
}

// Get reports the principal's own Channels permissions.
//
// The three permissions are evaluated independently and in separate checks.
// None of them implies another, and none of them is implied by mcp:connect or
// org:admin membership, so a single combined check would quietly widen one of
// them. The evaluation reads the live grants PrepareExternalContext resolved
// for this request rather than anything carried on the token.
func (s *ChannelsAccessService) Get(ctx context.Context, principal Principal) (GetMyChannelsAccessOutput, error) {
	permissions := make([]bool, 0, 3)
	for _, scope := range []authz.Scope{authz.ScopeChannelsConnect, authz.ScopeChannelsAdmin, authz.ScopeChannelsReadAll} {
		allowed, err := s.engine.Evaluate(ctx, authz.Check{Scope: scope, ResourceID: principal.OrganizationID})
		if err != nil {
			return GetMyChannelsAccessOutput{}, fmt.Errorf("evaluate %s: %w", scope, err)
		}
		permissions = append(permissions, allowed)
	}
	output := GetMyChannelsAccessOutput{
		EndpointURL:             channelsResource(platformBaseURL(ctx, s.serverURL)),
		CanConnect:              permissions[0],
		CanAdminister:           permissions[1],
		CanReadAllConversations: permissions[2],
	}
	switch {
	case output.CanConnect:
		output.NextAction = "Add endpoint_url to your MCP client as a remote server and sign in with your organization account when prompted. Then call channels_connect through it."
	case output.CanAdminister:
		output.NextAction = "You may administer Channels but not take part in it. Change availability and participation in the dashboard, or with channels_admin after adding endpoint_url to your MCP client; ask an organization administrator for channels:connect to join conversations."
	case output.CanReadAllConversations:
		output.NextAction = "You may observe conversations but not take part in them. Ask an organization administrator for channels:connect to join."
	default:
		output.UnavailableReason = "no_channels_permission"
		output.NextAction = "Ask an organization administrator to grant you channels:connect in access management. Channels permissions are separate from MCP access and are not granted by it."
	}
	return output, nil
}

func registerChannelsAccessTool(reg *Registrar, service *ChannelsAccessService) {
	if !service.valid() {
		addTool(reg, &mcp.Tool{
			Name:        getMyChannelsAccessName,
			Title:       "Get My Channels Access",
			Description: "Get the Channels endpoint and your own Channels permissions. Channels is not available on this deployment yet.",
			Annotations: readOnlyAnnotations(),
		}, getMyChannelsAccessMeta, unavailableTool("channels"))
		return
	}
	addTool(reg, &mcp.Tool{
		Name:        getMyChannelsAccessName,
		Title:       "Get My Channels Access",
		Description: getMyChannelsAccessDescription,
		Annotations: readOnlyAnnotations(),
	}, getMyChannelsAccessMeta, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, GetMyChannelsAccessOutput, error) {
		return principalToolCall(ctx, func(error) (*mcp.CallToolResult, bool) { return nil, false }, func(principal Principal) (GetMyChannelsAccessOutput, error) {
			return service.Get(ctx, principal)
		})
	})
}
