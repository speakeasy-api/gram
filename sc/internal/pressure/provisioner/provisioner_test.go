package provisioner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc/internal/pressure/host"
	"github.com/speakeasy-api/gram/sc/internal/pressure/provisioner"
)

// These are the tests of provisioner_test.go in PR #8377 against the fakes
// in fakes_test.go. TestProvisionerSealsAuthKeysUnderTheOrganizationKey is
// not ported: it tests orgencryption's envelope and column binding, which
// the Keys fake does not model.

type provisionerFixture struct {
	db          *fakeDB
	keys        *fakeKeys
	api         *fakeTailscale
	provisioner *provisioner.Provisioner
	ingressID   uuid.UUID
	orgID       string
	resources   provisioner.ResourceNames
}

func newProvisioner(t *testing.T, f *provisionerFixture, logger *slog.Logger, keys provisioner.Keys, cfg provisioner.Config) *provisioner.Provisioner {
	t.Helper()
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	p, err := provisioner.New(logger, f.db, keys, f.api, cfg)
	require.NoError(t, err)
	return p
}

func defaultConfig() provisioner.Config {
	return provisioner.Config{Tag: "tag:speakeasy", Slots: 3, AuthKeyTTL: 24 * time.Hour, StaleAfter: 2 * time.Minute, Now: time.Now}
}

func newProvisionerFixture(t *testing.T, name string) *provisionerFixture {
	t.Helper()
	db := newFakeDB()
	ingressID, orgID := db.seedIngress(name)
	f := &provisionerFixture{db: db, keys: &fakeKeys{db: db}, api: newFakeTailscale(), ingressID: ingressID, orgID: orgID, resources: provisioner.ResourceNames{OwnerID: ingressID}}
	f.provisioner = newProvisioner(t, f, nil, f.keys, defaultConfig())
	return f
}

func credentialsJSON(t *testing.T, id, secret string) []byte {
	t.Helper()
	encoded, err := json.Marshal(provisioner.Credentials{ClientID: id, ClientSecret: secret}) // #nosec G117 -- test credentials.
	require.NoError(t, err)
	return encoded
}

func (f *provisionerFixture) desired(t *testing.T) provisioner.Desired {
	t.Helper()
	return provisioner.Desired{ID: f.ingressID, OrganizationID: f.orgID, Provider: provisioner.ProviderTailscale, Hostname: "private", Credentials: credentialsJSON(t, "cid", "csecret")}
}

func (f *provisionerFixture) stateStore(t *testing.T, slot int32) host.NodeStateStore {
	t.Helper()
	store, err := f.db.NodeStateStore(f.ingressID, f.orgID, slot)
	require.NoError(t, err)
	return store
}

