package mcp

import "github.com/speakeasy-api/gram/server/internal/authenforcement"

// ResetAuthEnforcementForTest observes a mode change without waiting for the
// production gate's bounded cache to expire.
func ResetAuthEnforcementForTest(s *Service) {
	s.authEnforcement = authenforcement.NewGate(s.db)
}
