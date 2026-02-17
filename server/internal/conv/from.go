package conv

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// PtrEmpty returns a pointer to the given value or nil if the value is equal
// to the zero value of the same type. Example:
//
//	PtrEmpty(0)      // returns nil
//	PtrEmpty("")     // returns nil
//	PtrEmpty(false)  // returns nil
//	PtrEmpty(2)      // returns *int with value 2
func PtrEmpty[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}

	return &v
}

// PtrValOr returns a value of a given pointer or a default if the pointer is
// is nil. Example:
//
//	PtrValOr[int](nil, 5)         // returns *int with value 5
//	PtrValOr(Ptr(""), "foo")      // returns ""
//	PtrValOr(Ptr("jane"), "joe")  // returns *string with value "jane"
func PtrValOr[T any](ptr *T, def T) T {
	if ptr == nil {
		return def
	}

	return *ptr
}

// PtrValOrEmpty returns a value of a given pointer or a default if the pointer
// is is nil or the zero value. Example:
//
//	PtrValOrEmpty[int](nil, 5)         // returns *int with value 5
//	PtrValOrEmpty(Ptr(""), "foo")      // returns "foo"
//	PtrValOrEmpty(Ptr("jane"), "joe")  // returns *string with value "jane"
func PtrValOrEmpty[T comparable](ptr *T, def T) T {
	if ptr == nil {
		return def
	}

	return Default(*ptr, def)
}

// Default returns the given value or a default if the value is equal to the
// zero value of the same type. Example:
//
//	Default(0, 5)      // returns 5
//	Default("", "foo")  // returns "foo"
//	Default("jane", "joe") // returns "jane"
func Default[T comparable](val T, def T) T {
	var zero T
	if val == zero {
		return def
	}

	return val
}

// FromNullableUUID converts a uuid.NullUUID to a *string. If the NullUUID is
// not valid, it returns nil.
func FromNullableUUID(u uuid.NullUUID) *string {
	if !u.Valid {
		return nil
	}

	val := u.UUID.String()
	return &val
}

// FromPGText converts a pgtype.Text to a *string. If the Text is not valid, it
// returns nil.
func FromPGText[T ~string](t pgtype.Text) *T {
	if !t.Valid {
		return nil
	}

	val := T(t.String)
	return &val
}

// ToPGText converts a string to a pgtype.Text with Valid set to true regardless
// of whether the input is an empty string or not.
func ToPGText(t string) pgtype.Text {
	return pgtype.Text{String: t, Valid: true}
}

// ToPGTextEmpty converts a string to a pgtype.Text with Valid set to true only
// if the input value is not an empty string.
func ToPGTextEmpty(t string) pgtype.Text {
	return pgtype.Text{String: t, Valid: t != ""}
}

// PtrToPGText converts a string pointer to a pgtype.Text with Valid set to true
// regardless of whether the input is an empty string or not.
func PtrToPGText(t *string) pgtype.Text {
	if t == nil {
		return pgtype.Text{Valid: false, String: ""}
	}

	return pgtype.Text{String: *t, Valid: true}
}

// ToPGTextEmpty converts a string pointer to a pgtype.Text with Valid set to
// true only if the input value is not an empty string.
func PtrToPGTextEmpty(t *string) pgtype.Text {
	if t == nil {
		return pgtype.Text{Valid: false, String: ""}
	}

	return pgtype.Text{String: *t, Valid: *t != ""}
}

// PtrToPGBool converts a bool pointer to a pgtype.Bool. If the pointer is nil,
// the result has Valid set to false.
func PtrToPGBool(b *bool) pgtype.Bool {
	if b == nil {
		return pgtype.Bool{Bool: false, Valid: false}
	}

	return pgtype.Bool{Bool: *b, Valid: true}
}

// FromPGBool converts a pgtype.Bool to a bool or subtype of bool. If Bool is
// not valid, it returns nil.
func FromPGBool[T ~bool](t pgtype.Bool) *T {
	if !t.Valid {
		return nil
	}

	val := T(t.Bool)
	return &val
}

// FromBytes converts a byte slice to a string pointer. If the byte slice is
// empty, it returns nil.
func FromBytes(b []byte) *string {
	if len(b) == 0 {
		return nil
	}
	s := string(b)
	return &s
}

var cleanSlugRegex = regexp.MustCompile(`[^a-zA-Z0-9\s-]`) // Remove special characters leaving dashes
var dashCollapseRegex = regexp.MustCompile(`[-\s]+`)       // collapses multiple dashes or spaces into a single dash

// ToSlug converts a string to a URL-friendly "slug". It removes special
// characters, converts to lowercase, collapses multiple dashes or spaces into
// a single dash, and trims leading and trailing dashes.
func ToSlug(s string) string {
	s = cleanSlugRegex.ReplaceAllString(s, "")
	s = strings.ToLower(s)
	s = dashCollapseRegex.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-") // trim leading and trailing dashes
	return s
}

// GenerateRandomSlug generates a random slug of the given size using lowercase
// letters and digits. It returns an error if it fails to generate random bytes.
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

// ToLower converts a string or subtype of string to lowercase.
func ToLower[T ~string](s T) string {
	return strings.ToLower(string(s))
}

// AnySlice converts a slice of any type to a slice of empty interfaces.
func AnySlice[T any](vals []T) []any {
	anyVals := make([]any, len(vals))
	for i, v := range vals {
		anyVals[i] = v
	}
	return anyVals
}

// Ternary returns trueVal if condition is true, otherwise it returns falseVal.
func Ternary[T any](condition bool, trueVal, falseVal T) T {
	if condition {
		return trueVal
	}
	return falseVal
}
