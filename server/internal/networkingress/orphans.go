package networkingress

import (
	"context"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
)

const orphanInventorySnapshotAttempts = 3

type persistedNetworkIngressResource struct {
	provider string
	names    k8s.NetworkIngressResourceNames
}

// FindOrphans reports unmatched provider resources; it never deletes them.
func (e *Executor) FindOrphans(ctx context.Context) ([]k8s.NetworkIngressOrphan, error) {
	for range orphanInventorySnapshotAttempts {
		before, known, err := e.persistedNetworkIngressResources(ctx)
		if err != nil {
			return nil, err
		}
		orphans, err := e.registry.FindOrphans(ctx, known)
		if err != nil {
			return nil, reconcileFailure("orphan_inventory_unavailable")
		}
		after, _, err := e.persistedNetworkIngressResources(ctx)
		if err != nil {
			return nil, err
		}
		if samePersistedNetworkIngressResources(before, after) {
			return orphans, nil
		}
	}
	return nil, reconcileFailure("orphan_inventory_unstable")
}

func (e *Executor) persistedNetworkIngressResources(ctx context.Context) (map[uuid.UUID]persistedNetworkIngressResource, map[string][]k8s.NetworkIngressResourceNames, error) {
	persisted := make(map[uuid.UUID]persistedNetworkIngressResource)
	known := make(map[string][]k8s.NetworkIngressResourceNames)
	after := uuid.Nil
	for range 10 {
		rows, err := repo.New(e.db).ListPersistedNetworkIngressResources(ctx, repo.ListPersistedNetworkIngressResourcesParams{AfterID: after, PageSize: 1000})
		if err != nil {
			return nil, nil, reconcileFailure("database_unavailable")
		}
		for _, row := range rows {
			names, err := k8s.ParseNetworkIngressResourceNames(row.ProviderResources)
			if err != nil || names.OwnerID != row.ID {
				return nil, nil, reconcileFailure("invalid_desired_state")
			}
			persisted[row.ID] = persistedNetworkIngressResource{provider: row.Provider, names: names}
			known[row.Provider] = append(known[row.Provider], names)
		}
		if len(rows) < 1000 {
			return persisted, known, nil
		}
		after = rows[len(rows)-1].ID
	}
	return nil, nil, reconcileFailure("orphan_inventory_limit")
}

func samePersistedNetworkIngressResources(a, b map[uuid.UUID]persistedNetworkIngressResource) bool {
	if len(a) != len(b) {
		return false
	}
	for id, resource := range a {
		if b[id] != resource {
			return false
		}
	}
	return true
}
