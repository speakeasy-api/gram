package tailnetgateway_test

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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"tailscale.com/ipn"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/tailscaleapi"
)

type provisionerFixture struct {
	db          *pgxpool.Pool
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
	api := tailscaleapi.NewFake()
	// Heartbeats are stamped by the database clock, so the provisioner's
	// clock must be the real one for freshness to mean anything.
	now := time.Now().UTC()
	api.Now = time.Now
	ingressID, orgID := seedIngress(t, db, name)
	resources, err := k8s.NewNetworkIngressResourceNames(ingressID)
	require.NoError(t, err)
	provisioner, err := tailnetgateway.NewProvisioner(testenv.NewLogger(t), db, orgEncryption(t, db).Service, orgEncryption(t, db).Provisioner, api, tailnetgateway.ProvisionerConfig{
		Tag: tailnetgateway.DefaultTag, Slots: 3, AuthKeyTTL: 24 * time.Hour, StaleAfter: 2 * time.Minute, Now: time.Now,
	})
	require.NoError(t, err)
	// seedIngress creates a gateway ingress; tests of the operator path
	// clear the runtime with setRuntime.
	return &provisionerFixture{db: db, api: api, provisioner: provisioner, now: now, ingressID: ingressID, orgID: orgID, resources: resources}
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
		plain := openAuthKey(t, f.db, f.orgID, node.AuthKeyEncrypted.String)
		require.Equal(t, f.api.Keys()[i].Key, plain)
		require.False(t, node.RetiredAt.Valid)
	}
	// A second apply keeps the unexpired keys and re-upserts the service.
	_, err = f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Len(t, f.api.Keys(), 3)

	// A slot that registered no longer needs a key even when its key is gone;
	// a slot with only partial state still does.
	store := newStateStore(t, f.db, f.ingressID, f.orgID, 1)
	require.NoError(t, store.WriteState(ipn.KnownProfilesStateKey, []byte(`[{"ID":"p1"}]`)))
	partial := newStateStore(t, f.db, f.ingressID, f.orgID, 2)
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

	smaller, err := tailnetgateway.NewProvisioner(testenv.NewLogger(t), f.db, orgEncryption(t, f.db).Service, orgEncryption(t, f.db).Provisioner, f.api, tailnetgateway.ProvisionerConfig{
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
	require.NotErrorIs(t, err, k8s.ErrNetworkIngressProviderCredentialsRejected, "the teardown blocker is only for Delete")

	for _, tc := range []struct {
		reject   error
		code     string
		sentinel error
	}{
		{tailscaleapi.ErrForbidden, k8s.NetworkIngressErrorProviderForbidden, k8s.ErrNetworkIngressProviderForbidden},
		{tailscaleapi.ErrRateLimited, k8s.NetworkIngressErrorProviderRateLimited, k8s.ErrNetworkIngressProviderRateLimited},
		{tailscaleapi.ErrUnavailable, k8s.NetworkIngressErrorProviderAPIUnavailable, k8s.ErrNetworkIngressProviderAPIUnavailable},
	} {
		f.api.Reject = fmt.Errorf("upsert service: %w", tc.reject)
		observation, err = f.provisioner.Apply(ctx, f.desired(t))
		require.ErrorIs(t, err, tc.sentinel)
		require.Equal(t, tc.code, observation.ErrorCode)
	}
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
	stale, err := tailnetgateway.NewProvisioner(testenv.NewLogger(t), f.db, orgEncryption(t, f.db).Service, orgEncryption(t, f.db).Provisioner, f.api, tailnetgateway.ProvisionerConfig{
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
	store := newStateStore(t, f.db, f.ingressID, f.orgID, 0)
	require.NoError(t, store.WriteState(ipn.MachineKeyStateKey, []byte("registered")))
	require.NoError(t, store.WriteTLSCertAndKey("private.example.ts.net", []byte("cert"), []byte("key")))
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
	_, _, err = store.ReadTLSCertAndKey("private.example.ts.net")
	require.ErrorIs(t, err, ipn.ErrStateNotExist, "the connection's certificate is purged with it")

	// Teardown already removed the service with the slots, so a redelivered
	// delete finishes without calling Tailscale.
	require.NoError(t, f.provisioner.Delete(ctx, f.resources))
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

	f.api.Reject = tailscaleapi.ErrForbidden
	err = f.provisioner.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret"))
	require.ErrorIs(t, err, k8s.ErrNetworkIngressProviderForbidden)
	require.ErrorIs(t, err, k8s.ErrNetworkIngressProviderCredentialsRejected, "a missing permission blocks teardown like rejected credentials")

	f.api.Reject = tailscaleapi.ErrRateLimited
	err = f.provisioner.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret"))
	require.ErrorIs(t, err, k8s.ErrNetworkIngressProviderRateLimited)
	require.NotErrorIs(t, err, k8s.ErrNetworkIngressProviderCredentialsRejected)
}

func TestProvisionerApplyFailureKeepsOnlineDNSName(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovkeepdns")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	f.heartbeat(t, 0, tailnetgateway.NodeStatusOnline, "nONE")

	f.api.Reject = fmt.Errorf("upsert service: %w", tailscaleapi.ErrUnavailable)
	observation, err := f.provisioner.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, k8s.ErrNetworkIngressProviderAPIUnavailable)
	require.Equal(t, k8s.NetworkIngressErrorProviderAPIUnavailable, observation.ErrorCode)
	require.Equal(t, "private.example.ts.net", observation.DNSName, "a Tailscale outage must not cut traffic to an online host")
	require.NotNil(t, observation.ConnectedAt)

	f.heartbeat(t, 0, tailnetgateway.NodeStatusStopped, "nONE")
	observation, err = f.provisioner.Apply(ctx, f.desired(t))
	require.Error(t, err)
	require.Empty(t, observation.DNSName, "with no online host there is nothing to keep")
}

func TestProvisionerDeleteSkipsTailscaleWhenNothingReachedTheTailnet(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovwrongsecret")
	f.api.Reject = tailscaleapi.ErrUnauthorized
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, tailscaleapi.ErrUnauthorized)

	// The saved secret is still wrong, but no slot was ever created, so
	// deletion finishes instead of blocking on rejected credentials.
	require.NoError(t, f.provisioner.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "wrong")))
	require.NoError(t, f.provisioner.Delete(ctx, f.resources))
}

