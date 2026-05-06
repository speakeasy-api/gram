package conv_test

import (
	"math"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestFromPGInt4_Valid(t *testing.T) {
	t.Parallel()

	input := pgtype.Int4{Int32: 42, Valid: true}
	result := conv.FromPGInt4(input)

	require.NotNil(t, result)
	require.Equal(t, int32(42), *result)
}

func TestFromPGInt4_Invalid(t *testing.T) {
	t.Parallel()

	input := pgtype.Int4{Int32: 0, Valid: false}
	result := conv.FromPGInt4(input)

	require.Nil(t, result)
}

func TestPtrInt32ToInt_NonNil(t *testing.T) {
	t.Parallel()

	v := int32(99)
	result := conv.PtrInt32ToInt(&v)

	require.NotNil(t, result)
	require.Equal(t, 99, *result)
}

func TestPtrInt32ToInt_Nil(t *testing.T) {
	t.Parallel()

	result := conv.PtrInt32ToInt(nil)

	require.Nil(t, result)
}

func TestPtrEmpty_NonZero(t *testing.T) {
	t.Parallel()

	require.Equal(t, 7, *conv.PtrEmpty(7))
	require.Equal(t, "x", *conv.PtrEmpty("x"))
	require.Equal(t, true, *conv.PtrEmpty(true))
}

func TestPtrEmpty_Zero(t *testing.T) {
	t.Parallel()

	require.Nil(t, conv.PtrEmpty(0))
	require.Nil(t, conv.PtrEmpty(""))
	require.Nil(t, conv.PtrEmpty(false))
}

func TestPtrValOr_Nil(t *testing.T) {
	t.Parallel()

	require.Equal(t, 5, conv.PtrValOr[int](nil, 5))
	require.Equal(t, "fallback", conv.PtrValOr[string](nil, "fallback"))
}

func TestPtrValOr_NonNil(t *testing.T) {
	t.Parallel()

	v := 9
	require.Equal(t, 9, conv.PtrValOr(&v, 5))
}

func TestPtrValOr_NonNil_ZeroValueReturned(t *testing.T) {
	t.Parallel()

	// PtrValOr does NOT collapse zero values to the default; only nil triggers
	// the default. Empty string pointer returns empty string.
	empty := ""
	require.Equal(t, "", conv.PtrValOr(&empty, "fallback"))
}

func TestPtrValOrEmpty_NilReturnsDefault(t *testing.T) {
	t.Parallel()

	require.Equal(t, "fallback", conv.PtrValOrEmpty[string](nil, "fallback"))
}

func TestPtrValOrEmpty_ZeroPointerReturnsDefault(t *testing.T) {
	t.Parallel()

	empty := ""
	require.Equal(t, "fallback", conv.PtrValOrEmpty(&empty, "fallback"))
}

func TestPtrValOrEmpty_NonZeroPointer(t *testing.T) {
	t.Parallel()

	v := "real"
	require.Equal(t, "real", conv.PtrValOrEmpty(&v, "fallback"))
}

func TestDefault_ZeroReturnsDefault(t *testing.T) {
	t.Parallel()

	require.Equal(t, 5, conv.Default(0, 5))
	require.Equal(t, "fallback", conv.Default("", "fallback"))
	require.Equal(t, true, conv.Default(false, true))
}

func TestDefault_NonZeroReturnsValue(t *testing.T) {
	t.Parallel()

	require.Equal(t, 9, conv.Default(9, 5))
	require.Equal(t, "real", conv.Default("real", "fallback"))
}

func TestPtrToNullUUID_Nil(t *testing.T) {
	t.Parallel()

	got, err := conv.PtrToNullUUID(nil)
	require.NoError(t, err)
	require.False(t, got.Valid)
}

func TestPtrToNullUUID_Valid(t *testing.T) {
	t.Parallel()

	id := uuid.New().String()
	got, err := conv.PtrToNullUUID(&id)
	require.NoError(t, err)
	require.True(t, got.Valid)
	require.Equal(t, id, got.UUID.String())
}

func TestPtrToNullUUID_Invalid(t *testing.T) {
	t.Parallel()

	bad := "not-a-uuid"
	got, err := conv.PtrToNullUUID(&bad)
	require.Error(t, err)
	require.False(t, got.Valid)
}

func TestFromNullableUUID_Invalid(t *testing.T) {
	t.Parallel()

	require.Nil(t, conv.FromNullableUUID(uuid.NullUUID{Valid: false}))
}

func TestFromNullableUUID_Valid(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	got := conv.FromNullableUUID(uuid.NullUUID{UUID: id, Valid: true})
	require.NotNil(t, got)
	require.Equal(t, id.String(), *got)
}

func TestFromPGText_Invalid(t *testing.T) {
	t.Parallel()

	require.Nil(t, conv.FromPGText[string](pgtype.Text{Valid: false}))
}

func TestFromPGText_Valid(t *testing.T) {
	t.Parallel()

	got := conv.FromPGText[string](pgtype.Text{String: "hello", Valid: true})
	require.NotNil(t, got)
	require.Equal(t, "hello", *got)
}

func TestFromPGText_Subtype(t *testing.T) {
	t.Parallel()

	type Slug string
	got := conv.FromPGText[Slug](pgtype.Text{String: "sl", Valid: true})
	require.NotNil(t, got)
	require.Equal(t, Slug("sl"), *got)
}

func TestFromPGTextOrEmpty_Invalid(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", conv.FromPGTextOrEmpty[string](pgtype.Text{Valid: false}))
}

func TestFromPGTextOrEmpty_Valid(t *testing.T) {
	t.Parallel()

	require.Equal(t, "hi", conv.FromPGTextOrEmpty[string](pgtype.Text{String: "hi", Valid: true}))
}

func TestToPGText_AlwaysValid(t *testing.T) {
	t.Parallel()

	got := conv.ToPGText("")
	require.True(t, got.Valid)
	require.Equal(t, "", got.String)

	got = conv.ToPGText("hello")
	require.True(t, got.Valid)
	require.Equal(t, "hello", got.String)
}

func TestToPGTextEmpty_EmptyIsInvalid(t *testing.T) {
	t.Parallel()

	got := conv.ToPGTextEmpty("")
	require.False(t, got.Valid)
}

func TestToPGTextEmpty_NonEmptyIsValid(t *testing.T) {
	t.Parallel()

	got := conv.ToPGTextEmpty("hi")
	require.True(t, got.Valid)
	require.Equal(t, "hi", got.String)
}

func TestPtrToPGText_Nil(t *testing.T) {
	t.Parallel()

	got := conv.PtrToPGText(nil)
	require.False(t, got.Valid)
}

func TestPtrToPGText_NonNilEmpty(t *testing.T) {
	t.Parallel()

	s := ""
	got := conv.PtrToPGText(&s)
	require.True(t, got.Valid)
}

func TestPtrToPGText_NonNil(t *testing.T) {
	t.Parallel()

	s := "hi"
	got := conv.PtrToPGText(&s)
	require.True(t, got.Valid)
	require.Equal(t, "hi", got.String)
}

func TestPtrToPGTextEmpty_Nil(t *testing.T) {
	t.Parallel()

	got := conv.PtrToPGTextEmpty(nil)
	require.False(t, got.Valid)
}

func TestPtrToPGTextEmpty_NonNilEmpty(t *testing.T) {
	t.Parallel()

	s := ""
	got := conv.PtrToPGTextEmpty(&s)
	require.False(t, got.Valid)
}

func TestPtrToPGTextEmpty_NonNilNonEmpty(t *testing.T) {
	t.Parallel()

	s := "hi"
	got := conv.PtrToPGTextEmpty(&s)
	require.True(t, got.Valid)
	require.Equal(t, "hi", got.String)
}

func TestToPGTimestamptz(t *testing.T) {
	t.Parallel()

	now := time.Now()
	got := conv.ToPGTimestamptz(now)
	require.True(t, got.Valid)
	require.Equal(t, pgtype.Finite, got.InfinityModifier)
	require.True(t, got.Time.Equal(now))
}

func TestPtrToPGTimestamptz_Nil(t *testing.T) {
	t.Parallel()

	got := conv.PtrToPGTimestamptz(nil)
	require.False(t, got.Valid)
}

func TestPtrToPGTimestamptz_NonNil(t *testing.T) {
	t.Parallel()

	now := time.Now()
	got := conv.PtrToPGTimestamptz(&now)
	require.True(t, got.Valid)
	require.True(t, got.Time.Equal(now))
}

func TestPtrToPGInt8_Nil(t *testing.T) {
	t.Parallel()

	got := conv.PtrToPGInt8(nil)
	require.False(t, got.Valid)
}

func TestPtrToPGInt8_NonNil(t *testing.T) {
	t.Parallel()

	v := 42
	got := conv.PtrToPGInt8(&v)
	require.True(t, got.Valid)
	require.Equal(t, int64(42), got.Int64)
}

func TestPtrToPGBool_Nil(t *testing.T) {
	t.Parallel()

	got := conv.PtrToPGBool(nil)
	require.False(t, got.Valid)
}

func TestPtrToPGBool_True(t *testing.T) {
	t.Parallel()

	v := true
	got := conv.PtrToPGBool(&v)
	require.True(t, got.Valid)
	require.True(t, got.Bool)
}

func TestPtrToPGBool_False(t *testing.T) {
	t.Parallel()

	v := false
	got := conv.PtrToPGBool(&v)
	require.True(t, got.Valid)
	require.False(t, got.Bool)
}

func TestFromPGBool_Invalid(t *testing.T) {
	t.Parallel()

	require.Nil(t, conv.FromPGBool[bool](pgtype.Bool{Valid: false}))
}

func TestFromPGBool_Valid(t *testing.T) {
	t.Parallel()

	got := conv.FromPGBool[bool](pgtype.Bool{Bool: true, Valid: true})
	require.NotNil(t, got)
	require.True(t, *got)
}

func TestFromPGFloat8_Invalid(t *testing.T) {
	t.Parallel()

	require.Nil(t, conv.FromPGFloat8(pgtype.Float8{Valid: false}))
}

func TestFromPGFloat8_Valid(t *testing.T) {
	t.Parallel()

	got := conv.FromPGFloat8(pgtype.Float8{Float64: 3.14, Valid: true})
	require.NotNil(t, got)
	require.InDelta(t, 3.14, *got, 0.0001)
}

func TestFromBytes_Empty(t *testing.T) {
	t.Parallel()

	require.Nil(t, conv.FromBytes(nil))
	require.Nil(t, conv.FromBytes([]byte{}))
}

func TestFromBytes_NonEmpty(t *testing.T) {
	t.Parallel()

	got := conv.FromBytes([]byte("hi"))
	require.NotNil(t, got)
	require.Equal(t, "hi", *got)
}

func TestToSlug_Basic(t *testing.T) {
	t.Parallel()

	require.Equal(t, "hello-world", conv.ToSlug("Hello World"))
}

func TestToSlug_StripsSpecialChars(t *testing.T) {
	t.Parallel()

	require.Equal(t, "abc-123", conv.ToSlug("abc!@#$%^&*() 123"))
}

func TestToSlug_CollapsesWhitespaceAndDashes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "a-b-c", conv.ToSlug("a   b---c"))
}

