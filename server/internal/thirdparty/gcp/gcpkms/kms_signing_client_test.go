package gcpkms

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"

	kms "cloud.google.com/go/kms/apiv1"
	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// GCP rejects a digest whose length does not match the field it was placed in,
// so the width is checked locally to turn a remote InvalidArgument into a clear
// error at the call site.
func TestProtoDigest_RejectsWrongLengthDigest(t *testing.T) {
	t.Parallel()

	_, err := protoDigest(jose.RS256, []byte("too short"))
	require.ErrorContains(t, err, "expected 32")
}

func TestProtoDigest_PlacesSHA256InTheSHA256Field(t *testing.T) {
	t.Parallel()

	digest := make([]byte, sha256.Size)
	digest[0] = 0xab

	pb, err := protoDigest(jose.RS256, digest)
	require.NoError(t, err)
	require.Equal(t, digest, pb.GetSha256(), "a SHA-256 digest must land in the sha256 oneof arm")
}

// GCP's integrity guards use CRC-32C (Castagnoli), not the far more common
// IEEE polynomial. Substituting IEEE would leave every test in this package
// passing while every real request was rejected for a checksum mismatch, so the
// polynomial is pinned against the published CRC-32C check vector.
func TestCRC32C_UsesCastagnoliPolynomial(t *testing.T) {
	t.Parallel()

	require.Equal(t, int64(0xE3069283), crc32c([]byte("123456789")))
}

// fakeKMS is an in-process KeyManagementService. It exists so the real transport
// — request construction, the integrity guards, and PEM parsing — is testable
// without a GCP network path. Fields are set by each test to shape the response.
type fakeKMS struct {
	kmspb.UnimplementedKeyManagementServiceServer

	publicKey    *kmspb.PublicKey
	publicKeyErr error
	signResponse *kmspb.AsymmetricSignResponse
	signErr      error

	gotPublicKeyRequest *kmspb.GetPublicKeyRequest
	gotSignRequest      *kmspb.AsymmetricSignRequest
}

func (f *fakeKMS) GetPublicKey(_ context.Context, req *kmspb.GetPublicKeyRequest) (*kmspb.PublicKey, error) {
	f.gotPublicKeyRequest = req
	if f.publicKeyErr != nil {
		return nil, f.publicKeyErr
	}
	return f.publicKey, nil
}

func (f *fakeKMS) AsymmetricSign(_ context.Context, req *kmspb.AsymmetricSignRequest) (*kmspb.AsymmetricSignResponse, error) {
	f.gotSignRequest = req
	if f.signErr != nil {
		return nil, f.signErr
	}
	return f.signResponse, nil
}

// newFakeSigningClient wires the real signingClient to an in-process fake over
// bufconn, so every code path except the network itself is exercised.
func newFakeSigningClient(t *testing.T, fake *fakeKMS) SigningClient {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	kmspb.RegisterKeyManagementServiceServer(server, fake)

	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	raw, err := kms.NewKeyManagementClient(t.Context(), option.WithGRPCConn(conn))
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })

	return &kmsSigningClient{kms: raw}
}

// testPublicKeyPEM returns a valid RSA public key PEM and its parsed form.
func testPublicKeyPEM(t *testing.T) (string, *rsa.PublicKey) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)

	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Headers: nil, Bytes: der})), &key.PublicKey
}

func healthyPublicKey(t *testing.T) (*kmspb.PublicKey, *rsa.PublicKey) {
	t.Helper()

	pemData, parsed := testPublicKeyPEM(t)
	return &kmspb.PublicKey{
		Pem:       pemData,
		PemCrc32C: wrapperspb.Int64(crc32c([]byte(pemData))),
		Algorithm: kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_2048_SHA256,
		Name:      testResourceName,
	}, parsed
}

// The public key format MUST be left unspecified. Specifying one — even PEM —
// makes GCP populate the response's public_key field and leave pem empty, which
// would make every GetPublicKey call fail against real KMS while every fake in
// this package still passed.
func TestGetPublicKey_LeavesFormatUnspecified(t *testing.T) {
	t.Parallel()

	pk, _ := healthyPublicKey(t)
	fake := &fakeKMS{publicKey: pk}
	client := newFakeSigningClient(t, fake)

	_, err := client.GetPublicKey(t.Context(), testResourceName)
	require.NoError(t, err)

	require.Equal(t,
		kmspb.PublicKey_PUBLIC_KEY_FORMAT_UNSPECIFIED,
		fake.gotPublicKeyRequest.GetPublicKeyFormat(),
		"specifying a format moves the key out of the pem field this client reads",
	)
	require.Equal(t, testResourceName, fake.gotPublicKeyRequest.GetName())
}

func TestGetPublicKey_ParsesPEMAndAlgorithm(t *testing.T) {
	t.Parallel()

	pk, parsed := healthyPublicKey(t)
	client := newFakeSigningClient(t, &fakeKMS{publicKey: pk})

	public, err := client.GetPublicKey(t.Context(), testResourceName)
	require.NoError(t, err)
	require.Equal(t, jose.RS256, public.Algorithm)
	require.Equal(t, parsed, public.Key)
}

func TestGetPublicKey_RejectsChecksumMismatch(t *testing.T) {
	t.Parallel()

	pk, _ := healthyPublicKey(t)
	pk.PemCrc32C = wrapperspb.Int64(crc32c([]byte("something else")))
	client := newFakeSigningClient(t, &fakeKMS{publicKey: pk})

	_, err := client.GetPublicKey(t.Context(), testResourceName)
	require.ErrorContains(t, err, "pem checksum mismatch")
}

