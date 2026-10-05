// Package litellmacting mints and verifies LiteLLM acting-principal assertions.
package litellmacting

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/hooks/delegation"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ContractVersion = "litellm-acting-principal.v1"
	Issuer          = "gram:litellm:acting-principal:v1"
	Audience        = "gram:litellm:pre-inference:v1"
	TokenType       = "litellm-acting-principal+jwt"

	AssertionLifetime = 60 * time.Second
	tokenClockSkew    = 5 * time.Second
	keyDomain         = "litellm/acting-principal-assertion/v1"
)

type Signer struct {
	key    []byte
	keyID  string
	now    func() time.Time
	parser *jwt.Parser
}

// AssertionBinding is the tenant and invocation context expected by a verifier.
type AssertionBinding struct {
	OrganizationID string
	ProjectID      string
	InstanceID     string
	APIKeyID       string
	InvocationID   string
}

// AssertionIdentity is the authenticated identity carried by an assertion.
type AssertionIdentity struct {
	UserID string
	JTI    string
}

type assertionClaims struct {
	jwt.RegisteredClaims
	ContractVersion string `json:"ver"`
	OrganizationID  string `json:"org"`
	ProjectID       string `json:"project_id"`
	InstanceID      string `json:"instance_id"`
	APIKeyID        string `json:"api_key_id"`
	InvocationID    string `json:"invocation_id"`
	KeyID           string `json:"kid"`
}

func NewSigner(secret string) (*Signer, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("LiteLLM acting-principal signing secret is required")
	}
	key, err := hkdf.Key(sha256.New, []byte(secret), nil, keyDomain, sha256.Size)
	if err != nil {
		return nil, fmt.Errorf("derive LiteLLM acting-principal key: %w", err)
	}
	digest := sha256.Sum256(key)
	signer := &Signer{
		key:    key,
		keyID:  base64.RawURLEncoding.EncodeToString(digest[:]),
		now:    time.Now,
		parser: nil,
	}
	signer.parser = jwt.NewParser(
		jwt.WithAudience(Audience),
		jwt.WithIssuer(Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(tokenClockSkew),
		jwt.WithTimeFunc(func() time.Time { return signer.now() }),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	return signer, nil
}

func (s *Signer) MintAssertion(userID string, binding AssertionBinding) (string, error) {
	if s == nil {
		return "", errors.New("acting-principal signer is required")
	}
	subject, err := userSubject(userID)
	if err != nil {
		return "", err
	}
	if err := validateBinding(binding); err != nil {
		return "", err
	}

	now := s.now().UTC().Truncate(time.Second)
	jti, err := delegation.NewNonce()
	if err != nil {
		return "", fmt.Errorf("generate acting-principal assertion ID: %w", err)
	}
	claims := assertionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{Audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(AssertionLifetime)),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        jti,
		},
		ContractVersion: ContractVersion,
		OrganizationID:  binding.OrganizationID,
		ProjectID:       binding.ProjectID,
		InstanceID:      binding.InstanceID,
		APIKeyID:        binding.APIKeyID,
		InvocationID:    binding.InvocationID,
		KeyID:           s.keyID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["typ"] = TokenType
	token.Header["kid"] = s.keyID
	signed, err := token.SignedString(s.key)
	if err != nil {
		return "", fmt.Errorf("sign LiteLLM acting-principal assertion: %w", err)
	}
	return signed, nil
}

