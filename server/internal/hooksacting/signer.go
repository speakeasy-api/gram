// Package hooksacting mints and verifies proof-bound hook acting-user credentials.
package hooksacting

import (
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/speakeasy-api/gram/hooks/delegation"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	RefreshLifetime   = 30 * 24 * time.Hour
	AssertionLifetime = 30 * time.Second
	ProofClockSkew    = 60 * time.Second
	tokenClockSkew    = 5 * time.Second
)

type Signer struct {
	refreshKey   []byte
	assertionKey []byte
	now          func() time.Time
}

type refreshClaims struct {
	jwt.RegisteredClaims
	ContractVersion string `json:"ver"`
	OrganizationID  string `json:"org"`
	PublicKey       string `json:"public_key"`
	KeyID           string `json:"kid"`
}

type delegationClaims struct {
	jwt.RegisteredClaims
	ContractVersion string `json:"ver"`
	OrganizationID  string `json:"org"`
	Provider        string `json:"provider"`
	Event           string `json:"event"`
	SessionID       string `json:"session_id"`
	IdempotencyKey  string `json:"idempotency_key"`
	Observational   bool   `json:"observational,omitempty"`
	KeyID           string `json:"kid"`
}

type RefreshIdentity struct {
	UserID         string
	OrganizationID string
	PublicKey      ed25519.PublicKey
	KeyID          string
	RefreshJTI     string
}

type AssertionIdentity struct {
	UserID         string
	OrganizationID string
	Provider       string
	Event          string
	SessionID      string
	IdempotencyKey string
	Observational  bool
	KeyID          string
}

type AssertionBinding struct {
	OrganizationID string
	Provider       string
	Event          string
	SessionID      string
	IdempotencyKey string
	Observational  bool
}

func NewSigner(secret string) (*Signer, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("hooks acting-user signing secret is required")
	}
	// Keep using the existing configured signing secret, but derive independent
	// HMAC domains so refresh credentials and assertions are not interchangeable.
	refreshKey, err := hkdf.Key(sha256.New, []byte(secret), nil, "hooks/delegation-refresh/v1", sha256.Size)
	if err != nil {
		return nil, fmt.Errorf("derive hooks delegation refresh key: %w", err)
	}
	assertionKey, err := hkdf.Key(sha256.New, []byte(secret), nil, "hooks/acting-user-assertion/v1", sha256.Size)
	if err != nil {
		return nil, fmt.Errorf("derive hooks acting-user assertion key: %w", err)
	}
	return &Signer{refreshKey: refreshKey, assertionKey: assertionKey, now: time.Now}, nil
}

func (s *Signer) MintRefresh(userID, organizationID string, publicKey ed25519.PublicKey) (string, error) {
	if s == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(organizationID) == "" || len(publicKey) != ed25519.PublicKeySize {
		return "", errors.New("valid user, organization, and Ed25519 public key are required")
	}
	subject, err := urn.ParseSessionSubject(urn.NewUserSubject(userID).String())
	if err != nil {
		return "", fmt.Errorf("invalid hooks acting-user subject: %w", err)
	}
	now := s.now().UTC().Truncate(time.Second)
	jti, err := randomID()
	if err != nil {
		return "", err
	}
	claims := refreshClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: delegation.RefreshIssuer, Subject: subject.String(),
			Audience: jwt.ClaimStrings{delegation.RefreshAudience}, ID: jti,
			IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(RefreshLifetime)),
		},
		ContractVersion: delegation.ContractVersion, OrganizationID: organizationID,
		PublicKey: delegation.EncodePublicKey(publicKey), KeyID: delegation.KeyID(publicKey),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.refreshKey)
	if err != nil {
		return "", fmt.Errorf("sign hooks refresh credential: %w", err)
	}
	return signed, nil
}

func (s *Signer) VerifyRefresh(raw string) (RefreshIdentity, error) {
	if s == nil {
		return RefreshIdentity{}, errors.New("credential verifier is required")
	}
	claims := refreshClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: "", Subject: "", Audience: nil, ExpiresAt: nil, NotBefore: nil, IssuedAt: nil, ID: "",
		},
		ContractVersion: "",
		OrganizationID:  "",
		PublicKey:       "",
		KeyID:           "",
	}
	if err := s.parse(raw, delegation.RefreshIssuer, delegation.RefreshAudience, RefreshLifetime, s.refreshKey, &claims, &claims.RegisteredClaims); err != nil {
		return RefreshIdentity{}, err
	}
	userID, err := parseUserSubject(claims.Subject)
	if err != nil || claims.ContractVersion != delegation.ContractVersion || strings.TrimSpace(claims.OrganizationID) == "" {
		return RefreshIdentity{}, errors.New("invalid delegation refresh claims")
	}
	publicKey, err := delegation.ParsePublicKey(claims.PublicKey)
	if err != nil || claims.KeyID != delegation.KeyID(publicKey) {
		return RefreshIdentity{}, errors.New("invalid delegation refresh key binding")
	}
	return RefreshIdentity{UserID: userID, OrganizationID: claims.OrganizationID, PublicKey: publicKey, KeyID: claims.KeyID, RefreshJTI: claims.ID}, nil
}

