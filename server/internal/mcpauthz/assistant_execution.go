package mcpauthz

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
)

const AssistantExecutionType = "gram-assistant-execution+jwt"
const AssistantExecutionAudience = "urn:gram:assistant-execution"

// AssistantRuntimeTokenTTL covers queue wait and model and tool work for both
// legacy and execution runner credentials.
const AssistantRuntimeTokenTTL = 60 * time.Minute

// AssistantExecutionClaims is distinct from outbound identity assertions and
// legacy user-backed assistant tokens. No synthetic user claim is emitted.
type AssistantExecutionClaims struct {
	Execution assistantidentity.Execution `json:"execution"`
	jwt.RegisteredClaims
}

// MintAssistantExecution signs validated identity metadata, not a permission.
// Callers must revalidate live state before minting and on every use.
func (s *Issuer) MintAssistantExecution(e assistantidentity.Execution) (string, error) {
	if e.Issuer != s.issuer {
		return "", assistantidentity.ErrInvalidIdentity
	}
	if err := e.Check(); err != nil {
		return "", fmt.Errorf("assistant execution: %w", err)
	}
	now := time.Now()
	claims := AssistantExecutionClaims{Execution: e, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: s.issuer, Subject: e.Identity.Subject, Audience: jwt.ClaimStrings{AssistantExecutionAudience},
		NotBefore: nil, ID: "", IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(AssistantRuntimeTokenTTL)),
	}}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["typ"] = AssistantExecutionType
	token.Header["kid"] = s.kid
	raw, err := token.SignedString(s.key)
	if err != nil {
		return "", fmt.Errorf("sign assistant execution: %w", err)
	}
	return raw, nil
}

// ValidateAssistantExecution checks the dedicated type, exact audience and
// stable issuer/subject. It deliberately does not build a user AuthContext.
func (s *Issuer) ValidateAssistantExecution(raw string) (*AssistantExecutionClaims, error) {
	claims := new(AssistantExecutionClaims)
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Header["typ"] != AssistantExecutionType {
			return nil, errors.New("invalid execution token type")
		}
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, errors.New("missing execution key id")
		}
		key, ok := s.publicKeys.Key(kid)
		if !ok {
			return nil, errors.New("unknown execution key id")
		}
		return key, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}), jwt.WithIssuer(s.issuer), jwt.WithAudience(AssistantExecutionAudience), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || token == nil || !token.Valid {
		return nil, assistantidentity.ErrInvalidIdentity
	}
	if len(claims.Audience) != 1 || claims.Subject != claims.Execution.Identity.Subject || claims.Execution.Issuer != s.issuer || claims.IssuedAt == nil {
		return nil, assistantidentity.ErrInvalidIdentity
	}
	if err := claims.Execution.Check(); err != nil {
		return nil, fmt.Errorf("assistant execution: %w", err)
	}
	return claims, nil
}
