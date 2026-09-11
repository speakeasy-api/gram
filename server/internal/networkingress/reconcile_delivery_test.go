package networkingress_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/networkingress"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	networkingressv1 "github.com/speakeasy-api/gram/infra/gen/gram/networkingress/v1"
	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
)

type failingReconcileRequester struct{}

func (failingReconcileRequester) Enqueue(context.Context, pgx.Tx, string, uuid.UUID) error {
	return fmt.Errorf("delivery unavailable")
}

func TestNetworkIngressCreateRollsBackWhenEnqueueFails(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestServiceWithRequester(t, true, failingReconcileRequester{})
	_, err := ti.service.CreateIngress(ctx, &gen.CreateIngressPayload{Provider: networkingress.ProviderTailscale, Hostname: "private", OauthClientID: "client", OauthClientSecret: "secret"})
	require.Error(t, err)
	_, err = repo.New(ti.conn).GetNetworkIngressByOrganization(ctx, ti.orgID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestNetworkIngressLifecycleEnqueuesRedactedRequests(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := ti.create(t, ctx)
	_, err := ti.service.UpdateIngress(ctx, &gen.UpdateIngressPayload{Hostname: new("changed-ingress")})
	require.NoError(t, err)
	secret := "sentinel-do-not-persist-plaintext"
	_, err = ti.service.RotateCredentials(ctx, &gen.RotateCredentialsPayload{OauthClientID: "client", OauthClientSecret: secret})
	require.NoError(t, err)
	require.NoError(t, ti.service.DeleteIngress(ctx, &gen.DeleteIngressPayload{}))
	require.NoError(t, ti.service.DeleteIngress(ctx, &gen.DeleteIngressPayload{}))
	requests, err := repo.New(ti.conn).ListNetworkIngressReconcileRequests(ctx, ti.orgID)
	require.NoError(t, err)
	require.Len(t, requests, 5)
	for _, payload := range requests {
		var request networkingressv1.ReconcileRequested
		require.NoError(t, proto.Unmarshal(payload, &request))
		require.Equal(t, created.ID, request.GetIngressId())
		require.Equal(t, ti.orgID, request.GetOrganizationId())
		require.Equal(t, "test-network-ingress", request.GetTemporalTaskQueue())
		require.NotContains(t, string(payload), secret)
		require.NotContains(t, string(payload), "changed-ingress")
		_, err := uuid.Parse(request.GetIngressId())
		require.NoError(t, err)
	}
}
