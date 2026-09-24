package remotesessions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func preparationFixture(t *testing.T) (context.Context, *testInstance, remotesessions.PreparationInput) {
	t.Helper()
	ctx, ti := newTestService(t)
	issuer := uuid.MustParse(createRemoteIssuer(t, ctx, ti, "preparation", ""))
	user := createUserSessionIssuer(t, ctx, ti.conn, "preparation-human")
	preparationAdvertise(t, ctx, ti, issuer)
	auth, _ := contextvalues.GetAuthContext(ctx)
	client := preparationManualClient(t, ctx, ti, *auth.ProjectID, issuer, "downstream-client")
	return ctx, ti, remotesessions.PreparationInput{UserSessionIssuerID: user, RemoteSessionIssuerID: issuer, ClientID: client, Resource: "https://resource.example.com/", Mechanism: "manual", Scopes: []string{"openid"}}
}

func preparationAdvertise(t *testing.T, ctx context.Context, ti *testInstance, issuer uuid.UUID) {
	t.Helper()
	auth, _ := contextvalues.GetAuthContext(ctx)
	err := testrepo.New(ti.conn).SetPreparationFixtureIssuerCapability(ctx, testrepo.SetPreparationFixtureIssuerCapabilityParams{ID: issuer, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
	require.NoError(t, err)
}

func preparationRecordGrants(t *testing.T, ctx context.Context, ti *testInstance, client uuid.UUID, grants []string) {
	t.Helper()
	auth, _ := contextvalues.GetAuthContext(ctx)
	err := testrepo.New(ti.conn).SetPreparationFixtureClientGrants(ctx, testrepo.SetPreparationFixtureClientGrantsParams{ID: client, ProjectID: conv.ToNullUUID(*auth.ProjectID), GrantTypes: grants})
	require.NoError(t, err)
}

func TestPreparationIntegration_ManualGrantEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		grants []string
		state  string
	}{
		{"unknown", nil, "unknown_grants"},
		{"explicitly empty", []string{}, "manual_setup_required"},
		{"interactive only", []string{"authorization_code", "refresh_token"}, "manual_setup_required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti, in := preparationFixture(t)
			preparationRecordGrants(t, ctx, ti, in.ClientID, tc.grants)
			result, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, tc.state, result.State)
			require.NotEmpty(t, result.Stage)
			require.NotEmpty(t, result.Remediation)
			auth, _ := contextvalues.GetAuthContext(ctx)
			recorded, err := testrepo.New(ti.conn).GetPreparationFixtureClientGrants(ctx, testrepo.GetPreparationFixtureClientGrantsParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
			require.NoError(t, err)
			require.Equal(t, tc.grants, recorded, "preparation must not infer grants")
			in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer, oauthwire.GrantTypeJWTBearer}
			in.ExpectedGeneration = result.Generation
			confirmed, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, in.ClientID, confirmed.ClientID)
			require.Equal(t, "downstream-client", confirmed.ExternalClientID)
			require.Equal(t, []string{oauthwire.GrantTypeJWTBearer}, confirmed.GrantTypes)
			require.Equal(t, "administrator_declared", confirmed.GrantSource)
			require.Equal(t, "ready", confirmed.State)
			repeated, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, confirmed, repeated, "identical confirmation must replay its durable result without a new generation")
		})
	}
}

func TestPreparationIntegration_TenantAndIssuerSubstitution(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"user issuer", "remote issuer", "client", "wrong issuer client"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			ctx, ti, in := preparationFixture(t)
			other := createProject(t, ctx, ti.conn, "preparation-other")
			otherIssuer := createRemoteIssuerInProject(t, ctx, ti.conn, other, "other-issuer")
			switch field {
			case "user issuer":
				in.UserSessionIssuerID = createUserSessionIssuerInProject(t, ctx, ti.conn, other, "other-human")
			case "remote issuer":
				in.RemoteSessionIssuerID = otherIssuer
			case "client":
				in.ClientID = seedProjectRemoteClientNoOrg(t, ctx, ti.conn, other, otherIssuer, "other-client")
			case "wrong issuer client":
				auth, _ := contextvalues.GetAuthContext(ctx)
				issuer := uuid.MustParse(createRemoteIssuer(t, ctx, ti, "wrong-issuer", ""))
				in.ClientID = seedProjectRemoteClientNoOrg(t, ctx, ti.conn, *auth.ProjectID, issuer, "wrong-client")
			}
			in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
			_, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.Error(t, err, "substituted identifiers must not authorize preparation")
			auth, _ := contextvalues.GetAuthContext(ctx)
			count, err := testrepo.New(ti.conn).CountPreparationFixtureBindings(ctx, *auth.ProjectID)
			require.NoError(t, err)
			require.Zero(t, count, "rejected selection must not create a binding")
		})
	}
}

