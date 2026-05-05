// Package conv has dev-idp's tiny set of *string <-> sql.NullString and
// related helpers. Replaces pgtype-flavored helpers from server/internal/conv
// after the SQLite switch.
package conv

import "database/sql"

// PtrToNullString converts a nullable *string to sql.NullString. nil -> Valid=false.
func PtrToNullString(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{String: "", Valid: false}
	}
	return sql.NullString{String: *p, Valid: true}
}

// FromNullString returns a *string from sql.NullString. Invalid -> nil.
func FromNullString(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	s := n.String
	return &s
}

// FromNullStringOrEmpty returns "" for invalid, else the string.
func FromNullStringOrEmpty(n sql.NullString) string {
	if !n.Valid {
		return ""
	}
	return n.String
}

// PtrBool dereferences a *bool with the given default for nil.
func PtrBool(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}
