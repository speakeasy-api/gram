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

func mintStatusKey(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext, projectID uuid.NullUUID, scopes []string) {
	t.Helper()
	key := "gram_local_" + uuid.NewString()
	hash, err := auth.GetAPIKeyHash(key)
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAPIKey(t.Context(), keysrepo.CreateAPIKeyParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       projectID,
		CreatedByUserID: authCtx.UserID,
		Name:            "status-" + uuid.NewString(),
		KeyPrefix:       key[:16],
		KeyHash:         hash,
		Scopes:          scopes,
	})
	require.NoError(t, err)
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

func TestHooks_GetStatus_ProjectBoundHooksKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ownProject := uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}

	// A key without the hooks scope does not count.
	mintStatusKey(t, ti, authCtx, ownProject, []string{"producer"})
	status, err := ti.service.GetStatus(ctx, getStatusPayload())
	require.NoError(t, err)
	require.False(t, status.Configured)

	mintStatusKey(t, ti, authCtx, ownProject, []string{"hooks"})
	status, err = ti.service.GetStatus(ctx, getStatusPayload())
	require.NoError(t, err)
	require.True(t, status.Configured)
	require.True(t, status.AgentHooksKey)
	require.False(t, status.AnthropicInferenceHooks)
}

func TestHooks_GetStatus_OrganizationWideHooksKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// A key with no project binding feeds every project in the organization.
	mintStatusKey(t, ti, authCtx, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, []string{"hooks"})

	status, err := ti.service.GetStatus(ctx, getStatusPayload())
	require.NoError(t, err)
	require.True(t, status.Configured)
	require.True(t, status.AgentHooksKey)
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

func TestHooks_GetStatus_RequiresProjectRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	ctx = authztest.WithExactGrants(t, ctx,
		authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)},
	)

	_, err := ti.service.GetStatus(ctx, getStatusPayload())
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
