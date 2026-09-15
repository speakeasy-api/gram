package identityproviders

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeOktaDomainRepairsAdminConsoleHostsIdempotently(t *testing.T) {
	t.Parallel()

	require.Equal(t, "example.okta.com", normalizeOktaDomain("example-admin.okta.com"))
	require.Equal(t, "example.okta.com", normalizeOktaDomain(normalizeOktaDomain("example-admin.okta.com")))
	require.Equal(t, "example.okta.com", normalizeOktaDomain("example-admin-admin.okta.com"))
	require.Equal(t, "example.oktapreview.com", normalizeOktaDomain("example-admin.oktapreview.com"))
	require.Equal(t, "example.okta-emea.com", normalizeOktaDomain("example-admin.okta-emea.com"))
	require.Equal(t, "example-admin.example.com", normalizeOktaDomain("example-admin.example.com"))
}
