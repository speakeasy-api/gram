package conv_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/stretchr/testify/require"
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
