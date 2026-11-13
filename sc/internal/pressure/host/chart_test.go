package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

func testChart(t *testing.T) *sc.Machine[State, Slot] {
	t.Helper()
	m, err := newChart()
	require.NoError(t, err)
	return m
}

type stubNode struct{ name string }

func (*stubNode) Serve(http.Handler) error                  { return nil }
func (*stubNode) Status(context.Context) (NodeState, error) { return NodeState{}, nil }
func (*stubNode) Close(context.Context) error               { return nil }

var (
	t0        = time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	theNode   = &stubNode{name: "the node"}
	otherNode = &stubNode{name: "a node the slot no longer holds"}
	ingress   = Ingress{ID: uuid.MustParse("1a2b3c4d-0000-4000-8000-000000000000"), OrganizationID: "org", OrganizationSlug: "org", Label: "private"}

	running     = NodeState{Online: true, BackendState: "Running", NodeID: "n1", FQDN: "private.example.ts.net"}
	unapproved  = NodeState{BackendState: "Running", AwaitingApproval: true, NodeID: "n1", FQDN: "private.example.ts.net"}
	refused     = NodeState{BackendState: "NeedsLogin", NeedsLogin: true}
	uncertified = NodeState{BackendState: "Running", AwaitingCertificate: true, CertificateStatus: "Let's Encrypt is rate limiting new certificates for this tailnet; retrying after 2026-11-12 22:53 UTC", NodeID: "n1", FQDN: "private.example.ts.net"}
	connecting  = NodeState{BackendState: "Starting"}
	errGone     = errors.New("tsnet status: gone")
)

// observedMove is one move a step made: what the original's move() call
// would have recorded.
type observedMove struct {
	from, to NodeStatus
	message  string
	woke     bool
	// source is the state the move left.
	source State
	move   move
}

// moveOfEffect is the move a "move" action effect carries out. The decision
// is taken in the action, from the effect's move, so exploration, which
// never runs actions, has to reconstruct it here.
func moveOfEffect(e sc.Effect[Slot]) move {
	return moveOf(sc.Move[State]{Source: State(e.State), From: statesOf(e.From), To: statesOf(e.To), Event: e.Event})
}

func statesOf(names []string) []State {
	out := make([]State, 0, len(names))
	for _, n := range names {
		out = append(out, State(n))
	}
	return out
}

// movesOf reads the moves of one step from its effects. A move that writes
// no heartbeat (into attached) only changes the gauge and is left out.
func movesOf(effects []sc.Effect[Slot]) []observedMove {
	var moves []observedMove
	for _, e := range effects {
		if e.Kind != sc.EffectAction || e.Name != actionMove {
			continue
		}
		m := moveOfEffect(e)
		if m.Beat == "" {
			continue
		}
		from := leaf(statesOf(e.From))
		previous, _ := statusOf(from)
		moves = append(moves, observedMove{from: previous, to: m.Beat, message: e.Context.Message, woke: m.Wake, source: from, move: m})
	}
	return moves
}

// originalWake is where the original decided to wake, call site by call
// site, written independently of moveOf.
func originalWake(m observedMove, event any) bool {
	switch m.source {
	case StatePreparing:
		// hold(...) and fail(..., false) in start() (gateway.go:613-650).
		return false
	case StateRegistering:
		f, _ := event.(sc.Failed)
		if m.to == NodeStatusNeedsLogin {
			return true // gateway.go:667
		}
		_, reset := errors.AsType[keyResetError](f.Err)
		return reset // failRegistration, gateway.go:739
	default:
		return wakes(m.from, m.to) // probeTransition and stop
	}
}

func exploreKey(c Slot, now time.Time) any {
	return struct {
		Failures   int           `json:"failures"`
		Backoff    time.Duration `json:"backoff"`
		Final      NodeStatus    `json:"final"`
		Registered bool          `json:"registered"`
		HadKey     bool          `json:"had_key"`
		AuthKey    bool          `json:"auth_key"`
		Cleared    bool          `json:"cleared"`
		Node       bool          `json:"node"`
	}{min(c.Failures, maxStartFailures), max(0, c.RetryAt.Sub(now)), c.Final, c.Registered, c.HadKey, c.AuthKey != "", c.AuthKeyCleared, c.Node != nil}
}