func TestPreparationIntegration_ExplicitSelectionPreservesInteractiveClient(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	interactive := uuid.MustParse(createRemoteClient(t, ctx, ti, in.RemoteSessionIssuerID.String(), in.UserSessionIssuerID.String(), "interactive-client"))
	preparationRecordGrants(t, ctx, ti, interactive, []string{oauthwire.GrantTypeJWTBearer, "authorization_code", "refresh_token"})
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{oauthwire.GrantTypeJWTBearer})
	selected := in.ClientID
	in.ClientID = uuid.Nil
	missing, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "configuration_required", missing.State)
	require.Equal(t, uuid.Nil, missing.ClientID, "grant membership must not select a registration")
	in.ClientID = selected
	result, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", result.State)
	require.Equal(t, selected, result.ClientID)
	require.Equal(t, "downstream-client", result.ExternalClientID)
	require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, interactive, in.UserSessionIssuerID))
	require.Equal(t, 0, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, selected, in.UserSessionIssuerID))
}

func TestPreparationIntegration_ConcurrentIdempotencyAndGeneration(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{oauthwire.GrantTypeJWTBearer})
	type outcome struct {
		result *remotesessions.PreparationResult
		err    error
	}
	const callers = 6
	start := make(chan struct{})
	done := make(chan outcome, callers)
	for range callers {
		go func() {
			<-start
			result, err := ti.service.PrepareIdentityChaining(ctx, in)
			done <- outcome{result, err}
		}()
	}
	close(start)
	var first *remotesessions.PreparationResult
	for range callers {
		got := <-done
		require.NoError(t, got.err)
		require.Equal(t, "ready", got.result.State)
		require.Equal(t, in.ClientID, got.result.ClientID)
		if first == nil {
			first = got.result
		}
		require.Equal(t, first.BindingID, got.result.BindingID)
		require.Equal(t, first.Generation, got.result.Generation)
	}
	require.NotEqual(t, uuid.Nil, first.BindingID)
	in.ExpectedGeneration = first.Generation
	unlinked, err := ti.service.UnlinkIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "unlinked", unlinked.State)
	require.Greater(t, unlinked.Generation, first.Generation)
	stale, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "configuration_required", stale.State, "stale generation must not revive an unlinked binding")
	require.Equal(t, unlinked.Generation, stale.Generation)
	auth, _ := contextvalues.GetAuthContext(ctx)
	in.ClientID = preparationManualClient(t, ctx, ti, *auth.ProjectID, in.RemoteSessionIssuerID, "replacement-client")
	in.ExpectedGeneration = unlinked.Generation
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	rebound, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", rebound.State)
	require.Equal(t, first.BindingID, rebound.BindingID)
	require.Equal(t, in.ClientID, rebound.ClientID)
	require.Greater(t, rebound.Generation, unlinked.Generation)
	current, err := ti.service.ReadIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, rebound.Generation, current.Generation)
	require.Equal(t, rebound.ClientID, current.ClientID)
}

