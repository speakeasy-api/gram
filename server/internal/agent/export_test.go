package agent

import "github.com/speakeasy-api/gram/server/internal/aivendors"

// SetAICatalog replaces the AI client catalog store only in the test binary.
func (s *Service) SetAICatalog(store *aivendors.Store) {
	s.aiCatalog = store
}
