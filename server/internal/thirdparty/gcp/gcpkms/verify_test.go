package gcpkms

import (
	"context"
	"fmt"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestVerifySigningKey_RS256(t *testing.T) {
	t.Parallel()

	client, err := NewLocalSigningClient(jose.RS256)
	require.NoError(t, err)

	result := VerifySigningKey(t.Context(), client, testResourceName, jose.RS256)
	require.True(t, result.Verified, "detail: %s", result.Detail)
	require.Equal(t, ReasonVerified, result.Reason)
	require.Equal(t, jose.RS256, result.Algorithm)
	require.Empty(t, result.Detail)
}

func TestVerifySigningKey_ES256(t *testing.T) {
	t.Parallel()

	client, err := NewLocalSigningClient(jose.ES256)
	require.NoError(t, err)

	result := VerifySigningKey(t.Context(), client, testResourceName, jose.ES256)
	require.True(t, result.Verified, "detail: %s", result.Detail)
	require.Equal(t, ReasonVerified, result.Reason)
	require.Equal(t, jose.ES256, result.Algorithm)
	require.Empty(t, result.Detail)
}

// The stored algorithm drives how Gram advertises the key, so a healthy key
// configured as the wrong algorithm must fail loudly and report what it is.
func TestVerifySigningKey_ReportsAlgorithmMismatch(t *testing.T) {
	t.Parallel()

	client, err := NewLocalSigningClient(jose.ES256)
	require.NoError(t, err)

	result := VerifySigningKey(t.Context(), client, testResourceName, jose.RS256)
	require.False(t, result.Verified)
	require.Equal(t, ReasonAlgorithmMismatch, result.Reason)
	require.Equal(t, jose.ES256, result.Algorithm, "the key's real algorithm must be reported")
	require.Contains(t, result.Detail, "ES256")
	require.Contains(t, result.Detail, "RS256")
}

func TestVerifySigningKey_ReportsInvalidResourceName(t *testing.T) {
	t.Parallel()

	client, err := NewLocalSigningClient(jose.RS256)
	require.NoError(t, err)

	result := VerifySigningKey(t.Context(), client, "not-a-resource-name", jose.RS256)
	require.False(t, result.Verified)
	require.Equal(t, ReasonInvalidResourceName, result.Reason)
	require.Empty(t, result.Algorithm)
	require.Contains(t, result.Detail, "invalid gcp kms resource name")
}

// tamperingClient returns a signature that does not correspond to the digest,
// standing in for a key whose public half no longer matches what signs.
type tamperingClient struct {
	SigningClient
}

func (c tamperingClient) AsymmetricSign(ctx context.Context, resourceName string, alg jose.SignatureAlgorithm, digest []byte) ([]byte, error) {
	signature, err := c.SigningClient.AsymmetricSign(ctx, resourceName, alg, digest)
	if err != nil {
		return nil, fmt.Errorf("tampering client: %w", err)
	}

	signature[len(signature)-1] ^= 0xff
	return signature, nil
}

func TestVerifySigningKey_RejectsSignatureThatDoesNotValidate(t *testing.T) {
	t.Parallel()

	local, err := NewLocalSigningClient(jose.RS256)
	require.NoError(t, err)

	result := VerifySigningKey(t.Context(), tamperingClient{SigningClient: local}, testResourceName, jose.RS256)
	require.False(t, result.Verified)
	require.Equal(t, ReasonSignatureInvalid, result.Reason)
	require.Contains(t, result.Detail, "did not verify against the key's own public half")
}

// unreachableClient fails GetPublicKey with a caller-supplied error, standing in
// for the ways a real key can be out of reach. The error is wrapped, mirroring
// the real client, so classification is exercised through the wrapping too.
type unreachableClient struct {
	SigningClient
	err error
}

func (c unreachableClient) GetPublicKey(context.Context, string) (*PublicKey, error) {
	return nil, fmt.Errorf("get gcp kms public key: %w", c.err)
}

// The reason is what lets a caller tell "the customer must fix their IAM" apart
// from "retry this". Collapsing both into Detail would have the dashboard tell a
// customer to change their permissions during a transient outage.
func TestVerifySigningKey_ClassifiesProviderFailures(t *testing.T) {
	t.Parallel()

	local, err := NewLocalSigningClient(jose.RS256)
	require.NoError(t, err)

	for _, tc := range []struct {
		err  error
		want VerifyReason
	}{
		{status.Error(codes.PermissionDenied, "permission denied on resource"), ReasonPermissionDenied},
		{status.Error(codes.Unauthenticated, "invalid credentials"), ReasonPermissionDenied},
		{status.Error(codes.NotFound, "key version not found"), ReasonKeyNotFound},
		{status.Error(codes.FailedPrecondition, "key version is not enabled"), ReasonKeyUnusable},
		{status.Error(codes.Unavailable, "backend unavailable"), ReasonUnavailable},
		{status.Error(codes.DeadlineExceeded, "context deadline exceeded"), ReasonUnavailable},
		{status.Error(codes.ResourceExhausted, "quota exceeded"), ReasonUnavailable},
		{status.Error(codes.InvalidArgument, "malformed request"), ReasonUnexpected},
		{ErrUnsupportedAlgorithm, ReasonUnsupportedAlgorithm},

		// Bare context errors carry no gRPC status. Without explicit handling they
		// would classify as unexpected, and a caller would not retry a timeout.
		{context.DeadlineExceeded, ReasonUnavailable},
		{context.Canceled, ReasonUnavailable},
	} {
		client := unreachableClient{SigningClient: local, err: tc.err}

		result := VerifySigningKey(t.Context(), client, testResourceName, jose.RS256)
		require.False(t, result.Verified)
		require.Equal(t, tc.want, result.Reason, "classifying %v", tc.err)
		require.NotEmpty(t, result.Detail, "a negative result must always explain itself")
	}
}
