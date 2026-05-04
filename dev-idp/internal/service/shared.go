// Package service contains the dev-idp's Goa service implementations
// (organizations, users, memberships, devIdp). All endpoints are
// permanently unauthenticated — see idp-design.md §6.
package service

import (
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

// timeFormat is the wire format for every timestamptz the dev-idp returns
// over its management API. RFC3339 with seconds; matches Goa's
// FormatDateTime expectation.
const timeFormat = time.RFC3339

// paginate trims a fetched page produced by the standard `limit + 1`
// keyset-pagination idiom: callers pass `MaxRows = limit + 1` to the SQL
// query so this function can detect whether another page exists. When the
// fetch returned more than `limit` rows, the trailing row is dropped and
// `nextCursor` is set to the last row of the kept page.
func paginate[T any](rows []T, limit int, cursorOf func(T) string) (page []T, nextCursor string) {
	if len(rows) <= limit {
		return rows, ""
	}
	page = rows[:limit]
	return page, cursorOf(page[len(page)-1])
}

// optionalUUID parses a *string into a uuid.NullUUID for use as a sqlc
// nullable filter parameter. Returns Valid=false when the pointer is nil or
// points to "". Wraps parse errors as a CodeBadRequest oops naming the
// caller-facing field.
func optionalUUID(in *string, field string) (uuid.NullUUID, error) {
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if in == nil || *in == "" {
		return none, nil
	}
	parsed, err := uuid.Parse(*in)
	if err != nil {
		return none, oops.E(oops.CodeBadRequest, err, "invalid %s", field)
	}
	return uuid.NullUUID{UUID: parsed, Valid: true}, nil
}

