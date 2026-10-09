package mv_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/mv"
)

func TestClassifyTunneledMcpConnection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		sourceStatus    string
		everSeen        bool
		liveConnections int
		want            types.TunneledMcpConnectionStatus
	}{
		{name: "live connection on a new source", sourceStatus: "created", everSeen: false, liveConnections: 1, want: "connected"},
		{name: "live connection on an active source", sourceStatus: "active", everSeen: true, liveConnections: 2, want: "connected"},
		{name: "new source never seen", sourceStatus: "created", everSeen: false, liveConnections: 0, want: "never_connected"},
		{name: "new source seen before", sourceStatus: "created", everSeen: true, liveConnections: 0, want: "inactive"},
		{name: "active source without live connections", sourceStatus: "active", everSeen: true, liveConnections: 0, want: "inactive"},
		{name: "active source without a recorded sighting", sourceStatus: "active", everSeen: false, liveConnections: 0, want: "inactive"},
		{name: "revoked source", sourceStatus: "revoked", everSeen: false, liveConnections: 0, want: "inactive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, mv.ClassifyTunneledMcpConnection(tc.sourceStatus, tc.everSeen, tc.liveConnections))
		})
	}
}
