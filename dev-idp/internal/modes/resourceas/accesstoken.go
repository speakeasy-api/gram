package resourceas

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
)

// accessTokenJWTType is the RFC 9068 `typ` header for a JWT access token. A
// resource server checks it to be sure it was handed an access token rather
// than an id_token or an ID-JAG that happens to verify against the same key.
const accessTokenJWTType = "at+jwt"

// AccessTokenClaims is the claim set on a resource access token, following
// RFC 9068.
//
// `aud` is the MCP server this token is good for, not the authorization
// server that issued it -- that binding is the whole point of the profile,
// and putting it in a signed claim is what lets a resource server enforce it
// without calling back here.
type AccessTokenClaims struct {
	ClientID string `json:"client_id"`
	Scope    string `json:"scope,omitempty"`
	jwt.RegisteredClaims
}

// accessTokenClaims carries what the mint path knows about a token being
// issued. Unexported and positional-free so the call site reads as prose.
type accessTokenClaims struct {
	jti      string
	subject  string
	clientID string
	scope    string
	issuedAt time.Time
	expires  time.Time
}

func (h *Handler) signAccessToken(resource *repo.EmaResource, in accessTokenClaims) (string, error) {
	claims := AccessTokenClaims{
		ClientID: in.clientID,
		Scope:    in.scope,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        in.jti,
			Issuer:    h.issuer(resource.Slug),
			Subject:   in.subject,
			Audience:  jwt.ClaimStrings{resource.ResourceIdentifier},
			IssuedAt:  jwt.NewNumericDate(in.issuedAt),
			ExpiresAt: jwt.NewNumericDate(in.expires),
			NotBefore: nil,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = h.keystore.KID()
	token.Header["typ"] = accessTokenJWTType

	signed, err := token.SignedString(h.keystore.PrivateKey())
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return signed, nil
}

// parseAccessToken verifies a token this resource authorization server
// issued and returns its claims. Signature, issuer, audience, `typ` and
// expiry are all checked here, so a caller only has to consider revocation.
func (h *Handler) parseAccessToken(resource *repo.EmaResource, raw string) (AccessTokenClaims, error) {
	var claims AccessTokenClaims
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{h.keystore.SigningAlg()}),
		jwt.WithIssuer(h.issuer(resource.Slug)),
		jwt.WithAudience(resource.ResourceIdentifier),
		jwt.WithExpirationRequired(),
	)

	token, err := parser.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) {
		return h.keystore.PublicKey(), nil
	})
	if err != nil {
		return AccessTokenClaims{}, fmt.Errorf("access token did not verify: %w", err)
	}
	if typ, _ := token.Header["typ"].(string); typ != accessTokenJWTType {
		return AccessTokenClaims{}, fmt.Errorf("token typ header is %q, want %q", typ, accessTokenJWTType)
	}
	return claims, nil
}
