package metamcp

import (
	"context"

	"github.com/speakeasy-api/gram/server/gen/types"
)

// Populate capabilities only on authorized API responses, after transaction
// completion. Audit snapshots keep the persisted configuration alone.
// Code Mode also needs sign-in: anonymous callers never get it.
func withGatewayCapabilities(view *types.MetaMcpServer, codeModeAllowed bool) *types.MetaMcpServer {
	code := codeModeAllowed && view.UserSessionIssuerID != nil
	view.CodeModeEnabled = &code
	return view
}

// codeModeAllowed reports whether the organization may select Code Mode:
// the runtime is available and the organization has the entitlement.
func (s *Service) codeModeAllowed(ctx context.Context, organizationID string) bool {
	return s.codeModeSelection.Allows(ctx, organizationID)
}
