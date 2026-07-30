package gcpkms

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"hash/crc32"

	kms "cloud.google.com/go/kms/apiv1"
	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// castagnoli is the CRC-32C polynomial GCP KMS uses for its integrity checks.
var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// crc32c returns the CRC-32C of data as the int64 the KMS wrappers use. The
// conversion is lossless: a CRC is 32 bits, so it always fits.
func crc32c(data []byte) int64 {
	return int64(crc32.Checksum(data, castagnoli))
}

var _ SigningClient = (*kmsSigningClient)(nil)

// kmsSigningClient is the SigningClient backed by real GCP Cloud KMS.
type kmsSigningClient struct {
	kms *kms.KeyManagementClient
}

// NewSigningClient opens an authenticated GCP KMS client. The caller owns its
// lifetime and MUST Close it: each client holds a gRPC connection, so a missed
// Close leaks one silently.
func NewSigningClient(ctx context.Context, tokenSource oauth2.TokenSource) (SigningClient, error) {
	c, err := kms.NewKeyManagementClient(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("build gcp kms client: %w", err)
	}

	return &kmsSigningClient{kms: c}, nil
}

// Close releases the underlying gRPC connection. It is not optional: each
// client holds its own connection, so skipping Close leaks one per call site
// with no visible symptom until the process runs out of them.
func (c *kmsSigningClient) Close() error {
	if err := c.kms.Close(); err != nil {
		return fmt.Errorf("close gcp kms client: %w", err)
	}

	return nil
}

// GetPublicKey reads a key version's public half and reports the JOSE algorithm
// it actually signs with, which is what lets callers detect a key pointed at a
// row configured for a different algorithm.
//
// It fails rather than returning a key when the algorithm is one Gram does not
// publish (ErrUnsupportedAlgorithm), when the PEM checksum does not match, or
// when the PEM cannot be parsed. Note this is a KMS management-tier operation:
// its quota is far below that of the cryptographic operations, so it is not
// safe to call on a per-signature path.
func (c *kmsSigningClient) GetPublicKey(ctx context.Context, resourceName string) (*PublicKey, error) {
	if err := ValidateKeyVersionName(resourceName); err != nil {
		return nil, err
	}

	// The format MUST be left unspecified. Per the API contract, specifying one
	// (even PublicKey_PEM) routes the key into the response's public_key field
	// instead, and leaves pem / pem_crc32c empty — which the code below reads.
	// Unspecified is what populates pem for non-PQC algorithms.
	resp, err := c.kms.GetPublicKey(ctx, &kmspb.GetPublicKeyRequest{
		Name:            resourceName,
		PublicKeyFormat: kmspb.PublicKey_PUBLIC_KEY_FORMAT_UNSPECIFIED,
	})
	if err != nil {
		return nil, fmt.Errorf("get gcp kms public key: %w", err)
	}

	// GCP returns a CRC-32C over the PEM so callers can detect corruption in
	// transit. An absent checksum means the field was not populated, which is not
	// the same as the payload being trustworthy.
	pemData := resp.GetPem()
	checksum := resp.GetPemCrc32C()
	switch {
	case checksum == nil:
		return nil, errors.New("get gcp kms public key: response omitted the pem checksum")
	case checksum.GetValue() != crc32c([]byte(pemData)):
		return nil, errors.New("get gcp kms public key: pem checksum mismatch, response corrupted in transit")
	}

	// GCP asks callers to confirm the response describes the key they requested;
	// the checksums only prove the bytes arrived intact, not that they came from
	// the right resource.
	if got := resp.GetName(); got != resourceName {
		return nil, fmt.Errorf("get gcp kms public key: response describes %q, requested %q", got, resourceName)
	}

	alg, err := joseAlgorithm(resp.GetAlgorithm())
	if err != nil {
		return nil, err
	}

	key, err := parsePublicKeyPEM(pemData)
	if err != nil {
		return nil, err
	}

	return &PublicKey{Algorithm: alg, Key: key}, nil
}

// AsymmetricSign signs a digest and returns the signature in the provider's own
// encoding: PKCS#1 v1.5 for RSA, ASN.1 DER for ECDSA. Converting DER to the
// fixed-width form JOSE requires is the signer's job, not this layer's.
//
// The digest must already be hashed with the algorithm's hash and be exactly
// that hash's width; KMS never sees the payload. Both CRC-32C guards GCP
// provides are enforced, so a request whose digest arrived corrupted, or a
// response whose signature did, is an error rather than a bad signature handed
// back to the caller.
func (c *kmsSigningClient) AsymmetricSign(ctx context.Context, resourceName string, alg jose.SignatureAlgorithm, digest []byte) ([]byte, error) {
	if err := ValidateKeyVersionName(resourceName); err != nil {
		return nil, err
	}

	pbDigest, err := protoDigest(alg, digest)
	if err != nil {
		return nil, err
	}

	resp, err := c.kms.AsymmetricSign(ctx, &kmspb.AsymmetricSignRequest{
		Name:         resourceName,
		Digest:       pbDigest,
		DigestCrc32C: wrapperspb.Int64(crc32c(digest)),
		Data:         nil,
		DataCrc32C:   nil,
	})
	if err != nil {
		return nil, fmt.Errorf("gcp kms asymmetric sign: %w", err)
	}

	// Confirm the signature came from the key version that was asked for, as GCP
	// documents; the checksums below only prove the bytes survived the wire.
	if got := resp.GetName(); got != resourceName {
		return nil, fmt.Errorf("gcp kms asymmetric sign: response signed with %q, requested %q", got, resourceName)
	}

	// GCP echoes back whether it received an intact digest, plus a checksum over
	// the signature. Both must hold, or something was corrupted on the wire.
	if !resp.GetVerifiedDigestCrc32C() {
		return nil, errors.New("gcp kms asymmetric sign: server could not verify the digest checksum, request corrupted in transit")
	}

	signature := resp.GetSignature()
	sigChecksum := resp.GetSignatureCrc32C()
	switch {
	case sigChecksum == nil:
		return nil, errors.New("gcp kms asymmetric sign: response omitted the signature checksum")
	case sigChecksum.GetValue() != crc32c(signature):
		return nil, errors.New("gcp kms asymmetric sign: signature checksum mismatch, response corrupted in transit")
	}

	return signature, nil
}

// protoDigest wraps a digest in the request field matching its hash. GCP rejects
// a digest placed in the wrong field, so the length check here turns a remote
// InvalidArgument into a local, explicit error.
func protoDigest(alg jose.SignatureAlgorithm, digest []byte) (*kmspb.Digest, error) {
	hash, err := digestHash(alg)
	if err != nil {
		return nil, err
	}

	if len(digest) != hash.Size() {
		return nil, fmt.Errorf("digest is %d bytes, expected %d for %s", len(digest), hash.Size(), alg)
	}

	switch hash {
	case crypto.SHA256:
		return &kmspb.Digest{Digest: &kmspb.Digest_Sha256{Sha256: digest}}, nil
	default:
		return nil, fmt.Errorf("%w: no kms digest field for %s", ErrUnsupportedAlgorithm, alg)
	}
}
