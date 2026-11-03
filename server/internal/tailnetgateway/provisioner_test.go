package tailnetgateway_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"tailscale.com/ipn"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/tailscaleapi"
)

type provisionerFixture struct {
	db          *pgxpool.Pool
	enc         *encryption.Client
	api         *tailscaleapi.Fake
	provisioner *tailnetgateway.Provisioner
	now         time.Time
	ingressID   uuid.UUID
	orgID       string
	resources   k8s.NetworkIngressResourceNames
}

func newProvisionerFixture(t *testing.T, name string) *provisionerFixture {
	t.Helper()
	db, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	enc := testenv.NewEncryptionClient(t)
	api := tailscaleapi.NewFake()
	// Heartbeats are stamped by the database clock, so the provisioner's
	// clock must be the real one for freshness to mean anything.
	now := time.Now().UTC()
	api.Now = time.Now
	ingressID, orgID := seedIngress(t, db, name)
	resources, err := k8s.NewNetworkIngressResourceNames(ingressID)
	require.NoError(t, err)
	provisioner, err := tailnetgateway.NewProvisioner(db, enc, api, tailnetgateway.ProvisionerConfig{
		Tag: tailnetgateway.DefaultTag, Slots: 3, AuthKeyTTL: 24 * time.Hour, StaleAfter: 2 * time.Minute, Now: time.Now,
	})
	require.NoError(t, err)
	// seedIngress leaves the ingress on the gateway; tests of the operator
	// path move it back with setRuntime.
	return &provisionerFixture{db: db, enc: enc, api: api, provisioner: provisioner, now: now, ingressID: ingressID, orgID: orgID, resources: resources}
}

// setRuntime records which runtime hosts the fixture's ingress; empty means
// the operator.
func (f *provisionerFixture) setRuntime(t *testing.T, runtime string) {
	t.Helper()
	_, err := repo.New(f.db).MarkNetworkIngressRuntime(t.Context(), repo.MarkNetworkIngressRuntimeParams{
		Runtime: pgtype.Text{String: runtime, Valid: runtime != ""}, ID: f.ingressID, OrganizationID: f.orgID,
	})
	require.NoError(t, err)
}

func credentialsJSON(t *testing.T, id, secret string) []byte {
	t.Helper()
	encoded, err := json.Marshal(k8s.TailscaleCredentials{ClientID: id, ClientSecret: secret})
	require.NoError(t, err)
	return encoded
}

func (f *provisionerFixture) desired(t *testing.T) k8s.NetworkIngressDesired {
	t.Helper()
	return k8s.NetworkIngressDesired{
		ID: f.ingressID, OrganizationID: f.orgID, Provider: k8s.NetworkIngressProviderTailscale, Hostname: "private",
		Credentials: credentialsJSON(t, "cid", "csecret"), Resources: f.resources, AttestorImage: "", BackendService: "", BackendPort: 0,
	}
}

func (f *provisionerFixture) heartbeat(t *testing.T, slot int32, status tailnetgateway.NodeStatus, nodeID string) {
	t.Helper()
	_, err := repo.New(f.db).HeartbeatNetworkIngressNode(t.Context(), repo.HeartbeatNetworkIngressNodeParams{
		PodName: conv.ToPGText("gram-tailnet-gateway-0"), Status: string(status), NodeID: conv.ToPGTextEmpty(nodeID),
		DnsName: conv.ToPGText("private.example.ts.net"), LastError: conv.ToPGTextEmpty(""),
		NetworkIngressID: f.ingressID, OrganizationID: f.orgID, Slot: slot,
	})
	require.NoError(t, err)
}

