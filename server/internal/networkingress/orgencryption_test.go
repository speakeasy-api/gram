package networkingress_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestOrgEncryptedIngressCredentialsAreAppliedFromTheOrganizationCopy(t *testing.T) {
	t.Parallel()
	ctx, ti := newOrgEncryptedTestService(t)
	id := uuid.MustParse(ti.create(t, ctx).ID)

	row := loadRow(t, ctx, ti)
	require.True(t, row.CredentialsEncrypted.Valid, "the application-key copy is kept for rollback")
	require.True(t, row.CredentialsOrgEncrypted.Valid)
	require.NotContains(t, row.CredentialsOrgEncrypted.String, "client-secret")

	// With the application-key copy unreadable, Apply can only receive the
	// credentials through the organization copy.
	_, err := repo.New(ti.conn).RotateNetworkIngressCredentials(ctx, repo.RotateNetworkIngressCredentialsParams{
		CredentialsEncrypted: conv.ToPGText("not-a-ciphertext"), CredentialsOrgEncrypted: row.CredentialsOrgEncrypted, OrganizationID: ti.orgID,
	})
	require.NoError(t, err)

	var applied []byte
	provider := lifecycleProvider{
		apply: func(_ context.Context, desired k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
			applied = desired.Credentials
			return k8s.NetworkIngressObservation{Status: "online", DNSName: "private.example.ts.net", ErrorCode: "", ConnectedAt: nil}, nil
		},
		observe: nil,
		delete:  func(context.Context, k8s.NetworkIngressResourceNames) error { return nil },
	}
	registry, err := k8s.NewNetworkIngressProvisionerRegistry(map[string]k8s.NetworkIngressProvisioner{"tailscale": provider}, testenv.NewLogger(t), nil)
	require.NoError(t, err)
	executor := networkingress.NewExecutor(ti.conn, ti.credentials, registry, networkingress.ExecutorOptions{Queue: "test", Image: "image", BackendService: "backend", BackendPort: 443, CanApply: func(context.Context) error { return nil }})

	_, err = executor.Reconcile(ctx, ti.orgID, id)
	require.NoError(t, err)
	require.JSONEq(t, `{"client_id":"client-id","client_secret":"client-secret"}`, string(applied))
	require.Equal(t, "online", loadRow(t, ctx, ti).Status)

	require.NoError(t, ti.service.DeleteIngress(ctx, &gen.DeleteIngressPayload{}))
	_, err = executor.Reconcile(ctx, ti.orgID, id)
	require.NoError(t, err)
	cleaned, err := repo.New(ti.conn).GetNetworkIngressByID(ctx, repo.GetNetworkIngressByIDParams{ID: id, OrganizationID: ti.orgID})
	require.NoError(t, err)
	require.False(t, cleaned.CredentialsEncrypted.Valid)
	require.False(t, cleaned.CredentialsOrgEncrypted.Valid, "cleanup clears both copies")
}

func TestOrgEncryptedIngressRetriesWhileTheOrganizationKeyIsUnavailable(t *testing.T) {
	t.Parallel()
	ctx, ti := newOrgEncryptedTestService(t)
	id := uuid.MustParse(ti.create(t, ctx).ID)
	require.NoError(t, ti.orgKeys.Provisioner.Suspend(ctx, ti.orgID, "customer disabled the key", orgencryption.SystemActor()))

	provider := lifecycleProvider{
		apply: func(context.Context, k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
			t.Fatal("Apply must not run without credentials")
			return k8s.NetworkIngressObservation{}, nil
		},
		observe: nil,
		delete:  nil,
	}
	registry, err := k8s.NewNetworkIngressProvisionerRegistry(map[string]k8s.NetworkIngressProvisioner{"tailscale": provider}, testenv.NewLogger(t), nil)
	require.NoError(t, err)
	executor := networkingress.NewExecutor(ti.conn, ti.credentials, registry, networkingress.ExecutorOptions{Queue: "test", Image: "image", BackendService: "backend", BackendPort: 443, CanApply: func(context.Context) error { return nil }})

	_, err = executor.Reconcile(ctx, ti.orgID, id)
	var reconcileErr *networkingress.ReconcileError
	require.ErrorAs(t, err, &reconcileErr)
	require.Equal(t, "credentials_unavailable", reconcileErr.Code)
	require.True(t, reconcileErr.Retryable)
	require.NotEqual(t, "error", loadRow(t, ctx, ti).Status, "an unavailable key is not recorded as invalid credentials")

	_, err = ti.service.RotateCredentials(ctx, &gen.RotateCredentialsPayload{OauthClientID: "next-client", OauthClientSecret: "next-secret"})
	requireOopsCode(t, err, oops.CodeUnexpected)
}
