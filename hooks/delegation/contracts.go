// Package delegation defines the versioned proof-of-possession contract shared by
// the hooks relay and Gram. It contains no signing secrets.
package delegation

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	ContractVersion   = "hooks-acting-user.v1"
	RefreshAudience   = "gram:hooks:delegation-mint:v1"
	AssertionAudience = "gram:hooks:ingest:v1"
	// RefreshIssuer and AssertionIssuer are deliberately distinct JWT trust
	// domains even though deployments currently derive both HMAC keys from the
	// existing server signing secret. Each token type also has its own audience.
	RefreshIssuer   = "gram:hooks:delegation-refresh:v1"
	AssertionIssuer = "gram:hooks:acting-user-assertion:v1"

	ProviderClaude = "claude"
	ProviderCodex  = "codex"

	EventUserPromptSubmit = "UserPromptSubmit"
	EventPreToolUse       = "PreToolUse"

	IdentityFailureMessage = "Speakeasy could not verify your current organization membership for this AI action. Reconnect Speakeasy hooks and try again."
)

// MintRequest asks Gram to mint one short-lived assertion. Signature is an
// Ed25519 signature over ProofMessage(req), made by the key enrolled through
// the session-authenticated PKCE exchange.
type MintRequest struct {
	RefreshToken    string `json:"refresh_token"`
	ContractVersion string `json:"contract_version"`
	Provider        string `json:"provider"`
	Event           string `json:"event"`
	SessionID       string `json:"session_id"`
	IdempotencyKey  string `json:"idempotency_key"`
	Observational   bool   `json:"observational,omitempty"`
	SignedAt        int64  `json:"signed_at"`
	Nonce           string `json:"nonce"`
	Signature       string `json:"signature"`
}

// MintResponse contains the assertion accepted only by unified hook ingest.
type MintResponse struct {
	Assertion string `json:"assertion"`
	ExpiresIn int    `json:"expires_in"`
}

// RedeemResponse is the PKCE redemption result consumed by the local relay.
// The access token remains the telemetry transport credential; RefreshToken is
// separately proof-bound and can mint assertions only with the private key.
type RedeemResponse struct {
	AccessToken    string `json:"access_token"`
	UserEmail      string `json:"user_email"`
	ProjectSlug    string `json:"project_slug"`
	OrganizationID string `json:"organization_id"`
	RefreshToken   string `json:"delegation_refresh_token"`
}

// Binding is one provider-native live checkpoint governed by v1.
type Binding struct {
	Provider    string
	Event       string
	ResourceKey string
}

var approvedBindings = []Binding{
	{Provider: ProviderClaude, Event: EventUserPromptSubmit, ResourceKey: "claude:user_prompt_submit"},
	{Provider: ProviderClaude, Event: EventPreToolUse, ResourceKey: "claude:pre_tool_use"},
	{Provider: ProviderCodex, Event: EventUserPromptSubmit, ResourceKey: "codex:user_prompt_submit"},
	{Provider: ProviderCodex, Event: EventPreToolUse, ResourceKey: "codex:pre_tool_use"},
}

// ApprovedBindings returns a copy of the complete governed checkpoint set.
func ApprovedBindings() []Binding {
	return append([]Binding(nil), approvedBindings...)
}

// Approved reports whether provider/event is one of the approved bindings.
func Approved(provider, event string) bool {
	_, ok := ResourceKey(provider, event)
	return ok
}

// ResourceKey returns the registered hook_activity resource key.
func ResourceKey(provider, event string) (string, bool) {
	for _, binding := range approvedBindings {
		if binding.Provider == provider && binding.Event == event {
			return binding.ResourceKey, true
		}
	}
	return "", false
}

// ProofMessage is the canonical message signed by the enrolled local key. The
// refresh credential is represented by its digest so it is bound without
// copying the bearer value into diagnostics that print the message.
func ProofMessage(req MintRequest) ([]byte, error) {
	if strings.TrimSpace(req.RefreshToken) == "" {
		return nil, errors.New("refresh token is required")
	}
	digest := sha256.Sum256([]byte(req.RefreshToken))
	message := struct {
		RefreshSHA256   string `json:"refresh_sha256"`
		ContractVersion string `json:"contract_version"`
		Provider        string `json:"provider"`
		Event           string `json:"event"`
		SessionID       string `json:"session_id"`
		IdempotencyKey  string `json:"idempotency_key"`
		Observational   bool   `json:"observational"`
		SignedAt        int64  `json:"signed_at"`
		Nonce           string `json:"nonce"`
	}{
		RefreshSHA256:   base64.RawURLEncoding.EncodeToString(digest[:]),
		ContractVersion: req.ContractVersion,
		Provider:        req.Provider,
		Event:           req.Event,
		SessionID:       req.SessionID,
		IdempotencyKey:  req.IdempotencyKey,
		Observational:   req.Observational,
		SignedAt:        req.SignedAt,
		Nonce:           req.Nonce,
	}
	return json.Marshal(message)
}

// NewNonce returns a 256-bit base64url proof nonce suitable for one mint.
func NewNonce() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("read cryptographic randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

// ValidNonce reports whether value is the canonical encoding produced by NewNonce.
func ValidNonce(value string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(raw) == 32 && value == base64.RawURLEncoding.EncodeToString(raw)
}

func EncodePublicKey(key ed25519.PublicKey) string {
	return base64.RawURLEncoding.EncodeToString(key)
}

func ParsePublicKey(encoded string) (ed25519.PublicKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("invalid Ed25519 public key")
	}
	return ed25519.PublicKey(raw), nil
}

func EncodePrivateKey(key ed25519.PrivateKey) string {
	return base64.RawURLEncoding.EncodeToString(key)
}

func ParsePrivateKey(encoded string) (ed25519.PrivateKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid Ed25519 private key")
	}
	return ed25519.PrivateKey(raw), nil
}

func KeyID(key ed25519.PublicKey) string {
	digest := sha256.Sum256(key)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func Sign(key ed25519.PrivateKey, req MintRequest) (string, error) {
	message, err := ProofMessage(req)
	if err != nil {
		return "", err
	}
	if len(key) != ed25519.PrivateKeySize {
		return "", errors.New("invalid Ed25519 private key")
	}
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, message)), nil
}

func Verify(key ed25519.PublicKey, req MintRequest) error {
	if len(key) != ed25519.PublicKeySize {
		return errors.New("invalid Ed25519 public key")
	}
	message, err := ProofMessage(req)
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(req.Signature))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("invalid Ed25519 signature")
	}
	if !ed25519.Verify(key, message, signature) {
		return fmt.Errorf("invalid proof-of-possession signature")
	}
	return nil
}
