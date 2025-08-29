package conv

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func Ptr[T any](v T) *T {
	return &v
}

func PtrEmpty[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}

	return &v
}

func PtrValOr[T any](ptr *T, def T) T {
	if ptr == nil {
		return def
	}

	return *ptr
}

func PtrValOrEmpty[T comparable](ptr *T, def T) T {
	if ptr == nil {
		return def
	}

	return Default(*ptr, def)
}

func Default[T comparable](val T, def T) T {
	var zero T
	if val == zero {
		return def
	}

	return val
}

func FromNullableUUID(u uuid.NullUUID) *string {
	if !u.Valid {
		return nil
	}

	val := u.UUID.String()
	return &val
}

func FromPGText[T ~string](t pgtype.Text) *T {
	if !t.Valid {
		return nil
	}

	val := T(t.String)
	return &val
}

func ToPGText(t string) pgtype.Text {
	return pgtype.Text{String: t, Valid: true}
}

func ToPGTextEmpty(t string) pgtype.Text {
	return pgtype.Text{String: t, Valid: t != ""}
}

func PtrToPGText(t *string) pgtype.Text {
	if t == nil {
		return pgtype.Text{Valid: false, String: ""}
	}

	return pgtype.Text{String: *t, Valid: true}
}

func PtrToPGTextEmpty(t *string) pgtype.Text {
	if t == nil {
		return pgtype.Text{Valid: false, String: ""}
	}

	return pgtype.Text{String: *t, Valid: *t != ""}
}

func FromPGBool[T ~bool](t pgtype.Bool) *T {
	if !t.Valid {
		return nil
	}

	val := T(t.Bool)
	return &val
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

func GenerateRandomSlug(size int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyz123456789"

	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random slug: %w", err)
	}

	result := make([]byte, size)
	for i := range result {
		result[i] = charset[bytes[i]%byte(len(charset))]
	}

	return string(result), nil
}

func ToLower[T ~string](s T) string {
	return strings.ToLower(string(s))
}

func AnySlice[T any](vals []T) []any {
	anyVals := make([]any, len(vals))
	for i, v := range vals {
		anyVals[i] = v
	}
	return anyVals
}

func Ternary[T any](condition bool, trueVal, falseVal T) T {
	if condition {
		return trueVal
	}
	return falseVal
}