func TestProvisionerReplacesRefusedIdentity(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovrefused")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	store := newStateStore(t, f.db, f.ingressID, f.orgID, 0)
	require.NoError(t, store.WriteState(ipn.MachineKeyStateKey, []byte("machine-key")))
	require.NoError(t, store.WriteState(ipn.KnownProfilesStateKey, []byte(`{"profile-1":{"ID":"profile-1","Name":"speakeasy-gw-0"}}`)))
	require.NoError(t, repo.New(f.db).ClearNetworkIngressNodeAuthKey(ctx, repo.ClearNetworkIngressNodeAuthKeyParams{NetworkIngressID: f.ingressID, OrganizationID: f.orgID, Slot: 0}))
	f.heartbeat(t, 0, tailnetgateway.NodeStatusNeedsLogin, "nREMOVED")
	keys := len(f.api.Keys())

	_, err = f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Equal(t, []string{"nREMOVED"}, f.api.DeletedDevices(), "the refused device is removed from the tailnet")
	registered, err := store.Registered(ctx)
	require.NoError(t, err)
	require.False(t, registered, "the refused identity is reset")
	require.Len(t, f.api.Keys(), keys+1, "a fresh key is minted for the slot")
	slot, err := repo.New(f.db).GetNetworkIngressNodeSlot(ctx, repo.GetNetworkIngressNodeSlotParams{NetworkIngressID: f.ingressID, OrganizationID: f.orgID, Slot: 0})
	require.NoError(t, err)
	require.True(t, slot.AuthKeyEncrypted.Valid)

	// Until the gateway logs in again, later reconciles leave the slot alone.
	_, err = f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Len(t, f.api.DeletedDevices(), 1)
	require.Len(t, f.api.Keys(), keys+1)

	require.Equal(t, string(tailnetgateway.NodeStatusPending), slot.Status, "the fresh key returns the slot to pending")
	observation, err := f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressStatusPending, observation.Status, "the replacement is in progress")
}

func TestProvisionerReportsSlotWaitingForLoginAsError(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovneedslogin")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	f.heartbeat(t, 0, tailnetgateway.NodeStatusNeedsLogin, "nREMOVED")

	observation, err := f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressStatusError, observation.Status, "a slot waiting for login is not healthy")
}

