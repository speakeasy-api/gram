package conv_test

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/stretchr/testify/require"
)

func TestStringToNullUUID_Valid(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	result := conv.StringToNullUUID("  " + id.String() + "  ")

	require.True(t, result.Valid)
	require.Equal(t, id, result.UUID)
}

func TestStringToNullUUID_EmptyIsInvalid(t *testing.T) {
	t.Parallel()

	require.False(t, conv.StringToNullUUID("   ").Valid)
}

func TestStringToNullUUID_UnparseableIsInvalid(t *testing.T) {
	t.Parallel()

	require.False(t, conv.StringToNullUUID("not-a-uuid").Valid)
}

func TestNilableToNullUUID_Valid(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	result := conv.NilableToNullUUID(id)

	require.True(t, result.Valid)
	require.Equal(t, id, result.UUID)
}

func TestNilableToNullUUID_NilIsInvalid(t *testing.T) {
	t.Parallel()

	require.False(t, conv.NilableToNullUUID(uuid.Nil).Valid)
}

func TestPtrToPGTextTrimmed_Trims(t *testing.T) {
	t.Parallel()

	input := "  My IdP  "
	result := conv.PtrToPGTextTrimmed(&input)

	require.True(t, result.Valid)
	require.Equal(t, "My IdP", result.String)
}

func TestPtrToPGTextTrimmed_WhitespaceOnlyIsInvalid(t *testing.T) {
	t.Parallel()

	input := "   "
	result := conv.PtrToPGTextTrimmed(&input)

	require.False(t, result.Valid)
}

func TestPtrToPGTextTrimmed_EmptyIsInvalid(t *testing.T) {
	t.Parallel()

	input := ""
	result := conv.PtrToPGTextTrimmed(&input)

	require.False(t, result.Valid)
}

func TestPtrToPGTextTrimmed_NilIsInvalid(t *testing.T) {
	t.Parallel()

	result := conv.PtrToPGTextTrimmed(nil)

	require.False(t, result.Valid)
}

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

func TestFromPGTimestamptz_Valid(t *testing.T) {
	t.Parallel()

	input := pgtype.Timestamptz{Time: time.Date(2024, 11, 15, 15, 4, 5, 0, time.FixedZone("test", 2*60*60)), Valid: true}

	require.Equal(t, "2024-11-15T13:04:05Z", conv.FromPGTimestamptz(input))
}

func TestFromPGTimestamptz_Invalid(t *testing.T) {
	t.Parallel()

	require.Empty(t, conv.FromPGTimestamptz(pgtype.Timestamptz{}))
}

func TestURLToSlug_HostAndPath(t *testing.T) {
	t.Parallel()

	require.Equal(t, "api-example-com-mcp", conv.URLToSlug("api.example.com/mcp"))
}

func TestURLToSlug_HostOnly(t *testing.T) {
	t.Parallel()

	require.Equal(t, "api-example-com", conv.URLToSlug("api.example.com"))
}

func TestURLToSlug_Lowercase(t *testing.T) {
	t.Parallel()

	require.Equal(t, "api-example-com-mcp", conv.URLToSlug("API.Example.COM/MCP"))
}

func TestURLToSlug_HostWithPort(t *testing.T) {
	t.Parallel()

	require.Equal(t, "example-com-8080-mcp", conv.URLToSlug("example.com:8080/mcp"))
}

func TestURLToSlug_TrailingSlashTrimmed(t *testing.T) {
	t.Parallel()

	require.Equal(t, "example-com-mcp", conv.URLToSlug("example.com/mcp/"))
}

func TestURLToSlug_RunsCollapse(t *testing.T) {
	t.Parallel()

	// Adjacent separators collapse to a single hyphen rather than producing
	// double-hyphens.
	require.Equal(t, "example-com-mcp", conv.URLToSlug("example.com//mcp"))
}

func TestURLToSlug_Empty(t *testing.T) {
	t.Parallel()

	require.Empty(t, conv.URLToSlug(""))
}

func TestURLToSlug_OnlySeparators(t *testing.T) {
	t.Parallel()

	require.Empty(t, conv.URLToSlug("///..."))
}

func TestClampedUint32ToInt(t *testing.T) {
	t.Parallel()

	out, clamped := conv.ClampedUint32ToInt(0)
	require.Equal(t, 0, out)
	require.False(t, clamped)

	out, clamped = conv.ClampedUint32ToInt(42)
	require.Equal(t, 42, out)
	require.False(t, clamped)

	// On 64-bit platforms every uint32 fits; on 32-bit builds MaxUint32
	// exceeds MaxInt and must clamp.
	out, clamped = conv.ClampedUint32ToInt(math.MaxUint32)
	if uint64(math.MaxInt) >= uint64(math.MaxUint32) {
		require.False(t, clamped)
		require.Equal(t, uint64(math.MaxUint32), uint64(out))
	} else {
		require.True(t, clamped)
		require.Equal(t, math.MaxInt, out)
	}
}

