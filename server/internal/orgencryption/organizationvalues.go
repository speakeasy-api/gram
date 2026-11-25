package orgencryption

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// NewOrganizationValues builds OrganizationValues from a ListOrganizationsWith*
// query's columns.
func NewOrganizationValues(organizationID string, values int64, lastWrittenAt pgtype.Timestamptz) OrganizationValues {
	return OrganizationValues{OrganizationID: organizationID, Values: values, LastWrittenAt: LastWrittenAt(lastWrittenAt)}
}

// LastWrittenAt converts a query's last_written_at, NULL when no row stores a
// value, to the zero time when it is NULL.
func LastWrittenAt(lastWrittenAt pgtype.Timestamptz) time.Time {
	if !lastWrittenAt.Valid {
		return time.Time{}
	}

	return lastWrittenAt.Time
}

// ListOrganizationsWithValues returns every organization storing at least one
// value in any of stores, with its values summed and its latest write across
// them, most recently written first.
func ListOrganizationsWithValues(ctx context.Context, stores []CopyStore) ([]OrganizationValues, error) {
	merged := make(map[string]OrganizationValues)
	for _, store := range stores {
		organizations, err := store.ListOrganizationsWithValues(ctx)
		if err != nil {
			return nil, fmt.Errorf("list organizations with values in %s: %w", store.Columns().PlatformColumn, err)
		}

		for _, o := range organizations {
			current := merged[o.OrganizationID]
			merged[o.OrganizationID] = OrganizationValues{
				OrganizationID: o.OrganizationID,
				Values:         current.Values + o.Values,
				LastWrittenAt:  later(current.LastWrittenAt, o.LastWrittenAt),
			}
		}
	}

	organizations := slices.Collect(maps.Values(merged))
	slices.SortFunc(organizations, func(a, b OrganizationValues) int {
		return cmp.Or(b.LastWrittenAt.Compare(a.LastWrittenAt), cmp.Compare(a.OrganizationID, b.OrganizationID))
	})

	return organizations, nil
}

// later returns the later of a and b.
func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}
