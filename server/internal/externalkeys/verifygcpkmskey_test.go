package externalkeys_test

import (
	"context"
	"errors"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	extcredrepo "github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

func adminCtx(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	return authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource))
}

// failingSigningClient answers every call with one provider error, so tests can
// drive the gRPC status codes a local key cannot produce.
type failingSigningClient struct {
	err error
}

func (c failingSigningClient) GetPublicKey(_ context.Context, _ string) (*gcpkms.PublicKey, error) {
	return nil, c.err
}

func (c failingSigningClient) AsymmetricSign(_ context.Context, _ string, _ jose.SignatureAlgorithm, _ []byte) ([]byte, error) {
	return nil, c.err
}

func (c failingSigningClient) Close() error { return nil }

func failWith(ti *testInstance, err error) {
	ti.kms.SetBuild(func(_ context.Context, _ oauth2.TokenSource) (gcpkms.SigningClient, error) {
		return failingSigningClient{err: err}, nil
	})
}

func TestVerifyGcpKmsKey_Verified(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	key := createGcpKmsKey(t, ctx, ti, "signing-key", credID)

	result, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.True(t, result.Verified)
	require.Equal(t, "verified", result.ProbeOutcome)
	require.Nil(t, result.Detail)

	// Each client owns a gRPC connection, so a probe that does not close the one
	// it built leaks it with no other visible symptom.
	require.Equal(t, 1, ti.kms.Closed())
}

// The probe is ephemeral: it must leave the key exactly as it found it.
func TestVerifyGcpKmsKey_PersistsNothing(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	key := createGcpKmsKey(t, ctx, ti, "signing-key", credID)

	_, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	after, err := ti.service.GetGcpKmsKey(adminCtx(t, ctx), &gen.GetGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, key.UpdatedAt, after.UpdatedAt)
}

// A healthy key that signs with something other than what Gram recorded is fatal
// even though nothing is broken on the provider's side: signing RS256 with an
// ES256 key mints tokens no verifier accepts.
func TestVerifyGcpKmsKey_AlgorithmMismatch(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	// The fixture records ES256.
	key := createGcpKmsKey(t, ctx, ti, "mismatched", credID)

	ti.kms.SetBuild(func(_ context.Context, _ oauth2.TokenSource) (gcpkms.SigningClient, error) {
		return gcpkms.NewLocalSigningClient(jose.RS256)
	})

	result, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.False(t, result.Verified)
	require.Equal(t, "algorithm_mismatch", result.ProbeOutcome)
	require.NotNil(t, result.Detail)
	require.Contains(t, *result.Detail, "RS256")
	require.Contains(t, *result.Detail, "ES256")
	require.Equal(t, 1, ti.kms.Closed())
}

// A missing roles/cloudkms.signerVerifier grant is the single most likely reason
// a correctly configured key fails, so it must read as a reportable outcome
// rather than a request error.
func TestVerifyGcpKmsKey_PermissionDenied(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	key := createGcpKmsKey(t, ctx, ti, "ungranted", credID)

	failWith(ti, status.Error(codes.PermissionDenied, "caller lacks cloudkms.cryptoKeyVersions.viewPublicKey"))

	result, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.False(t, result.Verified)
	require.Equal(t, "permission_denied", result.ProbeOutcome)
	require.Equal(t, 1, ti.kms.Closed())
}

// A DISABLED, DESTROYED or still PENDING_GENERATION key version comes back as
// FAILED_PRECONDITION.
func TestVerifyGcpKmsKey_KeyUnusable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	key := createGcpKmsKey(t, ctx, ti, "disabled", credID)

	failWith(ti, status.Error(codes.FailedPrecondition, "key version is not enabled, current state is: DISABLED"))

	result, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.False(t, result.Verified)
	require.Equal(t, "key_unusable", result.ProbeOutcome)
}

func TestVerifyGcpKmsKey_KeyNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	key := createGcpKmsKey(t, ctx, ti, "gone", credID)

	failWith(ti, status.Error(codes.NotFound, "CryptoKeyVersion not found"))

	result, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.False(t, result.Verified)
	require.Equal(t, "key_not_found", result.ProbeOutcome)
}

// Failing to build the client is Gram's problem, not the customer's, so it is a
// request error rather than a "not verified" result that would blame their
// configuration.
func TestVerifyGcpKmsKey_ClientBuildFailureIsRequestError(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	key := createGcpKmsKey(t, ctx, ti, "unbuildable", credID)

	ti.kms.SetBuild(func(_ context.Context, _ oauth2.TokenSource) (gcpkms.SigningClient, error) {
		return nil, errors.New("dial kms: no route to host")
	})

	_, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeUnexpected)
}

// Being unable to assume the credential's identity is the customer's to fix, so
// it reports rather than errors — and no KMS client is built for an identity
// that could not be resolved.
func TestVerifyGcpKmsKey_IdentityNotAssumable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	key := createGcpKmsKey(t, ctx, ti, "unreachable", credID)

	ti.gcpResolver.SetResolve(func(_ context.Context, cred gcpauth.Credential) (gcpauth.Principal, error) {
		if cred.ImpersonateServiceAccount == "" {
			return gcpauth.Principal{Email: gcpauth.StubResolverPrincipal, Source: gcpauth.SourceMetadataServer}, nil
		}
		return gcpauth.Principal{Email: "", Source: ""}, errors.New("caller does not have roles/iam.serviceAccountTokenCreator")
	})

	result, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.False(t, result.Verified)
	require.Equal(t, "credential_unusable", result.ProbeOutcome)
	require.Equal(t, 0, ti.kms.Closed(), "must not build a client for an identity it could not assume")
}

