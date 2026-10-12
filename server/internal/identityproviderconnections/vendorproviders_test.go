package identityproviderconnections_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

const testVendorIssuer = "https://mcp.vendor.example"

func speakeasyProvider() *gen.VendorIdentityProviderInput {
	return &gen.VendorIdentityProviderInput{Kind: "speakeasy", RemoteSessionIssuerID: nil}
}

func externalProvider(id string) *gen.VendorIdentityProviderInput {
	return &gen.VendorIdentityProviderInput{Kind: "external", RemoteSessionIssuerID: conv.PtrEmpty(id)}
}

// seedRemoteIssuer creates an organization-level remote session issuer.
func seedRemoteIssuer(t *testing.T, ctx context.Context, si *serviceInstance) uuid.UUID {
	t.Helper()
	issuer, err := remotesessionsrepo.New(si.conn.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		OrganizationID: conv.ToPGText(si.orgID), Slug: "supplier-" + uuid.NewString()[:8], Issuer: "https://idp-" + uuid.NewString()[:8] + ".example.test",
		AuthorizationEndpoint: conv.ToPGText("https://idp.example.test/authorize"), TokenEndpoint: conv.ToPGText("https://idp.example.test/token"),
		ScopesSupported: []string{}, GrantTypesSupported: []string{}, AuthorizationGrantProfilesSupported: []string{}, ResponseTypesSupported: []string{},
		TokenEndpointAuthMethodsSupported: []string{}, CodeChallengeMethodsSupported: []string{},
		IntrospectionEndpointAuthMethodsSupported: []string{}, IDTokenSigningAlgValuesSupported: []string{}, ClaimsSupported: []string{},
	})
	require.NoError(t, err)
	return issuer.ID
}

// seedExternalSupplier creates a remote session issuer an organization user
// session issuer trusts, so identity chaining can exchange at it.
func seedExternalSupplier(t *testing.T, ctx context.Context, si *serviceInstance) string {
	t.Helper()
	issuer := seedRemoteIssuer(t, ctx, si)
	client := createSignInClient(t, ctx, si, issuer, []string{"openid"})
	createSignInUserSessionIssuer(t, ctx, si, "ema-"+uuid.NewString()[:8], uuid.NullUUID{UUID: issuer, Valid: true}, uuid.NullUUID{UUID: client, Valid: true})
	return issuer.String()
}

