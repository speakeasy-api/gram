package mcpskills

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func versionCandidate(pinned bool, effectiveAt time.Time) candidate {
	return candidate{
		entry: Entry{
			SkillID: uuid.Nil, VersionID: uuid.New(), PluginID: uuid.New(), Name: "skill",
			Content: "", RawSHA256: "", Frontmatter: nil, Pinned: pinned,
		},
		effectiveAt: effectiveAt,
	}
}

func TestPickWinnerSingleCandidate(t *testing.T) {
	t.Parallel()

	only := versionCandidate(false, time.Unix(100, 0))

	winner, conflicts := pickWinner([]candidate{only})
	require.Equal(t, only.entry, winner)
	require.Empty(t, conflicts)
}

func TestPickWinnerPinBeatsNewerLatest(t *testing.T) {
	t.Parallel()

	latest := versionCandidate(false, time.Unix(200, 0))
	pinned := versionCandidate(true, time.Unix(100, 0))

	winner, conflicts := pickWinner([]candidate{latest, pinned})
	require.Equal(t, pinned.entry, winner)
	require.Empty(t, conflicts, "a pin losing to latest is the rule, not a conflict")
}

func TestPickWinnerNewestPinWinsAndReportsConflict(t *testing.T) {
	t.Parallel()

	older := versionCandidate(true, time.Unix(100, 0))
	newer := versionCandidate(true, time.Unix(200, 0))

	winner, conflicts := pickWinner([]candidate{older, newer})
	require.Equal(t, newer.entry, winner)
	require.Equal(t, []uuid.UUID{older.entry.VersionID}, conflicts)

	winner, conflicts = pickWinner([]candidate{newer, older})
	require.Equal(t, newer.entry, winner, "order of candidates must not change the result")
	require.Equal(t, []uuid.UUID{older.entry.VersionID}, conflicts)
}

func TestPickWinnerSamePinTwiceIsNotAConflict(t *testing.T) {
	t.Parallel()

	first := versionCandidate(true, time.Unix(100, 0))
	second := first
	second.entry.PluginID = uuid.New()

	_, conflicts := pickWinner([]candidate{first, second})
	require.Empty(t, conflicts)
}

func TestPickWinnerSameTimeBreaksTieOnVersionID(t *testing.T) {
	t.Parallel()

	at := time.Unix(100, 0)
	low := versionCandidate(false, at)
	low.entry.VersionID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	high := versionCandidate(false, at)
	high.entry.VersionID = uuid.MustParse("00000000-0000-0000-0000-000000000002")

	winner, _ := pickWinner([]candidate{low, high})
	require.Equal(t, high.entry, winner)

	winner, _ = pickWinner([]candidate{high, low})
	require.Equal(t, high.entry, winner)
}
