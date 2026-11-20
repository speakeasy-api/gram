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
	// SessionMCPServers is the MCP server list the agent session reported.
	// It tells which route to an official server the agent already has. Nil
	// when the agent sends no list (Cursor); every route is then offered.
	SessionMCPServers []MCPServerEntry
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
	// A server linked to an official version is not something to ask access
	// to: the answer is to use the official one. No request link is minted and
	// no block effect is set, so nothing downstream offers "request access".
	if official, ok := s.shadowMCPOfficialServer(ctx, params.OrganizationID, params.ProjectID, params.Evidence.FullURL); ok {
		if route, ok := s.projectGatewayRoute(ctx, params.OrganizationID, params.ProjectID); ok {
			official.Routes = append(official.Routes, route)
		}
		return strings.TrimSpace(message) + "\n\n" + shadowMCPOfficialServerRedirect(official, mcpServerURLs(params.SessionMCPServers))
	}
	// The built-in blocklist has no policy to request a bypass of: its
	// servers are blocked because an administrator denied them.
	if params.RiskPolicyID == "" {
		return message
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
	none := shadowmcp.OfficialServer{MCPServerID: uuid.Nil, Name: "", Slug: "", URL: "", GatewayName: "", Routes: nil}
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

// sessionMCPServers reads the MCP server list the agent session reported at
// its start. A session without a cached list yields none, which only means
// a block message offers every route to an official server.
func (s *Service) sessionMCPServers(ctx context.Context, sessionID string) []MCPServerEntry {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	entries, err := s.getCachedMCPList(ctx, sessionID)
	if err != nil {
		return nil
	}
	return entries
}

func mcpServerURLs(entries []MCPServerEntry) []string {
	urls := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.URL != "" {
			urls = append(urls, entry.URL)
		}
	}
	return urls
}

// shadowMCPOfficialServerRedirect is the part of a block message that sends
// the agent to the official server. It is written for the agent to act on:
// each address stands on its own line so it can be copied or opened.
//
// When the session already has the official server connected, through its
// own address or a gateway that serves it, the message names that route.
// Otherwise it names every route, since Speakeasy cannot tell which one the
// person can add.
func shadowMCPOfficialServerRedirect(official shadowmcp.OfficialServer, connectedURLs []string) string {
	served := fmt.Sprintf("Your organization serves an official version of this MCP server: %q.", official.Name)
	if route, ok := official.ConnectedRoute(connectedURLs); ok {
		via := "This session already has it connected:"
		if route.GatewayName != "" {
			via = fmt.Sprintf("This session already has it connected through the gateway %q:", route.GatewayName)
		}
		return served + " " + via + "\n" + route.URL + "\n\nCall its tools instead of the direct connection."
	}

	// shadowMCPOfficialServer answers only for a server with an address, and
	// every address is a route, so there is at least one.
	routes := official.Routes
	if len(routes) == 1 {
		if routes[0].GatewayName != "" {
			served = fmt.Sprintf("Your organization serves an official version of this MCP server, %q, through the gateway %q.", official.Name, routes[0].GatewayName)
		}
		return served + " Use it instead of the direct connection:\n" + routes[0].URL +
			"\n\nIf that server is already connected, call its tools. If it is not, tell the user to add the URL above as an MCP server, then retry."
	}

	var b strings.Builder
	b.WriteString(served)
	b.WriteString(" Use it instead of the direct connection, through any of these routes:")
	for _, route := range routes {
		label := fmt.Sprintf("%q directly", official.Name)
		if route.GatewayName != "" {
			label = fmt.Sprintf("the gateway %q", route.GatewayName)
		}
		fmt.Fprintf(&b, "\n- %s: %s", label, route.URL)
	}
	b.WriteString("\n\nIf one of them is already connected, call its tools. If none is, tell the user to add one of the URLs above as an MCP server, then retry.")
	return b.String()
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
