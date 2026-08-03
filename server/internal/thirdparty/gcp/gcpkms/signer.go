package gcpkms

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"fmt"

	jose "github.com/go-jose/go-jose/v4"
)

// signer adapts a KMS key to go-jose's OpaqueSigner, so every JWS and JWT path
// in go-jose can sign with a key whose private half never leaves the provider.
type signer struct {
	// ctx bounds the KMS calls made by SignPayload. go-jose's OpaqueSigner takes
	// no context, so it has to live here; a signer is therefore request-scoped and
	// must not be cached across requests.
	//
	//nolint:containedctx // forced by jose.OpaqueSigner, whose SignPayload has no context parameter
	ctx          context.Context
	client       SigningClient
	resourceName string
	alg          jose.SignatureAlgorithm
	public       *jose.JSONWebKey
}

var _ jose.OpaqueSigner = (*signer)(nil)

// NewSigner builds a jose.OpaqueSigner backed by a KMS key version.
//
// The public key is supplied rather than fetched. GetPublicKey is a KMS
// management-tier operation whose quota sits far below that of the cryptographic
// operations, so a signer that fetched on construction would throttle on a
// per-signature path. Callers that do not already hold the public half can read
// it once via SigningClient.GetPublicKey and cache it.
//
// The returned signer is request-scoped: it captures ctx for the signing calls,
// and its client must outlive it.
func NewSigner(ctx context.Context, client SigningClient, resourceName, keyID string, public PublicKey) (jose.OpaqueSigner, error) {
	if err := ValidateKeyVersionName(resourceName); err != nil {
		return nil, err
	}

	if err := checkKeyMatchesAlgorithm(public); err != nil {
		return nil, err
	}

	return &signer{
		ctx:          ctx,
		client:       client,
		resourceName: resourceName,
		alg:          public.Algorithm,
		public: &jose.JSONWebKey{
			Key:                         public.Key,
			KeyID:                       keyID,
			Algorithm:                   string(public.Algorithm),
			Use:                         "sig",
			Certificates:                nil,
			CertificatesURL:             nil,
			CertificateThumbprintSHA1:   nil,
			CertificateThumbprintSHA256: nil,
		},
	}, nil
}

// checkKeyMatchesAlgorithm rejects a PublicKey whose key material could not have
// produced signatures for its stated algorithm.
//
// Callers are told to cache the public half separately from the configured
// algorithm, so the two can drift apart — and the consequence is silent: a
// mismatched pair would sign with one encoding while advertising another,
// producing tokens that fail verification everywhere with no error at mint time.
// Checking once at construction turns that into an immediate, obvious failure.
func checkKeyMatchesAlgorithm(public PublicKey) error {
	switch public.Algorithm {
	case jose.RS256:
		if _, ok := public.Key.(*rsa.PublicKey); !ok {
			return fmt.Errorf("%s key must be an *rsa.PublicKey, got %T", public.Algorithm, public.Key)
		}
		return nil

	case jose.ES256:
		pub, ok := public.Key.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("%s key must be an *ecdsa.PublicKey, got %T", public.Algorithm, public.Key)
		}
		// ES256 is P-256 by definition. A larger curve would produce wider R and S
		// halves than the algorithm's verifiers expect.
		if pub.Curve != elliptic.P256() {
			return fmt.Errorf("%s key must be on P-256, got %s", public.Algorithm, pub.Curve.Params().Name)
		}
		return nil

	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedAlgorithm, public.Algorithm)
	}
}

// Public returns the JWK go-jose publishes alongside the signature, which is
// where the `kid` in a JWS protected header comes from.
func (s *signer) Public() *jose.JSONWebKey {
	return s.public
}

// Algs reports the single algorithm this key signs with. A KMS key version has
// exactly one, so go-jose is never given a choice to get wrong.
func (s *signer) Algs() []jose.SignatureAlgorithm {
	return []jose.SignatureAlgorithm{s.alg}
}

// SignPayload hashes the payload, has KMS sign the digest, and returns the
// signature in the encoding JOSE expects. The payload itself never leaves the
// process — KMS only ever sees a digest.
func (s *signer) SignPayload(payload []byte, alg jose.SignatureAlgorithm) ([]byte, error) {
	if alg != s.alg {
		return nil, fmt.Errorf("%w: key signs %s, caller asked for %s", ErrUnsupportedAlgorithm, s.alg, alg)
	}

	digest, err := digestPayload(s.alg, payload)
	if err != nil {
		return nil, err
	}

	signature, err := s.client.AsymmetricSign(s.ctx, s.resourceName, s.alg, digest)
	if err != nil {
		return nil, fmt.Errorf("sign payload with %s: %w", s.resourceName, err)
	}

	// Dispatch on the algorithm rather than the key's Go type, so a signer can
	// never quietly fall through to the wrong encoding.
	switch s.alg {
	case jose.RS256:
		// RSA PKCS#1 v1.5 signatures are already in the encoding JOSE expects.
		return signature, nil

	case jose.ES256:
		// The provider returns ASN.1 DER; JOSE requires fixed-width R || S.
		pub, ok := s.public.Key.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("%s signer holds a %T public key", s.alg, s.public.Key)
		}

		joseSignature, err := ecdsaDERToJOSE(signature, coordinateBytes(pub))
		if err != nil {
			return nil, fmt.Errorf("convert ecdsa signature for %s: %w", s.resourceName, err)
		}
		return joseSignature, nil

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedAlgorithm, s.alg)
	}
}
