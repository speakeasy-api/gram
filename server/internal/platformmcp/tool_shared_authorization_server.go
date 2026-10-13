//nolint:exhaustruct // MCP manifests and generated payloads use optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
)

// SharedAuthorizationServerProvisioner creates an organization's shared
// authorization server exactly as the management API does: same
// authorization, same fixed defaults, same idempotency and audit.
type SharedAuthorizationServerProvisioner interface {
	CreateSharedIssuer(ctx context.Context, payload *orggen.CreateSharedIssuerPayload) (*types.UserSessionIssuer, error)
}

// WithSharedAuthorizationServers reuses the management API's organization
// issuer creation for the create_shared_authorization_server tool.
func (r *PostgresReader) WithSharedAuthorizationServers(provisioner SharedAuthorizationServerProvisioner) *PostgresReader {
	r.sharedAuthorizationServers = provisioner
	return r
}

type CreateSharedAuthorizationServerInput struct {
	IdempotencyKey string `json:"idempotency_key" jsonschema:"key chosen for this request and reused verbatim on retry; the same key returns the same authorization server and a new key creates another one"`
	Confirmed      bool   `json:"confirmed" jsonschema:"true only after the user confirms creating a new organization-wide shared authorization server"`
}

type CreateSharedAuthorizationServerOutput struct {
	UserSessionIssuerID     string `json:"user_session_issuer_id"`
	Slug                    string `json:"slug"`
	AuthorizationServerMode string `json:"authorization_server_mode"`
	Issuer                  string `json:"issuer" jsonschema:"the authorization server's issuer identifier, as its metadata publishes it"`
	TokenEndpoint           string `json:"token_endpoint" jsonschema:"the authorization server's token endpoint, where an external platform exchanges its workload token"`
	Message                 string `json:"message"`
}

type sharedAuthorizationServerRefusalError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Creation is external-only: it acts on the organization under the live
// administrator's identity, which a project assistant does not carry.
func registerCreateSharedAuthorizationServerTool(reg *Registrar, provisioner SharedAuthorizationServerProvisioner) {
	addTool(reg, &mcp.Tool{
		Name: "create_shared_authorization_server", Title: "Create a Shared Authorization Server",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
		Description: "Create a new organization-owned user session issuer that serves one shared OAuth authorization server for every MCP server later attached to it, so an external platform can exchange workload tokens at a single token endpoint. Everything is fixed by the server: a generated slug, interactive sign-in, a 336-hour maximum authorization lifetime and an issuer URL on this deployment. Workload grants keep their 15-minute, nonrefreshable lifetime. Explain that this adds an organization-wide authorization server and ask the user to confirm before calling. Choose an idempotency_key for the request and reuse it verbatim on retry: the same key returns the same authorization server instead of creating another. Requires organization administration. This does not attach any MCP server, grant agent access or change existing issuers; it returns the issuer and token endpoint to point the platform at. Read the organization's token endpoints afterwards to confirm it is listed.",
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateSharedAuthorizationServerInput) (*mcp.CallToolResult, CreateSharedAuthorizationServerOutput, error) {
		return principalToolCall(ctx, sharedAuthorizationServerResult, func(principal Principal) (CreateSharedAuthorizationServerOutput, error) {
			if provisioner == nil {
				return CreateSharedAuthorizationServerOutput{}, &sharedAuthorizationServerRefusalError{Code: unavailableCode, Message: "Shared authorization server creation is unavailable on this server."}
			}
			if !input.Confirmed {
				return CreateSharedAuthorizationServerOutput{}, &sharedAuthorizationServerRefusalError{Code: "confirmation_required", Message: "Confirm creating a new organization-wide shared authorization server before proceeding."}
			}
			if strings.TrimSpace(input.IdempotencyKey) == "" {
				return CreateSharedAuthorizationServerOutput{}, &sharedAuthorizationServerRefusalError{Code: "invalid_request", Message: "Choose an idempotency_key for this request and reuse it on retry."}
			}
			ac := &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}
			if current, ok := contextvalues.GetAuthContext(ctx); ok && current != nil {
				actor := *current
				actor.ActiveOrganizationID, actor.UserID, actor.ProjectID = principal.OrganizationID, principal.UserID, nil
				ac = &actor
			}
			issuer, err := provisioner.CreateSharedIssuer(contextvalues.SetAuthContext(ctx, ac), &orggen.CreateSharedIssuerPayload{IdempotencyKey: input.IdempotencyKey})
			if err != nil {
				return CreateSharedAuthorizationServerOutput{}, fmt.Errorf("create shared authorization server: %w", err)
			}
			if issuer.IssuerURL == nil {
				return CreateSharedAuthorizationServerOutput{}, fmt.Errorf("create shared authorization server: issuer %s has no pinned issuer URL", issuer.ID)
			}
			tokenEndpoint, err := authserver.SharedTokenEndpoint(*issuer.IssuerURL)
			if err != nil {
				return CreateSharedAuthorizationServerOutput{}, fmt.Errorf("create shared authorization server: %w", err)
			}
			return CreateSharedAuthorizationServerOutput{
				UserSessionIssuerID:     issuer.ID,
				Slug:                    issuer.Slug,
				AuthorizationServerMode: issuer.AuthorizationServerMode,
				Issuer:                  *issuer.IssuerURL,
				TokenEndpoint:           tokenEndpoint,
				Message:                 "The shared authorization server exists for this organization. Repeating this call with the same idempotency_key returns it again. No MCP server is attached to it yet and no agent access was granted.",
			}, nil
		})
	})
}

func (r *sharedAuthorizationServerRefusalError) Error() string {
	return r.Code + ": " + r.Message
}

func sharedAuthorizationServerResult(err error) (*mcp.CallToolResult, bool) {
	if refusal, ok := errors.AsType[*sharedAuthorizationServerRefusalError](err); ok {
		return sharedAuthorizationServerRefusalResult(*refusal)
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shareable.Code {
		case oops.CodeForbidden, oops.CodeUnauthorized:
			return sharedAuthorizationServerRefusalResult(sharedAuthorizationServerRefusalError{Code: "permission_denied", Message: "Creating a shared authorization server requires organization administration through a user session."})
		case oops.CodeBadRequest:
			return sharedAuthorizationServerRefusalResult(sharedAuthorizationServerRefusalError{Code: "invalid_request", Message: "Choose an idempotency_key of at most 200 characters and reuse it on retry."})
		case oops.CodeConflict:
			return sharedAuthorizationServerRefusalResult(sharedAuthorizationServerRefusalError{Code: "conflict", Message: "That idempotency_key was already used for an issuer that is no longer a usable shared authorization server. Choose a new key to create another one."})
		default:
			return sharedAuthorizationServerRefusalResult(sharedAuthorizationServerRefusalError{Code: unavailableCode, Message: "Shared authorization server creation is temporarily unavailable."})
		}
	}
	return nil, false
}

func sharedAuthorizationServerRefusalResult(refusal sharedAuthorizationServerRefusalError) (*mcp.CallToolResult, bool) {
	content, err := json.Marshal(refusal)
	if err != nil {
		return nil, false
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}}, true
}
