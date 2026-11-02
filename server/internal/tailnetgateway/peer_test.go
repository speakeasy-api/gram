package tailnetgateway_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/tailnetgateway"
)

func TestPeerAddr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		remoteAddr string
		forwarded  string
		want       string
		wantErr    bool
	}{
		{name: "direct tailnet peer", remoteAddr: "100.64.0.7:51234", forwarded: "", want: "100.64.0.7", wantErr: false},
		{name: "direct ipv6 peer", remoteAddr: "[fd7a:115c:a1e0::7]:443", forwarded: "", want: "fd7a:115c:a1e0::7", wantErr: false},
		{name: "loopback with forwarded peer", remoteAddr: "127.0.0.1:40000", forwarded: "100.64.0.9", want: "100.64.0.9", wantErr: false},
		{name: "loopback with forwarded chain", remoteAddr: "[::1]:40000", forwarded: "100.64.0.9, 10.0.0.1", want: "100.64.0.9", wantErr: false},
		{name: "loopback without forwarded header", remoteAddr: "127.0.0.1:40000", forwarded: "", want: "", wantErr: true},
		{name: "loopback with garbage forwarded header", remoteAddr: "127.0.0.1:40000", forwarded: "not-an-ip", want: "", wantErr: true},
		{name: "invalid remote", remoteAddr: "nope", forwarded: "", want: "", wantErr: true},
		{name: "forwarded header ignored for direct peers", remoteAddr: "100.64.0.7:1", forwarded: "100.64.0.9", want: "100.64.0.7", wantErr: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &http.Request{RemoteAddr: tc.remoteAddr, Header: http.Header{}}
			if tc.forwarded != "" {
				r.Header.Set("X-Forwarded-For", tc.forwarded)
			}
			got, err := tailnetgateway.PeerAddr(r)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got.String())
		})
	}
}
