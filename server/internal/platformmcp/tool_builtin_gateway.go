//nolint:exhaustruct // MCP manifests use optional zero values.
package platformmcp

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func registerBuiltinGatewayTools(reg *Registrar, service *GatewayService) {
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryMCPWrite}
	addTool(reg, &mcp.Tool{Name: "get_builtin_gateway_members", Title: "Get Built-in Gateway Members", Description: "Read built-in memberships and their version for an exact gateway in an explicit project. Separate from regular gateway members. Admin-only configuration; never returns credentials or grants runtime access. Human identities cannot see or call built-in tools through a gateway.", Annotations: readOnlyAnnotations()}, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input GetBuiltinGatewayMembersInput) (*mcp.CallToolResult, BuiltinGatewayMembers, error) {
		return principalToolCall(ctx, builtinGatewayToolResult, func(principal Principal) (BuiltinGatewayMembers, error) {
			return service.builtinGatewayMembers(ctx, principal, input, nil)
		})
	})
	addTool(reg, &mcp.Tool{Name: "configure_builtin_gateway_member", Title: "Configure Built-in Gateway Membership", Description: "Add, update, reorder or remove one built-in gateway membership without altering regular members. Read get_builtin_gateway_members first, show the exact change and obtain explicit confirmation, then supply confirmed:true and expected_version. present:false removes only the selected built-in. Omitted environment_id clears the membership override. Uses shared audited gateway writer and requests publication; result is committed configuration, not proof of publication or runtime access. All human identities remain forbidden from built-in runtime tools. A stale version requires a fresh read and confirmation.", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), OpenWorldHint: new(false)}}, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input ConfigureBuiltinGatewayMemberInput) (*mcp.CallToolResult, BuiltinGatewayMembers, error) {
		return principalToolCall(ctx, builtinGatewayToolResult, func(principal Principal) (BuiltinGatewayMembers, error) {
			return service.builtinGatewayMembers(ctx, principal, GetBuiltinGatewayMembersInput{ProjectID: input.ProjectID, GatewayID: input.GatewayID}, &input)
		})
	})
}
func builtinGatewayToolResult(err error) (*mcp.CallToolResult, bool) {
	if shared, ok := errors.AsType[*oops.ShareableError](err); ok && shared.Code == oops.CodeConflict {
		return gatewayToolResult(&GatewayError{Code: "conflict", Message: "The built-in gateway membership changed. Read get_builtin_gateway_members and confirm the new snapshot before retrying.", Cause: err})
	}
	if result, ok := builtinMCPToolResult(err); ok {
		return result, true
	}
	return gatewayToolResult(err)
}
