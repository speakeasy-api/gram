package networkingress_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type orphanInventoryProvider struct {
	find func(context.Context, []k8s.NetworkIngressResourceNames) ([]k8s.NetworkIngressOrphan, error)
}

func (orphanInventoryProvider) Apply(context.Context, k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
	panic("unexpected apply")
}

func (orphanInventoryProvider) Observe(context.Context, k8s.NetworkIngressResourceNames) (k8s.NetworkIngressObservation, error) {
	panic("unexpected observe")
}

func (orphanInventoryProvider) Delete(context.Context, k8s.NetworkIngressResourceNames) error {
	panic("unexpected delete")
}

func (p orphanInventoryProvider) FindOrphans(ctx context.Context, known []k8s.NetworkIngressResourceNames) ([]k8s.NetworkIngressOrphan, error) {
	return p.find(ctx, known)
}

func TestNetworkIngressExecutorOrphanScanRetriesWhenDesiredIdentitiesAppearDuringInventory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	id := uuid.MustParse(ti.create(t, ctx).ID)
	row := loadRow(t, ctx, ti)
	persisted := append([]byte(nil), row.ProviderResources...)
	_, err := ti.conn.Exec(ctx, `UPDATE network_ingresses SET provider_resources = '{}'::jsonb WHERE id = $1`, id)
	require.NoError(t, err)

	calls := 0
	provider := orphanInventoryProvider{find: func(ctx context.Context, known []k8s.NetworkIngressResourceNames) ([]k8s.NetworkIngressOrphan, error) {
		calls++
		switch calls {
		case 1:
			require.Empty(t, known)
			_, err := ti.conn.Exec(ctx, `UPDATE network_ingresses SET provider_resources = $1::jsonb WHERE id = $2`, persisted, id)
			require.NoError(t, err)
			return []k8s.NetworkIngressOrphan{{OwnerID: id, Kind: "tailnets"}}, nil
		case 2:
			require.Len(t, known, 1)
			require.Equal(t, id, known[0].OwnerID)
			return nil, nil
		default:
			t.Fatalf("unexpected inventory attempt %d", calls)
			return nil, nil
		}
	}}
	registry, err := k8s.NewNetworkIngressProvisionerRegistry(map[string]k8s.NetworkIngressProvisioner{"tailscale": provider}, testenv.NewLogger(t), nil)
	require.NoError(t, err)
	executor := networkingress.NewExecutor(ti.conn, nil, registry, networkingress.ExecutorOptions{})

	orphans, err := executor.FindOrphans(ctx)
	require.NoError(t, err)
	require.Empty(t, orphans)
	require.Equal(t, 2, calls)
}

func TestNetworkIngressExecutorEmptyProviderInventoryIsUnavailable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	registry, err := k8s.NewNetworkIngressProvisionerRegistry(nil, testenv.NewLogger(t), nil)
	require.NoError(t, err)
	executor := networkingress.NewExecutor(ti.conn, nil, registry, networkingress.ExecutorOptions{})

	_, err = executor.FindOrphans(ctx)
	var failure *networkingress.ReconcileError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "orphan_inventory_unavailable", failure.Code)
	require.True(t, failure.Retryable)
}

func TestNetworkIngressExecutorOrphanScanBoundsUnstableDesiredIdentities(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	id := uuid.MustParse(ti.create(t, ctx).ID)
	row := loadRow(t, ctx, ti)
	persisted := append([]byte(nil), row.ProviderResources...)

	calls := 0
	provider := orphanInventoryProvider{find: func(ctx context.Context, _ []k8s.NetworkIngressResourceNames) ([]k8s.NetworkIngressOrphan, error) {
		calls++
		resources := persisted
		if calls%2 == 1 {
			resources = []byte(`{}`)
		}
		_, err := ti.conn.Exec(ctx, `UPDATE network_ingresses SET provider_resources = $1::jsonb WHERE id = $2`, resources, id)
		require.NoError(t, err)
		return []k8s.NetworkIngressOrphan{{OwnerID: id, Kind: "tailnets"}}, nil
	}}
	registry, err := k8s.NewNetworkIngressProvisionerRegistry(map[string]k8s.NetworkIngressProvisioner{"tailscale": provider}, testenv.NewLogger(t), nil)
	require.NoError(t, err)
	executor := networkingress.NewExecutor(ti.conn, nil, registry, networkingress.ExecutorOptions{})

	_, err = executor.FindOrphans(ctx)
	var failure *networkingress.ReconcileError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "orphan_inventory_unstable", failure.Code)
	require.True(t, failure.Retryable)
	require.Equal(t, 3, calls)
}
