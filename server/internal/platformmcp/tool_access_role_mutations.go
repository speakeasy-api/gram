//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type accessRoleMutationRefusal struct {
	Code    string `json:"code"`
	Feature string `json:"feature"`
	Message string `json:"message"`
}

func registerAccessRoleMutationTools(reg *Registrar, mutations *AccessRoleMutationService) {
	create := unavailableAccessRoleMutationHandler[CreateMCPAccessRoleInput, CreateMCPAccessRoleOutput]()
	update := unavailableAccessRoleMutationHandler[UpdateMCPAccessRoleInput, UpdateMCPAccessRoleOutput]()
	assign := unavailableAccessRoleMutationHandler[AssignMCPAccessRoleInput, AssignMCPAccessRoleOutput]()
	if mutations != nil && mutations.valid() {
		create = func(ctx context.Context, _ *mcp.CallToolRequest, input CreateMCPAccessRoleInput) (*mcp.CallToolResult, CreateMCPAccessRoleOutput, error) {
			return principalToolCall(ctx, accessRoleMutationToolResult, func(principal Principal) (CreateMCPAccessRoleOutput, error) {
				return mutations.Create(ctx, principal, input)
			})
		}
		update = func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateMCPAccessRoleInput) (*mcp.CallToolResult, UpdateMCPAccessRoleOutput, error) {
			return principalToolCall(ctx, accessRoleMutationToolResult, func(principal Principal) (UpdateMCPAccessRoleOutput, error) {
				return mutations.Update(ctx, principal, input)
			})
		}
		if assignments, err := NewAccessRoleAssignmentService(mutations); err == nil {
			assign = func(ctx context.Context, _ *mcp.CallToolRequest, input AssignMCPAccessRoleInput) (*mcp.CallToolResult, AssignMCPAccessRoleOutput, error) {
				return principalToolCall(ctx, accessRoleMutationToolResult, func(principal Principal) (AssignMCPAccessRoleOutput, error) {
					return assignments.Assign(ctx, principal, input)
				})
			}
		}
	}
	meta := ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}
	addTool(reg, &mcp.Tool{
		Name: "create_mcp_access_role", Title: "Create MCP Access Role",
		Description: "Create a custom role in one explicit project with only server-generated MCP access selectors. Requires explicit confirmation and an idempotency key. An exact replay returns the stored committed role snapshot; use list_access_roles for current state before a later update.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false)},
	}, meta, create)
	addTool(reg, &mcp.Tool{
		Name: "update_mcp_access_role", Title: "Update MCP Access Role",
		Description: "Update a custom role through an opaque reference and expected version. Adds or removes exact MCP access rules while preserving every non-MCP grant. Requires explicit confirmation and an idempotency key.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true)},
	}, meta, update)
	addTool(reg, &mcp.Tool{
		Name: operationAssignMCPAccessRole, Title: "Assign MCP Access Role",
		Description: "When enabled for the selected project, add one custom role confined to the selected MCP without removing current member roles. New assignments require mcp_id, expected_role_version, fresh opaque member and role references, the member's expected_version, explicit confirmation of the complete role scope, and an idempotency key. Disabled projects return feature_unavailable without changing anything. Results describe committed local desired state, not verified provider convergence.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false)},
	}, meta, assign)
}

func unavailableAccessRoleMutationHandler[In, Out any]() mcp.ToolHandlerFor[In, Out] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		payload, err := json.Marshal(accessRoleMutationRefusal{Code: unavailableCode, Feature: "access_role_mutations", Message: "This Platform MCP capability is not enabled for the current rollout."})
		if err != nil {
			return nil, zero, errors.Join(errors.New("encode access role mutation refusal"), err)
		}
		return nil, zero, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
	}
}

func accessRoleMutationToolResult(err error) (*mcp.CallToolResult, bool) {
	var mutation *AccessRoleMutationError
	if !errors.As(err, &mutation) {
		return nil, false
	}
	payload, marshalErr := json.Marshal(accessRoleMutationRefusal{Code: mutation.Code, Feature: "access_role_mutations", Message: mutation.Message})
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
}
