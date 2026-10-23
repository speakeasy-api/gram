package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	gatewaygen "github.com/speakeasy-api/gram/server/gen/gateway_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const listGatewaySessionsToolName = "list_gateway_sessions"

// GatewaySessionManagement shares the dashboard's tenant checks, resource
// matching, filtering, and cursor contract. No mutations are exposed.
type GatewaySessionManagement interface {
	ListGatewaySessions(context.Context, *gatewaygen.ListGatewaySessionsPayload) (*gatewaygen.GatewaySessionsResult, error)
}

// ListGatewaySessionsInput selects one gateway without using a working project.
type ListGatewaySessionsInput struct {
	// Scope makes organization-wide reads an explicit choice.
	Scope string `json:"scope" jsonschema:"organization or gateway; gateway requires gateway_id"`
	// GatewayID names an exact project gateway in the caller's organization.
	GatewayID *string `json:"gateway_id,omitempty" jsonschema:"exact project gateway UUID; omit for organization scope"`
	// Search is supplied by the caller, never inferred from returned identities.
	Search *string `json:"search,omitempty" jsonschema:"literal identity name, email, or subject search; identities are omitted from results"`
	// ServerID restricts configured upstream associations, not historical usage.
	ServerID *string `json:"server_id,omitempty" jsonschema:"exact member server UUID"`
	// ProviderID restricts the upstream provider.
	ProviderID *string `json:"provider_id,omitempty" jsonschema:"exact upstream provider issuer UUID"`
	// Status defaults to active.
	Status string `json:"status,omitempty" jsonschema:"active, expired, revoked, or all; defaults to active"`
	// CreatedAfter is an inclusive RFC3339 timestamp.
	CreatedAfter *string `json:"created_after,omitempty"`
	// CreatedBefore is an exclusive RFC3339 timestamp.
	CreatedBefore *string `json:"created_before,omitempty"`
	// Sort defaults to newest.
	Sort string `json:"sort,omitempty" jsonschema:"newest, oldest, last_used, or expiry"`
	// Cursor must be reused only with identical filters and ordering.
	Cursor *string `json:"cursor,omitempty"`
	// Limit bounds one page, default 50 and maximum 100.
	Limit int `json:"limit,omitempty"`
}

// GatewaySessionSummary contains operational metadata, never subject identities,
// client-controlled names, workload claims, token identifiers, or credentials.
type GatewaySessionSummary struct {
	// ID identifies the authorization record, not a bearer credential.
	ID string `json:"id"`
	// SubjectType distinguishes people, agents, API keys, and workloads.
	SubjectType string `json:"subject_type"`
	// CreatedAt is the authorization's creation time.
	CreatedAt string `json:"created_at"`
	// LastUsedAt is approximate; omission means unknown, not never.
	LastUsedAt *string `json:"last_used_at,omitempty"`
	// AuthorizationExpiresAt is the inbound authorization deadline.
	AuthorizationExpiresAt string `json:"authorization_expires_at"`
	// AccessExpiresAt is separate from the authorization deadline.
	AccessExpiresAt string `json:"access_expires_at"`
	// RevokedAt marks an explicitly revoked authorization.
	RevokedAt *string `json:"revoked_at,omitempty"`
	// UpstreamAccounts counts configured upstream accounts, not usage.
	UpstreamAccounts int `json:"upstream_accounts"`
}

// ListGatewaySessionsOutput is a bounded, privacy-preserving authorization page.
type ListGatewaySessionsOutput struct {
	// Items contains only the selected gateway's authorized records.
	Items []GatewaySessionSummary `json:"items"`
	// NextCursor continues this exact query, when another page exists.
	NextCursor *string `json:"next_cursor,omitempty"`
}