// seedSpeakeasyIssuer creates the organization's Speakeasy issuer row.
func seedSpeakeasyIssuer(t *testing.T, ctx context.Context, si *serviceInstance, organizationID string) string {
	t.Helper()
	fixtures := testrepo.New(si.conn.conn)
	set, err := fixtures.SeedJsonWebKeySetFixture(ctx, testrepo.SeedJsonWebKeySetFixtureParams{OrganizationID: organizationID, Name: "speakeasy-issuer-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	issuer, err := fixtures.SeedInternalSessionIssuerFixture(ctx, testrepo.SeedInternalSessionIssuerFixtureParams{OrganizationID: organizationID, Issuer: "https://speakeasy.example.test/oauth/idp/org/" + organizationID, JsonWebKeySetID: set})
	require.NoError(t, err)
	return issuer.String()
}

func setVendorProviders(ctx context.Context, si *serviceInstance, vendor string, providers ...*gen.VendorIdentityProviderInput) (*gen.VendorIdentityProvidersResult, error) {
	if providers == nil {
		providers = []*gen.VendorIdentityProviderInput{}
	}
	result, err := si.svc.SetVendorIdentityProviders(ctx, &gen.SetVendorIdentityProvidersPayload{SessionToken: nil, VendorIssuer: vendor, Providers: providers})
	if err != nil {
		return nil, fmt.Errorf("set vendor identity providers: %w", err)
	}
	return result, nil
}

func getVendorProviders(t *testing.T, ctx context.Context, si *serviceInstance, vendor string) *gen.VendorIdentityProvidersResult {
	t.Helper()
	got, err := si.svc.GetVendorIdentityProviders(ctx, &gen.GetVendorIdentityProvidersPayload{SessionToken: nil, VendorIssuer: vendor})
	require.NoError(t, err)
	return got
}

// providerKeys renders an order as kind[:connection] for compact assertions.
func providerKeys(result *gen.VendorIdentityProvidersResult) []string {
	keys := make([]string, 0, len(result.Providers))
	for _, p := range result.Providers {
		key := p.Kind
		if p.RemoteSessionIssuerID != nil {
			key += ":" + *p.RemoteSessionIssuerID
		}
		if p.InternalSessionIssuerID != nil {
			key += ":" + *p.InternalSessionIssuerID
		}
		if p.Unavailable {
			key += ":unavailable"
		}
		keys = append(keys, key)
	}
	return keys
}

func TestVendorProviders_DefaultOrder(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	got := getVendorProviders(t, ctx, si, testVendorIssuer)
	require.True(t, got.IsDefault)
	require.Empty(t, providerKeys(got), "nothing is available yet")

	supplier := seedExternalSupplier(t, ctx, si)
	issuer := seedSpeakeasyIssuer(t, ctx, si, si.orgID)
	got = getVendorProviders(t, ctx, si, testVendorIssuer)
	require.True(t, got.IsDefault)
	require.Equal(t, []string{"external:" + supplier, "speakeasy:" + issuer}, providerKeys(got), "external connections first, then Speakeasy")

	_, err := testrepo.New(si.conn.conn).DisableInternalSessionIssuerFixture(ctx, testrepo.DisableInternalSessionIssuerFixtureParams{OrganizationID: si.orgID, ID: uuid.MustParse(issuer)})
	require.NoError(t, err)
	require.Equal(t, []string{"external:" + supplier}, providerKeys(getVendorProviders(t, ctx, si, testVendorIssuer)), "a turned-off Speakeasy issuer is not available")
}

func TestVendorProviders_SetReplacesNormalizesAndAudits(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	supplier := seedExternalSupplier(t, ctx, si)
	issuer := seedSpeakeasyIssuer(t, ctx, si, si.orgID)
	action := audit.ActionOrganizationEMAVendorIdentityProvidersUpdated

	set, err := setVendorProviders(ctx, si, "HTTPS://MCP.Vendor.Example/", speakeasyProvider(), externalProvider(supplier))
	require.NoError(t, err)
	require.Equal(t, testVendorIssuer, set.VendorIssuer)
	require.False(t, set.IsDefault)
	require.Equal(t, []string{"speakeasy:" + issuer, "external:" + supplier}, providerKeys(set))
	require.Equal(t, providerKeys(set), providerKeys(getVendorProviders(t, ctx, si, testVendorIssuer)))

	record, err := audittest.LatestAuditLogByAction(ctx, si.conn.conn, action)
	require.NoError(t, err)
	require.Equal(t, si.orgID, record.SubjectID)
	require.Equal(t, "organization", record.SubjectType)
	var after audit.OrganizationEMAVendorIdentityProvidersSnapshot
	require.NoError(t, json.Unmarshal(record.AfterSnapshot, &after))
	require.Equal(t, testVendorIssuer, after.VendorIssuer)
	require.Equal(t, []audit.OrganizationEMAVendorIdentityProvider{{Kind: "speakeasy", RemoteSessionIssuerID: "", InternalSessionIssuerID: issuer}, {Kind: "external", RemoteSessionIssuerID: supplier, InternalSessionIssuerID: ""}}, after.Providers)
	count, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, action)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	_, err = setVendorProviders(ctx, si, testVendorIssuer, speakeasyProvider(), externalProvider(supplier))
	require.NoError(t, err)
	unchanged, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, action)
	require.NoError(t, err)
	require.Equal(t, count, unchanged, "saving the same order records nothing")

	reordered, err := setVendorProviders(ctx, si, testVendorIssuer, externalProvider(supplier))
	require.NoError(t, err)
	require.Equal(t, []string{"external:" + supplier}, providerKeys(reordered))

	reset, err := setVendorProviders(ctx, si, testVendorIssuer)
	require.NoError(t, err)
	require.True(t, reset.IsDefault, "an empty order restores the default")
	final, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, action)
	require.NoError(t, err)
	require.Equal(t, int64(3), final)
}

