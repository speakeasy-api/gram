package host

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/speakeasy-api/gram/sc"
)

// State identifies a state of the slot chart.
//
// The original kept one field, the NodeStatus last written, plus flags
// (node != nil, inFlight, failures/retryAt, authKeyCleared). The chart makes
// node and inFlight states, so a status value can mean more than one state:
// starting is registering, attached or connecting, and error is an idle slot
// or a node that just failed. statusOf maps back.
type State string

const (
	// StateSlot is the root of every slot that is still known to the pod.
	StateSlot State = "slot"

	// StateIdle groups the slots that host no node and have no start in
	// flight.
	StateIdle State = "idle"
	// StateUnchanged is an idle slot that has recorded nothing new: a slot
	// never hosted by this process, or one whose start found the slot row
	// gone. Its status is whatever was last recorded.
	StateUnchanged  State = "unchanged"
	StatePending    State = "pending"
	StateError      State = "error"
	StateStopped    State = "stopped"
	StateNeedsLogin State = "needs_login"

	// StatePreparing reads the slot row, the persisted identity and the
	// one-shot key. The original did this in start() before recording
	// anything.
	StatePreparing State = "preparing"
	// StateRegistering starts the node (status starting).
	StateRegistering State = "registering"

	// StateRunning groups the states in which the slot holds a node and
	// probes it.
	StateRunning State = "running"
	// StateAttached is a node that came up and has not been probed yet. It
	// records nothing: the original wrote the next beat from the first probe.
	StateAttached   State = "attached"
	StateConnecting State = "connecting"
	StateOnline     State = "online"
	// StateClearingKey removes the one-shot key once the node is online;
	// StateServing is an online node with nothing left to do.
	StateClearingKey         State = "clearing_key"
	StateServing             State = "serving"
	StateAwaitingApproval    State = "awaiting_approval"
	StateAwaitingCertificate State = "awaiting_certificate"
	// StateRefused and StateFailed record the probe that found the node
	// unusable, then move straight on to draining in the same step.
	StateRefused State = "refused"
	StateFailed  State = "failed"

	// StateDraining closes the node; it still holds it until Close returns.
	StateDraining State = "draining"

	// StateForgotten is a slot the pod dropped from its map.
	StateForgotten State = "forgotten"
)

// statusOf is the status a slot in state s has recorded, as the original's
// slot.state field held it. It is false for the states whose status depends
// on what came before them (unchanged and preparing).
func statusOf(s State) (NodeStatus, bool) {
	switch s {
	case StatePending:
		return NodeStatusPending, true
	case StateError, StateFailed:
		return NodeStatusError, true
	case StateStopped:
		return NodeStatusStopped, true
	case StateNeedsLogin, StateRefused:
		return NodeStatusNeedsLogin, true
	case StateRegistering, StateAttached, StateConnecting:
		return NodeStatusStarting, true
	case StateOnline, StateClearingKey, StateServing:
		return NodeStatusOnline, true
	case StateAwaitingApproval:
		return NodeStatusAwaitingApproval, true
	case StateAwaitingCertificate:
		return NodeStatusAwaitingCertificate, true
	case StateDraining:
		return NodeStatusDraining, true
	default:
		return "", false
	}
}

// hostingStates are the states in which the slot holds a node: what the
// original called running().
var hostingStates = []State{
	StateRunning, StateAttached, StateConnecting, StateOnline, StateClearingKey, StateServing,
	StateAwaitingApproval, StateAwaitingCertificate, StateRefused, StateFailed, StateDraining,
}

// hostedStatus is the status the hosts gauge counts a slot in state s under,
// empty while it holds no node.
func hostedStatus(s State) NodeStatus {
	if !slices.Contains(hostingStates, s) {
		return ""
	}
	status, _ := statusOf(s)
	return status
}

const (
	// maxStartFailures is how many attempts in a row that did not reach
	// online a slot tolerates before it backs off.
	maxStartFailures = 3

	// backoffAfterFailures spaces retries once a slot keeps failing.
	backoffAfterFailures = 5 * time.Minute

	// actionMove is the chart action every recording state runs on entry.
	actionMove = "move"
)

