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

// DeniedServerURLs lists the canonical server URLs a standing denial blocks
// for one user in a project: denials for everyone, and denials narrowed to a
// person or role the user holds. They are the user's built-in blocklist.
func (c *Client) DeniedServerURLs(ctx context.Context, organizationID string, projectID uuid.UUID, userID string) ([]string, error) {
	principals, err := authz.ResolveActorPrincipals(ctx, c.db, organizationID, userID)
	if err != nil {
		return nil, fmt.Errorf("resolve principals for denied shadow mcp servers: %w", err)
	}
	urns := make([]string, 0, len(principals))
	for _, principal := range principals {
		urns = append(urns, principal.String())
	}
	urls, err := shadowmcprepo.New(c.db).ListDeniedShadowMCPServerURLs(ctx, shadowmcprepo.ListDeniedShadowMCPServerURLsParams{
		ProjectID:     projectID,
		PrincipalUrns: urns,
	})
	if err != nil {
		return nil, fmt.Errorf("list denied shadow mcp server urls: %w", err)
	}
	return urls, nil
}
