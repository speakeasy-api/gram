package mcpconnectionhealth

import "time"

// SetProblemsDeadline shortens the suspected problems budget in tests.
func SetProblemsDeadline(s *Service, d time.Duration) { s.problemsDeadline = d }

// SetProjectFlightHooks observes the shared project read: started runs in it, joined after each caller joins.
func SetProjectFlightHooks(s *Service, started, joined func()) {
	s.flightStarted, s.flightJoined = started, joined
}