func exploration(observe func(sc.Step[State, Slot], []observedMove)) sc.Exploration[State, Slot] {
	probe := func(state NodeState, err error) any { return Probed{Node: theNode, State: state, Err: err} }
	return sc.Exploration[State, Slot]{
		Events: []any{
			Start{Ingress: ingress}, Stop{}, Forget{},
			probe(running, nil), probe(unapproved, nil), probe(refused, nil),
			probe(uncertified, nil), probe(connecting, nil), probe(NodeState{}, errGone),
			Probed{Node: otherNode, State: running, Err: nil},
		},
		Outcomes: map[string][]sc.Outcome{
			servicePrepare: {
				sc.Succeeds(preparation{Outcome: prepGone}),
				sc.Succeeds(preparation{Outcome: prepAwaitingKey}),
				sc.Succeeds(preparation{Outcome: prepKeyExpired, HadKey: true}),
				sc.Succeeds(preparation{Outcome: prepRefused, Registered: true}),
				sc.Succeeds(preparation{Outcome: prepReady, AuthKey: "tskey", HadKey: true}),
				sc.Succeeds(preparation{Outcome: prepReady, Registered: true}),
				sc.Fails(errors.New("node state unreadable")),
			},
			serviceRegister: {
				sc.Succeeds(registration{Node: theNode, FQDN: "private.example.ts.net"}),
				sc.Fails(fmt.Errorf("bring tsnet node up: %w", ErrNodeNeedsLogin)),
				sc.Fails(keyResetError{err: errors.New("login timed out")}),
				sc.Fails(errors.New("advertise tailscale service: refused")),
			},
			serviceClose:        {sc.Succeeds(struct{}{}), sc.Fails(errors.New("close: timed out"))},
			serviceClearAuthKey: {sc.Succeeds(struct{}{}), sc.Fails(errors.New("database unavailable"))},
		},
		Key:              exploreKey,
		Waits:            []time.Duration{backoffAfterFailures},
		CheckDeterminism: true,
		Invariants: []sc.Invariant[State, Slot]{
			{Name: "a slot holds a node exactly while it hosts", Holds: func(s sc.Snapshot[State, Slot]) bool {
				return hosting(s) == (s.Context.Node != nil)
			}},
			{Name: "the plaintext auth key lives only while registering", Holds: func(s sc.Snapshot[State, Slot]) bool {
				return s.Context.AuthKey == "" || s.In(StateRegistering)
			}},
			{Name: "a slot backs off exactly after repeated failures", Holds: func(s sc.Snapshot[State, Slot]) bool {
				return (s.Context.Failures >= maxStartFailures) == !s.Context.RetryAt.IsZero()
			}},
		},
		Steps: []sc.StepInvariant[State, Slot]{
			{Name: "the reconciler is woken exactly when the original woke it", Holds: func(st sc.Step[State, Slot]) bool {
				moves := movesOf(st.Effects)
				if observe != nil {
					observe(st, moves)
				}
				for _, m := range moves {
					if m.woke != originalWake(m, st.Event) {
						return false
					}
				}
				return true
			}},
			{Name: "every status a step ends in is written, last", Holds: func(st sc.Step[State, Slot]) bool {
				moves := movesOf(st.Effects)
				before, _ := statusOf(current(st.From))
				after, records := statusOf(current(st.To))
				if len(moves) > 0 {
					return moves[len(moves)-1].to == after
				}
				return !records || before == after
			}},
			{Name: "the hosts gauge follows the slot from state to state", Holds: func(st sc.Step[State, Slot]) bool {
				gauge := hostedStatus(current(st.From))
				for _, e := range st.Effects {
					if e.Kind == sc.EffectAction && e.Name == actionMove {
						m := moveOfEffect(e)
						if m.GaugeFrom != gauge {
							return false
						}
						gauge = m.GaugeTo
					}
				}
				return gauge == hostedStatus(current(st.To))
			}},
			{Name: "a slot is never started before its retry time", Holds: func(st sc.Step[State, Slot]) bool {
				_, start := st.Event.(Start)
				return !start || !st.Accepted() || !st.At.Before(st.From.Context.RetryAt)
			}},
			{Name: "the one-shot key is cleared only by an online host", Holds: func(st sc.Step[State, Slot]) bool {
				for _, e := range st.Effects {
					if e.Kind == sc.EffectStartInvocation && e.Name == serviceClearAuthKey && !st.To.In(StateClearingKey) {
						return false
					}
				}
				return true
			}},
			{Name: "a known identity reconnects without a key and a new one always has one", Holds: func(st sc.Step[State, Slot]) bool {
				for _, e := range st.Effects {
					if e.Kind == sc.EffectStartInvocation && e.Name == serviceRegister && e.Context.Registered == (e.Context.AuthKey != "") {
						return false
					}
				}
				return true
			}},
			{Name: "a probe of another node is ignored", Holds: func(st sc.Step[State, Slot]) bool {
				p, ok := st.Event.(Probed)
				return !ok || p.Node == st.From.Context.Node || !st.Accepted()
			}},
		},
	}
}