func TestProvisionerApplyDefinesServiceAndMintsKeysPerSlot(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovapply")

	observation, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Equal(t, provisioner.StatusPending, observation.Status)

	services := f.api.Services()
	require.Contains(t, services, "svc:private")
	require.Equal(t, []string{"tcp:443"}, services["svc:private"].Ports)
	require.Equal(t, []string{"tag:speakeasy"}, services["svc:private"].Tags)
	require.Len(t, f.api.Keys(), 3, "one key per slot")

	nodes := f.db.slotRows(f.ingressID)
	require.Len(t, nodes, 3)
	for i, node := range nodes {
		require.EqualValues(t, i, node.Slot)
		key := f.db.authKey(f.ingressID, node.Slot)
		require.NotNil(t, key)
		plain, err := openAuthKey(f.orgID, *key)
		require.NoError(t, err)
		require.Equal(t, f.api.Keys()[i].Key, plain)
		require.False(t, node.Retired)
	}
	_, err = f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Len(t, f.api.Keys(), 3)

	// A slot that registered no longer needs a key even when its key is gone;
	// a slot with only partial state still does.
	require.NoError(t, f.stateStore(t, 1).WriteState(knownProfilesStateKey, []byte(`[{"ID":"p1"}]`)))
	require.NoError(t, f.stateStore(t, 2).WriteState(machineKeyStateKey, []byte("machine-only")))
	f.db.clearAuthKey(f.ingressID, 1)
	f.db.clearAuthKey(f.ingressID, 2)
	_, err = f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Len(t, f.api.Keys(), 4, "only the unregistered slot without a key is re-minted")

	f.db.heartbeat(f.ingressID, 0, host.NodeStatusOnline, "nONE")
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
	f.db.heartbeat(f.ingressID, 2, host.NodeStatusOnline, "nTHREE")

	cfg := defaultConfig()
	cfg.Slots = 2
	smaller := newProvisioner(t, f, nil, f.keys, cfg)
	_, err = smaller.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	nodes := f.db.slotRows(f.ingressID)
	require.Len(t, nodes, 3)
	require.True(t, nodes[2].Retired, "slot 2 is retired while its host is still live")
	require.Empty(t, f.api.DeletedDevices(), "a live host is not deleted from under the pod")

	f.db.heartbeat(f.ingressID, 2, host.NodeStatusStopped, "nTHREE")
	_, err = smaller.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Equal(t, []string{"nTHREE"}, f.api.DeletedDevices())
	require.Len(t, f.db.slotRows(f.ingressID), 2)

	_, err = f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	nodes = f.db.slotRows(f.ingressID)
	require.Len(t, nodes, 3)
	require.False(t, nodes[2].Retired)
	require.Empty(t, nodes[2].NodeID)
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
	require.Equal(t, provisioner.ErrorInvalidCredentials, observation.ErrorCode)

	f.api.reject(provisioner.ErrTailscaleUnauthorized)
	observation, err = f.provisioner.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, provisioner.ErrTailscaleUnauthorized)
	require.Equal(t, provisioner.ErrorInvalidCredentials, observation.ErrorCode)
	require.NotErrorIs(t, err, provisioner.ErrProviderCredentialsRejected, "the teardown blocker is only for Delete")

	for _, tc := range []struct {
		reject   error
		code     string
		sentinel error
	}{
		{provisioner.ErrTailscaleForbidden, provisioner.ErrorProviderForbidden, provisioner.ErrProviderForbidden},
		{provisioner.ErrTailscaleRateLimited, provisioner.ErrorProviderRateLimited, provisioner.ErrProviderRateLimited},
		{provisioner.ErrTailscaleUnavailable, provisioner.ErrorProviderAPIUnavailable, provisioner.ErrProviderAPIUnavailable},
	} {
		f.api.reject(fmt.Errorf("upsert service: %w", tc.reject))
		observation, err = f.provisioner.Apply(ctx, f.desired(t))
		require.ErrorIs(t, err, tc.sentinel)
		require.Equal(t, tc.code, observation.ErrorCode)
	}
	f.api.reject(nil)

	desired = f.desired(t)
	desired.Hostname = "Not A Label"
	observation, err = f.provisioner.Apply(ctx, desired)
	require.ErrorIs(t, err, provisioner.ErrInvalidDesiredState)
	require.Equal(t, provisioner.ErrorInvalidDesiredState, observation.ErrorCode)

	desired = f.desired(t)
	desired.OrganizationID = ""
	_, err = f.provisioner.Apply(ctx, desired)
	require.ErrorIs(t, err, provisioner.ErrInvalidDesiredState)
}

func TestProvisionerObserveFollowsHeartbeats(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovobserve")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)

	observation, err := f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, provisioner.StatusPending, observation.Status)

	f.db.heartbeat(f.ingressID, 0, host.NodeStatusStarting, "")
	observation, err = f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, provisioner.StatusPending, observation.Status)

	f.db.heartbeat(f.ingressID, 0, host.NodeStatusOnline, "nONE")
	observation, err = f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, provisioner.StatusOnline, observation.Status)
	require.Equal(t, "private.example.ts.net", observation.DNSName)
	require.NotNil(t, observation.ConnectedAt)

	f.db.heartbeat(f.ingressID, 0, host.NodeStatusError, "nONE")
	observation, err = f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, provisioner.StatusError, observation.Status)

	f.db.heartbeat(f.ingressID, 0, host.NodeStatusOnline, "nONE")
	cfg := defaultConfig()
	cfg.Now = func() time.Time { return time.Now().Add(time.Hour) }
	stale := newProvisioner(t, f, nil, f.keys, cfg)
	observation, err = stale.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, provisioner.StatusDegraded, observation.Status)
}

