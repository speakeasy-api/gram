package identitychaining

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
)

// grantClaims are the ID-JAG claims Gram binds before forwarding the grant
// (draft-ietf-oauth-identity-assertion-authz-grant §3.1).
type grantClaims struct {
	jwt.Claims

	// ClientID is the client the resource authorization server will see.
	ClientID string `json:"client_id"`

	// Resource is the RFC 8707 resource the grant is bound to, when present.
	Resource json.RawMessage `json:"resource"`

	// Scope is the space-delimited scope the grant carries, when present.
	Scope string `json:"scope"`
}

// check applies the claim checks to a signature-verified ID-JAG. Errors name
// the failed check only, never claim values.
func (claims grantClaims) check(header jose.Header, idpIssuer, upstreamSubject string, sel selection, now time.Time) error {
	typ, _ := header.ExtraHeaders[jose.HeaderType].(string)
	// RFC 7515 §4.1.9: media types are case-insensitive and may omit "application/".
	if !strings.EqualFold(strings.TrimPrefix(strings.ToLower(typ), "application/"), idjag.Type) {
		return errors.New("typ is not an ID-JAG")
	}
	if !remotesessions.IssuerURLsEqual(claims.Issuer, idpIssuer) {
		return errors.New("iss is not the trusted identity provider")
	}
	if len(claims.Audience) != 1 || !remotesessions.IssuerURLsEqual(claims.Audience[0], sel.issuer) {
		return errors.New("aud is not the resource authorization server")
	}
	if claims.Expiry == nil || claims.IssuedAt == nil {
		return errors.New("exp and iat are required")
	}
	if err := claims.ValidateWithLeeway(jwt.Expected{Issuer: "", Subject: "", AnyAudience: nil, ID: "", Time: now}, maxClockSkew); err != nil {
		return fmt.Errorf("temporal claims: %w", err)
	}
	if claims.Expiry.Time().Sub(now) > idjag.MaxLifetime+maxClockSkew {
		return errors.New("exp exceeds the maximum ID-JAG lifetime")
	}
	if claims.Subject == "" || claims.Subject != upstreamSubject {
		return errors.New("sub is not the delegating human")
	}
	if claims.ClientID != sel.externalClientID {
		return errors.New("client_id is not the selected resource client")
	}
	if len(claims.Resource) > 0 && string(claims.Resource) != "null" {
		resources, err := claims.resources()
		if err != nil || len(resources) != 1 || resources[0] != sel.resource {
			return errors.New("resource is not the canonical upstream resource")
		}
	}
	if len(sel.scopes) > 0 {
		for scope := range strings.FieldsSeq(claims.Scope) {
			if !slices.Contains(sel.scopes, scope) {
				return errors.New("scope exceeds the requested scope")
			}
		}
	}
	return nil
}

// resources reads a resource claim that may be one string or an array.
func (claims grantClaims) resources() ([]string, error) {
	var one string
	if err := json.Unmarshal(claims.Resource, &one); err == nil {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(claims.Resource, &many); err != nil {
		return nil, fmt.Errorf("decode resource claim: %w", err)
	}
	return many, nil
}
