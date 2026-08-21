package litellm

import (
	"testing"

	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

func TestAPIKeyAuthAllowsCookieSession(t *testing.T) {
	t.Parallel()
	service := unitService(t, nil, testAuthContext())

	ctx, err := service.APIKeyAuth(t.Context(), "", &security.APIKeyScheme{
		Name:           constants.SessionSecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{},
	})

	require.NoError(t, err)
	require.NotEqual(t, t.Context(), ctx)
}
