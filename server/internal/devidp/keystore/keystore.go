// Package keystore owns the dev-idp's single RSA keypair (idp-design.md
// §5.3): the only signing key the dev-idp uses, sourced from
// GRAM_DEVIDP_RSA_PRIVATE_KEY (PEM) at boot or freshly generated when the
// env var is unset. Used by the OIDC-shaped modes (oauth2-1, oauth2) for
// id_token signing and JWKS publication.
//
// `GRAM_JWT_SIGNING_KEY` is HS256 (symmetric) and is intentionally NOT
// consumed here — JWKS callers need the public half, which a symmetric
// key cannot provide.
package keystore

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const rsaKeyBits = 2048

// Keystore holds the dev-idp's RSA keypair plus a stable KID derived from
// the public-key SPKI DER digest. The KID is published in JWKS responses
// and embedded in every signed id_token's `kid` header.
type Keystore struct {
	private *rsa.PrivateKey
	public  *rsa.PublicKey
	kid     string
	logger  *slog.Logger
}

// New parses a PEM-encoded RSA private key (PKCS#8 or PKCS#1) when
// `pemBytes` is non-empty, otherwise generates a fresh 2048-bit keypair.
// The freshly-generated path is the dev-idp's default; tests that need a
// stable JWKS across restarts pass `--rsa-private-key`.
func New(pemBytes []byte, logger *slog.Logger) (*Keystore, error) {
	logger = logger.With(attr.SlogComponent("devidp.keystore"))

	priv, err := loadOrGenerate(pemBytes)
	if err != nil {
		return nil, err
	}

	pub, ok := priv.Public().(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("dev-idp keypair: public key is not RSA")
	}

	kid, err := computeKID(pub)
	if err != nil {
		return nil, fmt.Errorf("derive kid: %w", err)
	}

	return &Keystore{private: priv, public: pub, kid: kid, logger: logger}, nil
}

// PrivateKey returns the signing key. Used by OIDC modes to RS256-sign
// id_tokens.
func (k *Keystore) PrivateKey() *rsa.PrivateKey {
	return k.private
}

// KID is the JWK key id; it appears in every signed id_token's `kid`
// header so verifiers can pick the right key from the JWKS response.
func (k *Keystore) KID() string {
	return k.kid
}

// SigningMethod is RS256, the only algorithm the dev-idp signs with.
func (k *Keystore) SigningAlg() string {
	return "RS256"
}

// Signer adapts the private key for callers that take a crypto.Signer
// (e.g. JWT libraries).
func (k *Keystore) Signer() crypto.Signer {
	return k.private
}

// JWKSHandler returns an http.Handler that serves the RFC 7517 JWKS
// document for the public half of the keypair. Each OIDC mode mounts
// the same handler under its own /.well-known/jwks.json; the KID is
// shared across modes by design.
func (k *Keystore) JWKSHandler() http.Handler {
	doc := jwksDocument{
		Keys: []jwk{{
			Kty: "RSA",
			Use: "sig",
			Alg: k.SigningAlg(),
			Kid: k.kid,
			N:   base64URLBigInt(k.public.N),
			E:   base64URLInt(k.public.E),
		}},
	}
	body, _ := json.Marshal(doc)

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(body)
	})
}

// =============================================================================
// Internals
// =============================================================================

func loadOrGenerate(pemBytes []byte) (*rsa.PrivateKey, error) {
	if len(pemBytes) == 0 {
		key, err := rsa.GenerateKey(rand.Reader, rsaKeyBits)
		if err != nil {
			return nil, fmt.Errorf("generate rsa key: %w", err)
		}
		return key, nil
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("dev-idp keypair: PEM block not found")
	}

	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("dev-idp keypair: PKCS#8 key is not RSA")
		}
		return rsaKey, nil
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse rsa key (tried PKCS#8 and PKCS#1): %w", err)
	}
	return key, nil
}

// computeKID derives a stable KID from the SHA-256 digest of the SPKI
// DER encoding of the public key. Stability across boots requires a
// stable input keypair (i.e. `--rsa-private-key` was supplied).
func computeKID(pub *rsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("marshal public key: %w", err)
	}
	digest := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func base64URLBigInt(n *big.Int) string {
	return base64.RawURLEncoding.EncodeToString(n.Bytes())
}

func base64URLInt(i int) string {
	v := big.NewInt(int64(i))
	return base64URLBigInt(v)
}

// =============================================================================
// JWKS wire types (RFC 7517)
// =============================================================================

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}
