package externalcredentials_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
)

func TestVerifyGcpIamCredential_Verified(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-verify-ok")

	// A real impersonation resolve reports the target itself as the principal.
	var probed gcpauth.Credential
	ti.gcpResolver.SetResolve(func(_ context.Context, cred gcpauth.Credential) (gcpauth.Principal, error) {
		probed = cred
		return gcpauth.Principal{Email: cred.ImpersonateServiceAccount, Source: gcpauth.SourceImpersonation}, nil
	})

	result, err := ti.service.VerifyGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.VerifyGcpIamCredentialPayload{
		SessionToken: nil,
		ID:           created.ID,
	})
	require.NoError(t, err)

	require.True(t, result.Verified)
	require.Equal(t, "gram@customer.iam.gserviceaccount.com", *result.Principal)
	require.Nil(t, result.Detail)

	require.Equal(t, "gram@customer.iam.gserviceaccount.com", probed.ImpersonateServiceAccount)
	require.Empty(t, probed.WifPoolID, "the organization tier never probes WIF")
}

// A resolution failure is the expected signal that the customer has not granted
// Gram impersonation rights, so it is a reportable outcome rather than an error.
func TestVerifyGcpIamCredential_NotVerified(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-verify-denied")

	ti.gcpResolver.SetResolve(func(_ context.Context, _ gcpauth.Credential) (gcpauth.Principal, error) {
		return gcpauth.Principal{Email: "", Source: ""}, errors.New("caller does not have permission to impersonate")
	})

	result, err := ti.service.VerifyGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.VerifyGcpIamCredentialPayload{
		SessionToken: nil,
		ID:           created.ID,
	})
	require.NoError(t, err)

	require.False(t, result.Verified)
	require.Equal(t, "gram@customer.iam.gserviceaccount.com", *result.Principal)
	require.Contains(t, *result.Detail, "permission to impersonate")
}

// A credential written before this tier required impersonation names no target.
// Probing it would resolve Gram's ambient identity and report success, which
// would say nothing about the credential, so it reports unverified instead.
func TestVerifyGcpIamCredential_LegacyCredentialWithoutTarget(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPAmbientCredentialDirect(t, ctx, ti, "gcp-verify-legacy")

	probed := false
	ti.gcpResolver.SetResolve(func(_ context.Context, cred gcpauth.Credential) (gcpauth.Principal, error) {
		probed = true
		return gcpauth.Principal{Email: "gram@gram-platform.iam.gserviceaccount.com", Source: gcpauth.SourceMetadataServer}, nil
	})

	result, err := ti.service.VerifyGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.VerifyGcpIamCredentialPayload{
		SessionToken: nil,
		ID:           created.ID,
	})
	require.NoError(t, err)

	require.False(t, result.Verified)
	require.Contains(t, *result.Detail, "names no service account")
	require.False(t, probed, "must not fall back to probing gram's own ambient identity")
}

// A credential still carrying WIF columns resolves as WIF in real use, which
// gcpauth reports as unsupported. Probing only its impersonation hop would
// claim the credential works when nothing else can use it.
func TestVerifyGcpIamCredential_LegacyWifCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPWifCredentialDirect(t, ctx, ti, "gcp-verify-legacy-wif")

	probed := false
	ti.gcpResolver.SetResolve(func(_ context.Context, cred gcpauth.Credential) (gcpauth.Principal, error) {
		probed = true
		return gcpauth.Principal{Email: cred.ImpersonateServiceAccount, Source: gcpauth.SourceImpersonation}, nil
	})

	result, err := ti.service.VerifyGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.VerifyGcpIamCredentialPayload{
		SessionToken: nil,
		ID:           created.ID,
	})
	require.NoError(t, err)

	require.False(t, result.Verified)
	require.Contains(t, *result.Detail, "Workload Identity Federation")
	require.False(t, probed, "must not probe the impersonation hop of a WIF credential")
}