func (s *Signer) VerifyAssertion(raw string, expected AssertionBinding) (AssertionIdentity, error) {
	if s == nil {
		return AssertionIdentity{}, errors.New("acting-principal verifier is required")
	}
	if strings.TrimSpace(raw) == "" {
		return AssertionIdentity{}, errors.New("acting-principal assertion is required")
	}
	if err := validateBinding(expected); err != nil {
		return AssertionIdentity{}, fmt.Errorf("invalid expected acting-principal binding: %w", err)
	}

	claims := assertionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: "", Subject: "", Audience: nil, ExpiresAt: nil, NotBefore: nil, IssuedAt: nil, ID: "",
		},
		ContractVersion: "", OrganizationID: "", ProjectID: "", InstanceID: "", APIKeyID: "", InvocationID: "", KeyID: "",
	}
	token, err := s.parser.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 || token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing algorithm %q", token.Method.Alg())
		}
		if typ, ok := token.Header["typ"].(string); !ok || typ != TokenType {
			return nil, errors.New("invalid acting-principal token type")
		}
		if kid, ok := token.Header["kid"].(string); !ok || kid != s.keyID {
			return nil, errors.New("invalid acting-principal header key ID")
		}
		return s.key, nil
	})
	if err != nil {
		return AssertionIdentity{}, fmt.Errorf("verify LiteLLM acting-principal assertion: %w", err)
	}
	if token == nil || !token.Valid {
		return AssertionIdentity{}, errors.New("invalid LiteLLM acting-principal assertion")
	}
	if kid, ok := token.Header["kid"].(string); !ok || kid != s.keyID || claims.KeyID != s.keyID || claims.KeyID != kid {
		return AssertionIdentity{}, errors.New("invalid acting-principal key binding")
	}
	if claims.Issuer != Issuer || len(claims.Audience) != 1 || claims.Audience[0] != Audience ||
		claims.IssuedAt == nil || claims.NotBefore == nil || claims.ExpiresAt == nil ||
		!claims.IssuedAt.Equal(claims.NotBefore.Time) || claims.ExpiresAt.Sub(claims.IssuedAt.Time) != AssertionLifetime {
		return AssertionIdentity{}, errors.New("invalid acting-principal registered claims profile")
	}
	if claims.ContractVersion != ContractVersion {
		return AssertionIdentity{}, errors.New("invalid acting-principal contract version")
	}
	userID, err := parseUserSubject(claims.Subject)
	if err != nil {
		return AssertionIdentity{}, err
	}
	if !delegation.ValidNonce(claims.ID) {
		return AssertionIdentity{}, errors.New("invalid acting-principal JTI")
	}
	actual := AssertionBinding{
		OrganizationID: claims.OrganizationID,
		ProjectID:      claims.ProjectID,
		InstanceID:     claims.InstanceID,
		APIKeyID:       claims.APIKeyID,
		InvocationID:   claims.InvocationID,
	}
	if actual != expected {
		return AssertionIdentity{}, errors.New("acting-principal assertion binding mismatch")
	}
	return AssertionIdentity{UserID: userID, JTI: claims.ID}, nil
}

func userSubject(userID string) (string, error) {
	if strings.TrimSpace(userID) == "" || userID != strings.TrimSpace(userID) {
		return "", errors.New("valid acting user ID is required")
	}
	subject := urn.NewUserSubject(userID).String()
	if _, err := urn.ParseSessionSubject(subject); err != nil {
		return "", fmt.Errorf("invalid acting user subject: %w", err)
	}
	return subject, nil
}

func parseUserSubject(subject string) (string, error) {
	parsed, err := urn.ParseSessionSubject(subject)
	if err != nil || parsed.Kind != urn.SessionSubjectKindUser || parsed.String() != subject {
		return "", errors.New("invalid acting user subject")
	}
	return parsed.ID, nil
}

func validateBinding(binding AssertionBinding) error {
	if strings.TrimSpace(binding.OrganizationID) == "" || binding.OrganizationID != strings.TrimSpace(binding.OrganizationID) {
		return errors.New("valid organization ID is required")
	}
	for name, value := range map[string]string{
		"project":             binding.ProjectID,
		"LiteLLM instance":    binding.InstanceID,
		"integration API key": binding.APIKeyID,
	} {
		if _, err := canonicalUUID(value); err != nil {
			return fmt.Errorf("invalid %s ID: %w", name, err)
		}
	}
	invocationID, err := canonicalUUID(binding.InvocationID)
	if err != nil || invocationID.Version() != 7 || invocationID.Variant() != uuid.RFC4122 {
		return errors.New("invocation ID must be a canonical UUIDv7")
	}
	return nil
}

func canonicalUUID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != value {
		return uuid.Nil, errors.New("must be a canonical non-nil UUID")
	}
	return id, nil
}