func TestProvisionerReportsHostsAwaitingACertificate(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovcert")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)

	f.heartbeat(t, 0, tailnetgateway.NodeStatusAwaitingCertificate, "nONE")
	f.heartbeat(t, 1, tailnetgateway.NodeStatusError, "")
	observation, err := f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressStatusError, observation.Status, "a host without a certificate cannot serve")
	require.Equal(t, k8s.NetworkIngressErrorProviderCertificateUnavailable, observation.ErrorCode, "the specific cause wins over a generic failure")

	f.heartbeat(t, 1, tailnetgateway.NodeStatusAwaitingApproval, "nTWO")
	observation, err = f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressErrorProviderServiceNotApproved, observation.ErrorCode, "approval comes first: an unapproved host cannot get a certificate")

	f.heartbeat(t, 1, tailnetgateway.NodeStatusOnline, "nTWO")
	observation, err = f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressStatusOnline, observation.Status, "one serving host is enough")
}

func TestProvisionerReportsHostsAwaitingApproval(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovapproval")
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)

	f.heartbeat(t, 0, tailnetgateway.NodeStatusAwaitingApproval, "nONE")
	f.heartbeat(t, 1, tailnetgateway.NodeStatusError, "")
	observation, err := f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressStatusError, observation.Status)
	require.Equal(t, k8s.NetworkIngressErrorProviderServiceNotApproved, observation.ErrorCode, "the fixable cause wins over a generic failure")
	require.Empty(t, observation.DNSName, "an unapproved host gets no traffic, so no name is published")

	f.heartbeat(t, 1, tailnetgateway.NodeStatusOnline, "nTWO")
	observation, err = f.provisioner.Observe(ctx, f.resources)
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressStatusOnline, observation.Status, "one approved host is enough to serve")
	require.Equal(t, "private.example.ts.net", observation.DNSName)
}

type fakeOperator struct {
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
	return nil
}

func (f *fakeOperator) FindOrphans(context.Context, []k8s.NetworkIngressResourceNames) ([]k8s.NetworkIngressOrphan, error) {
	return nil, nil
}

func newFakeOperator() *fakeOperator {
	return &fakeOperator{deleteCalls: 0, observe: k8s.NetworkIngressObservation{Status: k8s.NetworkIngressStatusOnline, DNSName: "old.example.ts.net", ErrorCode: "", ConnectedAt: nil}, observed: 0, applied: 0}
}

func TestCompositeLeavesOperatorIngressesOnTheOperator(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetcompositeoperator")
	operator := newFakeOperator()
	composite, err := tailnetgateway.NewCompositeProvisioner(operator, f.provisioner, true)
	require.NoError(t, err)
	f.setRuntime(t, "")

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
	composite, err := tailnetgateway.NewCompositeProvisioner(operator, f.provisioner, true)
	require.NoError(t, err)

	observation, err := composite.Apply(ctx, f.desired(t))
	require.NoError(t, err)
	require.Equal(t, k8s.NetworkIngressStatusPending, observation.Status)
	require.Len(t, f.api.Keys(), 3)
	_, err = composite.Observe(ctx, f.resources)
	require.NoError(t, err)

	f.heartbeat(t, 0, tailnetgateway.NodeStatusStopped, "nONE")
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
	composite, err := tailnetgateway.NewCompositeProvisioner(operator, f.provisioner, false)
	require.NoError(t, err)

	observation, err := composite.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, k8s.ErrNetworkIngressProviderUnavailable)
	require.Equal(t, k8s.NetworkIngressStatusPending, observation.Status)
	require.Zero(t, operator.applied, "a gateway ingress never falls back to the operator")
	require.Empty(t, f.api.Keys())

	// Operator ingresses keep working on the operator meanwhile.
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

	// Without the operator, an operator ingress has no provider.
	f.setRuntime(t, "")
	_, err = composite.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, k8s.ErrNetworkIngressUnsupportedProvider)
	require.ErrorIs(t, composite.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret")), k8s.ErrNetworkIngressUnsupportedProvider)
}

