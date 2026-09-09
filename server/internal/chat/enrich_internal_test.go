package chat

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestNeedsClaudeTurnUsage(t *testing.T) {
	t.Parallel()

	ptr := func(s string) *string { return &s }

	require.False(t, needsClaudeTurnUsage(ptr("cursor"), nil))
	require.False(t, needsClaudeTurnUsage(ptr("codex"), nil))
	require.False(t, needsClaudeTurnUsage(nil, ptr("codex")))
	require.True(t, needsClaudeTurnUsage(ptr("claude-code"), nil))
	require.True(t, needsClaudeTurnUsage(ptr("litellm"), ptr("claude-code")))
	require.False(t, needsClaudeTurnUsage(ptr("litellm"), ptr("codex")))
	require.True(t, needsClaudeTurnUsage(ptr("litellm"), nil))
	require.True(t, needsClaudeTurnUsage(nil, nil))
	require.True(t, needsClaudeTurnUsage(ptr("new-agent-surface"), nil))
	require.True(t, needsClaudeTurnUsage(nil, ptr("unknown-client")))
}

func TestWorkUnitsTrendMetricsFrom(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	created := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	require.Equal(t, from.Add(-chatMetricsLookback), workUnitsTrendMetricsFrom(pgtype.Timestamptz{}, from))
	require.Equal(t, created.Add(-chatMetricsLookback), workUnitsTrendMetricsFrom(pgtype.Timestamptz{
		Time:             created,
		InfinityModifier: pgtype.Finite,
		Valid:            true,
	}, from))
}

func TestParseChatIDs(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	require.Equal(t, []uuid.UUID{id}, parseChatIDs([]string{"not-a-uuid", id.String()}))
	require.Empty(t, parseChatIDs(nil))
}
