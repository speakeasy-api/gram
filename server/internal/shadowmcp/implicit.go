package shadowmcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	shadowmcprepo "github.com/speakeasy-api/gram/server/internal/shadowmcp/repo"
)

// ImplicitPolicyName names the built-in blocklist in block messages. A user
// no blocking Shadow MCP policy covers is in this mode: every server
// connects except the ones an administrator denied them, and a server
// linked to an official version is redirected to it.
const ImplicitPolicyName = "Shadow MCP"

// BlockedServerURLs lists the canonical server URLs a standing decision
// blocks for one user in a project: denials for everyone or for a person or
// role the user holds, and approvals narrowed to people or roles the user
// holds none of. They are the user's built-in blocklist.
func (c *Client) BlockedServerURLs(ctx context.Context, organizationID string, projectID uuid.UUID, userID string) ([]string, error) {
	principals, err := authz.ResolveActorPrincipals(ctx, c.db, organizationID, userID)
	if err != nil {
		return nil, fmt.Errorf("resolve principals for blocked shadow mcp servers: %w", err)
	}
	urns := make([]string, 0, len(principals))
	for _, principal := range principals {
		urns = append(urns, principal.String())
	}
	urls, err := shadowmcprepo.New(c.db).ListBlockedShadowMCPServerURLs(ctx, shadowmcprepo.ListBlockedShadowMCPServerURLsParams{
		ProjectID:     projectID,
		PrincipalUrns: urns,
	})
	if err != nil {
		return nil, fmt.Errorf("list blocked shadow mcp server urls: %w", err)
	}
	return urls, nil
}
