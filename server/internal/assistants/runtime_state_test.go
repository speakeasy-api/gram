package assistants

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunnerStateIdleAndInFlight(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		threads  []runnerThreadState
		idle     *uint64
		inFlight bool
	}{
		{name: "no threads"},
		{name: "idle threads", threads: []runnerThreadState{{IdleSeconds: 90}, {IdleSeconds: 15}}, idle: new(uint64(15))},
		{name: "active turn", threads: []runnerThreadState{{IdleSeconds: 90}, {IdleSeconds: 0}}, idle: new(uint64(0)), inFlight: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := runnerStateResponse{Threads: tc.threads}
			require.Equal(t, tc.idle, state.minThreadIdle())
			require.Equal(t, tc.inFlight, state.turnInFlight())
		})
	}
}