// The write-time screening arrived with this endpoint, so rows created earlier
// were never screened. Verify has to re-apply it or it becomes an oracle for
// which service accounts in Gram's own project Gram can impersonate.
func TestVerifyGcpIamCredential_RescreensTargetInGramProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPCredentialDirect(t, ctx, ti, "gcp-verify-unscreened", repo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      uuid.Nil,
		ImpersonateServiceAccount: conv.ToPGText(gramProjectServiceAccount("internal")),
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
	})

	// Keep the default ambient answer so the screening compares against the same
	// project gramProjectServiceAccount built the target in; only record whether
	// the target itself was ever probed.
	probed := false
	ti.gcpResolver.SetResolve(func(_ context.Context, cred gcpauth.Credential) (gcpauth.Principal, error) {
		if cred.ImpersonateServiceAccount != "" {
			probed = true
			return gcpauth.Principal{Email: cred.ImpersonateServiceAccount, Source: gcpauth.SourceImpersonation}, nil
		}
		return gcpauth.Principal{Email: gcpauth.StubResolverPrincipal, Source: gcpauth.SourceMetadataServer}, nil
	})

	result, err := ti.service.VerifyGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.VerifyGcpIamCredentialPayload{
		SessionToken: nil,
		ID:           created.ID,
	})
	require.NoError(t, err)

	require.False(t, result.Verified)
	require.False(t, probed, "must not probe a target inside gram's own project")
}

func TestVerifyGcpIamCredential_RateLimited(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-verify-ratelimit")
	adminCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource))
	payload := &gen.VerifyGcpIamCredentialPayload{SessionToken: nil, ID: created.ID}

	// The bucket holds verifyRateBurst tokens and refills far slower than this
	// loop runs, so a caller that spends them all is refused.
	var lastErr error
	for range 10 {
		if _, err := ti.service.VerifyGcpIamCredential(adminCtx, payload); err != nil {
			lastErr = err
			break
		}
	}

	requireOopsCode(t, lastErr, oops.CodeRateLimitExceeded)
}

func TestVerifyGcpIamCredential_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.VerifyGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.VerifyGcpIamCredentialPayload{
		SessionToken: nil,
		ID:           uuid.NewString(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestVerifyGcpIamCredential_ForbiddenForReadOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-verify-forbidden")

	_, err := ti.service.VerifyGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.VerifyGcpIamCredentialPayload{
		SessionToken: nil,
		ID:           created.ID,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestVerifyGcpIamCredential_ForbiddenWithoutEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-verify-no-entitlement")
	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	_, err := ti.service.VerifyGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.VerifyGcpIamCredentialPayload{
		SessionToken: nil,
		ID:           created.ID,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestVerifyGcpIamCredential_ForbiddenWithoutEntitlementUnknownID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	// An unknown id still fails on the entitlement rather than reaching the
	// lookup, so the gate cannot be used to probe which ids exist.
	_, err := ti.service.VerifyGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.VerifyGcpIamCredentialPayload{
		SessionToken: nil,
		ID:           uuid.NewString(),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

// A screening the server cannot evaluate is Gram's fault, not the customer's, so
// verify errors rather than reporting the credential unverified.
func TestVerifyGcpIamCredential_ErrorsWhenScreeningUnevaluatable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Written through the repo rather than the API: creating through the API
	// resolves Gram's principal successfully and memoizes it, after which the
	// scripted failure below would never be consulted.
	created := createGCPCredentialDirect(t, ctx, ti, "gcp-verify-screening-broken", repo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      uuid.Nil,
		ImpersonateServiceAccount: conv.ToPGText("gram@customer.iam.gserviceaccount.com"),
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
	})

	ti.gcpResolver.SetResolve(func(_ context.Context, cred gcpauth.Credential) (gcpauth.Principal, error) {
		// Only the ambient probe fails, which is the one the screening depends on.
		if cred.ImpersonateServiceAccount != "" {
			return gcpauth.Principal{Email: cred.ImpersonateServiceAccount, Source: gcpauth.SourceImpersonation}, nil
		}
		return gcpauth.Principal{Email: "", Source: ""}, errors.New("no application default credentials")
	})

	_, err := ti.service.VerifyGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.VerifyGcpIamCredentialPayload{
		SessionToken: nil,
		ID:           created.ID,
	})
	requireOopsCode(t, err, oops.CodeUnexpected)
}
