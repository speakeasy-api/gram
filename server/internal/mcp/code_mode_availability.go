package mcp

// CodeModeAvailable reports deployment-wide runtime availability for new
// settings and connection overrides. The provider defaults to disabled;
// configuring it enables Code Mode for every organization on this deployment.
// Callers separately enforce settings RBAC. Existing executions interpret the
// stored policy through the serving path.
func (s *Service) CodeModeAvailable() bool {
	return s.codeExecutor.Enabled()
}
