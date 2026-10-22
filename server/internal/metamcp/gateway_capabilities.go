package metamcp

import "github.com/speakeasy-api/gram/server/gen/types"

// Populate capabilities only on authorized API responses, after transaction
// completion. Audit snapshots keep the persisted configuration alone.
func (s *Service) withGatewayCapabilities(view *types.MetaMcpServer) *types.MetaMcpServer {
	code := s.codeModeSelection.Allows()
	view.CodeModeEnabled = &code
	return view
}
