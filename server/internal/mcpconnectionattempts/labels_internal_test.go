package mcpconnectionattempts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKnownProviderHostsHaveNoTenantHosts(t *testing.T) {
	t.Parallel()
	tenantSuffixes := []string{".okta.com", ".oktapreview.com", ".auth0.com", ".my.salesforce.com", ".onelogin.com", ".b2clogin.com", ".ciamlogin.com", ".amazoncognito.com", ".zendesk.com", ".atlassian.net", ".service-now.com"}
	for host := range knownProviderHosts {
		for _, suffix := range tenantSuffixes {
			require.False(t, strings.HasSuffix(host, suffix), "%s is one customer's tenant host", host)
		}
	}
	require.Equal(t, LabelOther, ProviderLabel("acme.okta.com"))
}
