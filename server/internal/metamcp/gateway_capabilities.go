package metamcp

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
)

// Populate capabilities only on authorized API responses, after transaction
// completion. Audit snapshots keep the persisted configuration alone.
func (s *Service) withGatewayCapabilities(ctx context.Context, view *types.MetaMcpServer) *types.MetaMcpServer {
	projectID, err := uuid.Parse(view.ProjectID)
	code := err == nil && s.codeModeSelection.Allows(ctx, view.OrganizationID, projectID)
	view.CodeModeEnabled = &code
	return view
}
