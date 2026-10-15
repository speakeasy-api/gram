package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
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
	plantEMACredential(t, ctx, ti, other, remotesessions.EMAProviderSpeakeasy, time.Now().UTC(), true, internalIssuer)
	items = list()
	require.Nil(t, items[0].EmaProviders.Providers[1].LastAcceptedAt, "retiring a credential bumps updated_at without an exchange, so it is not acceptance")
	plantEMACredential(t, ctx, ti, other, remotesessions.EMAProviderSpeakeasy, accepted, false, internalIssuer)
	items = list()
	speakeasy := items[0].EmaProviders.Providers[1]
	require.Equal(t, "speakeasy", speakeasy.Kind)
	require.NotNil(t, speakeasy.LastAcceptedAt)
	require.Equal(t, accepted.Format(time.RFC3339), *speakeasy.LastAcceptedAt, "a live credential shows the vendor accepted Speakeasy")
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

func listEMAServicesByVendor(t *testing.T, ctx context.Context, ti *testInstance) map[string]*orgissuersgen.EMAService {
	t.Helper()
	out, err := ti.service.ListEMAServices(ctx, &orgissuersgen.ListEMAServicesPayload{SessionToken: nil})
	require.NoError(t, err)
	byVendor := map[string]*orgissuersgen.EMAService{}
	for _, item := range out.Items {
		byVendor[item.VendorIssuer] = item
	}
	return byVendor
}

func prepareEMABinding(t *testing.T, ctx context.Context, ti *testInstance, in remotesessions.PreparationInput) {
	t.Helper()
	_, err := ti.service.PrepareEMA(ctx, &clientsgen.PrepareEMAPayload{
		UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(),
		ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: in.Resource, Scopes: in.Scopes, Mechanism: "manual",
		ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer},
	})
	require.NoError(t, err)
}

func emaServiceServerNames(servers []*orgissuersgen.EMAServiceServer) []string {
	names := make([]string, 0, len(servers))
	for _, s := range servers {
		names = append(names, s.Name)
	}
	return names
}

func TestListEMAServices_ListsServersWithStatus(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.SetSpeakeasySigningAvailable(true)
	internalIssuer := seedInternalSessionIssuer(t, ctx, ti, auth.ActiveOrganizationID)
	prepareEMABinding(t, ctx, ti, in)
	vendor, err := remotesessions.NormalizeVendorIssuer("https://idp.example.com")
	require.NoError(t, err)

	first := seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "ema-alpha", in.Resource)
	removed := seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "ema-removed", in.Resource)
	_, err = mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: removed, ProjectID: *auth.ProjectID})
	require.NoError(t, err)

	svc := listEMAServicesByVendor(t, ctx, ti)[vendor]
	require.NotNil(t, svc)
	require.Equal(t, 1, svc.ServerCount, "a deleted server is not counted")
	require.Zero(t, svc.HiddenServerCount)
	require.Len(t, svc.Servers, 1, "a deleted server is not listed")
	server := svc.Servers[0]
	require.Equal(t, first.String(), server.ID)
	require.Equal(t, "ema-alpha", server.Name)
	require.Equal(t, "ema-alpha", server.Slug)
	require.Equal(t, auth.ProjectID.String(), server.ProjectID)
	require.NotEmpty(t, server.ProjectSlug)
	require.NotEmpty(t, server.ProjectName)
	require.Equal(t, "not_proven", server.Status, "on, with no acceptance yet")
	require.Nil(t, server.LastAcceptedAt)

	accepted := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	plantEMACredential(t, ctx, ti, in, remotesessions.EMAProviderSpeakeasy, accepted, false, internalIssuer)
	server = listEMAServicesByVendor(t, ctx, ti)[vendor].Servers[0]
	require.Equal(t, "working", server.Status)
	require.Equal(t, accepted.Format(time.RFC3339), conv.PtrValOr(server.LastAcceptedAt, ""))

	// An order naming a provider this server's sign-in can't use leaves
	// nothing to vouch, whatever was accepted before.
	q := repo.New(ti.conn)
	external := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, auth.ActiveOrganizationID, "idp-"+uuid.NewString()[:8])
	require.NoError(t, q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: auth.ActiveOrganizationID, VendorIssuer: vendor, RemoteSessionIssuerID: conv.ToNullUUID(external), InternalSessionIssuerID: uuid.NullUUID{}, Position: 0}))
	server = listEMAServicesByVendor(t, ctx, ti)[vendor].Servers[0]
	require.Equal(t, "needs_attention", server.Status)
	require.Nil(t, server.LastAcceptedAt, "Speakeasy's acceptance does not prove an order that no longer tries it")
}

func TestListEMAServices_HidesServersTheCallerCannotRead(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	prepareEMABinding(t, ctx, ti, in)
	vendor, err := remotesessions.NormalizeVendorIssuer("https://idp.example.com")
	require.NoError(t, err)
	readable := seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "ema-readable", in.Resource)
	seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "ema-secret", in.Resource)

	all := listEMAServicesByVendor(t, ctx, ti)[vendor]
	require.Equal(t, 2, all.ServerCount)
	require.Equal(t, []string{"ema-readable", "ema-secret"}, emaServiceServerNames(all.Servers))

	restricted := withExactAccessGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeOrgRead, auth.ActiveOrganizationID),
		authz.NewGrant(authz.ScopeMCPRead, readable.String()),
	)
	svc := listEMAServicesByVendor(t, restricted, ti)[vendor]
	require.Equal(t, 2, svc.ServerCount, "counts include servers the caller cannot read")
	require.Equal(t, 1, svc.HiddenServerCount)
	require.Equal(t, []string{"ema-readable"}, emaServiceServerNames(svc.Servers), "an unreadable server is never named")

	none := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgRead, auth.ActiveOrganizationID))
	svc = listEMAServicesByVendor(t, none, ti)[vendor]
	require.Empty(t, svc.Servers)
	require.Equal(t, 2, svc.HiddenServerCount)
}

func TestListEMAServices_MatchesServersByCachedCanonicalResource(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	vendor, err := remotesessions.NormalizeVendorIssuer("https://idp.example.com")
	require.NoError(t, err)
	canonical := in
	canonical.Resource = "https://alias.example.com"
	prepareEMABinding(t, ctx, ti, canonical)

	aliased := "https://alias.example.com/mcp"
	seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "ema-aliased", aliased)
	seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "ema-same-origin", "https://alias.example.com/other")
	require.Zero(t, listEMAServicesByVendor(t, ctx, ti)[vendor].ServerCount, "sharing an origin proves nothing")

	_, err = remotemcprepo.New(ti.conn).UpsertRemoteProtectedResource(ctx, remotemcprepo.UpsertRemoteProtectedResourceParams{
		ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, ResourceIdentifier: aliased,
		Metadata: `{"resource":"https://alias.example.com/","authorization_servers":["https://idp.example.com"]}`,
	})
	require.NoError(t, err)
	svc := listEMAServicesByVendor(t, ctx, ti)[vendor]
	require.Equal(t, 1, svc.ServerCount, "the server whose cached document names the binding's resource reaches the service")
	require.Equal(t, []string{"ema-aliased"}, emaServiceServerNames(svc.Servers))
}
