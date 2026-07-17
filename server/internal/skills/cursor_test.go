package skills

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSkillCursorRoundTrip(t *testing.T) {
	t.Parallel()

	cursor := encodeSkillCursor("release-notes")
	name, err := decodeSkillCursor(cursor)

	require.NoError(t, err)
	require.Equal(t, "release-notes", name)
}

func TestSkillCursorRejectsEmptyAndNonNormalizedNames(t *testing.T) {
	t.Parallel()

	_, emptyErr := decodeSkillCursor(base64.RawURLEncoding.EncodeToString(nil))
	_, normalizedErr := decodeSkillCursor(base64.RawURLEncoding.EncodeToString([]byte("Release_Notes")))

	require.Error(t, emptyErr)
	require.Error(t, normalizedErr)
}

func TestSkillVersionCursorRoundTrip(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.July, 15, 12, 34, 56, 789123000, time.UTC)
	id := uuid.MustParse("4dc20244-52a4-40ce-a09c-6bfa6f3fcf35")

	decodedTime, decodedID, err := decodeSkillVersionCursor(encodeSkillVersionCursor(createdAt, id))

	require.NoError(t, err)
	require.Equal(t, createdAt, decodedTime)
	require.Equal(t, id, decodedID)
}

func TestSkillVersionCursorRejectsNonCanonicalValues(t *testing.T) {
	t.Parallel()

	id := "4dc20244-52a4-40ce-a09c-6bfa6f3fcf35"
	nonUTC := base64.RawURLEncoding.EncodeToString([]byte("2026-07-15T14:34:56+02:00|" + id))
	nonCanonicalID := base64.RawURLEncoding.EncodeToString([]byte("2026-07-15T12:34:56Z|4DC20244-52A4-40CE-A09C-6BFA6F3FCF35"))

	_, _, timestampErr := decodeSkillVersionCursor(nonUTC)
	_, _, idErr := decodeSkillVersionCursor(nonCanonicalID)

	require.Error(t, timestampErr)
	require.Error(t, idErr)
}
