package identityproviders_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDescribeSetupReturnsConnectStep(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connection := createConnection(t, ctx, ti, "https://acme.okta.com")
	ctx = withExactScope(t, ctx, ti, authz.ScopeOrgRead)

	setup, err := ti.service.DescribeSetup(ctx, &gen.DescribeSetupPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, connection.ID, setup.ConnectionID)
	require.Len(t, setup.Steps, 2)
	require.Equal(t, &gen.IdentityProviderSetupStep{
		Key:   "connect",
		Title: "Connect Okta",
		Where: "their_console",
		Instructions: []string{
			"Granting API scopes to a service app needs an Okta Super Administrator; if that is not you, hand these steps to the person who is.",
			"Create an API Services app integration in the Okta Admin Console.",
			"Client authentication must be Public key / Private key with Use a URL; entering the URL alone is not enough",
			"Grant the API scopes and administrator roles shown below, then enter the app's Client ID in Speakeasy.",
		},
		DeepLink: new("https://acme-admin.okta.com/admin/apps/active"),
		PrintedValues: []*gen.IdentityProviderPrintedValue{
			{Label: "JWKS URL", Value: connection.JwksURL, Copyable: true},
			{Label: "API scopes", Value: "okta.apps.read okta.groups.read okta.users.read okta.apps.manage", Copyable: true},
			{Label: "Administrator roles", Value: "Read-only Administrator, Application Administrator", Copyable: false},
		},
		ExpectedValues: []*gen.IdentityProviderExpectedValue{{Key: "client_id", Label: "Client ID", Secret: false, CurrentValue: nil}},
		Claims:         nil,
		Repair:         nil,
		PortalIntent:   nil,
		State:          "awaiting_values",
		LastOutcome:    nil,
	}, setup.Steps[0])
	require.Equal(t, &gen.IdentityProviderSetupStep{
		Key:            "sign_in",
		Title:          "Configure Okta sign-in",
		Where:          "our_page",
		Instructions:   []string{"Verify the Okta connection before creating the sign-in application."},
		DeepLink:       nil,
		PrintedValues:  []*gen.IdentityProviderPrintedValue{},
		ExpectedValues: []*gen.IdentityProviderExpectedValue{},
		Claims: []*gen.IdentityProviderClaim{
			{Name: "email", Purpose: "identity", CarriesAccess: false},
			{Name: "first_name", Purpose: "display", CarriesAccess: false},
			{Name: "last_name", Purpose: "display", CarriesAccess: false},
			{Name: "groups", Purpose: "used by access rules", CarriesAccess: true},
		},
		Repair:       nil,
		PortalIntent: nil,
		State:        "not_started",
		LastOutcome:  nil,
	}, setup.Steps[1])
}

func TestDescribeSetupBuildsDeepLinksOnlyForSupportedOktaTenants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tenantURL string
		deepLink  *string
	}{
		{tenantURL: "https://one.okta.com", deepLink: new("https://one-admin.okta.com/admin/apps/active")},
		{tenantURL: "https://one-admin.okta.com", deepLink: new("https://one-admin.okta.com/admin/apps/active")},
		{tenantURL: "https://two.oktapreview.com", deepLink: new("https://two-admin.oktapreview.com/admin/apps/active")},
		{tenantURL: "https://three.okta-emea.com", deepLink: new("https://three-admin.okta-emea.com/admin/apps/active")},
		{tenantURL: "https://okta.com", deepLink: nil},
		{tenantURL: "https://nested.acme.okta.com", deepLink: nil},
		{tenantURL: "https://acme.example.com", deepLink: nil},
		{tenantURL: "https://acme.okta.com.example.com", deepLink: nil},
	}

	for _, testCase := range cases {
		ctx, ti := newTestService(t)
		createConnection(t, ctx, ti, testCase.tenantURL)
		setup, err := ti.service.DescribeSetup(ctx, &gen.DescribeSetupPayload{SessionToken: nil, ApikeyToken: nil})
		require.NoError(t, err)
		require.Len(t, setup.Steps, 2)
		require.Equal(t, testCase.deepLink, setup.Steps[0].DeepLink, testCase.tenantURL)
		require.Nil(t, setup.Steps[1].DeepLink, testCase.tenantURL)
	}
}

func TestDescribeSetupOmitsLastOutcomeWhenStoredEvidenceHasNoOutcome(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	createConnection(t, ctx, ti, "https://example.okta.com")
	setStoredClientID(t, ctx, ti)
	queries := repo.New(ti.conn)
	row, err := queries.GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	updated, err := queries.UpdateIdentityProviderVerification(ctx, repo.UpdateIdentityProviderVerificationParams{
		Status:                       row.Status,
		StatusDetail:                 row.StatusDetail,
		Capabilities:                 row.Capabilities,
		LastVerifiedAt:               row.LastVerifiedAt,
		VerifyEvidence:               []byte(`{"checked_at":"2026-09-15T00:00:00Z","reads":[]}`),
		OrganizationID:               ti.orgID,
		IdentityProviderConnectionID: row.ID,
		ClientID:                     row.ClientID,
		SigningKeyID:                 row.SigningKeyID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, updated)

	setup, err := ti.service.DescribeSetup(ctx, &gen.DescribeSetupPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Len(t, setup.Steps, 2)
	require.Nil(t, setup.Steps[0].LastOutcome)
}

func TestDescribeSetupReturnsNotFoundWhenAbsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.DescribeSetup(ctx, &gen.DescribeSetupPayload{SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDescribeSetupRequiresOrganizationRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.DescribeSetup(ctx, &gen.DescribeSetupPayload{SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}
