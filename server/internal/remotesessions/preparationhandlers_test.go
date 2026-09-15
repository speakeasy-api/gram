package remotesessions_test

import (
	"context"
	"testing"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/stretchr/testify/require"
)

func TestPreparationAPI_ExplicitSelectionReadAndUnlink(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	prepared, err := ti.service.PrepareEMA(ctx, &clientsgen.PrepareEMAPayload{
		UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(),
		ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: in.Resource, Scopes: in.Scopes, Mechanism: "manual",
		ConfirmGrants: []string{preparationJWTGrant},
	})
	require.NoError(t, err)
	require.Equal(t, "ready", prepared.State)
	require.Equal(t, in.ClientID.String(), *prepared.ClientID)
	require.Equal(t, "administrator_declared", prepared.GrantSource)
	read, err := ti.service.ReadEMA(ctx, &clientsgen.ReadEMAPayload{UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: in.Resource})
	require.NoError(t, err)
	require.Equal(t, prepared.Generation, read.Generation)
	require.Equal(t, prepared.ClientID, read.ClientID)
	err = ti.service.DeleteRemoteSessionClient(ctx, &clientsgen.DeleteRemoteSessionClientPayload{ID: in.ClientID.String()})
	require.ErrorContains(t, err, "explicitly unlink")
	unlinked, err := ti.service.UnlinkEMA(ctx, &clientsgen.UnlinkEMAPayload{UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: in.Resource, ExpectedGeneration: read.Generation})
	require.NoError(t, err)
	require.Equal(t, "unlinked", unlinked.State)
	require.Greater(t, unlinked.Generation, read.Generation)
	require.NoError(t, ti.service.DeleteRemoteSessionClient(ctx, &clientsgen.DeleteRemoteSessionClientPayload{ID: in.ClientID.String()}))
}

func TestPreparationAPI_RequiresAuthentication(t *testing.T) {
	t.Parallel()
	_, ti, in := preparationFixture(t)
	_, err := ti.service.PrepareEMA(context.Background(), &clientsgen.PrepareEMAPayload{UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: in.Resource})
	require.Error(t, err)
	_, err = ti.service.ReadEMA(context.Background(), &clientsgen.ReadEMAPayload{UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: in.Resource})
	require.Error(t, err)
	_, err = ti.service.UnlinkEMA(context.Background(), &clientsgen.UnlinkEMAPayload{UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: in.Resource})
	require.Error(t, err)
}

func TestPreparationAPI_RejectsStaleUnlink(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	in.ConfirmGrants = []string{preparationJWTGrant}
	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	_, err = ti.service.UnlinkEMA(ctx, &clientsgen.UnlinkEMAPayload{UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: in.Resource, ExpectedGeneration: prepared.Generation - 1})
	require.Error(t, err)
	_, err = ti.service.UpdateRemoteSessionClient(ctx, &clientsgen.UpdateRemoteSessionClientPayload{ID: in.ClientID.String(), Scope: []string{"changed"}})
	require.ErrorContains(t, err, "explicitly unlink")
}