func TestGetPublicKey_RejectsMissingChecksum(t *testing.T) {
	t.Parallel()

	pk, _ := healthyPublicKey(t)
	pk.PemCrc32C = nil
	client := newFakeSigningClient(t, &fakeKMS{publicKey: pk})

	_, err := client.GetPublicKey(t.Context(), testResourceName)
	require.ErrorContains(t, err, "omitted the pem checksum")
}

// The checksums prove the bytes survived the wire, not that they describe the
// key that was asked for. GCP documents the name field as the guard for that.
func TestGetPublicKey_RejectsResponseForADifferentKey(t *testing.T) {
	t.Parallel()

	pk, _ := healthyPublicKey(t)
	pk.Name = "projects/p/locations/l/keyRings/r/cryptoKeys/other/cryptoKeyVersions/9"
	client := newFakeSigningClient(t, &fakeKMS{publicKey: pk})

	_, err := client.GetPublicKey(t.Context(), testResourceName)
	require.ErrorContains(t, err, "response describes")
}

func TestGetPublicKey_RejectsUnsupportedAlgorithm(t *testing.T) {
	t.Parallel()

	pk, _ := healthyPublicKey(t)
	pk.Algorithm = kmspb.CryptoKeyVersion_RSA_SIGN_PSS_2048_SHA256
	client := newFakeSigningClient(t, &fakeKMS{publicKey: pk})

	_, err := client.GetPublicKey(t.Context(), testResourceName)
	require.ErrorIs(t, err, ErrUnsupportedAlgorithm)
	require.ErrorContains(t, err, "PS256")
}

func TestAsymmetricSign_SendsDigestAndChecksum(t *testing.T) {
	t.Parallel()

	digest := sha256.Sum256([]byte(ProbePayload))
	signature := []byte("signature-bytes")

	fake := &fakeKMS{signResponse: &kmspb.AsymmetricSignResponse{
		Name:                 testResourceName,
		Signature:            signature,
		SignatureCrc32C:      wrapperspb.Int64(crc32c(signature)),
		VerifiedDigestCrc32C: true,
	}}
	client := newFakeSigningClient(t, fake)

	got, err := client.AsymmetricSign(t.Context(), testResourceName, jose.RS256, digest[:])
	require.NoError(t, err)
	require.Equal(t, signature, got)

	require.Equal(t, digest[:], fake.gotSignRequest.GetDigest().GetSha256())
	require.Equal(t, crc32c(digest[:]), fake.gotSignRequest.GetDigestCrc32C().GetValue())
}

func TestAsymmetricSign_RejectsUnverifiedDigestChecksum(t *testing.T) {
	t.Parallel()

	digest := sha256.Sum256([]byte(ProbePayload))
	signature := []byte("signature-bytes")

	client := newFakeSigningClient(t, &fakeKMS{signResponse: &kmspb.AsymmetricSignResponse{
		Name:                 testResourceName,
		Signature:            signature,
		SignatureCrc32C:      wrapperspb.Int64(crc32c(signature)),
		VerifiedDigestCrc32C: false,
	}})

	_, err := client.AsymmetricSign(t.Context(), testResourceName, jose.RS256, digest[:])
	require.ErrorContains(t, err, "could not verify the digest checksum")
}

func TestAsymmetricSign_RejectsSignatureChecksumMismatch(t *testing.T) {
	t.Parallel()

	digest := sha256.Sum256([]byte(ProbePayload))

	client := newFakeSigningClient(t, &fakeKMS{signResponse: &kmspb.AsymmetricSignResponse{
		Name:                 testResourceName,
		Signature:            []byte("signature-bytes"),
		SignatureCrc32C:      wrapperspb.Int64(crc32c([]byte("different"))),
		VerifiedDigestCrc32C: true,
	}})

	_, err := client.AsymmetricSign(t.Context(), testResourceName, jose.RS256, digest[:])
	require.ErrorContains(t, err, "signature checksum mismatch")
}

func TestAsymmetricSign_RejectsSignatureFromADifferentKey(t *testing.T) {
	t.Parallel()

	digest := sha256.Sum256([]byte(ProbePayload))
	signature := []byte("signature-bytes")

	client := newFakeSigningClient(t, &fakeKMS{signResponse: &kmspb.AsymmetricSignResponse{
		Name:                 "projects/p/locations/l/keyRings/r/cryptoKeys/other/cryptoKeyVersions/9",
		Signature:            signature,
		SignatureCrc32C:      wrapperspb.Int64(crc32c(signature)),
		VerifiedDigestCrc32C: true,
	}})

	_, err := client.AsymmetricSign(t.Context(), testResourceName, jose.RS256, digest[:])
	require.ErrorContains(t, err, "response signed with")
}

// A gRPC status must survive the client's error wrapping, because VerifySigningKey
// classifies on it to tell a missing IAM grant apart from a transient outage.
func TestSigningClientErrors_PreserveGRPCStatus(t *testing.T) {
	t.Parallel()

	client := newFakeSigningClient(t, &fakeKMS{
		publicKeyErr: status.Error(codes.PermissionDenied, "permission denied on resource"),
	})

	_, err := client.GetPublicKey(t.Context(), testResourceName)
	require.Error(t, err)

	sts, ok := status.FromError(err)
	require.True(t, ok, "wrapping must not hide the gRPC status")
	require.Equal(t, codes.PermissionDenied, sts.Code())
}
