package orgprovision_test

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	"github.com/stretchr/testify/require"
)

func TestNameFromHostname(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ hostname, name string }{
		{"example.com", "example"},
		{"app.example.co.uk", "example"},
		{"a.b.example.com", "example"},
		{"app.tenant.github.io", "tenant"},
		{"www.xn--bcher-kva.de", "xn--bcher-kva"},
		{"www.city.kawasaki.jp", "city"},
		{"app.example.unknown-suffix", "example"},
		{"my-company.com", "my-company"},
		{"", ""},
		{"localhost", "localhost"},
		{"co.uk", "co.uk"},
		{"github.io", "github.io"},
		{"example..com", "example..com"},
		{"example.com.", "example.com."},
	} {
		t.Run(tc.hostname, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.name, orgprovision.NameFromHostname(tc.hostname))
		})
	}
}