func TestProvisionerDeleteWaitsForHostsThenRemovesDevices(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovdelete")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	store := f.stateStore(t, 0)
	require.NoError(t, store.WriteState(machineKeyStateKey, []byte("registered")))
	f.db.writeCert(f.ingressID, "private.example.ts.net")
	f.db.heartbeat(f.ingressID, 0, host.NodeStatusOnline, "nONE")
	f.db.heartbeat(f.ingressID, 1, host.NodeStatusOnline, "nTWO")

	require.ErrorIs(t, f.provisioner.Delete(ctx, f.resources), provisioner.ErrProviderCredentialsRejected)
	require.ErrorIs(t, f.provisioner.DeleteWithCredentials(ctx, f.resources, nil), provisioner.ErrProviderCredentialsRejected)

	err = f.provisioner.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret"))
	require.ErrorIs(t, err, provisioner.ErrDeletionPending, "live hosts must stop before devices are deleted")
	for _, node := range f.db.slotRows(f.ingressID) {
		require.True(t, node.Retired, "every slot is retired so gateways stop hosting")
	}

	f.db.heartbeat(f.ingressID, 0, host.NodeStatusStopped, "nONE")
	f.db.heartbeat(f.ingressID, 1, host.NodeStatusStopped, "nTWO")
	require.NoError(t, f.provisioner.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret")))
	require.ElementsMatch(t, []string{"nONE", "nTWO"}, f.api.DeletedDevices())
	require.NotContains(t, f.api.Services(), "svc:private")

	require.Empty(t, f.db.slotRows(f.ingressID))
	_, err = store.ReadState(machineKeyStateKey)
	require.ErrorIs(t, err, errStateNotExist, "node state is purged with the devices")
	require.False(t, f.db.hasCert(f.ingressID), "the connection's certificate is purged with it")

	require.NoError(t, f.provisioner.Delete(ctx, f.resources))
}

func TestProvisionerDeleteWithoutCredentialsIsNoOpBeforeApply(t *testing.T) {
	t.Parallel()
	f := newProvisionerFixture(t, "tailnetprovuntouched")
	f.db.setRuntime(f.ingressID, "")
	require.NoError(t, f.provisioner.Delete(t.Context(), f.resources), "nothing was created on the tailnet yet")
}

func TestProvisionerDeleteSurfacesRejectedCredentials(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovdeletecreds")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	f.db.heartbeat(f.ingressID, 0, host.NodeStatusStopped, "nONE")

	require.ErrorIs(t, f.provisioner.DeleteWithCredentials(ctx, f.resources, []byte("{")), provisioner.ErrProviderCredentialsRejected)
	f.api.reject(provisioner.ErrTailscaleUnauthorized)
	require.ErrorIs(t, f.provisioner.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret")), provisioner.ErrProviderCredentialsRejected)

	f.api.reject(provisioner.ErrTailscaleForbidden)
	err = f.provisioner.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret"))
	require.ErrorIs(t, err, provisioner.ErrProviderForbidden)
	require.ErrorIs(t, err, provisioner.ErrProviderCredentialsRejected, "a missing permission blocks teardown like rejected credentials")

	f.api.reject(provisioner.ErrTailscaleRateLimited)
	err = f.provisioner.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret"))
	require.ErrorIs(t, err, provisioner.ErrProviderRateLimited)
	require.NotErrorIs(t, err, provisioner.ErrProviderCredentialsRejected)
}

func TestProvisionerApplyFailureKeepsOnlineDNSName(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovkeepdns")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	f.db.heartbeat(f.ingressID, 0, host.NodeStatusOnline, "nONE")

	f.api.reject(fmt.Errorf("upsert service: %w", provisioner.ErrTailscaleUnavailable))
	observation, err := f.provisioner.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, provisioner.ErrProviderAPIUnavailable)
	require.Equal(t, provisioner.ErrorProviderAPIUnavailable, observation.ErrorCode)
	require.Equal(t, "private.example.ts.net", observation.DNSName, "a Tailscale outage must not cut traffic to an online host")
	require.NotNil(t, observation.ConnectedAt)

	f.db.heartbeat(f.ingressID, 0, host.NodeStatusStopped, "nONE")
	observation, err = f.provisioner.Apply(ctx, f.desired(t))
	require.Error(t, err)
	require.Empty(t, observation.DNSName, "with no online host there is nothing to keep")
}

func TestProvisionerDeleteSkipsTailscaleWhenNothingReachedTheTailnet(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovwrongsecret")
	f.api.reject(provisioner.ErrTailscaleUnauthorized)
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, provisioner.ErrTailscaleUnauthorized)

	require.NoError(t, f.provisioner.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "wrong")))
	require.NoError(t, f.provisioner.Delete(ctx, f.resources))
}

func TestProvisionerReplacesRefusedIdentity(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovrefused")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	store := f.stateStore(t, 0)
	require.NoError(t, store.WriteState(machineKeyStateKey, []byte("machine-key")))
	require.NoError(t, store.WriteState(knownProfilesStateKey, []byte(`{"profile-1":{"ID":"profile-1","Name":"speakeasy-gw-0"}}`)))
	f.db.clearAuthKey(f.ingressID, 0)
	f.db.heartbeat(f.ingressID, 0, host.NodeStatusNeedsLogin, "nREMOVED")
	keys := len(f.api.Keys())

	_, err = f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Equal(t, []string{"nREMOVED"}, f.api.DeletedDevices(), "the refused device is removed from the tailnet")
	registered, err := store.Registered(ctx)
	require.NoError(t, err)
	require.False(t, registered, "the refused identity is reset")
	require.Len(t, f.api.Keys(), keys+1, "a fresh key is minted for the slot")
	require.NotNil(t, f.db.authKey(f.ingressID, 0))

	_, err = f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Len(t, f.api.DeletedDevices(), 1)
	require.Len(t, f.api.Keys(), keys+1)

	require.Equal(t, string(host.NodeStatusPending), f.db.slotRows(f.ingressID)[0].Status, "the fresh key returns the slot to pending")
	observation, err := f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, provisioner.StatusPending, observation.Status, "the replacement is in progress")
}

