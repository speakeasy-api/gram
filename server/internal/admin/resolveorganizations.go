package admin

import (
	"context"
	"slices"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ResolveOrganizations reports what each of a list of ids and slugs points at,
// without changing anything. Matches come back once each, in request order, ids
// before slugs; identifiers that match nothing come back separately rather than
// failing the call. A caller holding identifiers from outside the system, such
// as a file an operator uploaded, uses it to show the operator what those
// identifiers name before acting on them.
func (s *Service) ResolveOrganizations(ctx context.Context, payload *gen.ResolveOrganizationsPayload) (*gen.AdminResolveOrganizationsResult, error) {
	ids := dedupeInOrder(payload.Ids)
	slugs := dedupeInOrder(payload.Slugs)
	if len(ids) == 0 && len(slugs) == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "at least one id or slug must be supplied")
	}
	// The design bounds each list on its own, and only at the HTTP boundary.
	// The batch limit is on the two together.
	if len(ids)+len(slugs) > constants.MaxResolveOrganizationsBatch {
		return nil, oops.E(oops.CodeBadRequest, nil, "at most %d organizations can be resolved at once", constants.MaxResolveOrganizationsBatch)
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
	// A list can name one organization twice, once by id and once by slug, and
	// it must come back once.
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
