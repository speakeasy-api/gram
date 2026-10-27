//nolint:exhaustruct // MCP manifests use optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

const (
	getInboundEMATrustToolName        = "get_inbound_ema_trust"
	trustInboundEMAProviderToolName   = "trust_inbound_ema_identity_provider"
	untrustInboundEMAProviderToolName = "untrust_inbound_ema_identity_provider"
)

// inboundEMATrustMeta is shared by the live and unavailable registrations.
// Trust is organization-wide administration, so only an external organization
// administrator uses these tools.
var inboundEMATrustMeta = ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone, DiscoveryScopes: []authz.Scope{authz.ScopeOrgAdmin}}

func registerInboundEMATrustTools(reg *Registrar, service *InboundEMATrustService) {
	addTool(reg, &mcp.Tool{
		Name:        getInboundEMATrustToolName,
		Title:       "Get Inbound Enterprise-Managed Authorization",
		Description: "Read whether MCP clients can sign in to Speakeasy with an identity assertion (ID-JAG) from the company identity provider. Lists each organization-level user session issuer with the identity provider it trusts, that provider's key set URL, the audience to configure in the identity provider, whether interactive sign-in is federated through it, and the organization gateway URL when the gateway uses that issuer. Also reports how many directory users are synced: users resolve only through an active synced directory user with the same email, so without directory sync every exchange is refused. Read-only. Requires org:admin.",
		Annotations: readOnlyAnnotations(),
	}, inboundEMATrustMeta, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, GetInboundEMATrustOutput, error) {
		return principalToolCall(ctx, inboundEMATrustToolResult, func(p Principal) (GetInboundEMATrustOutput, error) { return service.Get(ctx, p) })
	})
	addTool(reg, &mcp.Tool{
		Name:  trustInboundEMAProviderToolName,
		Title: "Trust an Identity Provider for Inbound Enterprise-Managed Authorization",
		Description: "Make one exact organization-level user session issuer accept ID-JAGs from an identity provider, given the provider's issuer URL. Speakeasy reads the provider's metadata, which must publish a key set (jwks_uri), reuses this organization's or Speakeasy's existing record for that issuer URL or creates one, and links it to the issuer. Only a record whose issuer matches the discovered issuer exactly is reused. Call with confirmed=false first: it changes nothing and reports the provider, whether it is reused or created, warnings, and expected_trusted_remote_session_issuer_id. Show that to the user and, once they agree, call again with confirmed=true and that expected value; a changed trust is refused as a conflict. " +
			"The trust is inbound only: it does not change which provider vouches for outbound enterprise-managed authorization. Replacing a different trusted provider is refused when interactive sign-in goes through it. Returns the committed state, the audience to configure in the identity provider, and whether directory sync is set up. Requires org:admin.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: true, DestructiveHint: new(false), OpenWorldHint: new(true)},
	}, inboundEMATrustMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input TrustInboundEMAIdentityProviderInput) (*mcp.CallToolResult, TrustInboundEMAIdentityProviderOutput, error) {
		return principalToolCall(ctx, inboundEMATrustToolResult, func(p Principal) (TrustInboundEMAIdentityProviderOutput, error) {
			return service.Trust(ctx, p, input)
		})
	})
	addTool(reg, &mcp.Tool{
		Name:        untrustInboundEMAProviderToolName,
		Title:       "Stop Trusting an Identity Provider for Inbound Enterprise-Managed Authorization",
		Description: "Remove one exact organization-level user session issuer's trusted identity provider, so it refuses every ID-JAG. Call with confirmed=false first to preview, then confirmed=true with the preview's expected_trusted_remote_session_issuer_id once the user agrees. Refused when interactive sign-in goes through that provider, because removing it would break sign-in; change that in the dashboard (IDP and SSO) instead. The identity provider record itself is kept. Requires org:admin.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: true, DestructiveHint: new(true), OpenWorldHint: new(false)},
	}, inboundEMATrustMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input UntrustInboundEMAIdentityProviderInput) (*mcp.CallToolResult, UntrustInboundEMAIdentityProviderOutput, error) {
		return principalToolCall(ctx, inboundEMATrustToolResult, func(p Principal) (UntrustInboundEMAIdentityProviderOutput, error) {
			return service.Untrust(ctx, p, input)
		})
	})
}

func inboundEMATrustToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	var payload any
	if refusal, ok := errors.AsType[*InboundEMATrustRefusalError](err); ok {
		payload = refusal
	} else if errors.Is(err, ErrUnavailable) {
		payload = featureUnavailableResult{Code: unavailableCode, Feature: "inbound_ema_trust", Message: "Inbound enterprise-managed authorization settings are unavailable on this server."}
	} else {
		return nil, false
	}
	content, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}}, true
}
