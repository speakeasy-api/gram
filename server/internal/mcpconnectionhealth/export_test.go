package mcpconnectionhealth

import "time"

// SetProblemsDeadline shortens the suspected problems budget in tests.
func SetProblemsDeadline(s *Service, d time.Duration) { s.problemsDeadline = d }