func TestProvisionerApplyDefinesServiceAndMintsKeysPerSlot(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovapply")

	observation, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressStatusPending, observation.Status)

	services := f.api.Services()
	require.Contains(t, services, "svc:private")
	require.Equal(t, []string{"tcp:443"}, services["svc:private"].Ports)
	require.Equal(t, []string{"tag:speakeasy"}, services["svc:private"].Tags)
	require.Len(t, f.api.Keys(), 3, "one key per slot")

	nodes, err := repo.New(f.db).ListNetworkIngressNodes(ctx, repo.ListNetworkIngressNodesParams{NetworkIngressID: f.ingressID, OrganizationID: f.orgID})
	require.NoError(t, err)
	require.Len(t, nodes, 3)
	for i, node := range nodes {
		require.EqualValues(t, i, node.Slot)
		require.True(t, node.AuthKeyEncrypted.Valid)
		plain, err := f.enc.Decrypt(node.AuthKeyEncrypted.String)
		require.NoError(t, err)
		require.Equal(t, f.api.Keys()[i].Key, plain)
		require.False(t, node.RetiredAt.Valid)
	}
	// A second apply keeps the unexpired keys and re-upserts the service.
	_, err = f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Len(t, f.api.Keys(), 3)

	// A slot that registered no longer needs a key even when its key is gone;
	// a slot with only partial state still does.
	store, err := tailnetgateway.NewPostgresStateStore(f.db, f.enc, f.ingressID, f.orgID, 1)
	require.NoError(t, err)
	require.NoError(t, store.WriteState(ipn.KnownProfilesStateKey, []byte(`[{"ID":"p1"}]`)))
	partial, err := tailnetgateway.NewPostgresStateStore(f.db, f.enc, f.ingressID, f.orgID, 2)
	require.NoError(t, err)
	require.NoError(t, partial.WriteState(ipn.MachineKeyStateKey, []byte("machine-only")))
	require.NoError(t, repo.New(f.db).ClearNetworkIngressNodeAuthKey(ctx, repo.ClearNetworkIngressNodeAuthKeyParams{NetworkIngressID: f.ingressID, OrganizationID: f.orgID, Slot: 1}))
	require.NoError(t, repo.New(f.db).ClearNetworkIngressNodeAuthKey(ctx, repo.ClearNetworkIngressNodeAuthKeyParams{NetworkIngressID: f.ingressID, OrganizationID: f.orgID, Slot: 2}))
	_, err = f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Len(t, f.api.Keys(), 4, "only the unregistered slot without a key is re-minted")

	// Renaming the label defines the new service and removes the old one.
	f.heartbeat(t, 0, tailnetgateway.NodeStatusOnline, "nONE")
	renamed := f.desired(t)
	renamed.Hostname = "speakeasy"
	_, err = f.provisioner.Apply(ctx, renamed)
	require.NoError(t, err)
	require.Contains(t, f.api.Services(), "svc:speakeasy")
	require.NotContains(t, f.api.Services(), "svc:private", "the previous label's service is deleted")
}

func TestProvisionerScaleDownPurgesRetiredSlotsAfterHostsStop(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovscale")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	f.heartbeat(t, 2, tailnetgateway.NodeStatusOnline, "nTHREE")

	smaller, err := tailnetgateway.NewProvisioner(f.db, f.enc, f.api, tailnetgateway.ProvisionerConfig{
		Tag: tailnetgateway.DefaultTag, Slots: 2, AuthKeyTTL: 24 * time.Hour, StaleAfter: 2 * time.Minute, Now: time.Now,
	})
	require.NoError(t, err)
	_, err = smaller.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	nodes, err := repo.New(f.db).ListNetworkIngressNodes(ctx, repo.ListNetworkIngressNodesParams{NetworkIngressID: f.ingressID, OrganizationID: f.orgID})
	require.NoError(t, err)
	require.Len(t, nodes, 3)
	require.True(t, nodes[2].RetiredAt.Valid, "slot 2 is retired while its host is still live")
	require.Empty(t, f.api.DeletedDevices(), "a live host is not deleted from under the pod")

	// The pod stops hosting the retired slot; the next apply removes it.
	f.heartbeat(t, 2, tailnetgateway.NodeStatusStopped, "nTHREE")
	_, err = smaller.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Equal(t, []string{"nTHREE"}, f.api.DeletedDevices())
	nodes, err = repo.New(f.db).ListNetworkIngressNodes(ctx, repo.ListNetworkIngressNodesParams{NetworkIngressID: f.ingressID, OrganizationID: f.orgID})
	require.NoError(t, err)
	require.Len(t, nodes, 2)

	// Raising the count again creates slot 2 as a fresh identity with a new key.
	_, err = f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	nodes, err = repo.New(f.db).ListNetworkIngressNodes(ctx, repo.ListNetworkIngressNodesParams{NetworkIngressID: f.ingressID, OrganizationID: f.orgID})
	require.NoError(t, err)
	require.Len(t, nodes, 3)
	require.False(t, nodes[2].RetiredAt.Valid)
	require.False(t, nodes[2].NodeID.Valid)
	require.Len(t, f.api.Keys(), 4)
}

