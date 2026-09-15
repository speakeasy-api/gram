package gram

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNetworkIngressLifecycleDeliveryReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		reconcileQueue   string
		temporalQueue    string
		devSingleProcess bool
		wantReady        bool
		wantErr          string
	}{
		{
			name:          "unconfigured",
			temporalQueue: "main",
		},
		{
			name:           "shared queue",
			reconcileQueue: "main",
			temporalQueue:  "main",
			wantReady:      true,
		},
		{
			name:           "different queue outside single process",
			reconcileQueue: "network-ingress",
			temporalQueue:  "main",
		},
		{
			name:             "different queue in single process",
			reconcileQueue:   "network-ingress",
			temporalQueue:    "main",
			devSingleProcess: true,
			wantErr:          `dev-single-process requires private ingress reconciliation task queue "network-ingress" to match Temporal task queue "main"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ready, err := networkIngressLifecycleDeliveryReady(tt.reconcileQueue, tt.temporalQueue, tt.devSingleProcess)
			require.Equal(t, tt.wantReady, ready)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tt.wantErr)
		})
	}
}
