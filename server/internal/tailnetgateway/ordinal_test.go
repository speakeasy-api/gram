package tailnetgateway_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/tailnetgateway"
)

func TestParsePodOrdinal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		explicit string
		podName  string
		want     int32
		wantErr  bool
	}{
		{name: "explicit index wins", explicit: "2", podName: "gram-tailnet-gateway-0", want: 2, wantErr: false},
		{name: "pod name suffix", explicit: "", podName: "gram-tailnet-gateway-1", want: 1, wantErr: false},
		{name: "whitespace around explicit", explicit: " 3 ", podName: "", want: 3, wantErr: false},
		{name: "no suffix", explicit: "", podName: "gateway", want: 0, wantErr: true},
		{name: "trailing dash", explicit: "", podName: "gateway-", want: 0, wantErr: true},
		{name: "negative", explicit: "-1", podName: "", want: 0, wantErr: true},
		{name: "too large", explicit: "100000", podName: "", want: 0, wantErr: true},
		{name: "non numeric suffix", explicit: "", podName: "gateway-abc", want: 0, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tailnetgateway.ParsePodOrdinal(tc.explicit, tc.podName)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestNodeNaming(t *testing.T) {
	t.Parallel()
	require.Equal(t, "speakeasy-gw-0", tailnetgateway.NodeHostname(0))
	require.Equal(t, "svc:speakeasy", tailnetgateway.ServiceName("speakeasy"))
	require.NoError(t, tailnetgateway.ValidateTag("tag:speakeasy"))
	require.Error(t, tailnetgateway.ValidateTag("speakeasy"))
	require.Error(t, tailnetgateway.ValidateTag("tag:"))
	require.NoError(t, tailnetgateway.ValidateLabel("speakeasy"))
	require.Error(t, tailnetgateway.ValidateLabel("Speakeasy"))
	require.Error(t, tailnetgateway.ValidateLabel(""))

	status, err := tailnetgateway.ParseNodeStatus("online")
	require.NoError(t, err)
	require.Equal(t, tailnetgateway.NodeStatusOnline, status)
	_, err = tailnetgateway.ParseNodeStatus("bogus")
	require.Error(t, err)
}