func TestProvisionerApplyRejectsBadCredentialsAndInput(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovreject")

	desired := f.desired(t)
	desired.Credentials = []byte(`{"client_id":"","client_secret":""}`)
	observation, err := f.provisioner.Apply(ctx, desired)
	require.Error(t, err)
	require.Equal(t, k8s.NetworkIngressErrorInvalidCredentials, observation.ErrorCode)

	f.api.Reject = tailscaleapi.ErrUnauthorized
	observation, err = f.provisioner.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, tailscaleapi.ErrUnauthorized)
	require.Equal(t, k8s.NetworkIngressErrorInvalidCredentials, observation.ErrorCode)
	f.api.Reject = nil

	desired = f.desired(t)
	desired.Hostname = "Not A Label"
	observation, err = f.provisioner.Apply(ctx, desired)
	require.ErrorIs(t, err, k8s.ErrNetworkIngressInvalidDesiredState)
	require.Equal(t, k8s.NetworkIngressErrorInvalidDesiredState, observation.ErrorCode)

	desired = f.desired(t)
	desired.OrganizationID = ""
	_, err = f.provisioner.Apply(ctx, desired)
	require.ErrorIs(t, err, k8s.ErrNetworkIngressInvalidDesiredState)
}

func TestProvisionerObserveFollowsHeartbeats(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovobserve")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)

	observation, err := f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressStatusPending, observation.Status)

	f.heartbeat(t, 0, tailnetgateway.NodeStatusStarting, "")
	observation, err = f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressStatusPending, observation.Status)

	f.heartbeat(t, 0, tailnetgateway.NodeStatusOnline, "nONE")
	observation, err = f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressStatusOnline, observation.Status)
	require.Equal(t, "private.example.ts.net", observation.DNSName)
	require.NotNil(t, observation.ConnectedAt)

	f.heartbeat(t, 0, tailnetgateway.NodeStatusError, "nONE")
	observation, err = f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressStatusError, observation.Status)

	// Heartbeats older than the staleness window mean the gateway is gone.
	f.heartbeat(t, 0, tailnetgateway.NodeStatusOnline, "nONE")
	stale, err := tailnetgateway.NewProvisioner(f.db, f.enc, f.api, tailnetgateway.ProvisionerConfig{
		Tag: tailnetgateway.DefaultTag, Slots: 3, AuthKeyTTL: time.Hour, StaleAfter: 2 * time.Minute,
		Now: func() time.Time { return time.Now().Add(time.Hour) },
	})
	require.NoError(t, err)
	observation, err = stale.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressStatusDegraded, observation.Status)
}

