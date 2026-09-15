package research

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSameRedirectSite(t *testing.T) {
	t.Parallel()

	parse := func(raw string) *url.URL {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		return u
	}

	tests := []struct {
		name string
		prev string
		next string
		want bool
	}{
		{name: "same host", prev: "https://vendor.example/a", next: "https://vendor.example/b", want: true},
		{name: "apex to www", prev: "https://vendor.example/", next: "https://www.vendor.example/", want: true},
		{name: "www to apex", prev: "https://www.vendor.example/", next: "https://vendor.example/", want: true},
		{name: "zero-padded port dials the same service", prev: "https://vendor.example/", next: "https://www.vendor.example:0443/", want: true},
		{name: "explicit default port", prev: "https://vendor.example:443/", next: "https://vendor.example/", want: true},
		{name: "a port change is another service", prev: "https://vendor.example/", next: "https://vendor.example:8443/", want: false},
		{name: "another host entirely", prev: "https://vendor.example/", next: "https://other.example/", want: false},
		{name: "deeper subdomain is not the www twin", prev: "https://vendor.example/", next: "https://docs.vendor.example/", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, sameRedirectSite(parse(tt.prev), parse(tt.next)))
		})
	}
}
