package mv_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mv"
)

// The toolset-scoped token is checked by toolsets.update, so like the
// server-scoped exposure_version it must move with the list and the target and
// ignore list order — and it must never collide with a server-scoped token.
func TestToolsetVersionTokenTracksListAndTarget(t *testing.T) {
	t.Parallel()

	projectID, toolsetID := uuid.New(), uuid.New()
	urns := []string{"tools:function:orders:create_order", "tools:http:billing:get_invoice"}
	base := mv.ToolsetVersionToken(projectID, toolsetID, 4, urns)
	require.Len(t, base, 64)

	require.Equal(t, base, mv.ToolsetVersionToken(projectID, toolsetID, 4, []string{urns[1], urns[0]}))
	require.NotEqual(t, base, mv.ToolsetVersionToken(projectID, toolsetID, 5, urns))
	require.NotEqual(t, base, mv.ToolsetVersionToken(projectID, toolsetID, 4, urns[:1]))
	require.NotEqual(t, base, mv.ToolsetVersionToken(projectID, uuid.New(), 4, urns))
	require.NotEqual(t, base, mv.ToolsetVersionToken(uuid.New(), toolsetID, 4, urns))
	require.NotEqual(t, base, mv.ToolListVersionToken(projectID, uuid.New(), toolsetID, 4, urns))
}
