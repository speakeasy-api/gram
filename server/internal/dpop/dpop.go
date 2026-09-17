// Package dpop implements the client side of RFC 9449, OAuth 2.0
// Demonstrating Proof of Possession: an ephemeral ES256 proof key, the proof
// JWT builder (§4), nonce handling for authorization and resource servers
// (§8, §9), and the header, claim, error, and token-type identifiers the RFC
// registers (§12). Proof verification (§4.3) is a server concern and lives
// with whichever service accepts DPoP-bound tokens. Authorization code
// binding (§10) does not apply: every consumer here uses a direct grant.
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
	// HeaderName carries the proof JWT on requests (RFC 9449 §4.1, §12.8).
	HeaderName = "DPoP"

	// NonceHeaderName carries a server-provided nonce on responses (RFC 9449 §8, §9, §12.8).
	NonceHeaderName = "DPoP-Nonce"

	// ProofType is the JOSE typ header of a proof JWT (RFC 9449 §4.2, §12.5).
	ProofType = "dpop+jwt"

	// TokenType is the token_type value and Authorization scheme of a
	// DPoP-bound access token (RFC 9449 §5, §7.1, §12.1, §12.4).
	TokenType = "DPoP"

	// ErrorCodeUseNonce is the error a server returns when the proof must
	// carry a nonce from NonceHeaderName (RFC 9449 §8, §9, §12.2).
	ErrorCodeUseNonce = oautherr.CodeUseDPoPNonce

	// ClaimJTI is the unique proof identifier claim (RFC 9449 §4.2).
	ClaimJTI = "jti"

	// ClaimHTM is the HTTP method claim (RFC 9449 §4.2).
	ClaimHTM = "htm"

	// ClaimHTU is the target URI claim without query or fragment (RFC 9449 §4.2).
	ClaimHTU = "htu"

	// ClaimIAT is the issued-at claim (RFC 9449 §4.2).
	ClaimIAT = "iat"

	// ClaimATH is the access token hash claim (RFC 9449 §4.2, §7).
	ClaimATH = "ath"

	// ClaimNonce is the server-provided nonce claim (RFC 9449 §4.2, §8).
	ClaimNonce = "nonce"
)

// HTU normalizes target into the htu claim value: the target URI without
// query and fragment (RFC 9449 §4.2), emitted in the canonical form servers
// compare against after RFC 3986 §6.2.2 and §6.2.3 normalization (§4.3).
func HTU(target *url.URL) string {
	u := *target
	// RFC 9449 §4.2: htu carries no query or fragment.
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	// RFC 9110 §7.1: a target URI never carries userinfo.
	u.User = nil
	// RFC 3986 §6.2.2.1: scheme and host are case-insensitive, so emit lowercase.
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	// RFC 3986 §6.2.3: drop the scheme's default port and use "/" for an empty path.
	if port := u.Port(); (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		u.Host = strings.TrimSuffix(u.Host, ":"+port)
	}
	if u.Host != "" && u.Path == "" {
		u.Path = "/"
		u.RawPath = ""
	}
	return u.String()
}

// AccessTokenHash computes the ath claim: unpadded base64url of the SHA-256
// of the token's ASCII bytes (RFC 9449 §4.2).
func AccessTokenHash(accessToken string) string {
	sum := sha256.Sum256([]byte(accessToken))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Key is an ephemeral ES256 proof key. Every proof it signs embeds the
// public JWK so the server can bind tokens to its thumbprint (RFC 9449 §6.1).
// The private key never leaves the signer, and a fresh Key per client keeps
// unrelated clients from sharing a binding.
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

	// RFC 9449 §4.2: typ is dpop+jwt and jwk embeds only the public key; §11.6: ES256 is an asymmetric alg, never none or a MAC.
	opts := (&jose.SignerOptions{NonceSource: nil, EmbedJWK: true, ExtraHeaders: nil}).WithType(ProofType)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: private}, opts)
	if err != nil {
		return nil, fmt.Errorf("configure dpop signer: %w", err)
	}

	return &Key{signer: signer, thumbprint: base64.RawURLEncoding.EncodeToString(thumb)}, nil
}

// Thumbprint is the base64url SHA-256 JWK thumbprint (RFC 7638) that a
// DPoP-bound token's cnf.jkt must match (RFC 9449 §6.1).
func (k *Key) Thumbprint() string {
	return k.thumbprint
}

// ProofOptions are the per-request inputs to a proof.
type ProofOptions struct {
	// AccessToken, when non-empty, binds the proof to that token via ath.
	AccessToken string

	// Nonce, when non-empty, is echoed in the nonce claim.
	Nonce string

	// IssuedAt is the iat claim; the zero value means the current time.
	IssuedAt time.Time
}

// Proof signs one proof JWT for method and target. Callers sign a new proof
// for every attempt, including retries, so jti and iat are never reused
// (RFC 9449 §7.3, §11.1).
func (k *Key) Proof(method string, target *url.URL, opts ProofOptions) (string, error) {
	issuedAt := opts.IssuedAt
	if issuedAt.IsZero() {
		issuedAt = time.Now()
	}
	claims := map[string]any{
		// RFC 9449 §4.2: jti is a version 4 UUID, unique per proof.
		ClaimJTI: uuid.NewString(),
		// RFC 9449 §4.2: htm is the request method verbatim; RFC 9110 §9.1 methods are case-sensitive.
		ClaimHTM: method,
		// RFC 9449 §4.2: htu is the target URI without query and fragment.
		ClaimHTU: HTU(target),
		// RFC 9449 §4.2: iat is the proof's creation time; §11.2 forbids pre-generation.
		ClaimIAT: issuedAt.Unix(),
	}
	// RFC 9449 §4.2, §8: nonce echoes the most recent DPoP-Nonce when the server supplied one.
	if opts.Nonce != "" {
		claims[ClaimNonce] = opts.Nonce
	}
	// RFC 9449 §4.2, §7: ath binds the proof to the access token it accompanies.
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
// can carry it (RFC 9449 §8.2: keep one nonce and use it until the server
// supplies a new one). The zero value is ready to use.
type NonceCache struct {
	mu    sync.Mutex
	nonce string
}

// Remember stores the NonceHeaderName value from header, if present. Callers
// pass every response header, since a new nonce may arrive on any response
// including a success (RFC 9449 §8.2).
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
// carry NonceHeaderName and name ErrorCodeUseNonce either as the error
// parameter of a WWW-Authenticate challenge (RFC 9449 §7.1, §9) or as the
// error member of the JSON error body (RFC 9449 §8).
func IsUseNonceChallenge(header http.Header, body []byte) bool {
	if header.Get(NonceHeaderName) == "" {
		return false
	}
	for _, challenge := range header.Values("WWW-Authenticate") {
		if challengeError(challenge) == ErrorCodeUseNonce {
			return true
		}
	}
	var parsed struct {
		Error string `json:"error"`
	}
	return json.Unmarshal(body, &parsed) == nil && parsed.Error == ErrorCodeUseNonce
}

// challengeError extracts the error auth-param (RFC 6750 §3, quoted-string or
// token per RFC 9110 §11.2) from one WWW-Authenticate field value.
func challengeError(challenge string) string {
	for part := range strings.SplitSeq(challenge, ",") {
		part = strings.TrimSpace(part)
		if i := strings.LastIndex(part, " "); i >= 0 {
			part = strings.TrimSpace(part[i+1:])
		}
		name, value, ok := strings.Cut(part, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "error") {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"`)
	}
	return ""
}
