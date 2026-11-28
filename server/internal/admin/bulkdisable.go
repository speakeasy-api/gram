package admin

import (
	"context"
	"errors"
	"slices"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// BulkDisableOrganizations removes each organization from the whitelist and
// stamps disabled_at, all in one transaction, so a failure part way leaves
// every organization as it was. Each change goes through the same helpers, and
// writes the same audit events, as the single-organization controls.
func (s *Service) BulkDisableOrganizations(ctx context.Context, payload *gen.BulkDisableOrganizationsPayload) (*gen.AdminBulkDisableOrganizationsResult, error) {
	requested := dedupeInOrder(payload.Ids)
	// Repeated here because the design's bounds only run at the HTTP boundary.
	if len(requested) == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "at least one id must be supplied")
	}
	if len(requested) > constants.MaxBulkDisableOrganizations {
		return nil, oops.E(oops.CodeBadRequest, nil, "at most %d organizations can be disabled at once", constants.MaxBulkDisableOrganizations)
	}
	if slices.Contains(requested, "") {
		return nil, oops.E(oops.CodeBadRequest, nil, "ids must not be empty")
	}

	// Rows are locked in id order, so two overlapping batches take their locks
	// in the same order and cannot deadlock each other.
	ids := slices.Clone(requested)
	slices.Sort(ids)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin bulk disable").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	actor, actorDisplayName, _ := adminActor(ctx)
	missing := make(map[string]struct{})
	disabled := make([]string, 0, len(ids))
	for _, id := range ids {
		_, err := SetOrganizationAccessTx(ctx, tx, s.audit, id, false, actor, actorDisplayName)
		if errors.Is(err, ErrOrganizationAccessNotFound) {
			missing[id] = struct{}{}
			continue
		}
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "disable organization").LogError(ctx, s.logger)
		}
		// The access lock above already holds this row, so the whitelist change
		// cannot find it gone.
		if _, err := SetOrganizationWhitelistTx(ctx, tx, s.audit, id, false, actor, actorDisplayName); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "remove organization from whitelist").LogError(ctx, s.logger)
		}
		disabled = append(disabled, id)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit bulk disable").LogError(ctx, s.logger)
	}

	missingIDs := make([]string, 0, len(missing))
	for _, id := range requested {
		if _, ok := missing[id]; ok {
			missingIDs = append(missingIDs, id)
		}
	}

	return &gen.AdminBulkDisableOrganizationsResult{
		DisabledIds: disabled,
		MissingIds:  missingIDs,
	}, nil
}
