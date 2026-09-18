package remotesessions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// provisionManagedClient creates an organization-level issuer through the
// management API and runs the real identity provider connection provisioner
// against it, yielding a client that carries the managed-by marker.
func provisionManagedClient(t *testing.T, ctx context.Context, ti *testInstance, slug string) (string, *provisiontest.Fixture) {
	t.Helper()

	return provisionManagedClientForPayload(t, ctx, ti, newCreateIssuerPayload(slug, nil))
}

func provisionManagedClientForPayload(t *testing.T, ctx context.Context, ti *testInstance, payload *orgissuersgen.CreateIssuerPayload) (string, *provisiontest.Fixture) {
	t.Helper()

	issuer, err := ti.service.CreateIssuer(ctx, payload)
	require.NoError(t, err)

	fx := provisiontest.Provision(t, ctx, ti.conn, activeOrganizationID(t, ctx), uuid.MustParse(issuer.ID), testServerURL)

	return issuer.ID, fx
}

// Every organization-tier client mutation refuses a managed client, while the
// read paths keep showing it.
func TestManagedClient_RefusesOrganizationTierMutations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	organizationID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, organizationID)

	issuerID, fx := provisionManagedClient(t, ctx, ti, "managed-guard-issuer")
	clientID := fx.Client.ClientRowID.String()

	got, err := ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{ID: clientID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, clientID, got.ID)

	listed, err := ti.service.ListClients(ctx, &orgclientsgen.ListClientsPayload{IssuerID: issuerID, Cursor: nil, Limit: nil, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	require.Equal(t, clientID, listed.Items[0].Client.ID)

	method := "client_secret_basic"
	_, err = ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{ID: clientID, TokenEndpointAuthMethod: &method})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.RotateClient(ctx, &orgclientsgen.RotateClientPayload{ID: clientID})
	requireOopsCode(t, err, oops.CodeConflict)

	err = ti.service.DeleteClient(ctx, &orgclientsgen.DeleteClientPayload{ID: clientID})
	requireOopsCode(t, err, oops.CodeConflict)

	setID := createJsonWebKeySet(t, ctx, ti.conn, organizationID, "managed-guard-set")
	_, err = ti.service.AttachClientKeySet(ctx, &orgclientsgen.AttachClientKeySetPayload{ID: clientID, JSONWebKeySetID: setID.String()})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.DetachClientKeySet(ctx, &orgclientsgen.DetachClientKeySetPayload{ID: clientID})
	requireOopsCode(t, err, oops.CodeConflict)

	// Nothing moved: the client is still live, still private_key_jwt, still on
	// its managed set.
	after, err := ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{ID: clientID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.NotNil(t, after.TokenEndpointAuthMethod)
	require.Equal(t, "private_key_jwt", *after.TokenEndpointAuthMethod)
	require.NotNil(t, after.JSONWebKeySetID)
	require.Equal(t, fx.Client.JSONWebKeySetID.String(), *after.JSONWebKeySetID)
}

func TestManagedKeySet_RefusesOrdinaryClientAttachment(t *testing.T) {
	t.Parallel()

	for _, surface := range []string{"organization", "project"} {
		t.Run(surface, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			organizationID := activeOrganizationID(t, ctx)
			ti.enableCustomerManagedKeys(t, ctx, organizationID)
			_, fx := provisionManagedClient(t, ctx, ti, "managed-target-issuer")

			issuerID := createRemoteIssuer(t, ctx, ti, "ordinary-issuer", "")
			userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "ordinary-user-issuer")
			clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "ordinary-client")
			setID := createJsonWebKeySet(t, ctx, ti.conn, organizationID, "ordinary-set")
			_, err := ti.service.AttachClientKeySet(ctx, &orgclientsgen.AttachClientKeySetPayload{ID: clientID, JSONWebKeySetID: setID.String()})
			require.NoError(t, err)

			before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientAttachJsonWebKeySet)
			require.NoError(t, err)
			managedSetID := fx.Client.JSONWebKeySetID.String()
			if surface == "organization" {
				_, err = ti.service.AttachClientKeySet(ctx, &orgclientsgen.AttachClientKeySetPayload{ID: clientID, JSONWebKeySetID: managedSetID})
			} else {
				_, err = ti.service.AttachKeySet(ctx, &clientsgen.AttachKeySetPayload{ID: clientID, JSONWebKeySetID: managedSetID})
			}
			requireOopsCode(t, err, oops.CodeConflict)
			require.ErrorContains(t, err, "managed by an identity provider connection")

			got, err := ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{ID: clientID})
			require.NoError(t, err)
			require.NotNil(t, got.JSONWebKeySetID)
			require.Equal(t, setID.String(), *got.JSONWebKeySetID)
			after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientAttachJsonWebKeySet)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

