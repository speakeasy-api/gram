package networkingress

import (
	"context"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
)

// FindOrphans reports unmatched provider resources; it never deletes them.
func (e *Executor) FindOrphans(ctx context.Context) ([]k8s.NetworkIngressOrphan, error) {
	known := make(map[string][]k8s.NetworkIngressResourceNames)
	after := uuid.Nil
	for range 10 {
		rows, err := repo.New(e.db).ListPersistedNetworkIngressResources(ctx, repo.ListPersistedNetworkIngressResourcesParams{AfterID: after, PageSize: 1000})
		if err != nil {
			return nil, reconcileFailure("database_unavailable")
		}
		for _, row := range rows {
			names, err := k8s.ParseNetworkIngressResourceNames(row.ProviderResources)
			if err != nil || names.OwnerID != row.ID {
				return nil, reconcileFailure("invalid_desired_state")
			}
			known[row.Provider] = append(known[row.Provider], names)
		}
		if len(rows) < 1000 {
			orphans, err := e.registry.FindOrphans(ctx, known)
			if err != nil {
				return nil, reconcileFailure("orphan_inventory_unavailable")
			}
			return orphans, nil
		}
		after = rows[len(rows)-1].ID
	}
	return nil, reconcileFailure("orphan_inventory_limit")
}
