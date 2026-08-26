package audit_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
)

func TestAuditCursor(t *testing.T) {
	t.Parallel()

	cursor := audit.EncodeCursor(42, "row-id")
	require.Equal(t, base64.RawURLEncoding.EncodeToString([]byte("42:row-id")), cursor)

	seq, err := audit.DecodeCursor(cursor)
	require.NoError(t, err)
	require.Equal(t, int64(42), seq)

	for _, cursor := range []string{"%%%", base64.RawURLEncoding.EncodeToString([]byte("42")), base64.RawURLEncoding.EncodeToString([]byte("nope:row-id"))} {
		_, err := audit.DecodeCursor(cursor)
		require.Error(t, err)
	}
}