// The issuer a managed client sits on is pinned by the connection, so editing
// or moving it is refused too.
func TestManagedClient_RefusesIssuerMutations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID, _ := provisionManagedClient(t, ctx, ti, "managed-guard-pinned-issuer")

	name := "renamed"
	_, err := ti.service.UpdateIssuer(ctx, &orgissuersgen.UpdateIssuerPayload{ID: issuerID, Name: &name})
	requireOopsCode(t, err, oops.CodeConflict)

	projectID := createProject(t, ctx, ti.conn, "managed-guard-project").String()
	_, err = ti.service.MoveIssuer(ctx, &orgissuersgen.MoveIssuerPayload{ID: issuerID, ProjectID: &projectID})
	requireOopsCode(t, err, oops.CodeConflict)

	target, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("managed-guard-migrate-target", nil))
	require.NoError(t, err)
	_, err = ti.service.MigrateIssuer(ctx, &orgissuersgen.MigrateIssuerPayload{SourceID: issuerID, TargetID: target.ID, SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)
	still, err := ti.service.GetIssuer(ctx, &orgissuersgen.GetIssuerPayload{ID: issuerID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, issuerID, still.ID, "the source issuer must not have been tombstoned")

	// An unrelated issuer in the same organization stays editable.
	other, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("managed-guard-unrelated", nil))
	require.NoError(t, err)
	_, err = ti.service.UpdateIssuer(ctx, &orgissuersgen.UpdateIssuerPayload{ID: other.ID, Name: &name})
	require.NoError(t, err)
}

// Refreshing discovered metadata rewrites the same endpoints UpdateIssuer is
// refused on, so it is refused for a managed issuer too.
func TestManagedClient_RefusesIssuerMetadataRefresh(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, nil)

	payload := newCreateIssuerPayload("managed-guard-refresh-issuer", nil)
	payload.Issuer = upstream.URL
	stale := "https://stale.example.com/authorize"
	payload.AuthorizationEndpoint = &stale
	issuerID, _ := provisionManagedClientForPayload(t, ctx, ti, payload)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)

	_, err = ti.service.RefreshIssuerMetadata(ctx, &orgissuersgen.RefreshIssuerMetadataPayload{ID: issuerID, SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, "managed by an identity provider connection")

	still, err := ti.service.GetIssuer(ctx, &orgissuersgen.GetIssuerPayload{ID: issuerID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.NotNil(t, still.AuthorizationEndpoint)
	require.Equal(t, stale, *still.AuthorizationEndpoint, "refresh must not overwrite a managed issuer's endpoints")
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, before, after)

	// An unrelated issuer in the same organization still refreshes.
	other := newCreateIssuerPayload("managed-guard-refresh-unrelated", nil)
	other.Issuer = upstream.URL
	other.AuthorizationEndpoint = &stale
	created, err := ti.service.CreateIssuer(ctx, other)
	require.NoError(t, err)
	refreshed, err := ti.service.RefreshIssuerMetadata(ctx, &orgissuersgen.RefreshIssuerMetadataPayload{ID: created.ID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, upstream.URL+"/authorize", *refreshed.Issuer.AuthorizationEndpoint)
}

// A managed client's JWKS document is served with the short freshness window,
// and revoking the connection flips it to an empty set at the same URL rather
// than a 404.
func TestHandleClientJSONWebKeySet_ManagedClientShortCacheAndEmptyAfterRevoke(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, fx := provisionManagedClient(t, ctx, ti, "managed-guard-jwks-issuer")
	clientID := fx.Client.ClientRowID.String()

	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)

	rec := httptest.NewRecorder()
	require.NoError(t, mgr.HandleClientJSONWebKeySet(rec, clientJSONWebKeySetRequest(t, clientID, false)))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "public, max-age=300", rec.Header().Get("Cache-Control"))

	var document struct {
		Keys []map[string]any `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &document))
	require.Len(t, document.Keys, 1)
	require.Equal(t, fx.Client.ActiveKid, document.Keys[0]["kid"])

	revoked, err := fx.Provisioner.RevokeClient(ctx, activeOrganizationID(t, ctx), fx.ConnectionID)
	require.NoError(t, err)
	require.Equal(t, 1, revoked)

	after := httptest.NewRecorder()
	require.NoError(t, mgr.HandleClientJSONWebKeySet(after, clientJSONWebKeySetRequest(t, clientID, false)))
	require.Equal(t, http.StatusOK, after.Code)
	require.Equal(t, "public, max-age=300", after.Header().Get("Cache-Control"))
	require.JSONEq(t, `{"keys":[]}`, after.Body.String())
	require.NotEqual(t, rec.Header().Get("ETag"), after.Header().Get("ETag"), "a cached copy must not validate against the emptied set")
}

// The registration swap refuses a managed row even when the compare-and-swap
// values match, so no rotation path can replace its credentials.
func TestManagedClient_RegistrationReplaceRefusesManagedRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, fx := provisionManagedClient(t, ctx, ti, "managed-guard-replace-issuer")

	before, err := repo.New(ti.conn).GetRemoteSessionClientForRotation(ctx, fx.Client.ClientRowID)
	require.NoError(t, err)
	currentClientID := before.RemoteSessionClient.ClientID
	updatedAt := before.RemoteSessionClient.UpdatedAt

	_, err = repo.New(ti.conn).ReplaceRemoteSessionClientRegistration(ctx, repo.ReplaceRemoteSessionClientRegistrationParams{
		ClientID:                "rotated-" + uuid.NewString(),
		ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
		ClientIDIssuedAt:        pgtype.Timestamptz{Time: updatedAt.Time, InfinityModifier: pgtype.Finite, Valid: true},
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: updatedAt.Time, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: pgtype.Text{String: "client_secret_basic", Valid: true},
		ID:                      fx.Client.ClientRowID,
		ExpectedClientID:        currentClientID,
		ExpectedUpdatedAt:       updatedAt,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "managed row must not be replaceable")

	after, err := repo.New(ti.conn).GetRemoteSessionClientForRotation(ctx, fx.Client.ClientRowID)
	require.NoError(t, err)
	require.Equal(t, currentClientID, after.RemoteSessionClient.ClientID)
}
