package mcp

// CodeModeAvailable reports deployment-wide runtime availability for new
// settings and connection overrides. The provider defaults to disabled;
// configuring it enables Code Mode for every organization on this deployment,
// and it reads false while the runtime cannot start runners, such as during a
// deploy that has not installed the sandbox template yet. Callers separately
// enforce settings RBAC and sign-in. Existing executions interpret the stored
// policy through the serving path.
func (s *Service) CodeModeAvailable() bool {
	return s.codeExecutor.Available()
}