func allStates() []State {
	return []State{
		StateSlot, StateIdle, StateUnchanged, StatePending, StateError, StateStopped,
		StateNeedsLogin, StatePreparing, StateRegistering, StateRunning, StateAttached, StateConnecting, StateOnline, StateClearingKey, StateServing,
		StateAwaitingApproval, StateAwaitingCertificate, StateRefused, StateFailed, StateDraining, StateForgotten,
	}
}

func TestSlotChartHoldsTheGatewayRules(t *testing.T) {
	t.Parallel()
	m := testChart(t)
	report, err := m.Explore(Slot{Ingress: ingress}, exploration(nil))
	require.NoError(t, err)

	for _, v := range report.Violations {
		t.Errorf("%s:\n%s", v.Invariant, m.FormatTrace(v.Trace))
	}
	require.Empty(t, report.Stuck)
	require.Empty(t, report.Trapped, "every slot can be stopped and forgotten")
	require.False(t, report.Truncated)
	for _, s := range allStates() {
		// Refused and failed are left by an eventless transition in the
		// step that enters them, and still count as reached.
		require.True(t, report.Reached(s), "%s is reachable", s)
	}
	require.Equal(t, []string{"action move", "service clearAuthKey", "service close", "service prepare", "service register"}, m.Unbound(),
		"the chart is explored without the gateway it runs against")
	t.Logf("%d states", report.States)
}

// TestWakesOnServingBoundaryAndCustomerWaits is the table of
// machine_internal_test.go. The rule itself is checked as the original
// checked it; then every move the chart can make is collected by exploring
// it, and each row must be a move the chart makes, with the same decision.
func TestWakesOnServingBoundaryAndCustomerWaits(t *testing.T) {
	t.Parallel()

	type pair struct{ from, to NodeStatus }
	seen := map[pair]map[bool]bool{}
	_, err := testChart(t).Explore(Slot{Ingress: ingress}, exploration(func(st sc.Step[State, Slot], moves []observedMove) {
		for _, m := range moves {
			if m.source == StateRegistering && m.woke != wakes(m.from, m.to) {
				// A start's explicit decision about a consumed key, checked
				// by the exploration.
				continue
			}
			p := pair{m.from, m.to}
			if seen[p] == nil {
				seen[p] = map[bool]bool{}
			}
			seen[p][m.woke] = true
		}
	}))
	require.NoError(t, err)

	for _, tc := range []struct {
		from, to NodeStatus
		wake     bool
	}{
		{NodeStatusStarting, NodeStatusOnline, true},
		{NodeStatusAwaitingApproval, NodeStatusOnline, true},
		{NodeStatusOnline, NodeStatusDraining, true},
		{NodeStatusOnline, NodeStatusError, true},
		{NodeStatusOnline, NodeStatusAwaitingApproval, true},
		{NodeStatusStarting, NodeStatusAwaitingApproval, true},
		{NodeStatusStarting, NodeStatusNeedsLogin, true},
		{NodeStatusAwaitingApproval, NodeStatusNeedsLogin, true},
		{NodeStatusOnline, NodeStatusOnline, false},
		{NodeStatusAwaitingApproval, NodeStatusAwaitingApproval, false},
		{NodeStatusStarting, NodeStatusAwaitingCertificate, true},
		{NodeStatusAwaitingApproval, NodeStatusAwaitingCertificate, true},
		{NodeStatusAwaitingCertificate, NodeStatusAwaitingCertificate, false},
		{NodeStatusAwaitingCertificate, NodeStatusOnline, true},
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
		if tc.from == NodeStatusNeedsLogin && tc.to == NodeStatusNeedsLogin {
			// The chart makes this move only as the hold "awaiting
			// re-registration", out of preparing, and a move cannot see the
			// status the slot had before it started preparing, so it is
			// seen as "" -> needs_login. The original passed wake=false
			// there explicitly, which the exploration checks.
			require.Equal(t, map[bool]bool{false: true}, seen[pair{"", tc.to}])
			continue
		}
		require.Equal(t, map[bool]bool{tc.wake: true}, seen[pair{tc.from, tc.to}], "%q -> %q is a move the chart makes", tc.from, tc.to)
	}
}

