package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

const catalogResetScopeWarning = "This resets only this destination's visible catalog. Existing independent preview databases are unaffected; future clones inherit the source database state. Testing edits are discarded. Records are retained, not deleted; installed connections and credentials are untouched. Structural conflicts prevent a complete reset: only the exact safe operations in this plan will be applied."

// CatalogResetter shares refresh's transactional plan machinery, but requires a
// distinct operation and destination-specific confirmation even for receipt replay.
// It is staff-only; it is not an Admin MCP write/proposal approval surface.
type CatalogResetter struct{ refresh *CatalogRefresher }

func NewCatalogResetter(registry *Service, environment, sourceIdentity, destination string, source CatalogRefreshSource, images CatalogRefreshAssets) *CatalogResetter {
	r := NewCatalogRefresher(registry, environment, sourceIdentity, source, images)
	r.operation = "reset"
	r.destination = destination
	return &CatalogResetter{refresh: r}
}

func (r *CatalogResetter) Preview(ctx context.Context, owner string) (CatalogRefreshPlan, error) {
	return r.refresh.Preview(ctx, owner)
}

func (r *CatalogResetter) Apply(ctx context.Context, owner string, id uuid.UUID, confirmation string) (CatalogRefreshReceipt, error) {
	// Per-call authorization must not mutate shared state across concurrent calls.
	refresh := *r.refresh
	refresh.confirmation = confirmation
	return refresh.Apply(ctx, owner, id)
}

func (r *CatalogResetter) Status(ctx context.Context) (CatalogRefreshStatus, error) {
	return r.refresh.Status(ctx)
}

func resetConfirmation(destination string, id uuid.UUID) string {
	return "RESET " + destination + " " + id.String()
}

func (r *CatalogRefresher) authorizePlan(plan CatalogRefreshPlan) error {
	if r.operation == "reset" {
		if plan.Operation != "reset" || plan.Destination != r.destination || plan.Confirmation != resetConfirmation(r.destination, plan.ID) || r.confirmation != plan.Confirmation {
			return fmt.Errorf("%w: reset requires the exact destination-specific reset confirmation", ErrConflict)
		}
	} else if (plan.Operation != "" && plan.Operation != "refresh") || plan.Destination != "" || plan.Confirmation != "" {
		return fmt.Errorf("%w: a reset plan cannot authorize refresh", ErrConflict)
	}
	return nil
}

func resetDecision(local, upstream json.RawMessage, published bool) (action, reason string) {
	if len(upstream) == 0 {
		if published {
			return "unpublish", "retained_destination_only"
		}
		return "preserved", "retained_unpublished"
	}
	if len(local) == 0 {
		return "create", "production_entry"
	}
	if checkStructure(local, upstream) != nil {
		return "conflict", "immutable_structure"
	}
	if !published {
		return "republish", "restore_production"
	}
	if equalJSON(local, upstream) {
		return "adopt", "matches_production"
	}
	return "update", "discard_testing_edits"
}
