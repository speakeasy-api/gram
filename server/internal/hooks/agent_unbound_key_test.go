package hooks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// schemeAuthorizer answers each security scheme with its own context, the way
// Goa runs the key scheme and then the project scheme for one request.
type schemeAuthorizer map[string]context.Context

func (a schemeAuthorizer) Authorize(_ context.Context, _ string, schema *security.APIKeyScheme) (context.Context, error) {
	return a[schema.Name], nil
}

// unboundAgentKey returns an admitted agent credential as the key scheme
// leaves it for a key bound to no project, and as the project scheme leaves
// it once Gram-Project resolves.
func unboundAgentKey(t *testing.T, ctx context.Context, ti *testInstance) (keyCtx, projectCtx context.Context) {
	t.Helper()
	projectCtx = grantedAgentContext(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(projectCtx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	unbound := *authCtx
	unbound.ProjectID = nil
	unbound.ProjectSlug = nil
	return contextvalues.SetAuthContext(projectCtx, &unbound), projectCtx
}

// An agent key bound to no project names its project with Gram-Project. The
// key scheme runs first, before that project resolves, so the project-scoped
// hooks check must wait for the project scheme rather than refuse.
func TestAPIKeyAuth_UnboundAgentKeyResolvesItsProjectFirst(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"cursor", "codex", "logs", "metrics"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			keyCtx, projectCtx := unboundAgentKey(t, ctx, ti)
			keyCtx = context.WithValue(keyCtx, goa.MethodKey, method)
			projectCtx = context.WithValue(projectCtx, goa.MethodKey, method)
			ti.service.auth = schemeAuthorizer{
				constants.KeySecurityScheme:         keyCtx,
				constants.ProjectSlugSecuritySchema: projectCtx,
			}

			afterKey, err := ti.service.APIKeyAuth(keyCtx, "gram_local_test", &security.APIKeyScheme{
				Name: constants.KeySecurityScheme, Scopes: []string{}, RequiredScopes: []string{"hooks"},
			})
			require.NoError(t, err, "the key scheme must not decide the project-scoped check")

			_, err = ti.service.APIKeyAuth(afterKey, "project", &security.APIKeyScheme{
				Name: constants.ProjectSlugSecuritySchema, Scopes: []string{}, RequiredScopes: []string{"hooks"},
			})
			require.NoError(t, err, "the hooks check runs once the project is known")
		})
	}
}

// The same holds on the optional-auth plugin path (claude, ingest), which
// authorizes the key and the project slug by hand.
func TestAuthorizePluginRequest_UnboundAgentKeyResolvesItsProjectFirst(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	keyCtx, projectCtx := unboundAgentKey(t, ctx, ti)
	ti.service.auth = schemeAuthorizer{
		constants.KeySecurityScheme:         keyCtx,
		constants.ProjectSlugSecuritySchema: projectCtx,
	}

	_, err := ti.service.authorizePluginRequest(t.Context(), "gram_local_test", "project")
	require.NoError(t, err)
}
