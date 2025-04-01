package conv

import (
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

func Ptr[T any](v T) *T {
	return &v
}

func FromPGText(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func FromBytes(b []byte) *string {
	if len(b) == 0 {
		return nil
	}
	s := string(b)
	return &s
}

var cleanSlugRegex = regexp.MustCompile(`[^a-zA-Z0-9 ]+`)

func ToSlug(s string) string {
	s = cleanSlugRegex.ReplaceAllString(s, "")
	s = strings.ToLower(s)
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "-")
	return s
}
