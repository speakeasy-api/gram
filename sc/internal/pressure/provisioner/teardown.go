package provisioner

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/sc"
)

// TeardownState identifies a state of the teardown chart.
type TeardownState string

const (
	TeardownListing         TeardownState = "listing"
	TeardownRetiring        TeardownState = "retiring"
	TeardownDeletingDevices TeardownState = "deleting_devices"
	TeardownDeletingService TeardownState = "deleting_service"
	TeardownPurging         TeardownState = "purging"

	// Final states: what one DeleteWithCredentials call ended with.

	// TeardownUntouched means Apply never created a slot, so nothing reached
	// the tailnet and Tailscale is not called.
	TeardownUntouched TeardownState = "untouched"
	// TeardownRejected means credentials are missing or unreadable.
	TeardownRejected TeardownState = "rejected"
	// TeardownWaiting means a pod still hosts a slot; the reconciler retries.
	TeardownWaiting TeardownState = "waiting_for_hosts"
	TeardownDeleted TeardownState = "deleted"
	TeardownFailed  TeardownState = "failed"
)

// teardownPass is the teardown chart's context. The credentials themselves
// are bound into the services, not kept here.
type teardownPass struct {
	Owner Owner `json:"owner"`
	// HasCredentials and CredentialsOK say what parsing the stored
	// credentials found.
	HasCredentials bool `json:"has_credentials"`
	CredentialsOK  bool `json:"credentials_ok"`

	// NodeIDs are the devices to delete, from the slots after retiring.
	NodeIDs []string `json:"node_ids"`
	// HostedSlot is a slot a pod still hosts when waiting.
	HostedSlot int32 `json:"hosted_slot"`

	Failure error `json:"-"`
}

// retired is what retiring found once every slot was retired.
type retired struct {
	NodeIDs    []string `json:"node_ids"`
	Hosted     bool     `json:"hosted"`
	HostedSlot int32    `json:"hosted_slot"`
}

// Teardown chart service names.
const (
	teardownCountSlots    = "countSlots"
	teardownRetire        = "retire"
	teardownDeleteDevices = "deleteDevices"
	teardownDeleteService = "deleteService"
	teardownPurge         = "purge"
)

func teardownFailed(p teardownPass, f sc.Failed) teardownPass {
	p.Failure = f.Err
	return p
}

