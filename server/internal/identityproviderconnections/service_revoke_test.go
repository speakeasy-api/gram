package identityproviderconnections_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/conv"
	extkeysrepo "github.com/speakeasy-api/gram/server/internal/externalkeys/repo"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	resourceconnectionsrepo "github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
)

// managedLeftovers reports whether each of the connection's managed rows is
// visible in the organization-tier lists.
type managedLeftovers struct {
	issuer, client, set, key bool
}

func listManagedLeftovers(t *testing.T, ctx context.Context, si *serviceInstance, connectionID uuid.UUID) managedLeftovers {
	t.Helper()

	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, connectionID)
	require.NoError(t, err)

	issuers, err := remotesessionsrepo.New(si.conn.conn).ListOrganizationRemoteSessionIssuers(ctx, remotesessionsrepo.ListOrganizationRemoteSessionIssuersParams{
		OrganizationID: conv.ToPGText(si.orgID),
		IncludeGlobal:  false,
		Cursor:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		LimitValue:     100,
	})
	require.NoError(t, err)
	clients, err := remotesessionsrepo.New(si.conn.conn).ListOrganizationRemoteSessionClientsByIssuerID(ctx, remotesessionsrepo.ListOrganizationRemoteSessionClientsByIssuerIDParams{
		RemoteSessionIssuerID: managed.IssuerID,
		OrganizationID:        conv.ToPGText(si.orgID),
		Cursor:                uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		LimitValue:            100,
	})
	require.NoError(t, err)
	sets, err := jwksrepo.New(si.conn.conn).ListJsonWebKeySets(ctx, si.orgID)
	require.NoError(t, err)
	keys, err := extkeysrepo.New(si.conn.conn).ListExternalKeys(ctx, extkeysrepo.ListExternalKeysParams{
		OrganizationID: conv.ToPGText(si.orgID),
		Provider:       conv.ToPGText("gcp_kms"),
	})
	require.NoError(t, err)

	var out managedLeftovers
	for _, row := range issuers {
		out.issuer = out.issuer || row.RemoteSessionIssuer.ID == managed.IssuerID
	}
	for _, row := range clients {
		out.client = out.client || row.RemoteSessionClient.ID == managed.ClientRowID
	}
	for _, row := range sets {
		out.set = out.set || row.ID == managed.JSONWebKeySetID
	}
	for _, row := range keys {
		out.key = out.key || row.ID == managed.ExternalKeyID
	}
	return out
}

// The rows a revoked connection leaves live for its JWKS URL drop out of the
// organization-tier lists, while the URL itself keeps serving an empty set.
func TestRevoke_HidesManagedLeftoversFromOrganizationLists(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	connectionID := mustParseUUID(t, created.ID)
	require.Equal(t, managedLeftovers{issuer: true, client: true, set: true, key: true}, listManagedLeftovers(t, ctx, si, connectionID))

	_, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, managedLeftovers{issuer: false, client: false, set: false, key: false}, listManagedLeftovers(t, ctx, si, connectionID))

	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, connectionID)
	require.NoError(t, err)
	require.JSONEq(t, `{"keys":[]}`, managedJWKS(t, ctx, si, managed))

	_, err = si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, managedLeftovers{issuer: false, client: false, set: false, key: false}, listManagedLeftovers(t, ctx, si, connectionID))
}

// A resource connection that references a snapshot app must not block the
// snapshot delete: the foreign key once nulled the tenant columns and failed.
func TestRevoke_WithResourceConnectionReferencingSnapshotApp(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)

	setFixtures(si, []okta.App{fixtureApp(appA, "Linear", "oidc_client")}, nil, nil)
	runSync(t, ctx, newSyncer(t, si), id)
	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, id)
	require.NoError(t, err)
	q := resourceconnectionsrepo.New(si.conn.conn)
	_, err = q.UpsertResourceConnection(ctx, resourceconnectionsrepo.UpsertResourceConnectionParams{
		OrganizationID: si.orgID, IdentityProviderConnectionID: id, RemoteSessionIssuerID: managed.IssuerID,
		Resource: "https://resource.example.com/mcp", Audience: "https://audience.example.com",
		OktaApplicationID: pgtype.Text{String: appA, Valid: true},
	})
	require.NoError(t, err)

	_, err = si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: verified.ID})
	require.NoError(t, err)

	rows, err := q.ListResourceConnections(ctx, resourceconnectionsrepo.ListResourceConnectionsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id})
	require.NoError(t, err)
	require.Empty(t, rows)
	require.Zero(t, countRows(t, ctx, si, "okta_applications", id))
}
