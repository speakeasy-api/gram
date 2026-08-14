package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOrganizationSlugFromDestinationURL pins the destination URL shapes the
// admin dashboard emits. The admin dashboard builds them in
// client/admin/src/lib/impersonation.ts and puts them in the `redirect` query
// parameter of the login URL, which becomes loginState.FinalDestinationURL.
// A shape this function rejects has no visible failure mode: the operator
// silently lands on their own default organization instead of the target.
func TestOrganizationSlugFromDestinationURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		destinationURL string
		want           string
	}{
		{
			name:           "relative path, slug only",
			destinationURL: "/acme-placeholder",
			want:           "acme-placeholder",
		},
		{
			name:           "relative path with trailing segments",
			destinationURL: "/acme-placeholder/projects/default",
			want:           "acme-placeholder",
		},
		{
			name:           "absolute URL, slug only",
			destinationURL: "https://app.example.test/acme-placeholder",
			want:           "acme-placeholder",
		},
		{
			name:           "absolute URL with trailing segments",
			destinationURL: "https://app.example.test/acme-placeholder/projects/default",
			want:           "acme-placeholder",
		},
		{
			name:           "slug survives a query string",
			destinationURL: "/acme-placeholder?tab=tools",
			want:           "acme-placeholder",
		},
		{
			name:           "empty destination",
			destinationURL: "",
			want:           "",
		},
		{
			name:           "root path only",
			destinationURL: "/",
			want:           "",
		},
		{
			name:           "absolute URL with no path",
			destinationURL: "https://app.example.test",
			want:           "",
		},
	}

	for _, tt := range tests {
		require.Equal(t, tt.want, organizationSlugFromDestinationURL(tt.destinationURL), tt.name)
	}
}