func TestProvisionerDeleteWaitsForHostsThenRemovesDevices(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovdelete")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	store, err := tailnetgateway.NewPostgresStateStore(f.db, f.enc, f.ingressID, f.orgID, 0)
	require.NoError(t, err)
	require.NoError(t, store.WriteState(ipn.MachineKeyStateKey, []byte("registered")))
	f.heartbeat(t, 0, tailnetgateway.NodeStatusOnline, "nONE")
	f.heartbeat(t, 1, tailnetgateway.NodeStatusOnline, "nTWO")

	// Credentials are mandatory once the gateway has touched the tailnet.
	require.ErrorIs(t, f.provisioner.Delete(ctx, f.resources), k8s.ErrNetworkIngressProviderCredentialsRejected)
	require.ErrorIs(t, f.provisioner.DeleteWithCredentials(ctx, f.resources, nil), k8s.ErrNetworkIngressProviderCredentialsRejected)

	err = f.provisioner.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret"))
	require.ErrorIs(t, err, k8s.ErrNetworkIngressDeletionPending, "live hosts must stop before devices are deleted")
	nodes, err := repo.New(f.db).ListNetworkIngressNodes(ctx, repo.ListNetworkIngressNodesParams{NetworkIngressID: f.ingressID, OrganizationID: f.orgID})
	require.NoError(t, err)
	for _, node := range nodes {
		require.True(t, node.RetiredAt.Valid, "every slot is retired so gateways stop hosting")
	}

	f.heartbeat(t, 0, tailnetgateway.NodeStatusStopped, "nONE")
	f.heartbeat(t, 1, tailnetgateway.NodeStatusStopped, "nTWO")
	require.NoError(t, f.provisioner.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret")))
	require.ElementsMatch(t, []string{"nONE", "nTWO"}, f.api.DeletedDevices())
	require.NotContains(t, f.api.Services(), "svc:private")

	nodes, err = repo.New(f.db).ListNetworkIngressNodes(ctx, repo.ListNetworkIngressNodesParams{NetworkIngressID: f.ingressID, OrganizationID: f.orgID})
	require.NoError(t, err)
	require.Empty(t, nodes)
	_, err = store.ReadState(ipn.MachineKeyStateKey)
	require.ErrorIs(t, err, ipn.ErrStateNotExist, "node state is purged with the devices")

	// The service may still exist even with no node rows, so a gateway
	// ingress keeps requiring credentials until it is torn down with them.
	require.ErrorIs(t, f.provisioner.Delete(ctx, f.resources), k8s.ErrNetworkIngressProviderCredentialsRejected)
}

func TestProvisionerDeleteWithoutCredentialsIsNoOpBeforeApply(t *testing.T) {
	t.Parallel()
	f := newProvisionerFixture(t, "tailnetprovuntouched")
	f.setRuntime(t, "")
	require.NoError(t, f.provisioner.Delete(t.Context(), f.resources), "nothing was created on the tailnet yet")
}

func TestProvisionerDeleteSurfacesRejectedCredentials(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovdeletecreds")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	f.heartbeat(t, 0, tailnetgateway.NodeStatusStopped, "nONE")

	require.ErrorIs(t, f.provisioner.DeleteWithCredentials(ctx, f.resources, []byte("{")), k8s.ErrNetworkIngressProviderCredentialsRejected)
	f.api.Reject = tailscaleapi.ErrUnauthorized
	require.ErrorIs(t, f.provisioner.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret")), k8s.ErrNetworkIngressProviderCredentialsRejected)
}

type fakeOperator struct {
	present     bool
	deleteErr   error
	deleteCalls int
	observe     k8s.NetworkIngressObservation
	observed    int
	applied     int
}

func (f *fakeOperator) Apply(context.Context, k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
	f.applied++
	return f.observe, nil
}

func (f *fakeOperator) Observe(context.Context, k8s.NetworkIngressResourceNames) (k8s.NetworkIngressObservation, error) {
	f.observed++
	return f.observe, nil
}

func (f *fakeOperator) Delete(context.Context, k8s.NetworkIngressResourceNames) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.present = false
	return nil
}

func (f *fakeOperator) HasResources(context.Context, k8s.NetworkIngressResourceNames) (bool, error) {
	return f.present, nil
}

func (f *fakeOperator) FindOrphans(context.Context, []k8s.NetworkIngressResourceNames) ([]k8s.NetworkIngressOrphan, error) {
	return nil, nil
}

func TestCompositeMovesOperatorIngressToGatewayAfterTeardown(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetcomposite")
	operator := &fakeOperator{present: true, deleteErr: k8s.ErrNetworkIngressDeletionPending, deleteCalls: 0, observe: k8s.NetworkIngressObservation{Status: k8s.NetworkIngressStatusOnline, DNSName: "old.example.ts.net", ErrorCode: "", ConnectedAt: nil}, observed: 0, applied: 0}
	composite, err := tailnetgateway.NewCompositeProvisioner(operator, f.provisioner, true)
	require.NoError(t, err)
	f.setRuntime(t, "")

	// An ingress that has not been moved stays entirely on the operator,
	// however often it is reconciled.
	observation, err := composite.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Equal(t, "old.example.ts.net", observation.DNSName)
	observation, err = composite.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, "old.example.ts.net", observation.DNSName)
	require.Equal(t, 1, operator.applied)
	require.Equal(t, 1, operator.observed)
	require.Zero(t, operator.deleteCalls, "enabling the gateway never tears down an unmoved ingress")
	require.Empty(t, f.api.Keys())
	require.Empty(t, f.api.Services())

	// Moving the organization is what hands the ingress to the gateway.
	f.setRuntime(t, "gateway")

	// Invalid desired state never dismantles the operator backend.
	invalid := f.desired(t)
	invalid.Hostname = "Not A Label"
	_, err = composite.Apply(ctx, invalid)
	require.ErrorIs(t, err, k8s.ErrNetworkIngressInvalidDesiredState)
	require.Zero(t, operator.deleteCalls)

	// Operator resources still exist: apply waits for their teardown.
	observation, err = composite.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, k8s.ErrNetworkIngressDeletionPending)
	require.Equal(t, k8s.NetworkIngressStatusPending, observation.Status)
	require.Empty(t, f.api.Keys())

	operator.deleteErr = nil
	observation, err = composite.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressStatusPending, observation.Status)
	require.Len(t, f.api.Keys(), 3)
	require.Equal(t, 2, operator.deleteCalls)

	// Once moved, the operator is no longer consulted.
	_, err = composite.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, 1, operator.observed)
	require.Equal(t, 1, operator.applied)

	f.heartbeat(t, 0, tailnetgateway.NodeStatusStopped, "nONE")
	require.NoError(t, composite.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret")))
	require.Equal(t, []string{"nONE"}, f.api.DeletedDevices())
}