func TestToSlug_TrimsLeadingTrailingDashes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "hello", conv.ToSlug("---hello---"))
}

func TestToSlug_AllSpecialCharsBecomesEmpty(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", conv.ToSlug("!@#$%^&*()"))
}

func TestGenerateRandomSlug_Length(t *testing.T) {
	t.Parallel()

	for _, n := range []int{1, 8, 32, 128} {
		got, err := conv.GenerateRandomSlug(n)
		require.NoError(t, err)
		require.Len(t, got, n)
	}
}

func TestGenerateRandomSlug_Charset(t *testing.T) {
	t.Parallel()

	got, err := conv.GenerateRandomSlug(64)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^[a-z1-9]+$`), got)
}

func TestGenerateRandomSlug_Distinct(t *testing.T) {
	t.Parallel()

	a, err := conv.GenerateRandomSlug(32)
	require.NoError(t, err)
	b, err := conv.GenerateRandomSlug(32)
	require.NoError(t, err)
	require.NotEqual(t, a, b)
}

func TestGenerateRandomSlug_Zero(t *testing.T) {
	t.Parallel()

	got, err := conv.GenerateRandomSlug(0)
	require.NoError(t, err)
	require.Equal(t, "", got)
}

func TestToLower(t *testing.T) {
	t.Parallel()

	require.Equal(t, "hello", conv.ToLower("HELLO"))
	require.Equal(t, "hello", conv.ToLower("Hello"))

	type Tag string
	require.Equal(t, "tag", conv.ToLower(Tag("TAG")))
}

func TestAnySlice(t *testing.T) {
	t.Parallel()

	got := conv.AnySlice([]int{1, 2, 3})
	require.Equal(t, []any{1, 2, 3}, got)
}

func TestAnySlice_Empty(t *testing.T) {
	t.Parallel()

	require.Equal(t, []any{}, conv.AnySlice([]string{}))
}

func TestAnySlice_Nil(t *testing.T) {
	t.Parallel()

	require.Equal(t, []any{}, conv.AnySlice[string](nil))
}

func TestTernary_True(t *testing.T) {
	t.Parallel()

	require.Equal(t, "yes", conv.Ternary(true, "yes", "no"))
}

func TestTernary_False(t *testing.T) {
	t.Parallel()

	require.Equal(t, "no", conv.Ternary(false, "yes", "no"))
}

func TestSafeInt32_InRange(t *testing.T) {
	t.Parallel()

	require.Equal(t, int32(0), conv.SafeInt32(0))
	require.Equal(t, int32(42), conv.SafeInt32(42))
	require.Equal(t, int32(-7), conv.SafeInt32(-7))
}

func TestSafeInt32_ClampsHigh(t *testing.T) {
	t.Parallel()

	require.Equal(t, int32(math.MaxInt32), conv.SafeInt32(math.MaxInt32))
	require.Equal(t, int32(math.MaxInt32), conv.SafeInt32(math.MaxInt32+1))
}

func TestSafeInt32_ClampsLow(t *testing.T) {
	t.Parallel()

	require.Equal(t, int32(math.MinInt32), conv.SafeInt32(math.MinInt32))
	require.Equal(t, int32(math.MinInt32), conv.SafeInt32(math.MinInt32-1))
}

func TestClampedUintToInt32_InRange(t *testing.T) {
	t.Parallel()

	got, clamped := conv.ClampedUintToInt32(42)
	require.Equal(t, int32(42), got)
	require.False(t, clamped)
}

func TestClampedUintToInt32_Zero(t *testing.T) {
	t.Parallel()

	got, clamped := conv.ClampedUintToInt32(0)
	require.Equal(t, int32(0), got)
	require.False(t, clamped)
}

func TestClampedUintToInt32_Clamps(t *testing.T) {
	t.Parallel()

	got, clamped := conv.ClampedUintToInt32(uint(math.MaxInt32) + 1)
	require.Equal(t, int32(math.MaxInt32), got)
	require.True(t, clamped)
}

func TestClampedIntToUint8_InRange(t *testing.T) {
	t.Parallel()

	got, clamped := conv.ClampedIntToUint8(42)
	require.Equal(t, uint8(42), got)
	require.False(t, clamped)
}

func TestClampedIntToUint8_ClampsHigh(t *testing.T) {
	t.Parallel()

	got, clamped := conv.ClampedIntToUint8(math.MaxUint8 + 1)
	require.Equal(t, uint8(math.MaxUint8), got)
	require.True(t, clamped)
}

func TestClampedIntToUint8_ClampsLow(t *testing.T) {
	t.Parallel()

	got, clamped := conv.ClampedIntToUint8(-1)
	require.Equal(t, uint8(0), got)
	require.True(t, clamped)
}

func TestSafeInt_InRange(t *testing.T) {
	t.Parallel()

	require.Equal(t, 42, conv.SafeInt(42))
	require.Equal(t, -7, conv.SafeInt(-7))
}

func TestSafeInt_ClampsHigh(t *testing.T) {
	t.Parallel()

	maxInt := int64(^uint(0) >> 1)
	if maxInt == math.MaxInt64 {
		// On 64-bit platforms there is no value that exceeds int64 max.
		require.Equal(t, int(maxInt), conv.SafeInt(math.MaxInt64))
		return
	}
	require.Equal(t, int(maxInt), conv.SafeInt(maxInt+1))
}

func TestSafeInt_ClampsLow(t *testing.T) {
	t.Parallel()

	maxInt := int64(^uint(0) >> 1)
	minInt := -maxInt - 1
	if minInt == math.MinInt64 {
		require.Equal(t, int(minInt), conv.SafeInt(math.MinInt64))
		return
	}
	require.Equal(t, int(minInt), conv.SafeInt(minInt-1))
}