// newTeardownChart declares the teardown chart. An instance starts listing
// the ingress's slots at once.
func newTeardownChart() (*sc.Machine[TeardownState, teardownPass], error) {
	m, err := sc.New("tailnet-gateway-teardown", func(b *sc.Builder[TeardownState, teardownPass]) {
		b.Initial(TeardownListing)

		list := b.State(TeardownListing).Requires[int](teardownCountSlots)
		// Apply creates the slot rows right after defining the service, so
		// an ingress without them never got a device onto the tailnet.
		list.OnDone().When("noSlots", func(_ teardownPass, d sc.Done[int]) bool { return d.Result == 0 }).To(TeardownUntouched)
		list.OnDone().When("noCredentials", func(p teardownPass, _ sc.Done[int]) bool { return !p.HasCredentials || !p.CredentialsOK }).To(TeardownRejected)
		list.OnDone().To(TeardownRetiring)
		list.OnError().Assign("failed", teardownFailed).To(TeardownFailed)

		retire := b.State(TeardownRetiring).Requires[retired](teardownRetire)
		retire.OnDone().
			When("stillHosted", func(_ teardownPass, d sc.Done[retired]) bool { return d.Result.Hosted }).
			Assign("hostedSlot", func(p teardownPass, d sc.Done[retired]) teardownPass {
				p.HostedSlot = d.Result.HostedSlot
				return p
			}).
			To(TeardownWaiting)
		retire.OnDone().
			Assign("devices", func(p teardownPass, d sc.Done[retired]) teardownPass {
				p.NodeIDs = d.Result.NodeIDs
				return p
			}).
			To(TeardownDeletingDevices)
		retire.OnError().Assign("failed", teardownFailed).To(TeardownFailed)

		devices := b.State(TeardownDeletingDevices).Requires[struct{}](teardownDeleteDevices)
		devices.OnDone().To(TeardownDeletingService)
		devices.OnError().Assign("failed", teardownFailed).To(TeardownFailed)

		service := b.State(TeardownDeletingService).Requires[struct{}](teardownDeleteService)
		service.OnDone().To(TeardownPurging)
		service.OnError().Assign("failed", teardownFailed).To(TeardownFailed)

		purge := b.State(TeardownPurging).Requires[struct{}](teardownPurge)
		purge.OnDone().To(TeardownDeleted)
		purge.OnError().Assign("failed", teardownFailed).To(TeardownFailed)

		for _, final := range []TeardownState{TeardownUntouched, TeardownRejected, TeardownWaiting, TeardownDeleted, TeardownFailed} {
			b.Final(final)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("build teardown chart: %w", err)
	}
	return m, nil
}

// teardownPassEnv carries out the teardown chart for one ingress.
type teardownPassEnv struct {
	p     *Provisioner
	id    ResourceNames
	creds Credentials
}

// bind gives the teardown chart this call's services.
func (e teardownPassEnv) bind(m *sc.Machine[TeardownState, teardownPass]) (*sc.Machine[TeardownState, teardownPass], error) {
	bound, err := m.Bind(func(b *sc.Binder[TeardownState, teardownPass]) {
		b.Service(teardownCountSlots, e.countSlots)
		b.Service(teardownRetire, e.retire)
		b.Service(teardownDeleteDevices, e.deleteDevices)
		b.Service(teardownDeleteService, e.deleteService)
		b.Service(teardownPurge, e.purge)
	})
	if err != nil {
		return nil, fmt.Errorf("bind teardown chart: %w", err)
	}
	return bound, nil
}

func (e teardownPassEnv) countSlots(ctx context.Context, t teardownPass) (int, error) {
	existing, err := e.p.store.ListSlots(ctx, e.id.OwnerID, t.Owner.OrganizationID)
	if err != nil {
		return 0, fmt.Errorf("list gateway slots: %w", err)
	}
	return len(existing), nil
}

func (e teardownPassEnv) retire(ctx context.Context, t teardownPass) (retired, error) {
	none := retired{NodeIDs: nil, Hosted: false, HostedSlot: 0}
	if err := e.p.store.RetireSlots(ctx, e.id.OwnerID, t.Owner.OrganizationID, 0); err != nil {
		return none, fmt.Errorf("retire gateway slots: %w", err)
	}
	nodes, err := e.p.store.ListSlots(ctx, e.id.OwnerID, t.Owner.OrganizationID)
	if err != nil {
		return none, fmt.Errorf("list gateway slots: %w", err)
	}
	now := e.p.cfg.Now()
	for _, node := range nodes {
		if e.p.fresh(node, now) && hostingStatus(node.Status) {
			return retired{NodeIDs: nil, Hosted: true, HostedSlot: node.Slot}, nil
		}
	}
	var ids []string
	for _, node := range nodes {
		if node.NodeID != "" {
			ids = append(ids, node.NodeID)
		}
	}
	return retired{NodeIDs: ids, Hosted: false, HostedSlot: 0}, nil
}

func (e teardownPassEnv) deleteDevices(ctx context.Context, t teardownPass) (struct{}, error) {
	for _, id := range t.NodeIDs {
		if err := e.p.api.DeleteDevice(ctx, e.creds, id); err != nil {
			return struct{}{}, mapAPIError(err)
		}
	}
	return struct{}{}, nil
}

func (e teardownPassEnv) deleteService(ctx context.Context, t teardownPass) (struct{}, error) {
	if t.Owner.Hostname == "" {
		return struct{}{}, nil
	}
	if err := e.p.api.DeleteService(ctx, e.creds, serviceName(t.Owner.Hostname)); err != nil {
		return struct{}{}, mapAPIError(err)
	}
	return struct{}{}, nil
}

func (e teardownPassEnv) purge(ctx context.Context, t teardownPass) (struct{}, error) {
	id, org := e.id.OwnerID, t.Owner.OrganizationID
	if err := e.p.store.DeleteStates(ctx, id, org); err != nil {
		return struct{}{}, fmt.Errorf("delete gateway node state: %w", err)
	}
	if err := e.p.store.DeleteCertificateStates(ctx, id, org); err != nil {
		return struct{}{}, fmt.Errorf("delete gateway certificate state: %w", err)
	}
	if err := e.p.store.DeleteSlots(ctx, id, org); err != nil {
		return struct{}{}, fmt.Errorf("delete gateway slots: %w", err)
	}
	return struct{}{}, nil
}
