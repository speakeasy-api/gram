package keys

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestKeyListCursor_BoundToScope(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 678900000, time.UTC)
	id := uuid.New()
	scope := keyListCursorScope("org-a", "deploy")
	cursor := encodeKeyListCursor(createdAt, id, scope)

	gotCreatedAt, gotID, err := decodeKeyListCursor(cursor, scope)
	require.NoError(t, err)
	require.True(t, createdAt.Equal(gotCreatedAt))
	require.Equal(t, id, gotID)

	for name, other := range map[string]string{
		"other organization": keyListCursorScope("org-b", "deploy"),
		"other search":       keyListCursorScope("org-a", "deplo"),
		"no search":          keyListCursorScope("org-a", ""),
	} {
		_, _, err := decodeKeyListCursor(cursor, other)
		require.Error(t, err, name)
	}

	// The NUL separator keeps the two halves apart: moving characters between
	// organization and search gives a different scope.
	require.NotEqual(t, keyListCursorScope("org-a", "b"), keyListCursorScope("org-", "ab"))

	// A long search term still gives a cursor well under the API's 512 limit.
	long := encodeKeyListCursor(createdAt, id, keyListCursorScope("org-a", string(make([]byte, 255))))
	require.Less(t, len(long), 512)
}
