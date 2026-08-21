package externalcredentials_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
)

func TestVerifyGcpIamPlatformCredential_Resolves(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-verify")

	ti.gcpResolver.SetResolve(func(_ context.Context, _ gcpauth.Credential) (gcpauth.Principal, error) {
		return gcpauth.Principal{Email: "gram@gram-platform.iam.gserviceaccount.com", Source: gcpauth.SourceMetadataServer}, nil
	})

	result, err := ti.service.VerifyGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.VerifyGcpIamPlatformCredentialPayload{
		ID:           cred.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Verified)
	require.NotNil(t, result.Principal)
	require.Equal(t, "gram@gram-platform.iam.gserviceaccount.com", *result.Principal)
	require.NotNil(t, result.IdentitySource)
	require.Equal(t, "metadata_server", *result.IdentitySource)
	require.Nil(t, result.Detail, "a fully resolved principal needs no detail note")
}

// A resolve that succeeds without an email (e.g. user-backed local ADC) still
// counts as verified, with a detail note explaining the missing principal.
func TestVerifyGcpIamPlatformCredential_ResolvesWithoutEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-verify-noemail")

	ti.gcpResolver.SetResolve(func(_ context.Context, _ gcpauth.Credential) (gcpauth.Principal, error) {
		return gcpauth.Principal{Email: "", Source: gcpauth.SourceADC}, nil
	})

	result, err := ti.service.VerifyGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.VerifyGcpIamPlatformCredentialPayload{
		ID:           cred.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.True(t, result.Verified)
	require.Nil(t, result.Principal)
	require.NotNil(t, result.IdentitySource)
	require.Equal(t, "application_default_credentials", *result.IdentitySource)
	require.NotNil(t, result.Detail)
}

func TestVerifyGcpIamPlatformCredential_UnsupportedMode(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-verify-unsupported")

	ti.gcpResolver.SetResolve(func(_ context.Context, _ gcpauth.Credential) (gcpauth.Principal, error) {
		return gcpauth.Principal{}, gcpauth.ErrUnsupportedMode
	})

	result, err := ti.service.VerifyGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.VerifyGcpIamPlatformCredentialPayload{
		ID:           cred.ID,
		SessionToken: nil,
	})
	require.NoError(t, err, "an unresolvable probe is a reportable outcome, not a request error")
	require.False(t, result.Verified)
	require.Nil(t, result.Principal)
	require.Nil(t, result.IdentitySource)
	require.NotNil(t, result.Detail)
	require.Contains(t, *result.Detail, "ambient")
}

func TestVerifyGcpIamPlatformCredential_ResolveFailure(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-verify-fail")

	ti.gcpResolver.SetResolve(func(_ context.Context, _ gcpauth.Credential) (gcpauth.Principal, error) {
		return gcpauth.Principal{}, errors.New("metadata server unreachable")
	})

	result, err := ti.service.VerifyGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.VerifyGcpIamPlatformCredentialPayload{
		ID:           cred.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.False(t, result.Verified)
	require.NotNil(t, result.Detail)
}

func TestVerifyGcpIamPlatformCredential_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.VerifyGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.VerifyGcpIamPlatformCredentialPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
