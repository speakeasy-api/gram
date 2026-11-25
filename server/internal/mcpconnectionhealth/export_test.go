package mcpconnectionhealth

import (
	"log/slog"
	"time"
)

// SetProblemsDeadline shortens the suspected problems budget in tests.
func SetProblemsDeadline(s *Service, d time.Duration) { s.problemsDeadline = d }

// SetProjectFlightHooks observes the shared project read: started runs in it, joined after each caller joins, done when it ends.
func SetProjectFlightHooks(s *Service, started, joined, done func()) {
	s.flightStarted, s.flightJoined, s.flightDone = started, joined, done
}

// SetLogger replaces the service's logger in tests.
func SetLogger(s *Service, logger *slog.Logger) { s.logger = logger }