// reach drives a new slot to target, one of the probe-able states.
func reach(t *testing.T, m *sc.Machine[State, Slot], target State) sc.Snapshot[State, Slot] {
	t.Helper()
	snap, _ := m.Start(Slot{Ingress: ingress}, t0)
	send := func(ev any) {
		next, _ := m.Transition(snap, ev, t0)
		require.NotEqual(t, snap.Seq(), next.Seq(), "%T was taken", ev)
		snap = next
	}
	service := func() string {
		services := snap.Services()
		require.Len(t, services, 1)
		return services[0].ID
	}
	send(Start{Ingress: ingress})
	send(sc.Done[preparation]{ID: service(), Result: preparation{Outcome: prepReady, AuthKey: "tskey", HadKey: true}})
	send(sc.Done[registration]{ID: service(), Result: registration{Node: theNode, FQDN: "private.example.ts.net"}})
	switch target {
	case StateConnecting:
		send(Probed{Node: theNode, State: connecting})
	case StateOnline:
		send(Probed{Node: theNode, State: running})
	case StateAwaitingApproval:
		send(Probed{Node: theNode, State: unapproved})
	default:
	}
	require.True(t, snap.In(target), "in %s", current(snap))
	return snap
}

// TestProbeTransitionFollowsTheNode is the probeTransition table of
// machine_internal_test.go, run against the chart.
func TestProbeTransitionFollowsTheNode(t *testing.T) {
	t.Parallel()
	m := testChart(t)

	type decided struct {
		to      NodeStatus
		message string
		wake    bool
	}
	for _, tc := range []struct {
		name  string
		from  State
		state NodeState
		err   error
		want  decided
	}{
		{"comes online", StateConnecting, running, nil, decided{NodeStatusOnline, "", true}},
		{"stays online", StateOnline, running, nil, decided{NodeStatusOnline, "", false}},
		{"waits for approval", StateConnecting, unapproved, nil, decided{NodeStatusAwaitingApproval, "the tailnet has not approved this node to host svc:private", true}},
		{"keeps waiting", StateAwaitingApproval, unapproved, nil, decided{NodeStatusAwaitingApproval, "the tailnet has not approved this node to host svc:private", false}},
		{"approval revoked", StateOnline, unapproved, nil, decided{NodeStatusAwaitingApproval, "the tailnet has not approved this node to host svc:private", true}},
		{"waits for a certificate", StateConnecting, uncertified, nil, decided{NodeStatusAwaitingCertificate, uncertified.CertificateStatus, true}},
		{"identity refused", StateOnline, refused, nil, decided{NodeStatusNeedsLogin, "NeedsLogin", true}},
		{"still connecting", StateConnecting, connecting, nil, decided{NodeStatusStarting, "Starting", false}},
		{"probe failed while online", StateOnline, NodeState{}, errGone, decided{NodeStatusError, "tsnet status: gone", true}},
		{"probe failed while starting", StateConnecting, NodeState{}, errGone, decided{NodeStatusError, "tsnet status: gone", false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			from := reach(t, m, tc.from)
			_, effects := m.Transition(from, Probed{Node: theNode, State: tc.state, Err: tc.err}, t0)
			moves := movesOf(effects)
			require.NotEmpty(t, moves)
			require.Equal(t, tc.want, decided{moves[0].to, moves[0].message, moves[0].woke})
		})
	}
}

// TestRefusedAndFailedNodesDrainInOneStep checks the sequence of heartbeats
// the original's probe-then-stop wrote: the probe's status, draining, and
// the probe's status again once closed, keeping its message.
func TestRefusedAndFailedNodesDrainInOneStep(t *testing.T) {
	t.Parallel()
	m := testChart(t)

	from := reach(t, m, StateOnline)
	draining, effects := m.Transition(from, Probed{Node: theNode, State: refused}, t0)
	summary := func(moves []observedMove) [][4]any {
		var out [][4]any
		for _, mv := range moves {
			out = append(out, [4]any{mv.from, mv.to, mv.message, mv.woke})
		}
		return out
	}
	require.Equal(t, [][4]any{
		{NodeStatusOnline, NodeStatusNeedsLogin, "NeedsLogin", true},
		{NodeStatusNeedsLogin, NodeStatusDraining, "NeedsLogin", false},
	}, summary(movesOf(effects)))

	closeRun := draining.Services()[0].ID
	stopped, effects := m.Transition(draining, sc.Done[struct{}]{ID: closeRun, Result: struct{}{}}, t0)
	require.Equal(t, [][4]any{{NodeStatusDraining, NodeStatusNeedsLogin, "NeedsLogin", false}}, summary(movesOf(effects)))
	require.Equal(t, StateNeedsLogin, current(stopped))
	require.Nil(t, stopped.Context.Node)
}

// TestSlotChartExports checks the chart exports, so it can be looked at in
// an XState visualiser.
func TestSlotChartExports(t *testing.T) {
	t.Parallel()
	out, err := json.Marshal(testChart(t).Definition())
	require.NoError(t, err)
	require.Contains(t, string(out), `"awaiting_certificate"`)
	require.Contains(t, string(out), `"entry":["move"]`)
}
