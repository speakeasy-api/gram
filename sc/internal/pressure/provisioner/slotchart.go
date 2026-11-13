package provisioner

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/sc"
	"github.com/speakeasy-api/gram/sc/internal/pressure/host"
)

// SlotState identifies a state of the slot reconcile chart.
type SlotState string

const (
	SlotClassify  SlotState = "classify"
	SlotPurging   SlotState = "purging"
	SlotChecking  SlotState = "checking"
	SlotResetting SlotState = "resetting"
	SlotMinting   SlotState = "minting"

	// Final states: what one Apply did with the slot.

	SlotHostStillLive SlotState = "host_still_live"
	SlotPurged        SlotState = "purged"
	SlotSurplus       SlotState = "surplus"
	SlotRegistered    SlotState = "registered"
	SlotKeyHeld       SlotState = "key_held"
	SlotMinted        SlotState = "minted"
	SlotFailed        SlotState = "failed"
)

// slotFacts is what one Apply reads about a slot before deciding.
type slotFacts struct {
	Slot   int32  `json:"slot"`
	NodeID string `json:"node_id"`
	// Retired is set once the slot count dropped below it or teardown began.
	Retired bool `json:"retired"`
	// Hosted means a pod still reports the slot live: a fresh heartbeat
	// whose status is not stopped, pending, error or needs_login.
	Hosted bool `json:"hosted"`
	// Refused means the pod recorded needs_login.
	Refused bool `json:"refused"`
	// KeyValid means the slot holds a key that outlives the renewal margin.
	KeyValid bool `json:"key_valid"`
	// Surplus means the slot is at or above the configured count but not
	// retired, which RetireSlots makes impossible.
	Surplus bool `json:"surplus"`
}

// slotPass is the slot chart's context: one Apply's view of one slot.
type slotPass struct {
	Facts slotFacts `json:"facts"`
	// Registered is what the registration check found.
	Registered bool `json:"registered"`
	// Reset is set once a refused identity was replaced.
	Reset bool `json:"reset"`
	// Failure is why the pass failed; it classifies itself with failureKind.
	Failure error `json:"-"`
}

// failureKind says which observation a failed step maps to.
type failureKind int

const (
	failureOther failureKind = iota
	failureAPI
	failureEncryption
)

// stepError is a failed step together with how Apply reports it.
type stepError struct {
	kind failureKind
	err  error
}

func (e stepError) Error() string { return e.err.Error() }
func (e stepError) Unwrap() error { return e.err }

// Slot chart service names. Apply binds them for its own ingress and
// credentials.
const (
	slotPurge           = "purge"
	slotCheckRegistered = "checkRegistered"
	slotReset           = "reset"
	slotMint            = "mint"
)

func failed(p slotPass, f sc.Failed) slotPass {
	p.Failure = f.Err
	return p
}