// Slot is the chart's context.
type Slot struct {
	Ingress Ingress `json:"ingress"`

	// Message is the text recorded with the next status.
	Message string `json:"message"`
	NodeID  string `json:"node_id"`
	DNSName string `json:"dns_name"`

	// Failures counts attempts in a row that did not reach online.
	Failures int `json:"failures"`
	// RetryAt is when a slot that reached maxStartFailures may start again,
	// as in the original.
	RetryAt time.Time `json:"retry_at"`

	// Final is the status a draining node records once it is closed.
	Final NodeStatus `json:"final"`

	// Registered, HadKey and AuthKey carry what preparing read to the
	// register service, which receives nothing but the context.
	Registered bool   `json:"registered"`
	HadKey     bool   `json:"had_key"`
	AuthKey    string `json:"-"`

	AuthKeyCleared       bool      `json:"auth_key_cleared"`
	CertificateExpiresAt time.Time `json:"certificate_expires_at"`

	// Node is the running node, nil unless the slot is running or draining.
	Node Node `json:"-"`
}

// Start asks an idle slot to host its ingress.
type Start struct{ Ingress Ingress }

// Stop asks a running slot to stop hosting: the ingress is no longer
// desired, was renamed, or the pod is draining.
type Stop struct{}

// Refresh updates the ingress a slot hosts, such as a new organization slug.
type Refresh struct{ Ingress Ingress }

// Forget drops an idle slot that is no longer desired.
type Forget struct{}

// Probed is one status probe of the slot's node.
type Probed struct {
	// Node is the node that was probed; a probe of a node the slot no longer
	// holds is ignored.
	Node  Node
	State NodeState
	Err   error
}

// preparationOutcome is what preparing found.
type preparationOutcome string

const (
	// prepGone means the slot row was retired or removed, or could not be
	// read; nothing is recorded.
	prepGone        preparationOutcome = "gone"
	prepAwaitingKey preparationOutcome = "awaiting_key"
	prepKeyExpired  preparationOutcome = "key_expired"
	prepRefused     preparationOutcome = "refused"
	prepReady       preparationOutcome = "ready"
)

type preparation struct {
	Outcome    preparationOutcome `json:"outcome"`
	AuthKey    string             `json:"-"`
	Registered bool               `json:"registered"`
	HadKey     bool               `json:"had_key"`
}

type registration struct {
	Node Node   `json:"-"`
	FQDN string `json:"fqdn"`
}

// keyResetError is a failed first registration whose consumed key and
// partial state were cleared, so the reconciler must mint a new key.
type keyResetError struct{ err error }

func (e keyResetError) Error() string { return e.err.Error() }
func (e keyResetError) Unwrap() error { return e.err }

// Service names. The chart declares them with Requires; New binds them to
// the gateway.
const (
	servicePrepare      = "prepare"
	serviceRegister     = "register"
	serviceClose        = "close"
	serviceClearAuthKey = "clearAuthKey"
)

// move is what one entry into a recording state does, the original's
// (*Gateway).move: the status written, the gauge change and the wake.
type move struct {
	// Beat is the status the heartbeat records; empty for a move that
	// writes none (into attached).
	Beat NodeStatus
	// GaugeFrom and GaugeTo are the statuses the hosts gauge moves between,
	// equal when it does not move.
	GaugeFrom, GaugeTo NodeStatus
	// Wake is set when the reconciler is woken after the heartbeat.
	Wake bool
}

// leaf is the innermost of the given active states.
func leaf(active []State) State {
	if len(active) == 0 {
		return ""
	}
	return active[len(active)-1]
}

// moveOf decides what entering mv.Source does. The original decided the
// wake at each call of move: from a probe or stop by its wakes rule, from a
// start explicitly. The chart action gets the same decision from the move it
// runs in: the state left says which caller it was, and the event says
// whether a failed registration consumed the key.
func moveOf(mv sc.Move[State]) move {
	from, to := leaf(mv.From), mv.Source
	beat, _ := statusOf(to)
	if to == StateAttached {
		beat = ""
	}
	m := move{Beat: beat, GaugeFrom: hostedStatus(from), GaugeTo: hostedStatus(to), Wake: false}

	switch from {
	case StatePreparing:
		// Holds, the starting beat and fail(..., false) never wake
		// (gateway.go:627-650).
		m.Wake = false
	case StateRegistering:
		// A refused identity wakes the reconciler to replace it
		// (gateway.go:667), and a consumed key to mint another
		// (gateway.go:739); every other start failure does not
		// (gateway.go:682, 687).
		f, failed := mv.Event.(sc.Failed)
		_, reset := errors.AsType[keyResetError](f.Err)
		m.Wake = to == StateNeedsLogin || (failed && to == StateError && reset)
	default:
		previous, _ := statusOf(from)
		m.Wake = beat != "" && wakes(previous, beat)
	}
	return m
}

