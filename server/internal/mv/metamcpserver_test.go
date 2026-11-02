package mv_test

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/stretchr/testify/require"
)

func TestMetaMCPServerViewUnknownDiscoveryModeUsesProgressive(t *testing.T) {
	t.Parallel()
	view := mv.BuildMetaMcpServerView(repo.MetaMcpServer{DiscoveryMode: "future_mode"})
	require.Equal(t, "progressive", view.DiscoveryMode)
}