// newSlotChart declares the slot chart. An instance starts with the facts
// one Apply read about one slot and classifies them at once.
func newSlotChart() (*sc.Machine[SlotState, slotPass], error) {
	m, err := sc.New("tailnet-gateway-slot-reconcile", func(b *sc.Builder[SlotState, slotPass]) {
		b.Initial(SlotClassify)

		classify := b.State(SlotClassify)
		// A retired slot whose host has stopped is purged: its device leaves
		// the tailnet and a later slot count increase starts a fresh
		// identity. A live one is left until its pod stops hosting it.
		classify.Always().When("retiredButHosted", func(p slotPass, _ struct{}) bool { return p.Facts.Retired && p.Facts.Hosted }).To(SlotHostStillLive)
		classify.Always().When("retired", func(p slotPass, _ struct{}) bool { return p.Facts.Retired }).To(SlotPurging)
		classify.Always().When("surplus", func(p slotPass, _ struct{}) bool { return p.Facts.Surplus }).To(SlotSurplus)
		classify.Always().To(SlotChecking)

		purge := b.State(SlotPurging).Requires[struct{}](slotPurge)
		purge.OnDone().To(SlotPurged)
		purge.OnError().Assign("failed", failed).To(SlotFailed)

		check := b.State(SlotChecking).Requires[bool](slotCheckRegistered)
		check.OnDone().
			When("registered", func(_ slotPass, d sc.Done[bool]) bool { return d.Result }).
			When("refused", func(p slotPass, _ sc.Done[bool]) bool { return p.Facts.Refused }).
			Assign("registered", registered).
			To(SlotResetting)
		check.OnDone().When("registered", func(_ slotPass, d sc.Done[bool]) bool { return d.Result }).Assign("registered", registered).To(SlotRegistered)
		check.OnDone().When("keyValid", func(p slotPass, _ sc.Done[bool]) bool { return p.Facts.KeyValid }).Assign("registered", registered).To(SlotKeyHeld)
		check.OnDone().Assign("registered", registered).To(SlotMinting)
		check.OnError().Assign("failed", failed).To(SlotFailed)

		reset := b.State(SlotResetting).Requires[struct{}](slotReset)
		reset.OnDone().
			When("keyValid", func(p slotPass, _ sc.Done[struct{}]) bool { return p.Facts.KeyValid }).
			Assign("reset", wasReset).
			To(SlotKeyHeld)
		reset.OnDone().Assign("reset", wasReset).To(SlotMinting)
		reset.OnError().Assign("failed", failed).To(SlotFailed)

		mint := b.State(SlotMinting).Requires[struct{}](slotMint)
		mint.OnDone().To(SlotMinted)
		mint.OnError().Assign("failed", failed).To(SlotFailed)

		for _, final := range []SlotState{SlotHostStillLive, SlotPurged, SlotSurplus, SlotRegistered, SlotKeyHeld, SlotMinted, SlotFailed} {
			b.Final(final)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("build slot reconcile chart: %w", err)
	}
	return m, nil
}

func registered(p slotPass, d sc.Done[bool]) slotPass {
	p.Registered = d.Result
	return p
}

func wasReset(p slotPass, _ sc.Done[struct{}]) slotPass {
	p.Registered, p.Reset = false, true
	return p
}

// hostingStatus reports whether a fresh heartbeat with this status means a
// pod still hosts the slot.
func hostingStatus(status string) bool {
	switch host.NodeStatus(status) {
	case host.NodeStatusStopped, host.NodeStatusPending, host.NodeStatusError, host.NodeStatusNeedsLogin:
		return false
	default:
		return true
	}
}

// slotPassEnv carries out the slot chart for one Apply.
type slotPassEnv struct {
	p       *Provisioner
	desired Desired
	creds   Credentials
}

// bind gives the slot chart this Apply's services. The credentials stay in
// the closures, out of the chart's context.
func (e slotPassEnv) bind(m *sc.Machine[SlotState, slotPass]) (*sc.Machine[SlotState, slotPass], error) {
	bound, err := m.Bind(func(b *sc.Binder[SlotState, slotPass]) {
		b.Service(slotPurge, e.purge)
		b.Service(slotCheckRegistered, e.checkRegistered)
		b.Service(slotReset, e.reset)
		b.Service(slotMint, e.mint)
	})
	if err != nil {
		return nil, fmt.Errorf("bind slot reconcile chart: %w", err)
	}
	return bound, nil
}

// purge deletes a retired slot's device and rows.
func (e slotPassEnv) purge(ctx context.Context, s slotPass) (struct{}, error) {
	if s.Facts.NodeID != "" {
		if err := e.p.api.DeleteDevice(ctx, e.creds, s.Facts.NodeID); err != nil {
			return struct{}{}, stepError{kind: failureAPI, err: fmt.Errorf("delete retired slot device: %w", err)}
		}
	}
	if err := e.p.store.DeleteSlotStates(ctx, e.desired.ID, e.desired.OrganizationID, s.Facts.Slot); err != nil {
		return struct{}{}, stepError{kind: failureAPI, err: fmt.Errorf("purge retired slot state: %w", err)}
	}
	if err := e.p.store.DeleteSlot(ctx, e.desired.ID, e.desired.OrganizationID, s.Facts.Slot); err != nil {
		return struct{}{}, stepError{kind: failureAPI, err: fmt.Errorf("purge retired slot: %w", err)}
	}
	return struct{}{}, nil
}

func (e slotPassEnv) stateStore(slot int32) (host.NodeStateStore, error) {
	store, err := e.p.store.NodeStateStore(e.desired.ID, e.desired.OrganizationID, slot)
	if err != nil {
		return nil, stepError{kind: failureOther, err: fmt.Errorf("build gateway slot state store: %w", err)}
	}
	return store, nil
}

func (e slotPassEnv) checkRegistered(ctx context.Context, s slotPass) (bool, error) {
	store, err := e.stateStore(s.Facts.Slot)
	if err != nil {
		return false, err
	}
	registered, err := store.Registered(ctx)
	if err != nil {
		return false, stepError{kind: failureEncryption, err: fmt.Errorf("check gateway slot registration: %w", err)}
	}
	return registered, nil
}

// reset replaces an identity control no longer accepts. The pod has already
// stopped hosting it.
func (e slotPassEnv) reset(ctx context.Context, s slotPass) (struct{}, error) {
	store, err := e.stateStore(s.Facts.Slot)
	if err != nil {
		return struct{}{}, err
	}
	if s.Facts.NodeID != "" {
		if err := e.p.api.DeleteDevice(ctx, e.creds, s.Facts.NodeID); err != nil {
			return struct{}{}, stepError{kind: failureAPI, err: fmt.Errorf("delete refused slot device: %w", err)}
		}
	}
	if err := store.Reset(ctx); err != nil {
		return struct{}{}, stepError{kind: failureAPI, err: fmt.Errorf("reset refused slot state: %w", err)}
	}
	return struct{}{}, nil
}

func (e slotPassEnv) mint(ctx context.Context, s slotPass) (struct{}, error) {
	p, now := e.p, e.p.cfg.Now()
	key, err := p.api.CreateAuthKey(ctx, e.creds, AuthKeyRequest{
		Tag: p.cfg.Tag, Description: authKeyDescription(e.desired.ID, s.Facts.Slot), TTL: p.cfg.AuthKeyTTL,
	})
	if err != nil {
		return struct{}{}, stepError{kind: failureAPI, err: err}
	}
	ciphertext, err := p.keys.EncryptAuthKey(ctx, e.desired.OrganizationID, key.Key)
	if err != nil {
		return struct{}{}, stepError{kind: failureEncryption, err: fmt.Errorf("encrypt gateway auth key: %w", err)}
	}
	expires := key.Expires
	if expires.IsZero() {
		expires = now.Add(p.cfg.AuthKeyTTL)
	}
	if _, err := p.store.SetAuthKey(ctx, e.desired.ID, e.desired.OrganizationID, s.Facts.Slot, ciphertext, expires); err != nil {
		return struct{}{}, stepError{kind: failureOther, err: fmt.Errorf("store gateway auth key: %w", err)}
	}
	return struct{}{}, nil
}

// failureObservation maps a failed slot pass to what Apply reports.
func failureObservation(err error) (Observation, error) {
	step, ok := errors.AsType[stepError](err)
	if !ok {
		return errorObservation(""), err
	}
	switch step.kind {
	case failureAPI:
		return apiFailure(step.err)
	case failureEncryption:
		return encryptionFailure(step.err)
	default:
		return errorObservation(""), step.err
	}
}
