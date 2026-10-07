package hooks

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	aiirepo "github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func getStatusPayload() *gen.GetStatusPayload {
	return &gen.GetStatusPayload{ApikeyToken: nil, SessionToken: nil, ProjectSlugInput: nil}
}

func TestHooks_GetStatus_Unconfigured(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	status, err := ti.service.GetStatus(ctx, getStatusPayload())
	require.NoError(t, err)
	require.False(t, status.Configured)
	require.False(t, status.AgentHooksKey)
	require.False(t, status.AnthropicInferenceHooks)
}

func TestHooks_GetStatus_HooksScopedKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mintKey := func(scopes []string) {
		key := "gram_local_" + uuid.NewString()
		hash, err := auth.GetAPIKeyHash(key)
		require.NoError(t, err)
		_, err = keysrepo.New(ti.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
			OrganizationID:  authCtx.ActiveOrganizationID,
			ProjectID:       uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
			CreatedByUserID: authCtx.UserID,
			Name:            "status-" + uuid.NewString(),
			KeyPrefix:       key[:16],
			KeyHash:         hash,
			Scopes:          scopes,
		})
		require.NoError(t, err)
	}

	// A key without the hooks scope does not count.
	mintKey([]string{"producer"})
	status, err := ti.service.GetStatus(ctx, getStatusPayload())
	require.NoError(t, err)
	require.False(t, status.Configured)

	mintKey([]string{"hooks"})
	status, err = ti.service.GetStatus(ctx, getStatusPayload())
	require.NoError(t, err)
	require.True(t, status.Configured)
	require.True(t, status.AgentHooksKey)
	require.False(t, status.AnthropicInferenceHooks)
}

func TestHooks_GetStatus_AnthropicInferenceHooks(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	_, err := aiirepo.New(ti.conn).InsertConfig(ctx, aiirepo.InsertConfigParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		Provider:               "anthropic_inference",
		ProjectID:              *authCtx.ProjectID,
		ExternalOrganizationID: pgtype.Text{String: "", Valid: false},
		ApiKeyEncrypted:        "encrypted",
		Enabled:                true,
		BillingMode:            pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	status, err := ti.service.GetStatus(ctx, getStatusPayload())
	require.NoError(t, err)
	require.True(t, status.Configured)
	require.False(t, status.AgentHooksKey)
	require.True(t, status.AnthropicInferenceHooks)
}

func TestHooks_GetStatus_RequiresOrgRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	projectID := authCtx.ProjectID.String()
	ctx = authztest.WithExactGrants(t, ctx,
		authz.Grant{Scope: authz.ScopeProjectRead, Selector: authz.NewSelector(authz.ScopeProjectRead, projectID)},
	)

	_, err := ti.service.GetStatus(ctx, getStatusPayload())
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