func TestProvisionerReportsSlotWaitingForLoginAsError(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovneedslogin")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	f.db.heartbeat(f.ingressID, 0, host.NodeStatusNeedsLogin, "nREMOVED")

	observation, err := f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, provisioner.StatusError, observation.Status, "a slot waiting for login is not healthy")
}

func TestProvisionerReportsHostsAwaitingACertificate(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovcert")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)

	f.db.heartbeat(f.ingressID, 0, host.NodeStatusAwaitingCertificate, "nONE")
	f.db.heartbeat(f.ingressID, 1, host.NodeStatusError, "")
	observation, err := f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, provisioner.StatusError, observation.Status, "a host without a certificate cannot serve")
	require.Equal(t, provisioner.ErrorProviderCertificateUnavailable, observation.ErrorCode, "the specific cause wins over a generic failure")

	f.db.heartbeat(f.ingressID, 1, host.NodeStatusAwaitingApproval, "nTWO")
	observation, err = f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, provisioner.ErrorProviderServiceNotApproved, observation.ErrorCode, "approval comes first: an unapproved host cannot get a certificate")

	f.db.heartbeat(f.ingressID, 1, host.NodeStatusOnline, "nTWO")
	observation, err = f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, provisioner.StatusOnline, observation.Status, "one serving host is enough")
}

func TestProvisionerReportsHostsAwaitingApproval(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovapproval")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)

	f.db.heartbeat(f.ingressID, 0, host.NodeStatusAwaitingApproval, "nONE")
	f.db.heartbeat(f.ingressID, 1, host.NodeStatusError, "")
	observation, err := f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, provisioner.StatusError, observation.Status)
	require.Equal(t, provisioner.ErrorProviderServiceNotApproved, observation.ErrorCode, "the fixable cause wins over a generic failure")
	require.Empty(t, observation.DNSName, "an unapproved host gets no traffic, so no name is published")

	f.db.heartbeat(f.ingressID, 1, host.NodeStatusOnline, "nTWO")
	observation, err = f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, provisioner.StatusOnline, observation.Status, "one approved host is enough to serve")
	require.Equal(t, "private.example.ts.net", observation.DNSName)
}

type fakeOperator struct {
	deleteCalls, observed, applied int
	observe                        provisioner.Observation
}

func (f *fakeOperator) Apply(context.Context, provisioner.Desired) (provisioner.Observation, error) {
	f.applied++
	return f.observe, nil
}

func (f *fakeOperator) Observe(context.Context, provisioner.ResourceNames) (provisioner.Observation, error) {
	f.observed++
	return f.observe, nil
}

func (f *fakeOperator) Delete(context.Context, provisioner.ResourceNames) error {
	f.deleteCalls++
	return nil
}

func (f *fakeOperator) FindOrphans(context.Context, []provisioner.ResourceNames) ([]provisioner.Orphan, error) {
	return nil, nil
}

func newFakeOperator() *fakeOperator {
	return &fakeOperator{observe: provisioner.Observation{Status: provisioner.StatusOnline, DNSName: "old.example.ts.net"}}
}

func TestCompositeLeavesOperatorIngressesOnTheOperator(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetcompositeoperator")
	operator := newFakeOperator()
	composite, err := provisioner.NewComposite(operator, f.provisioner, true)
	require.NoError(t, err)
	f.db.setRuntime(f.ingressID, "")

	observation, err := composite.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Equal(t, "old.example.ts.net", observation.DNSName)
	observation, err = composite.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, "old.example.ts.net", observation.DNSName)
	require.NoError(t, composite.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret")))

	require.Equal(t, 1, operator.applied)
	require.Equal(t, 1, operator.observed)
	require.Equal(t, 1, operator.deleteCalls, "teardown stays with the operator")
	require.Empty(t, f.api.Keys(), "the gateway never touches the customer's tailnet for an operator ingress")
	require.Empty(t, f.api.Services())
	require.Empty(t, f.api.DeletedDevices())
}