// wakes is the original rule (gateway.go:242): moving between two states
// wakes the reconciler when the host started or stopped serving, or started
// waiting on something only the customer can fix.
func wakes(from, to NodeStatus) bool {
	if from == to {
		return false
	}
	switch {
	case from == NodeStatusDraining:
		return false
	case from == NodeStatusOnline, to == NodeStatusOnline:
		return true
	case to == NodeStatusAwaitingApproval, to == NodeStatusAwaitingCertificate, to == NodeStatusNeedsLogin:
		return true
	default:
		return false
	}
}

func setMessage[Ev any](text func(Slot, Ev) string) func(Slot, Ev) Slot {
	return func(c Slot, ev Ev) Slot {
		c.Message = text(c, ev)
		return c
	}
}

// noteFailure counts an attempt that did not reach online; reaching
// maxStartFailures holds the slot until backoffAfterFailures from now.
func noteFailure(c Slot, now time.Time) Slot {
	c.Failures++
	if c.Failures >= maxStartFailures {
		c.RetryAt = now.Add(backoffAfterFailures)
	}
	return c
}

// probeOutcome is one way a probe can turn out, in the order
// probeTransition checked them.
type probeOutcome struct {
	guard   string
	matches func(Probed) bool
	target  State
	message func(Slot, Probed) string
}

var probeOutcomes = []probeOutcome{
	{"probeFailed", func(p Probed) bool { return p.Err != nil }, StateFailed, func(_ Slot, p Probed) string { return p.Err.Error() }},
	{"nodeOnline", func(p Probed) bool { return p.State.Online }, StateOnline, func(Slot, Probed) string { return "" }},
	{"identityRefused", func(p Probed) bool { return p.State.NeedsLogin }, StateRefused, func(_ Slot, p Probed) string { return p.State.BackendState }},
	{"awaitingApproval", func(p Probed) bool { return p.State.AwaitingApproval }, StateAwaitingApproval, func(c Slot, _ Probed) string {
		return "the tailnet has not approved this node to host " + ServiceName(c.Ingress.Label)
	}},
	{"awaitingCertificate", func(p Probed) bool { return p.State.AwaitingCertificate }, StateAwaitingCertificate, func(_ Slot, p Probed) string { return p.State.CertificateStatus }},
	{"stillConnecting", func(Probed) bool { return true }, StateConnecting, func(_ Slot, p Probed) string { return p.State.BackendState }},
}

// observeProbe copies what every probe reports, whatever it decides.
func observeProbe(c Slot, p Probed) Slot {
	if !p.State.CertificateExpiresAt.IsZero() {
		c.CertificateExpiresAt = p.State.CertificateExpiresAt
	}
	if p.State.NodeID != "" {
		c.NodeID = p.State.NodeID
	}
	if p.State.FQDN != "" {
		c.DNSName = p.State.FQDN
	}
	return c
}

// stopping prepares a drain that records final once the node is closed.
// keep says the stop is because of the current state, whose message the
// drain keeps (gateway.go:807-813).
func stopping[Ev any](final NodeStatus, keep bool) func(Slot, Ev, time.Time) Slot {
	return func(c Slot, _ Ev, now time.Time) Slot {
		if !keep {
			c.Message = ""
		}
		if final == NodeStatusError {
			c = noteFailure(c, now)
		}
		c.Final = final
		return c
	}
}

func startFailed(c Slot, f sc.Failed, now time.Time) Slot {
	c = noteFailure(c, now)
	c.Message = f.Err.Error()
	c.AuthKey = ""
	return c
}

// detach drops the closed node and the certificate it served.
func detach[Ev any](c Slot, _ Ev) Slot {
	c.Node = nil
	c.CertificateExpiresAt = time.Time{}
	c.Final = ""
	return c
}

