package remotesessions

import (
	"context"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestPreparationClientConfiguration_PublicAuthRequiresAdvertisement(t *testing.T) {
	t.Parallel()
	client := repo.RemoteSessionClient{TokenEndpointAuthMethod: conv.ToPGText(oauthwire.AuthMethodNone)}
	for _, tc := range []struct {
		name    string
		methods []string
		valid   bool
	}{
		{name: "absent"},
		{name: "empty", methods: []string{}},
		{name: "confidential_only", methods: []string{oauthwire.AuthMethodClientSecretBasic}},
		{name: "explicit_public", methods: []string{oauthwire.AuthMethodNone}, valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			issuer := repo.RemoteSessionIssuer{TokenEndpointAuthMethodsSupported: tc.methods}
			for _, readOnly := range []bool{false, true} {
				require.Equal(t, tc.valid, preparationClientConfigurationValid(context.Background(), nil, client, issuer, "test-org", readOnly))
			}
		})
	}
}
