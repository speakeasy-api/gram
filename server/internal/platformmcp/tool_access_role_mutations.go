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
	authClientAccess := unavailableAccessRoleMutationHandler[SetMCPRoleAuthClientAccessInput, SetMCPRoleAuthClientAccessOutput]()
	offerCredential := unavailableAccessRoleMutationHandler[OfferMCPUpstreamCredentialInput, OfferMCPUpstreamCredentialOutput]()
	headlessCredential := unavailableAccessRoleMutationHandler[ChooseMCPHeadlessCallerCredentialInput, ChooseMCPHeadlessCallerCredentialOutput]()
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
		if mutations.audit != nil {
			authClientAccess = func(ctx context.Context, _ *mcp.CallToolRequest, input SetMCPRoleAuthClientAccessInput) (*mcp.CallToolResult, SetMCPRoleAuthClientAccessOutput, error) {
				return principalToolCall(ctx, accessRoleMutationToolResult, func(principal Principal) (SetMCPRoleAuthClientAccessOutput, error) {
					return mutations.SetRoleAuthClientAccess(ctx, principal, input)
				})
			}
			offerCredential = func(ctx context.Context, _ *mcp.CallToolRequest, input OfferMCPUpstreamCredentialInput) (*mcp.CallToolResult, OfferMCPUpstreamCredentialOutput, error) {
				return principalToolCall(ctx, accessRoleMutationToolResult, func(principal Principal) (OfferMCPUpstreamCredentialOutput, error) {
					return mutations.OfferUpstreamCredential(ctx, principal, input)
				})
			}
			headlessCredential = func(ctx context.Context, _ *mcp.CallToolRequest, input ChooseMCPHeadlessCallerCredentialInput) (*mcp.CallToolResult, ChooseMCPHeadlessCallerCredentialOutput, error) {
				return principalToolCall(ctx, accessRoleMutationToolResult, func(principal Principal) (ChooseMCPHeadlessCallerCredentialOutput, error) {
					return mutations.ChooseHeadlessCallerCredential(ctx, principal, input)
				})
			}
		}
		if assignments, err := NewAccessRoleAssignmentService(mutations); err == nil {
			assign = func(ctx context.Context, _ *mcp.CallToolRequest, input AssignMCPAccessRoleInput) (*mcp.CallToolResult, AssignMCPAccessRoleOutput, error) {
				return principalToolCall(ctx, accessRoleMutationToolResult, func(principal Principal) (AssignMCPAccessRoleOutput, error) {
					return assignments.Assign(ctx, principal, input)
				})
			}
		}
	}
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}
	addTool(reg, &mcp.Tool{
		Name: "create_mcp_access_role", Title: "Create MCP Access Role",
		Description: "Create a custom role in one explicit project with only server-generated MCP access selectors. An empty rules list creates a role with no grants. Requires explicit confirmation and an idempotency key. An exact replay returns the stored committed role snapshot; use list_access_roles for current state before a later update.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false), OpenWorldHint: new(false)},
	}, meta, create)
	addTool(reg, &mcp.Tool{
		Name: "update_mcp_access_role", Title: "Update MCP Access Role",
		Description: "Update a custom role through an opaque reference and expected version. Adds or removes exact MCP access rules while preserving every non-MCP grant. Changes to connect rules can also add or remove servers in plugins assigned to this role and queue publication; read and write rules never change plugin contents; another assigned role may retain access and distribution. Plugin-content edits do not grant or revoke server access. Requires explicit confirmation and an idempotency key.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true), OpenWorldHint: new(false)},
	}, meta, update)
	addTool(reg, &mcp.Tool{
		Name: operationAssignMCPAccessRole, Title: "Assign MCP Access Role",
		Description: "When enabled for the selected project, add one custom role confined to the selected MCP without removing current member roles. New assignments require mcp_id, expected_role_version, fresh opaque member and role references, the member's expected_version, explicit confirmation of the complete role scope, and an idempotency key. Disabled projects return feature_unavailable without changing anything. Results describe committed local desired state, not verified provider convergence.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false), OpenWorldHint: new(false)},
	}, meta, assign)
	addTool(reg, &mcp.Tool{
		Name: operationSetMCPRoleAuthClientAccess, Title: "Set Role Access to a Shared Credential",
		Description: "Set whether one role may use one shared credential an MCP server offers to reach its upstream service: allowed, blocked, or not_granted (removes the role's own rule). Read get_mcp_access first and pass a role reference and auth_client_id it returned. The rule is on the credential, so it applies on every server that offers it. Rules on every credential or credential type, and a member's other roles, can still allow or block it; the result reports the role's effective access after the change. Personal connections cannot be set here because each member only ever uses their own. Requires explicit confirmation.",
		Annotations: writeAnnotations(true, true, false),
	}, meta, authClientAccess)
	addTool(reg, &mcp.Tool{
		Name: operationOfferMCPUpstreamCredential, Title: "Offer an Upstream Credential on Another Server",
		Description: "Let one more configured MCP offer an upstream credential another server already offers, instead of creating a copy. Read get_mcp_connection_settings on the server that offers it and pass the auth_client_id it reports. The credential stays one credential: changing, rotating or disabling it changes every server that offers it. Offering it grants nobody permission to use it or access to the server; set who may use it with set_mcp_role_auth_client_access. A credential confined to another project, hosted toolsets, and header credentials on tunneled servers are refused. New credentials and their secret values are created in the dashboard. Requires explicit confirmation.",
		Annotations: writeAnnotations(false, false, false),
	}, meta, offerCredential)
	addTool(reg, &mcp.Tool{
		Name: operationChooseMCPHeadlessCallerCredential, Title: "Choose the Credential an App Without a Browser Uses",
		Description: "See and choose which upstream credential one configured MCP serves an app or workload that signs in without a browser and so cannot choose on the consent screen. First call with confirmed:false to preview the server's credentials, every app registered with its sign-in (including apps not yet configured, with chosen:false), and every caller's current choice. Then, after the user confirms the exact caller (user_session_client_id, or workload_issuer_id with workload_subject), server and choice, call with confirmed:true and one of auth_client_id, no_auth or remove. Only this server's choice changes; the caller's other servers keep theirs. Sessions issued afterwards use it; existing sessions keep theirs, and a token request can still name its own choice. Workloads can use shared credentials only. Choosing grants nothing: each issuance checks the caller may use it. Applies only where typed upstream credentials are enforced. Returns names and IDs, never credential values.",
		Annotations: writeAnnotations(true, true, false),
	}, meta, headlessCredential)
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
