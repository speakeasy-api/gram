package remotesessions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// provisionManagedClient creates an organization-level issuer through the
// management API and runs the real identity provider connection provisioner
// against it, yielding a client that carries the managed-by marker.
func provisionManagedClient(t *testing.T, ctx context.Context, ti *testInstance, slug string) (string, *provisiontest.Fixture) {
	t.Helper()

	issuer, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload(slug, nil))
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