func TestCompositeHostsGatewayIngressesWithoutTheOperator(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetcompositegateway")
	operator := newFakeOperator()
	composite, err := provisioner.NewComposite(operator, f.provisioner, true)
	require.NoError(t, err)

	observation, err := composite.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Equal(t, provisioner.StatusPending, observation.Status)
	require.Len(t, f.api.Keys(), 3)
	_, err = composite.Observe(ctx, f.resources)
	require.NoError(t, err)

	f.db.heartbeat(f.ingressID, 0, host.NodeStatusStopped, "nONE")
	require.NoError(t, composite.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret")))
	require.Equal(t, []string{"nONE"}, f.api.DeletedDevices())
	require.Zero(t, operator.applied)
	require.Zero(t, operator.observed)
	require.Zero(t, operator.deleteCalls, "a gateway ingress never has operator resources to tear down")
}

func TestCompositeHoldsGatewayIngressesUntilTheGatewayIsDeployed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetcompositeundeployed")
	operator := newFakeOperator()
	composite, err := provisioner.NewComposite(operator, f.provisioner, false)
	require.NoError(t, err)

	observation, err := composite.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, provisioner.ErrProviderUnavailable)
	require.Equal(t, provisioner.StatusPending, observation.Status)
	require.Zero(t, operator.applied, "a gateway ingress never falls back to the operator")
	require.Empty(t, f.api.Keys())

	f.db.setRuntime(f.ingressID, "")
	_, err = composite.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Equal(t, 1, operator.applied)
}

func TestCompositeWithoutOperatorIsPlainGateway(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetcompositegw")
	composite, err := provisioner.NewComposite(nil, f.provisioner, true)
	require.NoError(t, err)
	_, err = composite.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Len(t, f.api.Keys(), 3)
	orphans, err := composite.FindOrphans(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, orphans)
	_, err = provisioner.NewComposite(nil, nil, true)
	require.Error(t, err)

	f.db.setRuntime(f.ingressID, "")
	_, err = composite.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, provisioner.ErrUnsupportedProvider)
	require.ErrorIs(t, composite.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret")), provisioner.ErrUnsupportedProvider)
}

func TestProvisionerReportsUnavailableOrganizationKey(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovsuspended")
	f.db.suspend(f.orgID)

	observation, err := f.provisioner.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, host.ErrEncryptionKeyUnavailable)
	require.Equal(t, provisioner.ErrorEncryptionKeyUnavailable, observation.ErrorCode)
	require.Empty(t, f.api.Keys(), "no auth key is minted that could not be stored")
}

func TestProvisionerApplyRequiresOrganizationEncryption(t *testing.T) {
	t.Parallel()
	f := newProvisionerFixture(t, "tailnetprovnoenc")
	cfg := defaultConfig()
	cfg.Slots = 1
	unconfigured := newProvisioner(t, f, nil, nil, cfg)

	_, err := unconfigured.Apply(t.Context(), f.desired(t))
	require.ErrorContains(t, err, "organization encryption")
	require.Empty(t, f.api.Services(), "nothing reaches the tailnet without somewhere to keep its secrets")
}

func TestProvisionerLogsTheCauseOfAFailedApply(t *testing.T) {
	t.Parallel()
	f := newProvisionerFixture(t, "tailnetprovlogs")
	var logs bytes.Buffer
	cfg := defaultConfig()
	cfg.Slots = 1
	p := newProvisioner(t, f, slog.New(slog.NewJSONHandler(&logs, nil)), f.keys, cfg)
	f.api.reject(errors.New("upsert service: when updating a service, addrs must contain 2 elements (400)"))

	_, err := p.Apply(t.Context(), f.desired(t))
	require.Error(t, err)
	require.Contains(t, logs.String(), "addrs must contain 2 elements", "the reconciler's own telemetry only carries an error code, so the cause is logged here")
	require.Contains(t, logs.String(), f.ingressID.String())
	require.NotContains(t, logs.String(), "csecret", "the OAuth client secret never reaches the logs")
}

func TestProvisionerLogsTheOrganizationOfAFailedTeardown(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovdeletelogs")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	var logs bytes.Buffer
	p := newProvisioner(t, f, slog.New(slog.NewJSONHandler(&logs, nil)), f.keys, defaultConfig())
	f.api.reject(fmt.Errorf("%w: delete service", provisioner.ErrTailscaleRateLimited))

	require.Error(t, p.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret")))
	require.Contains(t, logs.String(), f.orgID, "teardown failures name the organization")
	require.Contains(t, logs.String(), provisioner.ErrorProviderRateLimited, "and their bounded error code")
}
