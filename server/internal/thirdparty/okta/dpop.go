package okta

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
)

const dpopProofType = "dpop+jwt"

// dpopKey is an ephemeral per-instance DPoP signing key.
type dpopKey struct {
	signer     jose.Signer
	thumbprint string
}

func newDPoPKey() (*dpopKey, error) {
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

	opts := (&jose.SignerOptions{NonceSource: nil, EmbedJWK: true, ExtraHeaders: nil}).WithType(dpopProofType)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: private}, opts)
	if err != nil {
		return nil, fmt.Errorf("configure dpop signer: %w", err)
	}

	return &dpopKey{signer: signer, thumbprint: base64.RawURLEncoding.EncodeToString(thumb)}, nil
}

// proof signs one RFC 9449 proof; accessToken binds ath when non-empty.
func (k *dpopKey) proof(method string, target *url.URL, nonce, accessToken string, now time.Time) (string, error) {
	claims := map[string]any{
		"jti": uuid.NewString(),
		"htm": method,
		"htu": dpopHTU(target),
		"iat": now.Unix(),
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	if accessToken != "" {
		sum := sha256.Sum256([]byte(accessToken))
		claims["ath"] = base64.RawURLEncoding.EncodeToString(sum[:])
	}

	serialized, err := jwt.Signed(k.signer).Claims(claims).Serialize()
	if err != nil {
		return "", fmt.Errorf("sign dpop proof: %w", err)
	}
	return serialized, nil
}

// dpopHTU strips query and fragment per RFC 9449 §4.2.
func dpopHTU(target *url.URL) string {
	u := *target
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}
