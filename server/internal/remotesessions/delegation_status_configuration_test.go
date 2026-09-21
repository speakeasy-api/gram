package remotesessions_test

import (
	"testing"

	keysgen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/require"
)

func TestGetClientDelegationStatusSigningKeyLifecycle(t *testing.T) {
	t.Parallel()
	for _, transition := range []string{"retire", "revoke"} {
		t.Run(transition, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			org := activeOrganizationID(t, ctx)
			ti.enableCustomerManagedKeys(t, ctx, org)
			issuerID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "delegation-key-lifecycle")
			create := newCreateClientPayload(issuerID.String(), nil, nil)
			create.Scope = []string{"openid", "email"}
			client, err := ti.service.CreateClient(ctx, create)
			require.NoError(t, err)
			// Seed public key material without a live KMS; all registration and
			// lifecycle transitions below use supported service operations.
			setID := createJsonWebKeySet(t, ctx, ti.conn, org, "delegation-key-lifecycle")
			keyID := createJsonWebKey(t, ctx, ti.conn, org, setID, "active", "active-signer")
			attachJsonWebKeySet(t, ctx, ti, client.ID, setID)
			_, err = ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{ID: client.ID, TokenEndpointAuthMethod: conv.PtrEmpty("private_key_jwt")})
			require.NoError(t, err)
			payload := &orgclientsgen.GetClientDelegationStatusPayload{ID: client.ID}
			before, err := ti.service.GetClientDelegationStatus(ctx, payload)
			require.NoError(t, err)
			require.Equal(t, "unknown", before.Status)
			require.Empty(t, before.Observations)

			redisClient, err := infra.NewRedisClient(t, 0)
			require.NoError(t, err)
			logger := testenv.NewLogger(t)
			keys := jsonwebkeysets.NewService(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, ti.sessionManager,
				authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), audit.NewLogger(), nil, nil, ti.features, ratelimit.NewRedisStore(redisClient))
			if transition == "retire" {
				_, err = keys.RetireKey(ctx, &keysgen.RetireKeyPayload{ID: keyID.String()})
			} else {
				_, err = keys.RevokeKey(ctx, &keysgen.RevokeKeyPayload{ID: keyID.String()})
			}
			require.NoError(t, err)
			attached, err := ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{ID: client.ID})
			require.NoError(t, err)
			require.Equal(t, setID.String(), *attached.JSONWebKeySetID)
			after, err := ti.service.GetClientDelegationStatus(ctx, payload)
			require.NoError(t, err)
			require.Equal(t, "configuration_failure", after.Status)
			require.NotNil(t, after.Observations)
			require.Empty(t, after.Observations, "configuration failures must not synthesize human observations")
			require.NotEmpty(t, after.WindowStart)
		})
	}
}

func TestGetClientDelegationStatusMissingRequiredScopes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	issuerID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "delegation-missing-scopes")
	create := newCreateClientPayload(issuerID.String(), nil, nil)
	create.Scope = []string{"openid"}
	client, err := ti.service.CreateClient(ctx, create)
	require.NoError(t, err)
	got, err := ti.service.GetClientDelegationStatus(ctx, &orgclientsgen.GetClientDelegationStatusPayload{ID: client.ID})
	require.NoError(t, err)
	require.Equal(t, "configuration_failure", got.Status)
	require.NotNil(t, got.Observations)
	require.Empty(t, got.Observations)
}
