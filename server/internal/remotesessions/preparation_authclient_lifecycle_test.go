package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestPrepareEMA_TypedOwnershipLifecycle(t *testing.T) {
	t.Parallel()
	for _, transition := range []string{"unlink", "replace", "readiness_loss"} {
		t.Run(transition, func(t *testing.T) {
			t.Parallel()
			ctx, ti, in := preparationFixture(t)
			auth, _ := contextvalues.GetAuthContext(ctx)
			server := seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "lifecycle-ema", in.Resource)
			var source uuid.UUID
			fixture1, fixtureErr := repo.New(ti.conn).GetPreparationServerSourceFixture(ctx, server)
			require.NoError(t, fixtureErr)
			source = fixture1.UUID
			secret, err := testenv.NewEncryptionClient(t).Encrypt([]byte("lifecycle-header-secret"))
			require.NoError(t, err)
			// Seed only the source configuration. Typed state must come from normal preparation.
			err = repo.New(ti.conn).InsertPreparationSourceHeaderFixtureEnterprise(ctx, repo.InsertPreparationSourceHeaderFixtureEnterpriseParams{SourceID: source, EncryptedValue: conv.ToPGText(secret), ProjectID: *auth.ProjectID})
			require.NoError(t, err)
			payload := &clientsgen.PrepareEMAPayload{
				UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(),
				ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: in.Resource, Scopes: in.Scopes, Mechanism: "manual",
				ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer},
			}
			first, err := ti.service.PrepareEMA(ctx, payload)
			require.NoError(t, err)
			require.Equal(t, "ready", first.State)
			old := assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource)
			q := authclientbackfill.New(ti.conn)
			copies, err := q.ListAuthClientHeadersFixture(ctx, old)
			require.NoError(t, err)
			require.Len(t, copies, 1)
			governor := identitychaining.NewGovernor(testenv.NewLogger(t), ti.conn, testenv.NewEncryptionClient(t), nil)
			request := identitychaining.Request{OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, UserSessionIssuerID: in.UserSessionIssuerID, UpstreamResource: in.Resource, RemoteSessionClientID: conv.ToNullUUID(in.ClientID)}
			require.True(t, governor.Governs(ctx, request))
			payload.ExpectedGeneration = first.Generation
			var replacement uuid.UUID
			switch transition {
			case "unlink":
				result, err := ti.service.UnlinkEMA(ctx, &clientsgen.UnlinkEMAPayload{UserSessionIssuerID: payload.UserSessionIssuerID, RemoteSessionIssuerID: payload.RemoteSessionIssuerID, Resource: payload.Resource, ExpectedGeneration: first.Generation})
				require.NoError(t, err)
				require.Equal(t, "unlinked", result.State)
			case "replace":
				replacement = preparationManualClient(t, ctx, ti, *auth.ProjectID, in.RemoteSessionIssuerID, "replacement-lifecycle")
				payload.ClientID = conv.PtrEmpty(replacement.String())
				result, err := ti.service.PrepareEMA(ctx, payload)
				require.NoError(t, err)
				require.Equal(t, "ready", result.State)
			case "readiness_loss":
				payload.ConfirmGrants = []string{oauthwire.GrantTypeAuthorizationCode}
				result, err := ti.service.PrepareEMA(ctx, payload)
				require.NoError(t, err)
				require.Equal(t, "manual_setup_required", result.State)
			}
			var deleted bool
			fixture2, fixtureErr := repo.New(ti.conn).GetPreparationTypedClientDeletedFixture(ctx, old)
			require.NoError(t, fixtureErr)
			deleted = fixture2
			require.True(t, deleted, "old typed EMA ownership survived a normal binding transition")
			links, err := q.ListBackfillLinkedServersFixture(ctx, old)
			require.NoError(t, err)
			require.Empty(t, links)
			copies, err = q.ListAuthClientHeadersFixture(ctx, old)
			require.NoError(t, err)
			require.Empty(t, copies)
			require.False(t, governor.Governs(ctx, request), "runtime still governs the old selected registration")
			if replacement != uuid.Nil {
				id := assertPreparationTypedClient(t, ctx, ti, replacement, in.Resource)
				copies, err = q.ListAuthClientHeadersFixture(ctx, id)
				require.NoError(t, err)
				require.Len(t, copies, 1)
				request.RemoteSessionClientID = conv.ToNullUUID(replacement)
				require.True(t, governor.Governs(ctx, request))
			} else {
				standalone := authclientbackfill.SourceClientID(auth.ActiveOrganizationID, authclientbackfill.SourceKindRemote, source, authclientbackfill.TypeStaticCredential)
				links, err = q.ListBackfillLinkedServersFixture(ctx, standalone)
				require.NoError(t, err)
				require.Equal(t, []uuid.UUID{server}, links, "normal ownership removal must restore the eligible standalone option")
			}
		})
	}
}

func TestPrepareEMA_ResourceOwnershipTransitionKeepsSibling(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	firstResource, secondResource := in.Resource, "https://resource.example.com/second"
	firstServer := seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "first-resource", firstResource)
	secondServer := seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "second-resource", secondResource)
	payload := &clientsgen.PrepareEMAPayload{UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: firstResource, Mechanism: "manual", Scopes: in.Scopes, ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer}}
	first, err := ti.service.PrepareEMA(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "ready", first.State)
	firstID := assertPreparationTypedClient(t, ctx, ti, in.ClientID, firstResource)
	// Resources are immutable binding keys: moving configuration means prepare
	// the replacement resource, then explicitly unlink the old binding.
	payload.Resource = secondResource
	second, err := ti.service.PrepareEMA(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "ready", second.State)
	secondID := assertPreparationTypedClient(t, ctx, ti, in.ClientID, secondResource)
	require.NotEqual(t, firstID, secondID)
	_, err = ti.service.UnlinkEMA(ctx, &clientsgen.UnlinkEMAPayload{UserSessionIssuerID: payload.UserSessionIssuerID, RemoteSessionIssuerID: payload.RemoteSessionIssuerID, Resource: firstResource, ExpectedGeneration: first.Generation})
	require.NoError(t, err)
	q := authclientbackfill.New(ti.conn)
	oldLinks, err := q.ListBackfillLinkedServersFixture(ctx, firstID)
	require.NoError(t, err)
	require.Empty(t, oldLinks)
	newLinks, err := q.ListBackfillLinkedServersFixture(ctx, secondID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{secondServer}, newLinks)
	require.NotContains(t, newLinks, firstServer)
	governor := identitychaining.NewGovernor(testenv.NewLogger(t), ti.conn, testenv.NewEncryptionClient(t), nil)
	request := identitychaining.Request{OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, UserSessionIssuerID: in.UserSessionIssuerID, UpstreamResource: firstResource, RemoteSessionClientID: conv.ToNullUUID(in.ClientID)}
	require.False(t, governor.Governs(ctx, request))
	request.UpstreamResource = secondResource
	require.True(t, governor.Governs(ctx, request))
	require.Equal(t, secondID, assertPreparationTypedClient(t, ctx, ti, in.ClientID, secondResource))
}