func TestPreparationIntegration_CIMDExactGrantsStableIdentity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	issuer := createCIMDIssuer(t, ctx, ti, "preparation-cimd", "https://idp.example.com/authorize", "https://idp.example.com/token")
	preparationAdvertise(t, ctx, ti, issuer)
	user := createUserSessionIssuer(t, ctx, ti.conn, "cimd-human")
	client := createCimdClient(t, ctx, ti, issuer.String(), user.String(), []string{"openid"})
	otherUser := createUserSessionIssuer(t, ctx, ti.conn, "cimd-unselected-human")
	other := createCimdClient(t, ctx, ti, issuer.String(), otherUser.String(), []string{"openid"})
	grants := []string{"authorization_code", "refresh_token", oauthwire.GrantTypeJWTBearer}
	in := remotesessions.PreparationInput{UserSessionIssuerID: user, RemoteSessionIssuerID: issuer, ClientID: uuid.MustParse(client.ID), Resource: "https://resource.example.com/", Mechanism: "cimd", ConfirmGrants: grants, Scopes: []string{"openid"}}
	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, in.ClientID, prepared.ClientID)
	require.Equal(t, client.ClientID, prepared.ExternalClientID, "publishing grants must not rotate client identity")
	require.ElementsMatch(t, grants, prepared.GrantTypes)
	require.Equal(t, "published_acceptance_unverified", prepared.State)
	require.Equal(t, "cimd_published", prepared.GrantSource)
	require.NotEmpty(t, prepared.Remediation, "provider caching must be exposed without claiming usable access")
	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)
	for _, tc := range []struct {
		id, external string
		grants       []string
	}{{client.ID, client.ClientID, grants}, {other.ID, other.ClientID, []string{"authorization_code", "refresh_token"}}} {
		rec := httptest.NewRecorder()
		require.NoError(t, mgr.HandleClientMetadataDocument(rec, cimdDocumentRequest(t, tc.id, false)))
		require.Equal(t, http.StatusOK, rec.Code)
		var doc struct {
			ClientID   string   `json:"client_id"`
			GrantTypes []string `json:"grant_types"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
		require.Equal(t, tc.external, doc.ClientID)
		require.ElementsMatch(t, tc.grants, doc.GrantTypes, "JWT-bearer must only be published for the selected registration")
	}
	current, err := ti.service.ReadIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, prepared.State, current.State, "publication alone cannot establish provider acceptance")
}

// Use actual encrypted credentials: a bare seeded client must not pass auth readiness.
func preparationManualClient(t *testing.T, ctx context.Context, ti *testInstance, project, issuer uuid.UUID, external string) uuid.UUID {
	t.Helper()
	client := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, project, issuer, external)
	encrypted, err := testenv.NewEncryptionClient(t).Encrypt([]byte("preparation-test-secret"))
	require.NoError(t, err)
	err = testrepo.New(ti.conn).SetPreparationFixtureClientSecret(ctx, testrepo.SetPreparationFixtureClientSecretParams{ID: client, ProjectID: conv.ToNullUUID(project), Secret: conv.ToPGText(encrypted)})
	require.NoError(t, err)
	return client
}

func TestPreparationIntegration_StaleSelectionNeverMixesClientIdentities(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{oauthwire.GrantTypeJWTBearer})
	first, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", first.State)
	auth, _ := contextvalues.GetAuthContext(ctx)
	replacement := preparationManualClient(t, ctx, ti, *auth.ProjectID, in.RemoteSessionIssuerID, "replacement-external")
	preparationRecordGrants(t, ctx, ti, replacement, []string{oauthwire.GrantTypeJWTBearer, "refresh_token"})
	in.ClientID = replacement
	in.ExpectedGeneration = first.Generation - 1
	stale, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "configuration_required", stale.State)
	require.Equal(t, first.Generation, stale.Generation)
	if stale.ClientID != uuid.Nil {
		require.Equal(t, first.ClientID, stale.ClientID)
		require.Equal(t, first.ExternalClientID, stale.ExternalClientID, "response identity must come from the same client row")
		require.Equal(t, first.GrantTypes, stale.GrantTypes)
	} else {
		require.Empty(t, stale.ExternalClientID)
		require.Empty(t, stale.GrantTypes)
	}
	in.ClientID = uuid.Nil
	current, err := ti.service.ReadIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, first.ClientID, current.ClientID)
	require.Equal(t, first.ExternalClientID, current.ExternalClientID)
	require.Equal(t, first.Generation, current.Generation)
}

func TestPreparationIntegration_ReadRevalidatesExpiredCredential(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{oauthwire.GrantTypeJWTBearer})
	auth, _ := contextvalues.GetAuthContext(ctx)
	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", prepared.State)
	// Expire the persisted credential only after readiness is established.
	require.NoError(t, testrepo.New(ti.conn).SetPreparationFixtureClientSecretExpiry(ctx, testrepo.SetPreparationFixtureClientSecretExpiryParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID), ExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Minute))}))
	current, err := ti.service.ReadIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "manual_setup_required", current.State, "a historical ready binding cannot make an expired credential ready")
	require.Equal(t, prepared.Generation, current.Generation)
}

func TestPreparationIntegration_ResourceAssociationAndTrailingSlash(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{oauthwire.GrantTypeJWTBearer})
	in.ResourceMetadata = &remotesessions.PreparationResourceMetadata{Resource: in.Resource, AuthorizationServers: []string{"https://unrelated.example.com"}}
	_, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.Error(t, err)
	in.ResourceMetadata.AuthorizationServers = []string{"https://idp.example.com"}
	in.ResourceMetadata.Resource = "https://resource.example.com"
	_, err = ti.service.PrepareIdentityChaining(ctx, in)
	require.Error(t, err, "resource metadata must match exactly, including trailing slash")
	in.ResourceMetadata.Resource = in.Resource
	slash, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", slash.State)
	in.Resource = "https://resource.example.com"
	in.ResourceMetadata.Resource = in.Resource
	bare, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", bare.State)
	require.NotEqual(t, slash.BindingID, bare.BindingID, "distinct resource identifiers must not share binding generations")
	require.NotEqual(t, slash.Resource, bare.Resource)
}