// An empty impersonation target would authenticate as Gram's own ambient
// identity, which reports on a key Gram reaches by itself rather than on the
// customer's configuration.
func TestVerifyGcpKmsKey_LegacyCredentialWithoutTarget(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredentialDirect(t, ctx, ti, "legacy-ambient", extcredrepo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      uuid.Nil,
		ImpersonateServiceAccount: pgtype.Text{String: "", Valid: false},
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
	})
	key := createGcpKmsKey(t, ctx, ti, "behind-ambient", credID)

	result, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.False(t, result.Verified)
	require.Equal(t, "credential_unusable", result.ProbeOutcome)
	require.Equal(t, 0, ti.kms.Closed(), "must not probe as gram's own ambient identity")
}

// A WIF row's real resolution mode is WIF, which gcpauth reports as unsupported,
// so probing its impersonation hop alone would claim the credential works when
// nothing else can use it.
func TestVerifyGcpKmsKey_LegacyWifCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredentialDirect(t, ctx, ti, "legacy-wif", extcredrepo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      uuid.Nil,
		ImpersonateServiceAccount: conv.ToPGText("hop@customer.iam.gserviceaccount.com"),
		WifPoolID:                 conv.ToPGText("gram-pool"),
		WifProviderID:             conv.ToPGText("gram-provider"),
		WifProjectNumber:          conv.ToPGText("123456789012"),
	})
	key := createGcpKmsKey(t, ctx, ti, "behind-wif", credID)

	result, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.False(t, result.Verified)
	require.Equal(t, "credential_unusable", result.ProbeOutcome)
	require.Equal(t, 0, ti.kms.Closed())
}

// The write-time screen postdates the rows it screens, so verify re-runs it.
// Without that, a credential created before it could name a service account in
// Gram's own project and turn this endpoint into an inventory oracle for Gram's
// own KMS: the caller supplies the resource name.
func TestVerifyGcpKmsKey_RescreensTargetInGramProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredentialDirect(t, ctx, ti, "legacy-unscreened", extcredrepo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      uuid.Nil,
		ImpersonateServiceAccount: conv.ToPGText(gramProjectServiceAccount("internal")),
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
	})
	key := createGcpKmsKey(t, ctx, ti, "behind-internal-sa", credID)

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

	result, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.False(t, result.Verified)
	require.Equal(t, "credential_unusable", result.ProbeOutcome)
	require.False(t, probed, "must not probe a target inside gram's own project")
	require.Equal(t, 0, ti.kms.Closed())
}

// A platform administrator can grant a credential an exemption from that same
// refusal, which is how Speakeasy staff dogfood the feature. This path has no
// request actor to consult, so the exemption has to be readable from the row:
// without it the credential would save and then be refused on every probe.
func TestVerifyGcpKmsKey_HonorsStoredProjectVerificationExemption(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredentialDirect(t, ctx, ti, "staff-exempted", extcredrepo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      uuid.Nil,
		ImpersonateServiceAccount: conv.ToPGText(gramProjectServiceAccount("internal")),
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
		SkipProjectVerification:   true,
	})
	key := createGcpKmsKey(t, ctx, ti, "behind-exempted-sa", credID)

	result, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.True(t, result.Verified)
	require.Equal(t, "verified", result.ProbeOutcome)
	require.Equal(t, 1, ti.kms.Closed())
}

// external_credentials.deleted is a generated column, so a soft delete never
// fires the external_keys foreign key. The delete path now refuses to orphan a
// key, but rows orphaned before that guard existed still resolve here, and they
// must say what actually happened rather than report the key as missing.
func TestVerifyGcpKmsKey_OrphanedByDeletedCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "doomed-cred")
	key := createGcpKmsKey(t, ctx, ti, "orphan", credID)
	softDeleteCredentialDirect(t, ctx, ti, credID)

	result, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.False(t, result.Verified)
	require.Equal(t, "credential_deleted", result.ProbeOutcome)
	require.Equal(t, 0, ti.kms.Closed())

	// The key itself is still very much there.
	_, err = ti.service.GetGcpKmsKey(adminCtx(t, ctx), &gen.GetGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
}

func TestVerifyGcpKmsKey_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// An aws_kms key is a real record, but this endpoint cannot probe it: Gram holds
// no AWS identity to assume a customer role from.
func TestVerifyGcpKmsKey_AwsKeyNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	awsCredID := createAwsIamCredential(t, ctx, ti, "aws-cred")
	awsKey := createAwsKmsKey(t, ctx, ti, "aws-key", awsCredID)

	_, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           awsKey.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestVerifyGcpKmsKey_RateLimited(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	key := createGcpKmsKey(t, ctx, ti, "hammered", credID)
	payload := &gen.VerifyGcpKmsKeyPayload{ID: key.ID, SessionToken: nil}
	admin := adminCtx(t, ctx)

	// The bucket holds verifyRateBurst tokens and refills far slower than this
	// loop runs, so a caller that spends them all is refused.
	var lastErr error
	for range 20 {
		if _, err := ti.service.VerifyGcpKmsKey(admin, payload); err != nil {
			lastErr = err
			break
		}
	}

	requireOopsCode(t, lastErr, oops.CodeRateLimitExceeded)
}

func TestVerifyGcpKmsKey_ForbiddenWithoutEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	key := createGcpKmsKey(t, ctx, ti, "gated", credID)
	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	_, err := ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
