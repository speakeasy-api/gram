package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Pins the shapes client/admin/src/lib/impersonation.ts emits. A rejected
// shape fails silently: the operator lands on their own default org.
//
// The slug is read out of the destination by safeRedirectPath, so an absolute
// URL only yields a slug when it names the dashboard's own origin.
func TestOrganizationSlugFromDestinationURL(t *testing.T) {
	t.Parallel()

	s := &Service{siteOrigin: "https://app.example.test"}

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
		{
			name:           "absolute URL on another origin",
			destinationURL: "https://attacker.example.net/acme-placeholder",
			want:           "",
		},
	}

	for _, tt := range tests {
		require.Equal(t, tt.want, s.organizationSlugFromDestinationURL(tt.destinationURL), tt.name)
	}
}