func TestProvisionerSealsAuthKeysUnderTheOrganizationKey(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovorgkey")

	// The organization has no data key yet; Apply mints one on first use.
	_, err := f.provisioner.Apply(ctx, f.desired(t))
	require.NoError(t, err)

	nodes, err := repo.New(f.db).ListNetworkIngressNodes(ctx, repo.ListNetworkIngressNodesParams{NetworkIngressID: f.ingressID, OrganizationID: f.orgID})
	require.NoError(t, err)
	require.NotEmpty(t, nodes)
	ciphertext := nodes[0].AuthKeyEncrypted.String
	require.True(t, encryption.IsEnvelope(ciphertext), "auth keys use organization envelopes, not the application key")

	_, otherOrg := seedIngress(t, f.db, "tailnetprovorgkey-other")
	_, err = gatewayEncryptor(t, f.db, otherOrg).Decrypt(ctx, authKeyColumn, ciphertext)
	require.ErrorIs(t, err, orgencryption.ErrInvalidCiphertext, "another organization cannot open the key")
	_, err = gatewayEncryptor(t, f.db, f.orgID).Decrypt(ctx, nodeStateColumn, ciphertext)
	require.ErrorIs(t, err, orgencryption.ErrInvalidCiphertext, "the key is bound to its column")
}

func TestProvisionerReportsUnavailableOrganizationKey(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newProvisionerFixture(t, "tailnetprovsuspended")
	provisionOrg(t, f.db, f.orgID)
	suspendOrgEncryption(t, f.db, f.orgID)

	observation, err := f.provisioner.Apply(ctx, f.desired(t))
	require.ErrorIs(t, err, orgencryption.ErrSuspended)
	require.Equal(t, k8s.NetworkIngressErrorEncryptionKeyUnavailable, observation.ErrorCode)
	require.Empty(t, f.api.Keys(), "no auth key is minted that could not be stored")
}

func TestProvisionerApplyRequiresOrganizationEncryption(t *testing.T) {
	t.Parallel()
	f := newProvisionerFixture(t, "tailnetprovnoenc")
	unconfigured, err := tailnetgateway.NewProvisioner(testenv.NewLogger(t), f.db, nil, nil, f.api, tailnetgateway.ProvisionerConfig{
		Tag: tailnetgateway.DefaultTag, Slots: 1, AuthKeyTTL: time.Hour, StaleAfter: time.Minute, Now: time.Now,
	})
	require.NoError(t, err)

	_, err = unconfigured.Apply(t.Context(), f.desired(t))
	require.ErrorContains(t, err, "organization encryption")
	require.Empty(t, f.api.Services(), "nothing reaches the tailnet without somewhere to keep its secrets")
}

func TestProvisionerLogsTheCauseOfAFailedApply(t *testing.T) {
	t.Parallel()
	f := newProvisionerFixture(t, "tailnetprovlogs")
	var logs bytes.Buffer
	provisioner, err := tailnetgateway.NewProvisioner(slog.New(slog.NewJSONHandler(&logs, nil)), f.db, orgEncryption(t, f.db).Service, orgEncryption(t, f.db).Provisioner, f.api, tailnetgateway.ProvisionerConfig{
		Tag: tailnetgateway.DefaultTag, Slots: 1, AuthKeyTTL: time.Hour, StaleAfter: time.Minute, Now: time.Now,
	})
	require.NoError(t, err)
	f.api.Reject = errors.New("upsert service: when updating a service, addrs must contain 2 elements (400)")

	_, err = provisioner.Apply(t.Context(), f.desired(t))
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
	provisioner, err := tailnetgateway.NewProvisioner(slog.New(slog.NewJSONHandler(&logs, nil)), f.db, orgEncryption(t, f.db).Service, orgEncryption(t, f.db).Provisioner, f.api, tailnetgateway.ProvisionerConfig{
		Tag: tailnetgateway.DefaultTag, Slots: 3, AuthKeyTTL: time.Hour, StaleAfter: time.Minute, Now: time.Now,
	})
	require.NoError(t, err)
	f.api.Reject = fmt.Errorf("%w: delete service", tailscaleapi.ErrRateLimited)

	require.Error(t, provisioner.DeleteWithCredentials(ctx, f.resources, credentialsJSON(t, "cid", "csecret")))
	require.Contains(t, logs.String(), f.orgID, "teardown failures name the organization")
	require.Contains(t, logs.String(), k8s.NetworkIngressErrorProviderRateLimited, "and their bounded error code")
}
