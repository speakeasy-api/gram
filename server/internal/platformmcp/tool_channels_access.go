//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/channels"
	"github.com/speakeasy-api/gram/server/internal/feature"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

const getMyChannelsAccessName = "get_my_channels_access"

const getMyChannelsAccessDescription = "Get the Channels endpoint for one explicit project and which Channels permissions you personally hold in that project. " +
	"Channels is project-scoped messaging between dashboard users and agent instances. Supply exactly one of project_id or project_slug; there is no default project fallback. Requires live project:read access to the selected project. " +
	"Returns the endpoint URL to add to an MCP client, whether you may connect an instance, administer Channels, and read every conversation in that project, plus the next action. " +
	"It returns no messages, no conversations, no presence, no other member's permissions, and no credential: authorize against the returned endpoint to use Channels. " +
	"Channels must be enabled for your organization; otherwise this tool returns channels_not_enabled without an endpoint."

// getMyChannelsAccessMeta reports the caller's own connection target for an
// explicit project, only to the person's external client.
//
// Discovery requires live project:read access. A member who holds no Channels
// permission is exactly who needs the no_channels_permission answer and its
// instruction to ask for access; the handler evaluates each permission itself.
var getMyChannelsAccessMeta = ToolMeta{
	Authorization: ExternalAuthorizationMember,
	Audiences:     externalOnly,
	ProjectScope:  ProjectScopeExplicit,
}

type GetMyChannelsAccessInput struct {
	ProjectID   string `json:"project_id,omitempty" jsonschema:"exact project ID; supply exactly one project selector"`
	ProjectSlug string `json:"project_slug,omitempty" jsonschema:"exact project slug; supply exactly one project selector, with no default project fallback"`
}

// GetMyChannelsAccessOutput is the caller's own Channels access.
type GetMyChannelsAccessOutput struct {
	// EndpointURL is the Channels MCP endpoint to authorize against. It is
	// returned when Channels is enabled even if the caller holds no permission, because the endpoint
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

	// UnavailableReason is channels_not_enabled when Channels is off for the
	// organization, or no_channels_permission when the caller holds none of
	// the three permissions, and empty otherwise.
	UnavailableReason string `json:"unavailable_reason,omitempty"`

	// NextAction tells the caller what to do with the answer.
	NextAction string `json:"next_action"`
}

// ChannelsAccessService answers get_my_channels_access.
type ChannelsAccessService struct {
	engine         *authz.Engine
	db             *pgxpool.Pool
	serverURL      *url.URL
	flags          feature.Provider
	resolveProject func(context.Context, string, FindMCPInput) (ResolvedProject, error)
}

// NewChannelsAccessService returns a service that reports Channels access
// against engine, applies access exceptions stored in db, and names the
// endpoint under serverURL.
func NewChannelsAccessService(logger *slog.Logger, engine *authz.Engine, db *pgxpool.Pool, serverURL *url.URL, flags feature.Provider) *ChannelsAccessService {
	return &ChannelsAccessService{engine: engine, db: db, serverURL: serverURL, flags: flags, resolveProject: NewPostgresReader(logger, db).resolveInventoryProject}
}

func (s *ChannelsAccessService) valid() bool {
	return s != nil && s.engine != nil && s.db != nil && s.serverURL != nil && s.resolveProject != nil
}

