package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func createAgentGatewayIssuer(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()

	_, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:     conv.ToPGText(ti.orgID),
		Slug:               "agent-gateway-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
}

func TestGetPlugins_IncludesAvailableOrganizationGatewayURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	before, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	require.Nil(t, before.OrganizationGatewayURL)

	createAgentGatewayIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})

	require.NoError(t, err)
	require.NotEqual(t, before.Etag, res.Etag)
	require.NotNil(t, res.OrganizationGatewayURL)
	require.Equal(t, testServerURL+"/mcp/gateway/"+authCtx.OrganizationSlug, *res.OrganizationGatewayURL)
}

func TestGetPlugins_OmitsUnavailableOrganizationGatewayURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})

	require.NoError(t, err)
	require.Nil(t, res.OrganizationGatewayURL)
}