func TestClampedUint64ToInt64(t *testing.T) {
	t.Parallel()

	out, clamped := conv.ClampedUint64ToInt64(0)
	require.Equal(t, int64(0), out)
	require.False(t, clamped)

	out, clamped = conv.ClampedUint64ToInt64(42)
	require.Equal(t, int64(42), out)
	require.False(t, clamped)

	out, clamped = conv.ClampedUint64ToInt64(math.MaxInt64)
	require.Equal(t, int64(math.MaxInt64), out)
	require.False(t, clamped)

	out, clamped = conv.ClampedUint64ToInt64(math.MaxUint64)
	require.True(t, clamped)
	require.Equal(t, int64(math.MaxInt64), out)
}

func TestClampedUint64ToInt(t *testing.T) {
	t.Parallel()

	out, clamped := conv.ClampedUint64ToInt(0)
	require.Equal(t, 0, out)
	require.False(t, clamped)

	out, clamped = conv.ClampedUint64ToInt(42)
	require.Equal(t, 42, out)
	require.False(t, clamped)

	out, clamped = conv.ClampedUint64ToInt(math.MaxUint64)
	require.True(t, clamped)
	require.Equal(t, math.MaxInt, out)
}

func TestParseOptionalTimeWindow_NoBounds(t *testing.T) {
	t.Parallel()

	from, to, err := conv.ParseOptionalTimeWindow(nil, nil)

	require.NoError(t, err)
	require.Nil(t, from)
	require.Nil(t, to)
}

// The dashboard clears a range by sending the parameter empty rather than by
// dropping it, so a blank string has to widen back to "no bound" instead of
// parsing as the zero time and filtering every row out.
func TestParseOptionalTimeWindow_BlankIsNoBound(t *testing.T) {
	t.Parallel()

	blank := "   "
	from, to, err := conv.ParseOptionalTimeWindow(&blank, &blank)

	require.NoError(t, err)
	require.Nil(t, from)
	require.Nil(t, to)
}

func TestParseOptionalTimeWindow_NormalizesToUTC(t *testing.T) {
	t.Parallel()

	start := "2026-03-01T12:00:00+02:00"
	end := "2026-03-08T12:00:00Z"
	from, to, err := conv.ParseOptionalTimeWindow(&start, &end)

	require.NoError(t, err)
	require.NotNil(t, from)
	require.NotNil(t, to)
	require.Equal(t, "2026-03-01T10:00:00Z", from.Format(time.RFC3339))
	require.Equal(t, time.UTC, from.Location())
	require.Equal(t, "2026-03-08T12:00:00Z", to.Format(time.RFC3339))
}

func TestParseOptionalTimeWindow_OneBoundOnly(t *testing.T) {
	t.Parallel()

	start := "2026-03-01T00:00:00Z"

	from, to, err := conv.ParseOptionalTimeWindow(&start, nil)
	require.NoError(t, err)
	require.NotNil(t, from)
	require.Nil(t, to)

	from, to, err = conv.ParseOptionalTimeWindow(nil, &start)
	require.NoError(t, err)
	require.Nil(t, from)
	require.NotNil(t, to)
}

// An inverted or empty window is a caller error rather than an empty result:
// silently returning nothing would read as "this identity did nothing then".
func TestParseOptionalTimeWindow_RejectsInvertedAndEmpty(t *testing.T) {
	t.Parallel()

	early := "2026-03-01T00:00:00Z"
	late := "2026-03-08T00:00:00Z"

	_, _, err := conv.ParseOptionalTimeWindow(&late, &early)
	require.ErrorContains(t, err, "from must be before to")

	_, _, err = conv.ParseOptionalTimeWindow(&early, &early)
	require.ErrorContains(t, err, "from must be before to", "an empty window is rejected too")
}

func TestParseOptionalTimeWindow_RejectsUnparseable(t *testing.T) {
	t.Parallel()

	bad := "not-a-timestamp"
	valid := "2026-03-08T00:00:00Z"

	_, _, err := conv.ParseOptionalTimeWindow(&bad, nil)
	require.ErrorContains(t, err, "parse from")

	_, _, err = conv.ParseOptionalTimeWindow(&valid, &bad)
	require.ErrorContains(t, err, "parse to")
}
