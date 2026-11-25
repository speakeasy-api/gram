package remotesessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestSupportsClientIDMetadataDocument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		supported bool
		methods   []string
		want      bool
	}{
		{name: "advertised with public client method", supported: true, methods: []string{"none"}, want: true},
		{name: "advertised among other methods", supported: true, methods: []string{"client_secret_basic", "none"}, want: true},
		{name: "advertised without enumerated methods", supported: true, methods: nil, want: true},
		{name: "advertised but public clients refused", supported: true, methods: []string{"client_secret_basic"}, want: false},
		{name: "not advertised", supported: false, methods: []string{"none"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, remotesessions.SupportsClientIDMetadataDocument(test.supported, test.methods))
		})
	}
}
