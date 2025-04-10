package conv

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func Ptr[T any](v T) *T {
	return &v
}

func FromNullableUUID(u uuid.NullUUID) *string {
	if !u.Valid {
		return nil
	}

	val := u.UUID.String()
	return &val
}

func FromPGText(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func ToPGText(t string) pgtype.Text {
	return pgtype.Text{String: t, Valid: true}
}

func PtrToPGText(t *string) pgtype.Text {
	if t == nil {
		return pgtype.Text{Valid: false, String: ""}
	}

	return pgtype.Text{String: *t, Valid: true}
}

func FromBytes(b []byte) *string {
	if len(b) == 0 {
		return nil
	}
	s := string(b)
	return &s
}

var cleanSlugRegex = regexp.MustCompile(`[^a-zA-Z0-9\s-]`) // Remove special characters leaving dashes
var dashCollapseRegex = regexp.MustCompile(`[-\s]+`)       // collapses multiple dashes or spaces into a single dash

func ToSlug(s string) string {
	s = cleanSlugRegex.ReplaceAllString(s, "")
	s = strings.ToLower(s)
	s = dashCollapseRegex.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-") // trim leading and trailing dashes
	return s
}

func RedactedEnvironment(val string) string {
	if val == "" {
		return "<EMPTY>"
	}
	if len(val) <= 3 {
		return strings.Repeat("*", 5)
	}
	return val[:3] + strings.Repeat("*", 5)
}
