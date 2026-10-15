package remotesessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestListEMAServices_AggregatesServersAndAcceptanceAcrossTheOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org := auth.ActiveOrganizationID
	list := func() []*orgissuersgen.EMAService {
		out, err := ti.service.ListEMAServices(ctx, &orgissuersgen.ListEMAServicesPayload{SessionToken: nil})
		require.NoError(t, err)
		return out.Items
	}

	require.Empty(t, list(), "no binding and no saved order means no service")

	prepare := func(resource string) {
		_, err := ti.service.PrepareEMA(ctx, &clientsgen.PrepareEMAPayload{
			UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(),
			ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: resource, Scopes: in.Scopes, Mechanism: "manual",
			ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer},
		})
		require.NoError(t, err)
	}
	other := in
	other.Resource = "https://other.example.com/"
	prepare(in.Resource)
	prepare(other.Resource)

	items := list()
	require.Len(t, items, 1, "both bindings name the same vendor")
	require.Equal(t, in.RemoteSessionIssuerID.String(), conv.PtrValOr(items[0].RemoteSessionIssuerID, ""))
	require.Zero(t, items[0].ServerCount, "bindings with no live MCP server count no servers")

	seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "ema-a-"+uuid.NewString()[:8], in.Resource)
	seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "ema-b-"+uuid.NewString()[:8], in.Resource)
	seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "ema-c-"+uuid.NewString()[:8], other.Resource)
	items = list()
	require.Len(t, items, 1)
	require.Equal(t, 3, items[0].ServerCount, "each live MCP server counts once, even two on one binding")

	internalIssuer := seedInternalSessionIssuer(t, ctx, ti, org)
	items = list()
	require.True(t, items[0].EmaProviders.IsDefault)
	require.Len(t, items[0].EmaProviders.Providers, 2, "the organization-wide default lists each endpoint's own issuer, then Speakeasy")
	for _, p := range items[0].EmaProviders.Providers {
		require.Nil(t, p.LastAcceptedAt, "nothing accepted yet for %s", p.Kind)
	}

	accepted := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	plantEMACredential(t, ctx, ti, other, remotesessions.EMAProviderSpeakeasy, accepted, true, internalIssuer)
	items = list()
	speakeasy := items[0].EmaProviders.Providers[1]
	require.Equal(t, "speakeasy", speakeasy.Kind)
	require.NotNil(t, speakeasy.LastAcceptedAt)
	require.Equal(t, accepted.Format(time.RFC3339), *speakeasy.LastAcceptedAt, "a retired credential still shows the vendor accepted Speakeasy")
	require.Nil(t, items[0].EmaProviders.Providers[0].LastAcceptedAt, "Speakeasy's acceptance is not shown under the external entry")

	q := repo.New(ti.conn)
	savedOnly, err := remotesessions.NormalizeVendorIssuer("https://saved-only.example.com")
	require.NoError(t, err)
	require.NoError(t, q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: org, VendorIssuer: savedOnly, RemoteSessionIssuerID: uuid.NullUUID{}, InternalSessionIssuerID: conv.ToNullUUID(internalIssuer), Position: 0}))
	named := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(org), "named-"+uuid.NewString()[:8], "https://named.example.com/")
	namedVendor, err := remotesessions.NormalizeVendorIssuer("https://named.example.com")
	require.NoError(t, err)
	require.NoError(t, q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: org, VendorIssuer: namedVendor, RemoteSessionIssuerID: uuid.NullUUID{}, InternalSessionIssuerID: conv.ToNullUUID(internalIssuer), Position: 0}))

	byVendor := map[string]*orgissuersgen.EMAService{}
	for _, item := range list() {
		byVendor[item.VendorIssuer] = item
	}
	require.Len(t, byVendor, 3, "a saved order lists its vendor even with no binding")
	require.Nil(t, byVendor[savedOnly].RemoteSessionIssuerID, "no issuer record names this vendor")
	require.Zero(t, byVendor[savedOnly].ServerCount)
	require.False(t, byVendor[savedOnly].EmaProviders.IsDefault)
	require.Equal(t, named.String(), conv.PtrValOr(byVendor[namedVendor].RemoteSessionIssuerID, ""),
		"a saved order keeps an issuer record, matched across spellings, so it can still be changed")
}

func TestListEMAServices_SavedOrderNeverNamesAnotherTenantsIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org := auth.ActiveOrganizationID
	vendor, err := remotesessions.NormalizeVendorIssuer("https://shared-vendor.example.com")
	require.NoError(t, err)
	internalIssuer := seedInternalSessionIssuer(t, ctx, ti, org)
	require.NoError(t, repo.New(ti.conn).InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: org, VendorIssuer: vendor, RemoteSessionIssuerID: uuid.NullUUID{}, InternalSessionIssuerID: conv.ToNullUUID(internalIssuer), Position: 0}))

	// Another tenant's legacy project-owned issuer for the same vendor, with
	// no organization_id: it belongs to that tenant, not the platform.
	other := createOrganization(t, ctx, ti.conn, "other-"+uuid.NewString()[:8])
	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "other", Slug: "other-" + uuid.NewString()[:8], OrganizationID: other})
	require.NoError(t, err)
	seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{UUID: otherProject.ID, Valid: true}, pgtype.Text{}, "legacy-"+uuid.NewString()[:8], "https://shared-vendor.example.com")

	out, err := ti.service.ListEMAServices(ctx, &orgissuersgen.ListEMAServicesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	require.Nil(t, out.Items[0].RemoteSessionIssuerID, "another tenant's legacy issuer is never named")
	require.Empty(t, out.Items[0].Name)

	ownProject := createProject(t, ctx, ti.conn, "own-"+uuid.NewString()[:8])
	own := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{UUID: ownProject, Valid: true}, pgtype.Text{}, "own-"+uuid.NewString()[:8], "https://shared-vendor.example.com/")
	out, err = ti.service.ListEMAServices(ctx, &orgissuersgen.ListEMAServicesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, own.String(), conv.PtrValOr(out.Items[0].RemoteSessionIssuerID, ""), "the organization's own legacy project issuer is found through its project")
}
