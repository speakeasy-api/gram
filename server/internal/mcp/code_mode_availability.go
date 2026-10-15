package mcp

import (
	"context"

	"github.com/google/uuid"
)

// CodeModeAvailable reports whether a code runtime is configured and enabled
// for a new setting or connection override. There is no rollout gate. Callers
// separately enforce the discovery-settings entitlement and RBAC. Existing
// code-mode executions never consult this check.
func (s *Service) CodeModeAvailable(_ context.Context, _ string, _ uuid.UUID) bool {
	return s.codeExecutor != nil && s.codeExecutor.Enabled()
}
