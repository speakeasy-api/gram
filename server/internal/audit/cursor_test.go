package audit_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
)

func TestAuditCursor(t *testing.T) {
	t.Parallel()

	cursor := audit.EncodeCursor(audit.CategoryConfiguration, 42, "row-id")
	require.Equal(t, base64.RawURLEncoding.EncodeToString([]byte("42:row-id")), cursor, "configuration cursors keep the original format")

	decoded, err := audit.DecodeCursor(cursor)
	require.NoError(t, err)
	require.Equal(t, audit.Cursor{Seq: 42, Category: audit.CategoryConfiguration}, decoded)

	security := audit.EncodeCursor(audit.CategorySecurity, 1_000_000_000_042, "row-id")
	decoded, err = audit.DecodeCursor(security)
	require.NoError(t, err)
	require.Equal(t, audit.Cursor{Seq: 1_000_000_000_042, Category: audit.CategorySecurity}, decoded)

	for _, cursor := range []string{"%%%", base64.RawURLEncoding.EncodeToString([]byte("42")), base64.RawURLEncoding.EncodeToString([]byte("nope:row-id")), base64.RawURLEncoding.EncodeToString([]byte("security:42"))} {
		_, err := audit.DecodeCursor(cursor)
		require.Error(t, err)
	}
}
