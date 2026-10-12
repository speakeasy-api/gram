//nolint:exhaustruct // MCP manifests and generated payloads use optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	pluginsgen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	plugindelivery "github.com/speakeasy-api/gram/server/internal/plugins"
)

type RemovePluginServerInput struct {
	ProjectID    string `json:"project_id" jsonschema:"explicit project ID owning the plugin"`
	PluginID     string `json:"plugin_id" jsonschema:"exact plugin ID; no default or name fallback"`
	MembershipID string `json:"membership_id" jsonschema:"exact plugin server membership ID, not the backing MCP server ID; obtain from a fresh plugin read"`
	Confirmed    bool   `json:"confirmed" jsonschema:"true only after the user confirms removal of this exact membership"`
}

type RemovePluginServerOutput struct {
	ProjectID    string `json:"project_id"`
	PluginID     string `json:"plugin_id"`
	MembershipID string `json:"membership_id"`
	Removed      bool   `json:"removed"`
	Message      string `json:"message"`
}

// WithServerRemoval reuses the dashboard service's authorization, atomic audit
// and publication flow. Content removal never changes server permissions.
func (s *PluginsService) WithServerRemoval(service *plugindelivery.Service) *PluginsService {
	if s != nil {
		s.serverRemoval = service
	}
	return s
}

// Content mutations remain external-only, like the existing distribution and
// assignment tools; the managed-assistant confirmation flow is not admitted.
func registerRemovePluginServerTool(reg *Registrar, plugins *PluginsService) {
	addTool(reg, &mcp.Tool{
		Name: "remove_plugin_server", Title: "Remove an Exact Plugin Server Membership",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true},
		Description: "Remove one exact server membership from an existing plugin, including manually or automatically added entries. First read the plugin, explain which membership will be removed, and ask the user to confirm. Supply the exact project, plugin and membership IDs; never substitute a backing server ID. Requires organization administration or delegated plugin write permission. This changes distribution only: it does not delete the server or plugin, change audiences, or grant or revoke server access. The existing publication-update flow runs; this does not prove installed clients have refreshed. Unlike remove_mcp_from_plugin, this is not limited to undoing this connection's own onboarding distribution. Re-read the plugin after removal.",
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: []authz.Scope{authz.ScopePluginWrite, authz.ScopeOrgAdmin}}, func(ctx context.Context, _ *mcp.CallToolRequest, input RemovePluginServerInput) (*mcp.CallToolResult, RemovePluginServerOutput, error) {
		return principalToolCall(ctx, pluginServerRemovalResult, func(principal Principal) (RemovePluginServerOutput, error) {
			if plugins == nil || plugins.serverRemoval == nil {
				return RemovePluginServerOutput{}, &PluginAssignmentMutationError{Code: unavailableCode, Message: "Plugin server removal is unavailable on this server."}
			}
			if !input.Confirmed {
				return RemovePluginServerOutput{}, &PluginAssignmentMutationError{Code: "confirmation_required", Message: "Confirm removal of this exact plugin server membership before proceeding."}
			}
			projectID, err := uuid.Parse(input.ProjectID)
			if err != nil {
				return RemovePluginServerOutput{}, &PluginAssignmentMutationError{Code: "invalid_request", Message: "An exact project ID is required."}
			}
			// Membership IDs identify immutable attachment rows. A replay cannot remove
			// a later re-addition, which has a different ID; a missing row is not_found.
			ac := &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, ProjectID: &projectID}
			if current, ok := contextvalues.GetAuthContext(ctx); ok && current != nil {
				actor := *current
				actor.ActiveOrganizationID, actor.UserID, actor.ProjectID = principal.OrganizationID, principal.UserID, &projectID
				ac = &actor
			}
			err = plugins.serverRemoval.RemovePluginServer(contextvalues.SetAuthContext(ctx, ac), &pluginsgen.RemovePluginServerPayload{PluginID: input.PluginID, ID: input.MembershipID})
			if err != nil {
				return RemovePluginServerOutput{}, fmt.Errorf("remove plugin server: %w", err)
			}
			return RemovePluginServerOutput{ProjectID: projectID.String(), PluginID: input.PluginID, MembershipID: input.MembershipID, Removed: true, Message: "Removed this membership only. Server access and other plugin memberships are unchanged. Publication follows the existing update flow; installed-client refresh is not verified."}, nil
		})
	})
}

func pluginServerRemovalResult(err error) (*mcp.CallToolResult, bool) {
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		var result pluginRefusalResult
		switch shareable.Code {
		case oops.CodeForbidden:
			result = pluginRefusalResult{Code: "permission_denied", Message: "Removing plugin contents requires organization administration or plugin write permission for this project."}
		case oops.CodeNotFound:
			result = pluginRefusalResult{Code: "not_found", Message: "No matching plugin membership exists in this organization and project. Read the plugin again; nothing is selected by default."}
		case oops.CodeBadRequest:
			result = pluginRefusalResult{Code: "invalid_request", Message: "Exact plugin and membership IDs are required."}
		default:
			return pluginToolResult(err)
		}
		content, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return nil, false
		}
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}}, true
	}
	return pluginToolResult(err)
}
