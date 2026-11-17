package shadowmcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	shadowmcprepo "github.com/speakeasy-api/gram/server/internal/shadowmcp/repo"
)

// ImplicitPolicyName names the built-in blocklist in block messages. A project
// without a blocking Shadow MCP policy is in this mode: every server connects
// except the ones an administrator denied, and a server linked to an official
// version is redirected to it.
const ImplicitPolicyName = "Shadow MCP"

// DeniedServerURLs lists the canonical server URLs whose standing decision in
// the project is a denial. They are the built-in blocklist.
func (c *Client) DeniedServerURLs(ctx context.Context, projectID uuid.UUID) ([]string, error) {
	urls, err := shadowmcprepo.New(c.db).ListDeniedShadowMCPServerURLs(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list denied shadow mcp server urls: %w", err)
	}
	return urls, nil
}
