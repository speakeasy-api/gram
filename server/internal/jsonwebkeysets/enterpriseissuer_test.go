package jsonwebkeysets_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	gen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/emaissuer"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

const issuerOrigin = "https://app.example.com"

// withIssuer installs a real provisioner and signer over one in-process key.
func withIssuer(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()
	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	kms := provisiontest.NewKMSClients(t)
	provisioner := provisiontest.NewProvisioner(t, ti.conn, kms.Factory, issuerOrigin, credentialID, "")
	origin, err := url.Parse(issuerOrigin)
	require.NoError(t, err)
	signer := emaissuer.NewSigner(testenv.NewLogger(t), ti.conn, origin, gcpauth.NewIdentity(gcpauth.NewStubResolver()), func(ctx context.Context, ts oauth2.TokenSource) (gcpkms.SigningClient, error) {
		return kms.Factory(ctx, ts)
	}, emaissuer.SignerConfig{})
	ti.service.SetEnterpriseIssuer(provisioner, signer, origin)
	productfeaturestest.Enable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureSSO)
}

func TestEnterpriseIssuer_RequiresGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withIssuer(t, ctx, ti)

	_, err := ti.service.GetOrganizationEnterpriseIssuer(authztest.WithExactGrants(t, ctx), &gen.GetOrganizationEnterpriseIssuerPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	read := readCtx(t, ctx)
	_, err = ti.service.EnableOrganizationEnterpriseIssuer(read, &gen.EnableOrganizationEnterpriseIssuerPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.DisableOrganizationEnterpriseIssuer(read, &gen.DisableOrganizationEnterpriseIssuerPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.RotateOrganizationEnterpriseIssuer(read, &gen.RotateOrganizationEnterpriseIssuerPayload{ActiveKid: "kid", SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ReplaceOrganizationEnterpriseIssuerKey(read, &gen.ReplaceOrganizationEnterpriseIssuerKeyPayload{Kid: "kid", SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestEnterpriseIssuer_RequiresSSOEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withIssuer(t, ctx, ti)
	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureSSO)

	view, err := ti.service.GetOrganizationEnterpriseIssuer(readCtx(t, ctx), &gen.GetOrganizationEnterpriseIssuerPayload{SessionToken: nil})
	require.NoError(t, err)
	require.False(t, view.Available)
	require.False(t, view.Enabled)
	_, err = ti.service.EnableOrganizationEnterpriseIssuer(adminCtx(t, ctx), &gen.EnableOrganizationEnterpriseIssuerPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestEnterpriseIssuer_UnavailableWithoutProvisioner(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	productfeaturestest.Enable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureSSO)

	view, err := ti.service.GetOrganizationEnterpriseIssuer(readCtx(t, ctx), &gen.GetOrganizationEnterpriseIssuerPayload{SessionToken: nil})
	require.NoError(t, err)
	require.False(t, view.Available)
	_, err = ti.service.EnableOrganizationEnterpriseIssuer(adminCtx(t, ctx), &gen.EnableOrganizationEnterpriseIssuerPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestEnterpriseIssuer_Lifecycle(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withIssuer(t, ctx, ti)
	admin := adminCtx(t, ctx)
	wantIssuer := issuerOrigin + "/oauth/idp/org/" + ti.orgID

	enabled, err := ti.service.EnableOrganizationEnterpriseIssuer(admin, &gen.EnableOrganizationEnterpriseIssuerPayload{SessionToken: nil})
	require.NoError(t, err)
	require.True(t, enabled.Available)
	require.True(t, enabled.Enabled)
	require.True(t, enabled.SigningEnabled)
	require.Equal(t, wantIssuer, *enabled.Issuer)
	require.Equal(t, wantIssuer+"/jwks", *enabled.JwksURI)
	require.Len(t, enabled.Keys, 1)
	require.Equal(t, "active", enabled.Keys[0].State)
	active := enabled.Keys[0].Kid

	again, err := ti.service.EnableOrganizationEnterpriseIssuer(admin, &gen.EnableOrganizationEnterpriseIssuerPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, enabled.Keys, again.Keys, "enable is idempotent")

	// A rotation naming a kid the issuer never published is a stale read.
	_, err = ti.service.RotateOrganizationEnterpriseIssuer(admin, &gen.RotateOrganizationEnterpriseIssuerPayload{ActiveKid: "never-published", SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	replaced, err := ti.service.ReplaceOrganizationEnterpriseIssuerKey(admin, &gen.ReplaceOrganizationEnterpriseIssuerKeyPayload{Kid: "missing", SessionToken: nil})
	require.Nil(t, replaced)
	requireOopsCode(t, err, oops.CodeNotFound)

	read, err := ti.service.GetOrganizationEnterpriseIssuer(readCtx(t, ctx), &gen.GetOrganizationEnterpriseIssuerPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, wantIssuer, *read.Issuer)
	require.Equal(t, active, read.Keys[0].Kid)

	disabled, err := ti.service.DisableOrganizationEnterpriseIssuer(admin, &gen.DisableOrganizationEnterpriseIssuerPayload{SessionToken: nil})
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
	require.Nil(t, disabled.Issuer)
	_, err = ti.service.RotateOrganizationEnterpriseIssuer(admin, &gen.RotateOrganizationEnterpriseIssuerPayload{ActiveKid: active, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}
