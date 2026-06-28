package conv_test

import (
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
