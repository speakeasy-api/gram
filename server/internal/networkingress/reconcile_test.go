package networkingress_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type lifecycleProvider struct {
	apply   func(context.Context, k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error)
	observe func(context.Context, k8s.NetworkIngressResourceNames) (k8s.NetworkIngressObservation, error)
	delete  func(context.Context, k8s.NetworkIngressResourceNames) error
}

func (p lifecycleProvider) Apply(ctx context.Context, d k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
	return p.apply(ctx, d)
}
func (p lifecycleProvider) Observe(ctx context.Context, r k8s.NetworkIngressResourceNames) (k8s.NetworkIngressObservation, error) {
	return p.observe(ctx, r)
}
func (p lifecycleProvider) Delete(ctx context.Context, r k8s.NetworkIngressResourceNames) error {
	return p.delete(ctx, r)
}

func TestNetworkIngressExecutorDisabledGateObservesOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := ti.create(t, ctx)
	id := uuid.MustParse(created.ID)
	observed := false
	provider := lifecycleProvider{
		apply: func(context.Context, k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
			t.Fatal("Apply called while disabled")
			return k8s.NetworkIngressObservation{Status: "", DNSName: "", ErrorCode: "", ConnectedAt: nil}, nil
		},
		observe: func(context.Context, k8s.NetworkIngressResourceNames) (k8s.NetworkIngressObservation, error) {
			observed = true
			return k8s.NetworkIngressObservation{Status: "online", DNSName: "private.example.ts.net", ErrorCode: "", ConnectedAt: nil}, nil
		},
		delete: func(context.Context, k8s.NetworkIngressResourceNames) error {
			t.Fatal("unexpected teardown")
			return nil
		},
	}
	registry, err := k8s.NewNetworkIngressProvisionerRegistry(map[string]k8s.NetworkIngressProvisioner{"tailscale": provider}, testenv.NewLogger(t), nil)
	require.NoError(t, err)
	executor := networkingress.NewExecutor(ti.conn, testenv.NewEncryptionClient(t), registry, networkingress.ExecutorOptions{Queue: "test", Image: "image", BackendService: "backend", BackendPort: 443, CanApply: func(context.Context) error { return fmt.Errorf("off") }})
	result, err := executor.Reconcile(ctx, id)
	require.NoError(t, err)
	require.False(t, result.Requeue)
	require.True(t, observed)
	row := loadRow(t, ctx, ti)
	require.Equal(t, "pending", row.Status, "observation must not certify unapplied desired state")
	require.Equal(t, "provider_mutations_disabled", row.LastError.String)
}

func TestNetworkIngressExecutorRetainsCleanupUntilAbsent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := ti.create(t, ctx)
	id := uuid.MustParse(created.ID)
	require.NoError(t, ti.service.DeleteIngress(ctx, &gen.DeleteIngressPayload{}))
	pending := true
	deletes := 0
	provider := lifecycleProvider{apply: nil, observe: nil, delete: func(context.Context, k8s.NetworkIngressResourceNames) error {
		deletes++
		if pending {
			return k8s.ErrNetworkIngressDeletionPending
		}
		return nil
	}}
	registry, err := k8s.NewNetworkIngressProvisionerRegistry(map[string]k8s.NetworkIngressProvisioner{"tailscale": provider}, testenv.NewLogger(t), nil)
	require.NoError(t, err)
	executor := networkingress.NewExecutor(ti.conn, nil, registry, networkingress.ExecutorOptions{Queue: "", Image: "", BackendService: "", BackendPort: 0, CanApply: nil})
	_, err = executor.Reconcile(ctx, id)
	require.ErrorContains(t, err, "deletion_pending")
	row, err := repo.New(ti.conn).GetNetworkIngressForReconcile(ctx, id)
	require.NoError(t, err)
	require.True(t, row.CredentialsEncrypted.Valid)
	require.NotEqual(t, "{}", string(row.ProviderResources))
	pending = false
	_, err = executor.Reconcile(ctx, id)
	require.NoError(t, err)
	row, err = repo.New(ti.conn).GetNetworkIngressForReconcile(ctx, id)
	require.NoError(t, err)
	require.False(t, row.CredentialsEncrypted.Valid)
	require.JSONEq(t, "{}", string(row.ProviderResources))
	_, err = executor.Reconcile(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 2, deletes, "cleaned tombstone redelivery is a no-op")
	ti.create(t, ctx)
}