func TestVendorProviders_SpeakeasyNeedsAnIssuer(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	_, err := setVendorProviders(ctx, si, testVendorIssuer, speakeasyProvider())
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestVendorProviders_Validation(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	supplier := seedExternalSupplier(t, ctx, si)
	seedSpeakeasyIssuer(t, ctx, si, si.orgID)

	for _, tc := range []struct {
		name      string
		vendor    string
		providers []*gen.VendorIdentityProviderInput
	}{
		{name: "relative vendor issuer", vendor: "mcp.vendor.example", providers: []*gen.VendorIdentityProviderInput{speakeasyProvider()}},
		{name: "vendor issuer with query", vendor: testVendorIssuer + "?x=1", providers: []*gen.VendorIdentityProviderInput{speakeasyProvider()}},
		{name: "unknown kind", vendor: testVendorIssuer, providers: []*gen.VendorIdentityProviderInput{{Kind: "okta", RemoteSessionIssuerID: nil}}},
		{name: "external without connection", vendor: testVendorIssuer, providers: []*gen.VendorIdentityProviderInput{{Kind: "external", RemoteSessionIssuerID: nil}}},
		{name: "speakeasy with connection", vendor: testVendorIssuer, providers: []*gen.VendorIdentityProviderInput{{Kind: "speakeasy", RemoteSessionIssuerID: &supplier}}},
		{name: "malformed connection", vendor: testVendorIssuer, providers: []*gen.VendorIdentityProviderInput{externalProvider("not-a-uuid")}},
		{name: "unknown remote issuer", vendor: testVendorIssuer, providers: []*gen.VendorIdentityProviderInput{externalProvider(uuid.NewString())}},
		{name: "remote issuer nobody trusts", vendor: testVendorIssuer, providers: []*gen.VendorIdentityProviderInput{externalProvider(seedRemoteIssuer(t, ctx, si).String())}},
		{name: "duplicate", vendor: testVendorIssuer, providers: []*gen.VendorIdentityProviderInput{speakeasyProvider(), speakeasyProvider()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := setVendorProviders(ctx, si, tc.vendor, tc.providers...)
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}
	require.True(t, getVendorProviders(t, ctx, si, testVendorIssuer).IsDefault, "rejected orders save nothing")
}

func TestVendorProviders_UnavailableProvidersAreFlagged(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	supplier := seedExternalSupplier(t, ctx, si)
	issuer := seedSpeakeasyIssuer(t, ctx, si, si.orgID)
	_, err := setVendorProviders(ctx, si, testVendorIssuer, externalProvider(supplier), speakeasyProvider())
	require.NoError(t, err)

	_, err = remotesessionsrepo.New(si.conn.conn).SoftDeleteRemoteSessionIssuerFixture(ctx, uuid.MustParse(supplier))
	require.NoError(t, err)
	_, err = testrepo.New(si.conn.conn).DisableInternalSessionIssuerFixture(ctx, testrepo.DisableInternalSessionIssuerFixtureParams{OrganizationID: si.orgID, ID: uuid.MustParse(issuer)})
	require.NoError(t, err)
	got := getVendorProviders(t, ctx, si, testVendorIssuer)
	require.False(t, got.IsDefault, "stale entries never turn the order back into the default")
	require.Equal(t, []string{"external:" + supplier + ":unavailable", "speakeasy:" + issuer + ":unavailable"}, providerKeys(got))
}

func TestVendorProviders_RBAC(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	readerCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, si.orgID))
	getVendorProviders(t, readerCtx, si, testVendorIssuer)
	_, err := setVendorProviders(readerCtx, si, testVendorIssuer, speakeasyProvider())
	requireOopsCode(t, err, oops.CodeForbidden)

	noneCtx := authztest.WithExactGrants(t, ctx)
	_, err = si.svc.GetVendorIdentityProviders(noneCtx, &gen.GetVendorIdentityProvidersPayload{SessionToken: nil, VendorIssuer: testVendorIssuer})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = setVendorProviders(asSupportSession(ctx, si), si, testVendorIssuer, speakeasyProvider())
	requireOopsCode(t, err, oops.CodeForbidden)
	require.True(t, getVendorProviders(t, ctx, si, testVendorIssuer).IsDefault)
}

func TestVendorProviders_TenantIsolation(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	supplier := seedExternalSupplier(t, ctx, si)
	seedSpeakeasyIssuer(t, ctx, si, si.orgID)
	_, err := setVendorProviders(ctx, si, testVendorIssuer, speakeasyProvider())
	require.NoError(t, err)

	otherCtx, _ := asOtherOrganization(t, ctx, si)
	got := getVendorProviders(t, otherCtx, si, testVendorIssuer)
	require.True(t, got.IsDefault, "another organization's order never applies")
	require.Empty(t, providerKeys(got), "another organization's connections and issuer never appear")
	_, err = setVendorProviders(otherCtx, si, testVendorIssuer, speakeasyProvider())
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = setVendorProviders(otherCtx, si, testVendorIssuer, externalProvider(supplier))
	requireOopsCode(t, err, oops.CodeBadRequest)
}
