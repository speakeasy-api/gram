package admin

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Validate once, before querying, so direct service calls obey the same contract
// as HTTP requests and both the page and its total receive identical bounds.
type organizationListBounds struct {
	minMembers, maxMembers    pgtype.Int8
	createdAtGte, createdAtLt pgtype.Timestamptz
	disabledOnly              pgtype.Bool
}

func listOrganizationsBounds(p *gen.ListOrganizationsPayload) (organizationListBounds, error) {
	var bounds organizationListBounds
	for _, member := range []struct {
		name  string
		value *int64
		dest  *pgtype.Int8
	}{
		{"min_members", p.MinMembers, &bounds.minMembers},
		{"max_members", p.MaxMembers, &bounds.maxMembers},
	} {
		if member.value == nil {
			continue
		}
		if *member.value < 0 {
			return bounds, oops.E(oops.CodeInvalid, nil, "%s must be a nonnegative integer", member.name)
		}
		*member.dest = pgtype.Int8{Int64: *member.value, Valid: true}
	}
	if p.MinMembers != nil && p.MaxMembers != nil && *p.MinMembers > *p.MaxMembers {
		return bounds, oops.E(oops.CodeInvalid, nil, "min_members must not exceed max_members")
	}
	for _, date := range []struct {
		name  string
		value *string
		dest  *pgtype.Timestamptz
	}{
		{"created_from", p.CreatedFrom, &bounds.createdAtGte},
		{"created_to", p.CreatedTo, &bounds.createdAtLt},
	} {
		if date.value == nil {
			continue
		}
		parsed, err := time.Parse(time.DateOnly, *date.value)
		if err != nil || parsed.Format(time.DateOnly) != *date.value {
			return bounds, oops.E(oops.CodeInvalid, nil, "%s must be a valid YYYY-MM-DD UTC calendar date", date.name)
		}
		*date.dest = pgtype.Timestamptz{Time: parsed, Valid: true}
	}
	if bounds.createdAtGte.Valid && bounds.createdAtLt.Valid && bounds.createdAtGte.Time.After(bounds.createdAtLt.Time) {
		return bounds, oops.E(oops.CodeInvalid, nil, "created_from must not be after created_to")
	}
	if bounds.createdAtLt.Valid {
		// Calendar arithmetic includes the entire end day at database microsecond
		// precision, including month/year/leap-day rollover.
		bounds.createdAtLt.Time = bounds.createdAtLt.Time.AddDate(0, 0, 1)
	}
	if p.DisabledOnly != nil {
		bounds.disabledOnly = pgtype.Bool{Bool: *p.DisabledOnly, Valid: true}
	}
	return bounds, nil
}
