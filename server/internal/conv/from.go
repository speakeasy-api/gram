package conv

import (
	"crypto/rand"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// slugIDSuffixLen is how many hex characters from a resource ID
// [URLBackedSlug] embeds as a uniqueness suffix. Sized to keep slugs short
// while making suffix collisions across resources within a project rare
// enough to surface as a 409 rather than be designed around.
const slugIDSuffixLen = 4

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
//	PtrValOr(new(""), "foo")      // returns ""
//	PtrValOr(new("jane"), "joe")  // returns *string with value "jane"
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
//	PtrValOrEmpty(new(""), "foo")      // returns "foo"
//	PtrValOrEmpty(new("jane"), "joe")  // returns *string with value "jane"
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

// DefaultSlice returns the given slice or a default if the slice is empty.
func DefaultSlice[T any](val []T, def []T) []T {
	if len(val) == 0 {
		return def
	}

	return val
}

// PtrToNullUUID parses an optional string pointer into a uuid.NullUUID.
// If the pointer is nil, it returns an invalid NullUUID. Otherwise it parses
// the string as a UUID.
func PtrToNullUUID(s *string) (uuid.NullUUID, error) {
	if s == nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, err //nolint:wrapcheck // callers provide their own context
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

// ToNullUUID converts a required UUID into a valid uuid.NullUUID.
func ToNullUUID(id uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: id, Valid: true}
}

// StringToNullUUID parses a string into a uuid.NullUUID. A blank or
// unparseable value yields an invalid NullUUID rather than an error, for
// callers that treat a missing or malformed id as simply absent.
func StringToNullUUID(s string) uuid.NullUUID {
	id, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	return uuid.NullUUID{UUID: id, Valid: true}
}

// NilableToNullUUID converts a UUID into a uuid.NullUUID, treating the zero
// value (uuid.Nil) as absent — i.e. an invalid NullUUID.
func NilableToNullUUID(id uuid.UUID) uuid.NullUUID {
	if id == uuid.Nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	return uuid.NullUUID{UUID: id, Valid: true}
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

// FromPGTextOrEmpty converts a pgtype.Text to a string value. If the Text is
// not valid, it returns the zero value for T (empty string).
func FromPGTextOrEmpty[T ~string](t pgtype.Text) T {
	if !t.Valid {
		return T("")
	}
	return T(t.String)
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

// PtrToPGTextTrimmed converts a string pointer to a pgtype.Text, trimming
// surrounding whitespace and setting Valid to true only if the trimmed result
// is not empty. A nil pointer or a whitespace-only value yields an invalid
// (NULL) Text.
func PtrToPGTextTrimmed(t *string) pgtype.Text {
	if t == nil {
		return pgtype.Text{Valid: false, String: ""}
	}

	return ToPGTextEmpty(strings.TrimSpace(*t))
}

// ToPGTimestamptz converts a time.Time to a pgtype.Timestamptz with Valid set
// to true and InfinityModifier set to Finite.
func ToPGTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true, InfinityModifier: pgtype.Finite}
}

// PtrToPGTimestamptz converts a *time.Time to a pgtype.Timestamptz. If the
// pointer is nil, the result has Valid set to false.
func PtrToPGTimestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true, InfinityModifier: pgtype.Finite}
}

// FromPGTimestamptz converts a pgtype.Timestamptz to an RFC3339 UTC string. If
// the value is not valid, it returns an empty string.
func FromPGTimestamptz(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}

	return t.Time.UTC().Format(time.RFC3339)
}

// PtrToPGInterval converts a *time.Duration to a pgtype.Interval. If the
// pointer is nil, the result has Valid set to false (which becomes SQL NULL).
func PtrToPGInterval(d *time.Duration) pgtype.Interval {
	if d == nil {
		return pgtype.Interval{Microseconds: 0, Days: 0, Months: 0, Valid: false}
	}
	return pgtype.Interval{Microseconds: d.Microseconds(), Days: 0, Months: 0, Valid: true}
}

// PtrToPGInt8 converts an int pointer to a pgtype.Int8. If the pointer is nil,
// the result has Valid set to false.
func PtrToPGInt8(v *int) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{Int64: 0, Valid: false}
	}
	return pgtype.Int8{Int64: int64(*v), Valid: true}
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

// FromPGInt4 converts a pgtype.Int4 to *int32. If not valid, returns nil.
func FromPGInt4(t pgtype.Int4) *int32 {
	if !t.Valid {
		return nil
	}
	return &t.Int32
}

// PtrInt32ToInt converts a *int32 to *int. If nil, returns nil.
func PtrInt32ToInt(v *int32) *int {
	if v == nil {
		return nil
	}
	i := int(*v)
	return &i
}

// FromPGFloat8 converts a pgtype.Float8 to *float64. If not valid, returns nil.
func FromPGFloat8(t pgtype.Float8) *float64 {
	if !t.Valid {
		return nil
	}
	return &t.Float64
}