// Get reports the principal's own Channels permissions.
//
// The three permissions are evaluated independently and in separate checks.
// None of them implies another, and none of them is implied by mcp:connect or
// org:admin membership, so a single combined check would quietly widen one of
// them. The evaluation reads the live grants PrepareExternalContext resolved
// for this request rather than anything carried on the token, and the member's
// Channels access exception, if any, is then applied to them.
func (s *ChannelsAccessService) Get(ctx context.Context, principal Principal, input GetMyChannelsAccessInput) (GetMyChannelsAccessOutput, error) {
	if (input.ProjectID == "") == (input.ProjectSlug == "") {
		return GetMyChannelsAccessOutput{}, fmt.Errorf("exactly one of project_id or project_slug is required")
	}
	project, err := s.resolveProject(ctx, principal.OrganizationID, FindMCPInput{ProjectID: input.ProjectID, ProjectSlug: input.ProjectSlug})
	if err != nil {
		return GetMyChannelsAccessOutput{}, err
	}
	if err := s.engine.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: project.ID.String()}); err != nil {
		if isAuthorizationDenied(err) {
			return GetMyChannelsAccessOutput{}, ErrForbidden
		}
		return GetMyChannelsAccessOutput{}, err
	}
	// Keep discovery stable across rollouts, but never advertise an endpoint
	// or permissions for an organization where Channels is unavailable.
	if s.flags == nil {
		return channelsAccessNotEnabled(), nil
	}
	organization, err := organizationsrepo.New(s.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
	if err != nil {
		return GetMyChannelsAccessOutput{}, fmt.Errorf("%w: load channels organization: %w", ErrUnavailable, err)
	}
	enabled, err := s.flags.IsFlagEnabled(ctx, feature.FlagChannels, principal.OrganizationID, feature.OrgProjectGroups(organization.Slug, project.Slug))
	if err != nil {
		return GetMyChannelsAccessOutput{}, fmt.Errorf("%w: evaluate channels flag: %w", ErrUnavailable, err)
	}
	if !enabled {
		return channelsAccessNotEnabled(), nil
	}
	permissions := make([]bool, 0, 3)
	for _, scope := range []authz.Scope{authz.ScopeChannelsConnect, authz.ScopeChannelsAdmin, authz.ScopeChannelsReadAll} {
		allowed, err := s.engine.Evaluate(ctx, authz.Check{Scope: scope, ResourceID: project.ID.String()})
		if err != nil {
			return GetMyChannelsAccessOutput{}, fmt.Errorf("evaluate %s: %w", scope, err)
		}
		permissions = append(permissions, allowed)
	}
	// The answer is the access the Channels endpoint enforces: an access
	// exception overrides the role-granted connect and read-all permissions,
	// through the same resolution the endpoint itself uses.
	effective, err := channels.ResolveEffectiveAccess(ctx, s.db, channels.Principal{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID.String(),
		SubjectID:      principal.UserID,
		SubjectName:    "",
		CanConnect:     permissions[0],
		CanAdmin:       permissions[1],
		CanReadAll:     permissions[2],
	})
	if err != nil {
		return GetMyChannelsAccessOutput{}, fmt.Errorf("resolve channels access: %w", err)
	}
	output := GetMyChannelsAccessOutput{
		EndpointURL:             platformBaseURL(ctx, s.serverURL).JoinPath("channels", organization.Slug, project.Slug, "mcp").String(),
		CanConnect:              effective.CanConnect,
		CanAdminister:           effective.CanAdmin,
		CanReadAllConversations: effective.CanReadAll,
	}
	switch {
	case output.CanConnect:
		output.NextAction = "Add endpoint_url to your MCP client as a remote server and sign in with your organization account when prompted. Then call channels_connect through it."
	case output.CanAdminister:
		output.NextAction = "You may administer Channels but not take part in it. Change availability and participation in the dashboard, or with channels_admin after adding endpoint_url to your MCP client; ask an organization administrator for channels:connect on this project to join conversations."
	case output.CanReadAllConversations:
		output.NextAction = "You may observe conversations but not take part in them. Ask an organization administrator for channels:connect on this project to join."
	default:
		output.UnavailableReason = "no_channels_permission"
		output.NextAction = "Ask an organization administrator to grant you channels:connect on this project in access management. Channels permissions are separate from MCP access and are not granted by it."
	}
	return output, nil
}

func channelsAccessNotEnabled() GetMyChannelsAccessOutput {
	return GetMyChannelsAccessOutput{
		UnavailableReason: "channels_not_enabled",
		NextAction:        "Channels is not enabled for your organization. Contact your Speakeasy administrator.",
	}
}

func registerChannelsAccessTool(reg *Registrar, service *ChannelsAccessService) {
	addTool(reg, &mcp.Tool{
		Name:        getMyChannelsAccessName,
		Title:       "Get My Channels Access",
		Description: getMyChannelsAccessDescription,
		Annotations: readOnlyAnnotations(),
	}, getMyChannelsAccessMeta, func(ctx context.Context, req *mcp.CallToolRequest, input GetMyChannelsAccessInput) (*mcp.CallToolResult, GetMyChannelsAccessOutput, error) {
		if !service.valid() {
			result, _, err := unavailableTool("channels")(ctx, req, nil)
			return result, GetMyChannelsAccessOutput{}, err
		}
		return principalToolCall(ctx, func(error) (*mcp.CallToolResult, bool) { return nil, false }, func(principal Principal) (GetMyChannelsAccessOutput, error) {
			return service.Get(ctx, principal, input)
		})
	})
}
