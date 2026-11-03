package mcp

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// CodeModeAvailable reports whether an organization can use Code Mode now:
// the runtime is configured and able to start runners on this deployment, and
// the organization has the staff-managed Code Mode entitlement. Runtime
// availability reads false while the runtime cannot start runners, such as
// during a deploy that has not installed the sandbox template yet. A failed
// entitlement lookup reads as unavailable. Callers separately enforce
// settings RBAC and sign-in; gateways that cannot use Code Mode serve
// Progressive discovery.
func (s *Service) CodeModeAvailable(ctx context.Context, organizationID string) bool {
	if !s.codeExecutor.Available() || s.platformFeatureChecker == nil || organizationID == "" {
		return false
	}
	return s.platformFeatureChecker(ctx, organizationID, string(productfeatures.FeatureCodeMode))
}

// CodeExecutor returns the code runtime this service owns, so Platform MCP can
// run Code Mode programs on the same coordinator and limits.
func (s *Service) CodeExecutor() codemode.Executor {
	return s.codeExecutor
}
