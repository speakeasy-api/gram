package hooks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/hooks/wire"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/identity"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

const (
	shadowMCPApprovalRequestTokenTTL = 7 * 24 * time.Hour
	shadowMCPApprovalRequestPrompt   = "Would you like me to open this link in a browser?"
)

type shadowMCPRequestLinkParams struct {
	OrganizationID  string
	ProjectID       string
	RequesterUserID string
	UserMessage     *string
	AuditReason     string
	Evidence        shadowmcp.AccessEvidence
	ToolName        string
	ToolInput       any
	RiskPolicyID    string
	// PolicyName feeds the structured "block" effect on the canonical ingest
	// path; legacy per-provider handlers may leave it empty.
	PolicyName string
}

// shadowMCPRequestLink is a minted approval-request link plus the fields the
// structured "block" effect needs alongside it.
type shadowMCPRequestLink struct {
	URL        string
	Token      string
	ExpiresAt  time.Time
	ServerName string
	ServerURL  string
}

func (s *Service) renderShadowMCPUserBlockReason(ctx context.Context, params shadowMCPRequestLinkParams) string {
	server := conv.PtrValOr(redactedObservedName(params.Evidence, params.ToolName), "")
	message := renderPolicyUserMessage(params.UserMessage, params.AuditReason, params.PolicyName, server)
	// A server with an official version is not something to ask access to:
	// the answer is to use the official one. No request link is minted and no
	// block effect is set, so nothing downstream offers "request access".
	if official, ok := s.shadowMCPOfficialServer(ctx, params.OrganizationID, params.ProjectID, params.Evidence.FullURL); ok {
		return strings.TrimSpace(message) + "\n\n" + shadowMCPOfficialServerRedirect(official)
	}
	link, ok := s.shadowMCPApprovalRequestURL(ctx, params)
	if !ok {
		return message
	}
	expiresAt := ""
	if !link.ExpiresAt.IsZero() {
		expiresAt = link.ExpiresAt.UTC().Format(time.RFC3339)
	}
	setBlockEffect(ctx, wire.BlockEffect{
		V:                wire.BlockEffectVersion,
		Category:         string(categories.CategoryShadowMCP),
		Requestable:      true,
		RequestToken:     link.Token,
		RequestURL:       link.URL,
		RequestExpiresAt: expiresAt,
		ServerName:       link.ServerName,
		ServerURL:        link.ServerURL,
		PolicyName:       params.PolicyName,
		ToolName:         params.ToolName,
		BlockURL:         "",
	})
	return strings.TrimSpace(message) + "\n\nRequest access:\n" + link.URL + "\n\n" + shadowMCPApprovalRequestPrompt
}

// shadowMCPOfficialServer finds the official, Speakeasy-served version of a
// directly-used server URL. It answers only when that version has an address
// to send people to: an official server without an endpoint cannot replace
// the direct one yet. A failed lookup reads as "none", which keeps the guard
// on its ordinary rules rather than blocking on an infrastructure blip.
func (s *Service) shadowMCPOfficialServer(ctx context.Context, organizationID string, projectID string, fullURL string) (shadowmcp.OfficialServer, bool) {
	none := shadowmcp.OfficialServer{MCPServerID: uuid.Nil, Name: "", Slug: "", URL: "", GatewayName: ""}
	if s.shadowMCPClient == nil || strings.TrimSpace(fullURL) == "" {
		return none, false
	}
	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		return none, false
	}
	official, found, err := s.shadowMCPClient.OfficialServerForURL(ctx, organizationID, parsedProjectID, fullURL)
	if err != nil {
		s.logger.WarnContext(ctx, "resolve official server for shadow mcp url",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProjectID(projectID),
		)
		return none, false
	}
	if !found || official.URL == "" {
		return none, false
	}
	return official, true
}

