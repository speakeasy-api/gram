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
			name:           "dedicated worker queue",
			reconcileQueue: "network-ingress",
			temporalQueue:  "main",
			wantReady:      true,
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

func TestNetworkIngressAdmissionReady(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name               string
		reconcileQueue     string
		temporalQueue      string
		lifecycleReady     bool
		temporalConfigured bool
		want               bool
	}{
		{name: "dedicated worker", reconcileQueue: "network-ingress", temporalQueue: "main", lifecycleReady: true, temporalConfigured: true, want: true},
		{name: "shared queue starts fail closed", reconcileQueue: "main", temporalQueue: "main", lifecycleReady: true, temporalConfigured: true, want: false},
		{name: "missing temporal", reconcileQueue: "network-ingress", temporalQueue: "main", lifecycleReady: true},
		{name: "lifecycle disabled", reconcileQueue: "network-ingress", temporalQueue: "main", temporalConfigured: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, networkIngressAdmissionReady(
				tt.reconcileQueue,
				tt.temporalQueue,
				tt.lifecycleReady,
				tt.temporalConfigured,
			))
		})
	}
}
