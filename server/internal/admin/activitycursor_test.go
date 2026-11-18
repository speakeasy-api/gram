package admin

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
)

func TestActivityCursorRoundTrip(t *testing.T) {
	t.Parallel()

	cursor, err := decodeActivityCursor(activityCursor{
		configuration: activityLogPosition{seq: pgtype.Int8{Int64: 42, Valid: true}, done: false},
		security:      activityLogPosition{seq: pgtype.Int8{Int64: 0, Valid: false}, done: true},
	}.encode())
	require.NoError(t, err)
	require.Equal(t, int64(42), cursor.configuration.seq.Int64)
	require.True(t, cursor.security.done)
}

// A cursor from before the security log existed keeps paging the
// configuration log, and does not start the security log over.
func TestActivityCursorAcceptsALegacyConfigurationCursor(t *testing.T) {
	t.Parallel()

	cursor, err := decodeActivityCursor(audit.EncodeCursor(audit.CategoryConfiguration, 42, "row-id"))
	require.NoError(t, err)
	require.True(t, cursor.configuration.seq.Valid)
	require.Equal(t, int64(42), cursor.configuration.seq.Int64)
	require.True(t, cursor.security.done)

	_, err = decodeActivityCursor(audit.EncodeCursor(audit.CategorySecurity, 42, "row-id"))
	require.Error(t, err, "a security-only cursor is not a staff activity cursor")
	require.NoError(t, ValidateActivityCursor(audit.EncodeCursor(audit.CategoryConfiguration, 7, "row-id")))
}
