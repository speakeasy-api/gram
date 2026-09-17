// Package dpop implements the client side of RFC 9449 (Demonstrating Proof of
// Possession): an ephemeral ES256 proof key, the proof JWT builder, and the
// header, claim, and token-type identifiers shared by every DPoP integration.
package dpop

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
)

const (
	// HeaderName carries the proof JWT on requests (RFC 9449 §4.1).
	HeaderName = "DPoP"

	// NonceHeaderName carries a server-provided nonce on responses (RFC 9449 §8, §9).
	NonceHeaderName = "DPoP-Nonce"

	// ProofType is the JOSE typ header of a proof JWT (RFC 9449 §4.2).
	ProofType = "dpop+jwt"

	// TokenType is the token_type value and Authorization scheme of a
	// DPoP-bound access token (RFC 9449 §5, §7.1).
	TokenType = "DPoP"

	// ErrorCodeUseNonce is the error a server returns when the proof must
	// carry a nonce from NonceHeaderName.
	ErrorCodeUseNonce = oautherr.CodeUseDPoPNonce

	// ClaimJTI is the unique proof identifier claim.
	ClaimJTI = "jti"

	// ClaimHTM is the HTTP method claim.
	ClaimHTM = "htm"

	// ClaimHTU is the target URI claim without query or fragment.
	ClaimHTU = "htu"

	// ClaimIAT is the issued-at claim.
	ClaimIAT = "iat"

	// ClaimATH is the access token hash claim (RFC 9449 §4.2).
	ClaimATH = "ath"

	// ClaimNonce is the server-provided nonce claim (RFC 9449 §8).
	ClaimNonce = "nonce"
)

// HTU normalizes target into the htu claim value: scheme, host, and path
// with query and fragment removed (RFC 9449 §4.2).
func HTU(target *url.URL) string {
	u := *target
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

// AccessTokenHash computes the ath claim: base64url SHA-256 of the token.
func AccessTokenHash(accessToken string) string {
	sum := sha256.Sum256([]byte(accessToken))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Key is an ephemeral ES256 proof key. Every proof it signs embeds the
// public JWK so the server can bind tokens to its thumbprint.
type Key struct {
	signer     jose.Signer
	thumbprint string
}

// NewKey generates a fresh P-256 key and signer.
func NewKey() (*Key, error) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate dpop key: %w", err)
	}

	public := jose.JSONWebKey{
		Key:                         &private.PublicKey,
		KeyID:                       "",
		Algorithm:                   string(jose.ES256),
		Use:                         "sig",
		Certificates:                nil,
		CertificatesURL:             nil,
		CertificateThumbprintSHA1:   nil,
		CertificateThumbprintSHA256: nil,
	}
	thumb, err := public.Thumbprint(crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("compute dpop key thumbprint: %w", err)
	}

	opts := (&jose.SignerOptions{NonceSource: nil, EmbedJWK: true, ExtraHeaders: nil}).WithType(ProofType)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: private}, opts)
	if err != nil {
		return nil, fmt.Errorf("configure dpop signer: %w", err)
	}

	return &Key{signer: signer, thumbprint: base64.RawURLEncoding.EncodeToString(thumb)}, nil
}

// Thumbprint is the base64url SHA-256 JWK thumbprint (RFC 7638) that a
// DPoP-bound token's cnf.jkt must match.
func (k *Key) Thumbprint() string {
	return k.thumbprint
}

// ProofOptions are the per-request inputs to a proof.
type ProofOptions struct {
	// AccessToken, when non-empty, binds the proof to that token via ath.
	AccessToken string

	// Nonce, when non-empty, is echoed in the nonce claim.
	Nonce string

	// IssuedAt is the iat claim.
	IssuedAt time.Time
}

// Proof signs one proof JWT for method and target.
func (k *Key) Proof(method string, target *url.URL, opts ProofOptions) (string, error) {
	claims := map[string]any{
		ClaimJTI: uuid.NewString(),
		ClaimHTM: method,
		ClaimHTU: HTU(target),
		ClaimIAT: opts.IssuedAt.Unix(),
	}
	if opts.Nonce != "" {
		claims[ClaimNonce] = opts.Nonce
	}
	if opts.AccessToken != "" {
		claims[ClaimATH] = AccessTokenHash(opts.AccessToken)
	}

	serialized, err := jwt.Signed(k.signer).Claims(claims).Serialize()
	if err != nil {
		return "", fmt.Errorf("sign dpop proof: %w", err)
	}
	return serialized, nil
}

// NonceCache retains the most recent server-provided nonce so later proofs
// can carry it. The zero value is ready to use.
type NonceCache struct {
	mu    sync.Mutex
	nonce string
}

// Remember stores the NonceHeaderName value from header, if present.
func (c *NonceCache) Remember(header http.Header) {
	nonce := header.Get(NonceHeaderName)
	if nonce == "" {
		return
	}
	c.mu.Lock()
	c.nonce = nonce
	c.mu.Unlock()
}

// Current returns the last remembered nonce, or "" before any was seen.
func (c *NonceCache) Current() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nonce
}

// IsUseNonceChallenge reports whether a response demands a nonce: it must
// carry NonceHeaderName and name ErrorCodeUseNonce in either WWW-Authenticate
// (protected resource) or the JSON error body (authorization server).
func IsUseNonceChallenge(header http.Header, body []byte) bool {
	if header.Get(NonceHeaderName) == "" {
		return false
	}
	if strings.Contains(header.Get("WWW-Authenticate"), ErrorCodeUseNonce) {
		return true
	}
	var parsed struct {
		Error string `json:"error"`
	}
	return json.Unmarshal(body, &parsed) == nil && parsed.Error == ErrorCodeUseNonce
}
