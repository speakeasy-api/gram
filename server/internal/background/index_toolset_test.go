package background

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
)

func TestExecuteIndexToolsetWithoutTemporal(t *testing.T) {
	t.Parallel()

	run, err := ExecuteIndexToolset(t.Context(), nil, IndexToolsetParams{
		ProjectID:   uuid.New(),
		ToolsetSlug: types.Slug("unavailable-index"),
	})
	require.ErrorIs(t, err, ErrTemporalUnavailable)
	require.Nil(t, run)
}