// shadowMCPOfficialServerRedirect is the part of a block message that sends
// the agent to the official server. It is written for the agent to act on:
// the address stands on its own line so it can be copied or opened.
func shadowMCPOfficialServerRedirect(official shadowmcp.OfficialServer) string {
	served := fmt.Sprintf("Your organization serves an official version of this MCP server: %q.", official.Name)
	if official.GatewayName != "" {
		served = fmt.Sprintf("Your organization serves an official version of this MCP server, %q, through the gateway %q.", official.Name, official.GatewayName)
	}
	return served + " Use it instead of the direct connection:\n" + official.URL +
		"\n\nIf that server is already connected, call its tools. If it is not, tell the user to add the URL above as an MCP server, then retry."
}

func (s *Service) shadowMCPApprovalRequestURL(ctx context.Context, params shadowMCPRequestLinkParams) (shadowMCPRequestLink, bool) {
	if s.cache == nil || strings.TrimSpace(s.jwtSecret) == "" {
		return shadowMCPRequestLink{URL: "", Token: "", ExpiresAt: time.Time{}, ServerName: "", ServerURL: ""}, false
	}

	evidence := shadowmcp.NormalizeAccessEvidence(params.Evidence)
	if evidence.FullURL == "" && evidence.URLHost == "" && evidence.ServerIdentity == "" {
		return shadowMCPRequestLink{URL: "", Token: "", ExpiresAt: time.Time{}, ServerName: "", ServerURL: ""}, false
	}

	siteURL := s.orgSiteURL(ctx, params.OrganizationID)
	if siteURL == nil {
		return shadowMCPRequestLink{URL: "", Token: "", ExpiresAt: time.Time{}, ServerName: "", ServerURL: ""}, false
	}

	requestURL, token, expiry, err := risk.GeneratePolicyBypassRequestURL(ctx, s.cache, siteURL, risk.PolicyBypassRequestTokenInput{
		OrganizationID:         params.OrganizationID,
		ProjectID:              params.ProjectID,
		RequesterUserID:        params.RequesterUserID,
		ObservedName:           redactedObservedName(params.Evidence, params.ToolName),
		ObservedFullURL:        stringPtrOrNil(evidence.FullURL),
		ObservedURLHost:        stringPtrOrNil(evidence.URLHost),
		ObservedServerIdentity: stringPtrOrNil(evidence.ServerIdentity),
		ToolName:               stringPtrOrNil(params.ToolName),
		ToolCall:               nil,
		BlockReason:            stringPtrOrNil(params.AuditReason),
		RiskPolicyID:           params.RiskPolicyID,
		RiskResultID:           nil,
	}, shadowMCPApprovalRequestTokenTTL)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to generate shadow mcp approval request link",
			attr.SlogError(err),
			attr.SlogOrganizationID(params.OrganizationID),
			attr.SlogProjectID(params.ProjectID),
		)
		return shadowMCPRequestLink{URL: "", Token: "", ExpiresAt: time.Time{}, ServerName: "", ServerURL: ""}, false
	}

	return shadowMCPRequestLink{
		URL:        requestURL,
		Token:      token,
		ExpiresAt:  expiry,
		ServerName: conv.PtrValOr(redactedObservedName(params.Evidence, params.ToolName), ""),
		ServerURL:  redactedServerURL(evidence.FullURL),
	}, true
}

// redactedObservedName names the observed server for people and for the
// machine-readable block effect. A local server's identity can be its launch
// command, which may carry credentials, so it is redacted first. The raw
// identity still keys the request server side, where approvers see it
// redacted (policyBypassRequestTargetView).
func redactedObservedName(evidence shadowmcp.AccessEvidence, toolName string) *string {
	redacted := shadowmcp.NormalizeAccessEvidence(evidence)
	redacted.ServerIdentity = identity.RedactCommand(redacted.ServerIdentity)
	return shadowmcp.ObservedName(redacted, toolName)
}

// redactedServerURL reduces an observed server URL to the inventory
// convention (scheme/host/path — no userinfo, query, or fragment) before it
// rides the machine-readable block effect: NormalizeAccessEvidence keeps the
// query string and any embedded credentials, and this channel must not
// re-expose them.
func redactedServerURL(fullURL string) string {
	if fullURL == "" {
		return ""
	}
	inventory, ok := shadowmcp.CanonicalizeInventoryURL(fullURL)
	if !ok {
		return ""
	}
	return inventory.CanonicalURL
}

func stringPtrOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