func TestNetworkIngressExecutorGateClosesBeforeApply(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	id := uuid.MustParse(ti.create(t, ctx).ID)
	checks, observations := 0, 0
	provider := lifecycleProvider{
		apply: func(context.Context, k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
			t.Fatal("Apply called after gate closed")
			return k8s.NetworkIngressObservation{Status: "", DNSName: "", ErrorCode: "", ConnectedAt: nil}, nil
		},
		observe: func(context.Context, k8s.NetworkIngressResourceNames) (k8s.NetworkIngressObservation, error) {
			observations++
			return k8s.NetworkIngressObservation{Status: "pending", DNSName: "", ErrorCode: "", ConnectedAt: nil}, nil
		}, delete: nil,
	}
	registry, err := k8s.NewNetworkIngressProvisionerRegistry(map[string]k8s.NetworkIngressProvisioner{"tailscale": provider}, testenv.NewLogger(t), nil)
	require.NoError(t, err)
	executor := networkingress.NewExecutor(ti.conn, testenv.NewEncryptionClient(t), registry, networkingress.ExecutorOptions{Queue: "test", Image: "image", BackendService: "backend", BackendPort: 443, CanApply: func(context.Context) error {
		checks++
		if checks > 1 {
			return fmt.Errorf("gate now unavailable")
		}
		return nil
	}})
	_, err = executor.Reconcile(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 2, checks)
	require.Equal(t, 1, observations)
	require.Equal(t, "provider_mutations_disabled", loadRow(t, ctx, ti).LastError.String)
}

func TestNetworkIngressExecutorRotationSuppressesStaleObservation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	id := uuid.MustParse(ti.create(t, ctx).ID)
	calls := 0
	provider := lifecycleProvider{
		apply: func(_ context.Context, desired k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
			calls++
			if calls == 1 {
				_, err := ti.service.RotateCredentials(ctx, &gen.RotateCredentialsPayload{OauthClientID: "next-client", OauthClientSecret: "next-secret"})
				require.NoError(t, err)
			} else {
				require.Contains(t, string(desired.Credentials), "next-secret")
			}
			return k8s.NetworkIngressObservation{Status: "online", DNSName: "private.example.ts.net", ErrorCode: "", ConnectedAt: nil}, nil
		}, observe: nil, delete: nil,
	}
	registry, err := k8s.NewNetworkIngressProvisionerRegistry(map[string]k8s.NetworkIngressProvisioner{"tailscale": provider}, testenv.NewLogger(t), nil)
	require.NoError(t, err)
	executor := networkingress.NewExecutor(ti.conn, testenv.NewEncryptionClient(t), registry, networkingress.ExecutorOptions{Queue: "test", Image: "image", BackendService: "backend", BackendPort: 443, CanApply: func(context.Context) error { return nil }})
	result, err := executor.Reconcile(ctx, id)
	require.NoError(t, err)
	require.True(t, result.Requeue)
	require.Equal(t, "pending", loadRow(t, ctx, ti).Status)
	result, err = executor.Reconcile(ctx, id)
	require.NoError(t, err)
	require.False(t, result.Requeue)
	require.Equal(t, "online", loadRow(t, ctx, ti).Status)
}

func TestNetworkIngressExecutorDeleteWinsDuringPartialApply(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	id := uuid.MustParse(ti.create(t, ctx).ID)
	deleted := false
	provider := lifecycleProvider{
		apply: func(callCtx context.Context, d k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
			require.NotEmpty(t, d.Credentials)
			require.NoError(t, ti.service.DeleteIngress(ctx, &gen.DeleteIngressPayload{}))
			return k8s.NetworkIngressObservation{Status: "", DNSName: "", ErrorCode: "", ConnectedAt: nil}, fmt.Errorf("sentinel-provider-secret")
		},
		observe: nil,
		delete:  func(context.Context, k8s.NetworkIngressResourceNames) error { deleted = true; return nil },
	}
	registry, err := k8s.NewNetworkIngressProvisionerRegistry(map[string]k8s.NetworkIngressProvisioner{"tailscale": provider}, testenv.NewLogger(t), nil)
	require.NoError(t, err)
	executor := networkingress.NewExecutor(ti.conn, testenv.NewEncryptionClient(t), registry, networkingress.ExecutorOptions{Queue: "test", Image: "image", BackendService: "backend", BackendPort: 443, CanApply: func(context.Context) error { return nil }})
	_, err = executor.Reconcile(ctx, id)
	require.NoError(t, err)
	require.True(t, deleted)
	row, err := repo.New(ti.conn).GetNetworkIngressForReconcile(ctx, id)
	require.NoError(t, err)
	require.True(t, row.Deleted)
	require.False(t, row.CredentialsEncrypted.Valid)
}
