// Package platform re-serves the Platform MCP read tools
// (server/internal/platformmcp) as platform tools on the assistant runtime
// channel. The OAuth-facing Platform MCP surface authenticates org admins;
// here the caller is an assistant runtime whose auth context already carries
// the organization, so the tools reuse platformmcp.Reader with a principal
// built from that context and never touch the Platform MCP OAuth state.
package platform

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
)

// principalFromContext derives the Reader principal from the assistant
// runtime auth context. The Postgres reader scopes every query by
// organization ID alone, so no connection identity is required.
func principalFromContext(ctx context.Context) (platformmcp.Principal, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return platformmcp.Principal{}, fmt.Errorf("platform tools require organization auth context")
	}
	return platformmcp.Principal{
		Surface:        platformmcp.SurfaceProjectAssistant,
		UserID:         "",
		OrganizationID: authCtx.ActiveOrganizationID,
		ConnectionID:   "",
		Generation:     "",
		ClientID:       "",
	}, nil
}