func (s *Signer) MintAssertion(identity RefreshIdentity, req delegation.MintRequest) (string, error) {
	if s == nil {
		return "", errors.New("credential signer is required")
	}
	if req.ContractVersion != delegation.ContractVersion || !delegation.Approved(req.Provider, req.Event) ||
		req.SessionID == "" || req.SessionID != strings.TrimSpace(req.SessionID) ||
		req.IdempotencyKey == "" || req.IdempotencyKey != strings.TrimSpace(req.IdempotencyKey) || !delegation.ValidNonce(req.Nonce) {
		return "", errors.New("invalid governed hook binding")
	}
	now := s.now().UTC().Truncate(time.Second)
	if delta := now.Sub(time.Unix(req.SignedAt, 0)); delta < -ProofClockSkew || delta > ProofClockSkew {
		return "", errors.New("stale proof-of-possession request")
	}
	if err := delegation.Verify(identity.PublicKey, req); err != nil {
		return "", fmt.Errorf("verify hooks delegation proof: %w", err)
	}
	claims := delegationClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: delegation.AssertionIssuer, Subject: urn.NewUserSubject(identity.UserID).String(),
			Audience: jwt.ClaimStrings{delegation.AssertionAudience}, ID: req.Nonce,
			IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AssertionLifetime)),
		},
		ContractVersion: delegation.ContractVersion, OrganizationID: identity.OrganizationID,
		Provider: req.Provider, Event: req.Event, SessionID: req.SessionID,
		IdempotencyKey: req.IdempotencyKey, Observational: req.Observational, KeyID: identity.KeyID,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.assertionKey)
	if err != nil {
		return "", fmt.Errorf("sign hooks acting-user assertion: %w", err)
	}
	return signed, nil
}

func (s *Signer) VerifyAssertion(raw string, expected AssertionBinding) (AssertionIdentity, error) {
	if s == nil {
		return AssertionIdentity{}, errors.New("credential verifier is required")
	}
	claims := delegationClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: "", Subject: "", Audience: nil, ExpiresAt: nil, NotBefore: nil, IssuedAt: nil, ID: "",
		},
		ContractVersion: "",
		OrganizationID:  "",
		Provider:        "",
		Event:           "",
		SessionID:       "",
		IdempotencyKey:  "",
		Observational:   false,
		KeyID:           "",
	}
	if err := s.parse(raw, delegation.AssertionIssuer, delegation.AssertionAudience, AssertionLifetime, s.assertionKey, &claims, &claims.RegisteredClaims); err != nil {
		return AssertionIdentity{}, err
	}
	userID, err := parseUserSubject(claims.Subject)
	if err != nil || claims.ContractVersion != delegation.ContractVersion || claims.KeyID == "" {
		return AssertionIdentity{}, errors.New("invalid acting-user assertion claims")
	}
	if claims.OrganizationID != expected.OrganizationID || claims.Provider != expected.Provider || claims.Event != expected.Event || claims.SessionID != expected.SessionID || claims.IdempotencyKey != expected.IdempotencyKey || claims.Observational != expected.Observational {
		return AssertionIdentity{}, errors.New("acting-user assertion binding mismatch")
	}
	return AssertionIdentity{UserID: userID, OrganizationID: claims.OrganizationID, Provider: claims.Provider, Event: claims.Event, SessionID: claims.SessionID, IdempotencyKey: claims.IdempotencyKey, Observational: claims.Observational, KeyID: claims.KeyID}, nil
}

// AssertionExpiresIn returns the usable whole-second lifetime of a valid assertion.
func (s *Signer) AssertionExpiresIn(raw string) (int, error) {
	if s == nil {
		return 0, errors.New("credential verifier is required")
	}
	claims := delegationClaims{
		RegisteredClaims: jwt.RegisteredClaims{Issuer: "", Subject: "", Audience: nil, ExpiresAt: nil, NotBefore: nil, IssuedAt: nil, ID: ""}, ContractVersion: "", OrganizationID: "", Provider: "", Event: "",
		SessionID: "", IdempotencyKey: "", Observational: false, KeyID: "",
	}
	if err := s.parse(raw, delegation.AssertionIssuer, delegation.AssertionAudience, AssertionLifetime, s.assertionKey, &claims, &claims.RegisteredClaims); err != nil {
		return 0, err
	}
	remaining := claims.ExpiresAt.Sub(s.now())
	if remaining <= 0 {
		return 0, errors.New("acting-user assertion is expired")
	}
	return int(remaining / time.Second), nil
}

func (s *Signer) parse(raw, issuer, audience string, lifetime time.Duration, key []byte, claims jwt.Claims, registered *jwt.RegisteredClaims) error {
	if s == nil || strings.TrimSpace(raw) == "" {
		return errors.New("credential is required")
	}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 || token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing algorithm %q", token.Method.Alg())
		}
		return key, nil
	}, jwt.WithAudience(audience), jwt.WithIssuer(issuer), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(tokenClockSkew), jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithTimeFunc(s.now))
	if err != nil || token == nil || !token.Valid {
		return fmt.Errorf("verify hooks credential: %w", err)
	}
	if registered == nil || registered.Issuer != issuer || len(registered.Audience) != 1 || registered.Audience[0] != audience ||
		registered.ExpiresAt == nil || registered.IssuedAt == nil || registered.NotBefore == nil || strings.TrimSpace(registered.ID) == "" ||
		!registered.NotBefore.Equal(registered.IssuedAt.Time) || registered.ExpiresAt.Sub(registered.IssuedAt.Time) != lifetime {
		return errors.New("invalid hooks credential temporal or registered claims profile")
	}
	return nil
}

func parseUserSubject(subject string) (string, error) {
	parsed, err := urn.ParseSessionSubject(subject)
	if err != nil || parsed.Kind != urn.SessionSubjectKindUser {
		return "", errors.New("invalid user subject")
	}
	return parsed.ID, nil
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("read cryptographic randomness: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
