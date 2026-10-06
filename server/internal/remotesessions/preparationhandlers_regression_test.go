package remotesessions_test

import (
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/stretchr/testify/require"
)

func TestPrepareEMARejectsNilClientSelection(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	result, err := ti.service.PrepareEMA(ctx, &gen.PrepareEMAPayload{
		UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(),
		Resource: in.Resource, ClientID: conv.PtrEmpty(uuid.Nil.String()), Mechanism: "dcr",
	})
	require.ErrorContains(t, err, "invalid remote session client id")
	require.Nil(t, result)
}

func TestReadEMABootstrapGeneration(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	payload := &gen.ReadEMAPayload{
		UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: in.Resource,
	}
	initial, err := ti.service.ReadEMA(ctx, payload)
	require.NoError(t, err)
	require.Zero(t, initial.Generation)
	require.Nil(t, initial.ClientID)
	prepared, err := ti.service.PrepareEMA(ctx, &gen.PrepareEMAPayload{
		UserSessionIssuerID: payload.UserSessionIssuerID, RemoteSessionIssuerID: payload.RemoteSessionIssuerID,
		Resource: in.Resource, ClientID: conv.PtrEmpty(in.ClientID.String()), Mechanism: "manual", Scopes: in.Scopes,
		ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer}, ExpectedGeneration: initial.Generation,
	})
	require.NoError(t, err)
	require.NotNil(t, prepared.ClientID)
	current, err := ti.service.ReadEMA(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, prepared.Generation, current.Generation)
	require.Equal(t, prepared.ClientID, current.ClientID)
	unlinked, err := ti.service.UnlinkEMA(ctx, &gen.UnlinkEMAPayload{
		UserSessionIssuerID: payload.UserSessionIssuerID, RemoteSessionIssuerID: payload.RemoteSessionIssuerID,
		Resource: in.Resource, ExpectedGeneration: current.Generation,
	})
	require.NoError(t, err)
	discovered, err := ti.service.ReadEMA(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, unlinked.Generation, discovered.Generation)
	require.Greater(t, discovered.Generation, current.Generation)
	require.Nil(t, discovered.ClientID)
}

func TestEMAHandlersRejectInvalidResources(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	for _, resource := range []string{"", "relative/path", "https://resource.example.com/#fragment", "https://resource.example.com/?query=value"} {
		t.Run(resource, func(t *testing.T) {
			t.Parallel()
			_, err := ti.service.PrepareEMA(ctx, &gen.PrepareEMAPayload{
				UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: resource,
				ClientID: conv.PtrEmpty(in.ClientID.String()), Mechanism: "manual",
			})
			require.ErrorContains(t, err, "invalid canonical resource")
			_, err = ti.service.ReadEMA(ctx, &gen.ReadEMAPayload{
				UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: resource,
			})
			require.ErrorContains(t, err, "invalid canonical resource")
			_, err = ti.service.UnlinkEMA(ctx, &gen.UnlinkEMAPayload{
				UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: resource,
			})
			require.ErrorContains(t, err, "invalid canonical resource")
		})
	}
}
