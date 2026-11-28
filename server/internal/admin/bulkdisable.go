package admin

import (
	"context"
	"errors"
	"slices"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ResolveOrganizations is the read-only review step of a bulk disable. The
// dashboard parses the operator's CSV and sends whatever identifiers it held;
// this reports what each one points at, so nothing is written until the
// operator has seen the list.
func (s *Service) ResolveOrganizations(ctx context.Context, payload *gen.ResolveOrganizationsPayload) (*gen.AdminResolveOrganizationsResult, error) {
	ids := dedupeInOrder(payload.Ids)
	slugs := dedupeInOrder(payload.Slugs)
	if len(ids) == 0 && len(slugs) == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "at least one id or slug must be supplied")
	}
	// The design bounds each list on its own, and only at the HTTP boundary.
	// The review lists what the disable will be sent, so the two lists together
	// must fit inside one disable.
	if len(ids)+len(slugs) > constants.MaxBulkDisableOrganizations {
		return nil, oops.E(oops.CodeBadRequest, nil, "at most %d organizations can be resolved at once", constants.MaxBulkDisableOrganizations)
	}
	if slices.Contains(ids, "") || slices.Contains(slugs, "") {
		return nil, oops.E(oops.CodeBadRequest, nil, "ids and slugs must not be empty")
	}

	rows, err := repo.New(s.db).AdminResolveOrganizations(ctx, repo.AdminResolveOrganizationsParams{
		Ids:   ids,
		Slugs: slugs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve organizations").LogError(ctx, s.logger)
	}

	byID := make(map[string]repo.AdminResolveOrganizationsRow, len(rows))
	bySlug := make(map[string]repo.AdminResolveOrganizationsRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
		bySlug[row.Slug] = row
	}

	result := &gen.AdminResolveOrganizationsResult{
		Organizations: []*gen.AdminResolvedOrganization{},
		MissingIds:    []string{},
		MissingSlugs:  []string{},
	}
	// A CSV can name one organization twice, by id on one row and slug on
	// another, and the review must list it once.
	listed := make(map[string]struct{}, len(rows))
	add := func(row repo.AdminResolveOrganizationsRow) {
		if _, ok := listed[row.ID]; ok {
			return
		}
		listed[row.ID] = struct{}{}
		result.Organizations = append(result.Organizations, &gen.AdminResolvedOrganization{
			ID:          row.ID,
			Name:        row.Name,
			Slug:        row.Slug,
			AccountType: row.AccountType,
			Whitelisted: row.Whitelisted,
			DisabledAt:  pgTimestampPtr(row.DisabledAt),
		})
	}

	// Both maps are keyed on the row's own column, so an id is only ever
	// credited to the organization whose id it is, even when it also equals
	// some other organization's slug.
	for _, id := range ids {
		row, ok := byID[id]
		if !ok {
			result.MissingIds = append(result.MissingIds, id)
			continue
		}
		add(row)
	}
	for _, slug := range slugs {
		row, ok := bySlug[slug]
		if !ok {
			result.MissingSlugs = append(result.MissingSlugs, slug)
			continue
		}
		add(row)
	}

	return result, nil
}

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

// dedupeInOrder drops repeats and keeps first occurrences in their order, so a
// result that echoes identifiers back lines up with the operator's file.
func dedupeInOrder(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