// closed routes the close service's result to the final status. The
// original recorded the final status whether or not Close failed, so the
// error route goes to the same places.
func closed[Ev any](t func() *sc.Transition[State, Slot, Ev]) {
	for _, f := range []struct {
		status NodeStatus
		target State
	}{{NodeStatusError, StateError}, {NodeStatusNeedsLogin, StateNeedsLogin}, {NodeStatusStopped, StateStopped}} {
		t().When("final:"+string(f.status), func(c Slot, _ Ev) bool { return c.Final == f.status }).
			Assign("detach", detach[Ev]).
			To(f.target)
	}
}

// newChart declares the slot chart. Its services and its move action are
// bound by the gateway, so the chart itself has no dependencies and can be
// explored and exported on its own.
func newChart() (*sc.Machine[State, Slot], error) {
	m, err := sc.New("tailnet-gateway-slot", func(b *sc.Builder[State, Slot]) {
		b.RequiresAction(actionMove)
		b.Initial(StateSlot)

		slot := b.State(StateSlot, func(b *sc.Builder[State, Slot]) {
			b.Initial(StateIdle)

			idle := b.State(StateIdle, func(b *sc.Builder[State, Slot]) {
				b.Initial(StateUnchanged)
				b.State(StateUnchanged)
				for _, s := range []State{StatePending, StateError, StateStopped, StateNeedsLogin} {
					b.State(s).OnEntry(actionMove)
				}
			})
			idle.On[Start]().
				WhenAt("notBackingOff", func(c Slot, _ Start, now time.Time) bool { return !now.Before(c.RetryAt) }).
				Assign("target", func(c Slot, s Start) Slot {
					c.Ingress = s.Ingress
					return c
				}).
				To(StatePreparing)
			idle.On[Forget]().To(StateForgotten)

			prepare := b.State(StatePreparing).Requires[preparation](servicePrepare)
			prepare.OnDone().
				When("slotGone", func(_ Slot, d sc.Done[preparation]) bool { return d.Result.Outcome == prepGone }).
				To(StateUnchanged)
			prepare.OnDone().
				When("awaitingKey", func(_ Slot, d sc.Done[preparation]) bool {
					return d.Result.Outcome == prepAwaitingKey || d.Result.Outcome == prepKeyExpired
				}).
				Assign("message", setMessage(func(_ Slot, d sc.Done[preparation]) string {
					if d.Result.Outcome == prepKeyExpired {
						return "auth key expired"
					}
					return "awaiting auth key"
				})).
				To(StatePending)
			prepare.OnDone().
				When("identityRefused", func(_ Slot, d sc.Done[preparation]) bool { return d.Result.Outcome == prepRefused }).
				Assign("message", setMessage(func(Slot, sc.Done[preparation]) string { return "awaiting re-registration" })).
				To(StateNeedsLogin)
			prepare.OnDone().
				Assign("ready", func(c Slot, d sc.Done[preparation]) Slot {
					c.AuthKey, c.Registered, c.HadKey, c.Message = d.Result.AuthKey, d.Result.Registered, d.Result.HadKey, ""
					return c
				}).
				To(StateRegistering)
			prepare.OnError().AssignAt("startFailed", startFailed).To(StateError)

			registering := b.State(StateRegistering).OnEntry(actionMove)
			register := registering.Requires[registration](serviceRegister)
			register.OnDone().
				Assign("attach", func(c Slot, d sc.Done[registration]) Slot {
					c.Node, c.NodeID, c.DNSName, c.AuthKey = d.Result.Node, "", d.Result.FQDN, ""
					// The key row is the source of truth: a restart after a
					// failed clear still removes it once the node is online.
					c.AuthKeyCleared = !c.HadKey
					return c
				}).
				To(StateAttached)
			register.OnError().
				When("knownIdentityRefused", func(c Slot, f sc.Failed) bool { return c.Registered && errors.Is(f.Err, ErrNodeNeedsLogin) }).
				Assign("message", setMessage(func(_ Slot, f sc.Failed) string { return f.Err.Error() })).
				Assign("forgetKey", func(c Slot, _ sc.Failed) Slot {
					c.AuthKey = ""
					return c
				}).
				To(StateNeedsLogin)
			register.OnError().AssignAt("startFailed", startFailed).To(StateError)

			running := b.State(StateRunning, func(b *sc.Builder[State, Slot]) {
				b.Initial(StateAttached)
				for _, s := range []State{StateAttached, StateConnecting, StateAwaitingApproval, StateAwaitingCertificate} {
					b.State(s).OnEntry(actionMove)
				}
				// Every probe that finds the node online re-enters online, so
				// a key whose clear failed is tried again by the next one, as
				// the original did (gateway.go:778).
				b.State(StateOnline, func(b *sc.Builder[State, Slot]) {
					b.Initial(StateClearingKey)
					clearing := b.State(StateClearingKey)
					clearing.Always().
						When("keyCleared", func(c Slot, _ struct{}) bool { return c.AuthKeyCleared }).
						To(StateServing)
					clearKey := clearing.Requires[struct{}](serviceClearAuthKey)
					clearKey.OnDone().
						Assign("keyCleared", func(c Slot, _ sc.Done[struct{}]) Slot {
							c.AuthKeyCleared = true
							return c
						}).
						To(StateServing)
					clearKey.OnError().To(StateServing)
					b.State(StateServing)
				}).OnEntry(actionMove)

				b.State(StateRefused).OnEntry(actionMove).Always().
					AssignAt("stopping", stopping[struct{}](NodeStatusNeedsLogin, true)).
					To(StateDraining)
				b.State(StateFailed).OnEntry(actionMove).Always().
					AssignAt("stopping", stopping[struct{}](NodeStatusError, true)).
					To(StateDraining)
			})
			// One probe table for every running state. Each probe re-enters
			// the state it decides, so a probe that keeps the status writes
			// its heartbeat again, as the original's did.
			for _, o := range probeOutcomes {
				t := running.On[Probed]().
					When("sameNode", func(c Slot, p Probed) bool { return p.Node == c.Node }).
					When(o.guard, func(_ Slot, p Probed) bool { return o.matches(p) }).
					Assign("observe", observeProbe).
					Assign("message", setMessage(o.message))
				if o.target == StateOnline {
					// Serving is what counts as success for the backoff.
					t.Assign("serving", func(c Slot, _ Probed) Slot {
						c.Failures, c.RetryAt = 0, time.Time{}
						return c
					})
				}
				t.To(o.target)
			}
			running.On[Stop]().AssignAt("stopping", stopping[Stop](NodeStatusStopped, false)).To(StateDraining)

			draining := b.State(StateDraining).OnEntry(actionMove)
			closing := draining.Requires[struct{}](serviceClose)
			closed(closing.OnDone)
			closed(closing.OnError)
		})
		slot.On[Refresh]().
			When("changed", func(c Slot, r Refresh) bool { return r.Ingress != c.Ingress }).
			Assign("refresh", func(c Slot, r Refresh) Slot {
				c.Ingress = r.Ingress
				return c
			})
		b.Final(StateForgotten)
	})
	if err != nil {
		return nil, fmt.Errorf("build tailnet gateway slot chart: %w", err)
	}
	return m, nil
}

// current is the innermost active state of snap.
func current(snap sc.Snapshot[State, Slot]) State {
	return leaf(snap.Active())
}

// hosting reports whether the slot holds a node: running or draining. The
// original called this running().
func hosting(snap sc.Snapshot[State, Slot]) bool {
	return snap.In(StateRunning) || snap.In(StateDraining)
}

// bindChart gives the chart the gateway's services and move action.
func bindChart(m *sc.Machine[State, Slot], e gatewayEnv) (*sc.Machine[State, Slot], error) {
	bound, err := m.Bind(func(b *sc.Binder[State, Slot]) {
		b.Service(servicePrepare, e.prepare)
		b.Service(serviceRegister, e.register)
		b.Service(serviceClose, e.closeNode)
		b.Service(serviceClearAuthKey, e.clearAuthKey)
		b.Action(actionMove, func(ctx context.Context, c Slot, mv sc.Move[State]) {
			e.move(ctx, c, moveOf(mv))
		})
	})
	if err != nil {
		return nil, fmt.Errorf("bind tailnet gateway slot chart: %w", err)
	}
	return bound, nil
}
