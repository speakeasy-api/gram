// Shadow AI gateway blocking: refusing an AI tool an organization has decided
// may not reach its MCP servers.
//
// The check lives at the OAuth boundary rather than at tools/call because
// that is the last point where the caller's identity is a credential Gram
// verified rather than a name the client reported about itself. A blocked
// tool is turned away before a token exists, and the token endpoint re-checks
// so a token minted before the block dies on its next use.

package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

// AIToolBlockedError reports that the presented client belongs to an AI tool
// the organization has blocked. Like admission.DenialError it owns only why
// the client was refused and what to tell the end user; the OAuth error code
// and HTTP status stay with the transport.
type AIToolBlockedError struct {
	// TargetID is the scan target the client matched.
	TargetID string

	// DisplayName is that target's human-readable name.
	DisplayName string

	// RequestAccessURL is where the end user asks for the block to be
	// lifted. Empty when it could not be built.
	RequestAccessURL string
}

func (e *AIToolBlockedError) Error() string {
	return "ai tool blocked at the gateway: " + e.TargetID
}

// Description is the client-facing explanation.
//
// It names the tool and says plainly that an administrator has to act,
// because this failure is a dead end for the person hitting it: MCP clients
// commit to a client id at metadata-discovery time and do not fall back to
// dynamic registration when /authorize refuses them. The request-access link
// is the only recourse the text can offer, and it is why the dashboard warns
// an admin before they record the block.
func (e *AIToolBlockedError) Description() string {
	message := fmt.Sprintf(
		"%s is not permitted to connect to this organization's MCP servers; an organization administrator must approve it",
		e.DisplayName,
	)
	if e.RequestAccessURL != "" {
		message += ". Request access: " + e.RequestAccessURL
	}
	return message
}

// checkAIToolGatewayBlock refuses clientID when the organization has blocked
// the AI tool it belongs to. Nil means not blocked, the usual answer.
//
// The first query asks only whether the organization blocks anything at all,
// so the common case never touches the scan-target catalog.
//
// Fails OPEN on infrastructure trouble, unlike the killswitch checkpoints:
// this is on the connection path for every MCP client, and a database blip
// that locked everyone out would be a worse outage than a blocked tool
// connecting. Blocking is a policy posture; the killswitch fails closed.
func (s *Service) checkAIToolGatewayBlock(ctx context.Context, logger *slog.Logger, organizationID string, clientID string) error {
	if organizationID == "" || clientID == "" {
		return nil
	}

	queries := agentrepo.New(s.db)
	blocked, err := queries.ListBlockedDeviceAgentAITargetIDs(ctx, organizationID)
	if err != nil {
		logger.WarnContext(ctx, "ai tool gateway block unavailable; allowing the connection", attr.SlogError(err))
		return nil
	}
	if len(blocked) == 0 {
		return nil
	}

	list, err := aitargets.LoadOrganizationList(ctx, queries, organizationID)
	if err != nil {
		logger.WarnContext(ctx, "ai scan targets unavailable; allowing the connection", attr.SlogError(err))
		return nil
	}

	caller := aitargets.GatewayCaller{
		OAuthClientID:  clientID,
		CIMDVendorKey:  "",
		CIMDCatalogURL: "",
	}
	// A CIMD client_id is also a catalog entry, and the entry is the only
	// stable handle on a vendor that mints one document per server.
	if preset, known := admission.CatalogPreset(clientID); known {
		caller.CIMDVendorKey = preset.VendorKey
		caller.CIMDCatalogURL = preset.URL
	}

	target, matched := aitargets.MatchGatewayCaller(list.Snapshot.Targets(), caller)
	if !matched {
		return nil
	}
	blockedIDs := make(map[string]struct{}, len(blocked))
	for _, id := range blocked {
		blockedIDs[id] = struct{}{}
	}
	if _, isBlocked := blockedIDs[target.ID]; !isBlocked {
		return nil
	}

	logger.InfoContext(ctx, "ai tool blocked at the gateway",
		attr.SlogOAuthClientID(truncateClientIDForLog(clientID)),
	)
	return &AIToolBlockedError{
		TargetID:         target.ID,
		DisplayName:      target.DisplayName,
		RequestAccessURL: s.aiToolRequestAccessURL(ctx, organizationID, target),
	}
}

// aiToolRequestAccessURL builds the link the denial hands the end user. It
// runs only on the denial path, so the organization lookup it needs costs
// nothing on a normal connection.
func (s *Service) aiToolRequestAccessURL(ctx context.Context, organizationID string, target aitargets.Target) string {
	organization, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return ""
	}
	return mcpaccess.RequestAccessURL(s.siteURL, organization.Slug, mcpaccess.RequestAccessURLParams{
		Scope:        "mcp:connect",
		ResourceID:   target.ID,
		ResourceName: target.DisplayName,
	})
}