// PtrToPGFloat8 converts a float64 pointer to a pgtype.Float8. If the pointer
// is nil, the result has Valid set to false.
func PtrToPGFloat8(v *float64) pgtype.Float8 {
	if v == nil {
		return pgtype.Float8{Float64: 0, Valid: false}
	}
	return pgtype.Float8{Float64: *v, Valid: true}
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

// URLToSlug converts an input string to a URL-friendly "slug" by lowercasing
// it, mapping every non-alphanumeric character (including `.`, `/`, `:`,
// whitespace, and similar) to a single hyphen, collapsing runs of hyphens,
// and trimming trailing hyphens.
//
// Differs from [ToSlug] in that delimiters like dots and slashes are
// preserved as hyphen boundaries instead of being stripped, which makes it
// suitable for slugifying URL-shaped strings where those characters carry
// structural meaning (e.g. `api.example.com/mcp` → `api-example-com-mcp`).
// Callers are expected to strip the URL scheme themselves before passing
// the value in.
func URLToSlug(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevDash := true // treat start as "after dash" so leading separators are dropped
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// URLBackedSlug derives a slug for a URL-backed resource row from its URL and
// ID. The transform takes the URL host plus path (scheme stripped), runs it
// through [URLToSlug] so dots and slashes become hyphen boundaries, then
// appends the last [slugIDSuffixLen] hex characters of the ID so two
// resources in the same project with the same URL still land on distinct
// slugs.
func URLBackedSlug(rawURL string, id uuid.UUID) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}

	idStr := id.String()
	if len(idStr) < slugIDSuffixLen {
		return "", fmt.Errorf("uuid string too short for slug suffix: %d", len(idStr))
	}
	suffix := idStr[len(idStr)-slugIDSuffixLen:]

	base := URLToSlug(u.Host + u.Path)
	if base == "" {
		return suffix, nil
	}
	return base + "-" + suffix, nil
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

// SafeInt32 converts int to int32, clamping at boundaries.
func SafeInt32(v int) int32 {
	const maxInt32 = 1<<31 - 1
	const minInt32 = -(1 << 31)
	if v > maxInt32 {
		return maxInt32
	}
	if v < minInt32 {
		return minInt32
	}
	return int32(v)
}

// ClampedUintToInt32 converts a uint to an int32, clamping the value to
// math.MaxInt32 if it exceeds the maximum value for int32. The second return
// value indicates whether clamping occurred.
func ClampedUintToInt32(v uint) (out int32, clamped bool) {
	if v > math.MaxInt32 {
		return math.MaxInt32, true
	}
	return int32(v), false
}

// ClampedUint32ToInt converts a uint32 to an int, clamping the value to
// math.MaxInt if it exceeds the platform's maximum int value, which is only
// possible on 32-bit builds. The second return value indicates whether
// clamping occurred.
func ClampedUint32ToInt(v uint32) (out int, clamped bool) {
	if uint64(v) > uint64(math.MaxInt) {
		return math.MaxInt, true
	}
	return int(v), false
}

// ClampedIntToUint8 converts an int to a uint8, clamping the value to
// math.MaxUint8 if it exceeds the maximum value for uint8, and to 0 if it is
// negative. The second return value indicates whether clamping occurred.
func ClampedIntToUint8(v int) (out uint8, clamped bool) {
	if v > math.MaxUint8 {
		return math.MaxUint8, true
	}
	if v < 0 {
		return 0, true
	}
	return uint8(v), false
}

// ClampedUint64ToInt64 converts a uint64 to an int64, clamping the value to
// math.MaxInt64 if it exceeds the maximum value for int64. The second return
// value indicates whether clamping occurred.
func ClampedUint64ToInt64(v uint64) (out int64, clamped bool) {
	if v > math.MaxInt64 {
		return math.MaxInt64, true
	}
	return int64(v), false
}

// ClampedUint64ToInt converts a uint64 to an int, clamping the value to
// math.MaxInt if it exceeds the platform's maximum int value. The second
// return value indicates whether clamping occurred.
func ClampedUint64ToInt(v uint64) (out int, clamped bool) {
	if v > uint64(math.MaxInt) {
		return math.MaxInt, true
	}
	return int(v), false
}

// SafeInt converts int64 to int, clamping at the platform's int boundaries.
func SafeInt(v int64) int {
	const maxInt = int64(^uint(0) >> 1)
	const minInt = -maxInt - 1
	if v > maxInt {
		return int(maxInt)
	}
	if v < minInt {
		return int(minInt)
	}
	return int(v)
}

// ParseOptionalTimeWindow reads the optional RFC 3339 bounds a caller sends to
// scope a listing to a time range, and reports them as a half-open [from, to).
//
// A nil or blank bound is no bound rather than the zero time: the dashboard
// clears a range by sending the parameter empty, and reading that as year zero
// would silently filter every row out instead of widening back to everything.
// Both bounds are normalized to UTC so the comparison a caller builds does not
// depend on the offset the client happened to send.
func ParseOptionalTimeWindow(rawFrom, rawTo *string) (from *time.Time, to *time.Time, err error) {
	parse := func(raw *string, name string) (*time.Time, error) {
		if raw == nil {
			return nil, nil
		}
		trimmed := strings.TrimSpace(*raw)
		if trimmed == "" {
			return nil, nil
		}
		parsed, err := time.Parse(time.RFC3339Nano, trimmed)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		utc := parsed.UTC()
		return &utc, nil
	}

	from, err = parse(rawFrom, "from")
	if err != nil {
		return nil, nil, err
	}
	to, err = parse(rawTo, "to")
	if err != nil {
		return nil, nil, err
	}
	// An inverted or empty window is a caller error, not an empty result: it
	// would otherwise read as "this identity did nothing in that period".
	if from != nil && to != nil && !from.Before(*to) {
		return nil, nil, fmt.Errorf("from must be before to")
	}
	return from, to, nil
}
