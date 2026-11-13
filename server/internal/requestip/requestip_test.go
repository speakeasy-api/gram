package requestip_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/requestip"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []string
		remote string
		want   string
		source requestip.Source
	}{
		{name: "edge", values: []string{"203.0.113.7"}, remote: "192.0.2.1:4321", want: "203.0.113.7", source: requestip.SourceHeader},
		{name: "whitespace", values: []string{" 203.0.113.7\t"}, want: "203.0.113.7", source: requestip.SourceHeader},
		{name: "ipv6", values: []string{"2001:db8::7"}, want: "2001:db8::7", source: requestip.SourceHeader},
		{name: "mapped ipv4", values: []string{"::ffff:203.0.113.7"}, want: "203.0.113.7", source: requestip.SourceHeader},
		{name: "missing", remote: "192.0.2.1:4321", want: "192.0.2.1", source: requestip.SourceTransport},
		{name: "empty", values: []string{""}, remote: "192.0.2.1:4321", want: "192.0.2.1", source: requestip.SourceTransport},
		{name: "malformed", values: []string{"garbage"}, remote: "192.0.2.1:4321", want: "192.0.2.1", source: requestip.SourceTransport},
		{name: "list", values: []string{"203.0.113.7, 203.0.113.8"}, remote: "192.0.2.1:4321", want: "192.0.2.1", source: requestip.SourceTransport},
		{name: "duplicate", values: []string{"203.0.113.7", "203.0.113.7"}, remote: "192.0.2.1:4321", want: "192.0.2.1", source: requestip.SourceTransport},
		{name: "port", values: []string{"203.0.113.7:443"}, remote: "192.0.2.1:4321", want: "192.0.2.1", source: requestip.SourceTransport},
		{name: "zone", values: []string{"fe80::1%eth0"}, remote: "192.0.2.1:4321", want: "192.0.2.1", source: requestip.SourceTransport},
		{name: "bounded header", values: []string{strings.Repeat(" ", 4096) + "203.0.113.7"}, remote: "192.0.2.1:4321", want: "192.0.2.1", source: requestip.SourceTransport},
		{name: "ipv6 transport", remote: "[2001:db8::1]:4321", want: "2001:db8::1", source: requestip.SourceTransport},
		{name: "bare transport", remote: "192.0.2.1", want: "192.0.2.1", source: requestip.SourceTransport},
		{name: "unresolved", remote: "not-an-address"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remote
			for _, value := range tt.values {
				req.Header.Add("X-Client-IP", value)
			}
			req.Header.Set("X-Forwarded-For", "198.51.100.9, 198.51.100.10")
			req.Header.Set("X-Real-IP", "198.51.100.9")
			result := requestip.Resolve(req)
			require.Equal(t, tt.want, result.String())
			require.Equal(t, tt.source, result.Source)
		})
	}
}

func TestUnresolvedContextIsDistinctFromMissing(t *testing.T) {
	t.Parallel()
	_, found := requestip.FromContext(t.Context())
	require.False(t, found)
	result, found := requestip.FromContext(requestip.WithContext(t.Context(), requestip.Result{}))
	require.True(t, found)
	require.Empty(t, result.String())
}
