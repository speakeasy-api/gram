package tailnetgateway

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWakesOnServingBoundaryAndCustomerWaits(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		from, to NodeStatus
		wake     bool
	}{
		// Crossing the online boundary in either direction.
		{NodeStatusStarting, NodeStatusOnline, true},
		{NodeStatusAwaitingApproval, NodeStatusOnline, true},
		{NodeStatusOnline, NodeStatusDraining, true},
		{NodeStatusOnline, NodeStatusError, true},
		{NodeStatusOnline, NodeStatusAwaitingApproval, true},
		// Starting to wait on the customer.
		{NodeStatusStarting, NodeStatusAwaitingApproval, true},
		{NodeStatusStarting, NodeStatusNeedsLogin, true},
		{NodeStatusAwaitingApproval, NodeStatusNeedsLogin, true},
		// Everything else is noise the reconciler does not need.
		{NodeStatusOnline, NodeStatusOnline, false},
		{NodeStatusAwaitingApproval, NodeStatusAwaitingApproval, false},
		{NodeStatusNeedsLogin, NodeStatusNeedsLogin, false},
		{NodeStatusStarting, NodeStatusStarting, false},
		{NodeStatusStarting, NodeStatusError, false},
		{NodeStatusAwaitingApproval, NodeStatusDraining, false},
		{NodeStatusDraining, NodeStatusStopped, false},
		{NodeStatusDraining, NodeStatusNeedsLogin, false},
		{"", NodeStatusPending, false},
		{"", NodeStatusStarting, false},
	} {
		require.Equal(t, tc.wake, wakes(tc.from, tc.to), "%s -> %s", tc.from, tc.to)
	}
}

func TestProbeTransitionFollowsTheNode(t *testing.T) {
	t.Parallel()

	running := NodeState{Online: true, BackendState: "Running", NeedsLogin: false, AwaitingApproval: false, NodeID: "n1", FQDN: "private.example.ts.net"}
	unapproved := NodeState{Online: false, BackendState: "Running", NeedsLogin: false, AwaitingApproval: true, NodeID: "n1", FQDN: "private.example.ts.net"}
	refused := NodeState{Online: false, BackendState: "NeedsLogin", NeedsLogin: true, AwaitingApproval: false, NodeID: "", FQDN: ""}
	connecting := NodeState{Online: false, BackendState: "Starting", NeedsLogin: false, AwaitingApproval: false, NodeID: "", FQDN: ""}

	for _, tc := range []struct {
		name  string
		from  NodeStatus
		state NodeState
		err   error
		want  transition
	}{
		{"comes online", NodeStatusStarting, running, nil, transition{to: NodeStatusOnline, message: "", wake: true}},
		{"stays online", NodeStatusOnline, running, nil, transition{to: NodeStatusOnline, message: "", wake: false}},
		{"waits for approval", NodeStatusStarting, unapproved, nil, transition{to: NodeStatusAwaitingApproval, message: "the tailnet has not approved this node to host svc:private", wake: true}},
		{"keeps waiting", NodeStatusAwaitingApproval, unapproved, nil, transition{to: NodeStatusAwaitingApproval, message: "the tailnet has not approved this node to host svc:private", wake: false}},
		{"approval revoked", NodeStatusOnline, unapproved, nil, transition{to: NodeStatusAwaitingApproval, message: "the tailnet has not approved this node to host svc:private", wake: true}},
		{"identity refused", NodeStatusOnline, refused, nil, transition{to: NodeStatusNeedsLogin, message: "NeedsLogin", wake: true}},
		{"still connecting", NodeStatusStarting, connecting, nil, transition{to: NodeStatusStarting, message: "Starting", wake: false}},
		{"probe failed while online", NodeStatusOnline, NodeState{}, errors.New("tsnet status: gone"), transition{to: NodeStatusError, message: "tsnet status: gone", wake: true}},
		{"probe failed while starting", NodeStatusStarting, NodeState{}, errors.New("tsnet status: gone"), transition{to: NodeStatusError, message: "tsnet status: gone", wake: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, probeTransition(tc.from, "private", tc.state, tc.err))
		})
	}
}