func listGatewaySessions(ctx context.Context, service GatewaySessionManagement, principal Principal, input ListGatewaySessionsInput) (ListGatewaySessionsOutput, error) {
	var zero ListGatewaySessionsOutput
	if service == nil {
		return zero, ErrUnavailable
	}
	if (input.Scope != "organization" && input.Scope != "gateway") || (input.Scope == "gateway") != (input.GatewayID != nil) || input.Limit < 0 || input.Limit > 100 {
		return zero, oops.C(oops.CodeBadRequest)
	}
	actor, ok := contextvalues.GetAuthContext(ctx)
	if !ok || actor == nil || principal.OrganizationID == "" || principal.UserID == "" || actor.ActiveOrganizationID != principal.OrganizationID || actor.UserID != principal.UserID {
		return zero, ErrForbidden
	}
	grants, ok := authz.GrantsFromContext(ctx)
	if !ok || !grantsAuthorizeAnyScope(grants, principal.OrganizationID, []authz.Scope{authz.ScopeOrgAdmin}) {
		return zero, ErrForbidden
	}
	// Preserve the live actor and grants. The management service derives a manual
	// gateway's project; organization scope deliberately ignores the working one.
	result, err := service.ListGatewaySessions(ctx, &gatewaygen.ListGatewaySessionsPayload{
		GatewayID: input.GatewayID, SessionToken: nil, ApikeyToken: nil,
		Search: input.Search, ServerID: input.ServerID, ProviderID: input.ProviderID,
		Status: conv.Default(input.Status, "active"), CreatedAfter: input.CreatedAfter, CreatedBefore: input.CreatedBefore,
		Sort: conv.Default(input.Sort, "newest"), Cursor: input.Cursor, Limit: conv.Default(input.Limit, 50),
	})
	if err != nil {
		return zero, fmt.Errorf("list gateway sessions: %w", err)
	}
	output := ListGatewaySessionsOutput{Items: make([]GatewaySessionSummary, 0, len(result.Items)), NextCursor: result.NextCursor}
	for _, session := range result.Items {
		output.Items = append(output.Items, GatewaySessionSummary{ID: session.ID, SubjectType: session.SubjectType, CreatedAt: session.CreatedAt, LastUsedAt: session.LastUsedAt, AuthorizationExpiresAt: session.RefreshExpiresAt, AccessExpiresAt: session.ExpiresAt, RevokedAt: session.RevokedAt, UpstreamAccounts: len(session.Upstreams)})
	}
	return output, nil
}

func registerGatewaySessionTool(reg *Registrar, service GatewaySessionManagement) {
	// This can span every project in an organization. Managed assistants have no
	// organization-admin identity, so only external administrators are admitted.
	addTool(reg, &mcp.Tool{ //nolint:exhaustruct // Optional SDK fields use documented defaults.
		Name: listGatewaySessionsToolName, Title: "List Gateway Sessions",
		Description: "Inspect a bounded page of inbound gateway authorizations. Choose organization scope explicitly, or an exact project gateway ID. Filter by caller-supplied identity search, member server association, provider, status, and creation timestamps; order by creation, last use, or authorization expiry. Reuse next_cursor only with identical filters. Results omit identities, client names, JWT identifiers, workload claims, and secrets. Association does not prove historical use. An unexpired authorization deadline does not guarantee a refresh grant exists. Read-only; use the dashboard to review identities or revoke sessions.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone, DiscoveryScopes: nil}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListGatewaySessionsInput) (*mcp.CallToolResult, ListGatewaySessionsOutput, error) {
		return principalToolCall(ctx, gatewaySessionToolResult, func(principal Principal) (ListGatewaySessionsOutput, error) {
			return listGatewaySessions(ctx, service, principal, input)
		})
	})
}

// Refusals explain the recovery without returning database or identity details.
func gatewaySessionToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	result := featureUnavailableResult{Code: unavailableCode, Feature: "gateway_sessions", Message: "Gateway session inspection is temporarily unavailable."}
	if errors.Is(err, ErrForbidden) {
		result.Code, result.Message = "permission_denied", "Gateway session inspection requires an organization administrator."
	} else if shared, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shared.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			result.Code, result.Message = "invalid_request", "Choose organization scope or gateway scope with an exact gateway UUID. Use supported filters, RFC3339 date bounds, a limit from 1 to 100, and a cursor from the same query."
		case oops.CodeNotFound, oops.CodeForbidden:
			result.Code, result.Message = "not_found", "That gateway is not available to you."
		case oops.CodeUnauthorized:
			result.Code, result.Message = "permission_denied", "Sign in as an organization administrator."
		default:
			return nil, false
		}
	} else if !errors.Is(err, ErrUnavailable) {
		return nil, false
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Meta: nil, StructuredContent: nil, InputRequests: nil, RequestState: "", Content: []mcp.Content{&mcp.TextContent{Meta: nil, Annotations: nil, Text: string(payload)}}, IsError: true}, true
}