func TestCompositeHoldsGatewayIngressesUntilTheGatewayIsDeployed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetcompositeundeployed")
	operator := &fakeOperator{present: false, deleteErr: nil, deleteCalls: 0, observe: k8s.NetworkIngressObservation{Status: k8s.NetworkIngressStatusOnline, DNSName: "old.example.ts.net", ErrorCode: "", ConnectedAt: nil}, observed: 0, applied: 0}
	composite, err := tailnetgateway.NewCompositeProvisioner(operator, f.provisioner, false)
	require.NoError(t, err)

	observation, err := composite.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, k8s.ErrNetworkIngressProviderUnavailable)
	require.Equal(t, k8s.NetworkIngressStatusPending, observation.Status)
	require.Zero(t, operator.applied, "a gateway ingress never falls back to the operator")
	require.Empty(t, f.api.Keys())

	// Ingresses that were never moved keep working on the operator meanwhile.
	f.setRuntime(t, "")
	_, err = composite.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Equal(t, 1, operator.applied)
}

func TestCompositeWithoutOperatorIsPlainGateway(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetcompositegw")
	composite, err := tailnetgateway.NewCompositeProvisioner(nil, f.provisioner, true)
	require.NoError(t, err)
	_, err = composite.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Len(t, f.api.Keys(), 3)
	orphans, err := composite.FindOrphans(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, orphans)
	_, err = tailnetgateway.NewCompositeProvisioner(nil, nil, true)
	require.Error(t, err)

	// Without the operator, an ingress that was never moved has no provider.
	f.setRuntime(t, "")
	_, err = composite.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, k8s.ErrNetworkIngressUnsupportedProvider)
}
