// Package assertion contains the verification mechanics shared by JWT
// assertion profiles. A profile decides which claims it accepts and when a
// verified assertion's identifier is spent.
package assertion

import (
	"context"
	"fmt"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

// MaxSkew is the clock difference allowed on temporal assertion claims.
const MaxSkew = time.Minute

// ReplayHoldFor includes both sides of the accepted clock-skew window.
func ReplayHoldFor(maxLifetime time.Duration) time.Duration {
	return maxLifetime + 2*MaxSkew
}

// ParseSigned bounds a compact assertion and rejects algorithms outside the
// shared public-key signature allowlist before any claim is read.
func ParseSigned(raw string, maxBytes int) (*jwt.JSONWebToken, error) {
	if len(raw) > maxBytes {
		return nil, fmt.Errorf("assertion exceeds %d bytes", maxBytes)
	}
	token, err := jwt.ParseSigned(raw, jwks.AllowedSignatureAlgorithms())
	if err != nil {
		return nil, fmt.Errorf("parse signed assertion: %w", err)
	}
	return token, nil
}

// VerificationStage identifies which part of signed-claim extraction failed.
type VerificationStage uint8

const (
	// VerificationKeyResolution means no trusted verification key was resolved.
	VerificationKeyResolution VerificationStage = iota + 1

	// VerificationSignature means the resolved key did not verify the payload.
	VerificationSignature
)

// VerificationError keeps key resolution distinguishable from a bad signature.
type VerificationError struct {
	// Stage identifies the failed operation.
	Stage VerificationStage

	// Err retains the underlying error for classification and diagnostics.
	Err error
}

func (e *VerificationError) Error() string { return e.Err.Error() }
func (e *VerificationError) Unwrap() error { return e.Err }

// VerificationKeys resolves a public key under a profile's storage policy.
type VerificationKeys interface {
	VerificationKey(ctx context.Context, source jwks.Source, kid string) (*jose.JSONWebKey, error)
}

// VerifiedClaims resolves a key under the caller's storage and rate-limit
// policy, then fills every destination from one signature-verified payload.
func VerifiedClaims(ctx context.Context, keys VerificationKeys, source jwks.Source, token *jwt.JSONWebToken, dest ...any) error {
	if len(token.Headers) != 1 {
		return &VerificationError{Stage: VerificationSignature, Err: fmt.Errorf("assertion has %d signature headers", len(token.Headers))}
	}
	key, err := keys.VerificationKey(ctx, source, token.Headers[0].KeyID)
	if err != nil {
		return &VerificationError{Stage: VerificationKeyResolution, Err: err}
	}
	if err := token.Claims(key, dest...); err != nil {
		return &VerificationError{Stage: VerificationSignature, Err: err}
	}
	return nil
}

// Reserve spends an assertion identifier until the end of its skew-adjusted
// validity window. Profiles choose when to call it.
func Reserve(ctx context.Context, guard ReplayGuard, key replay.Key, expiresAt time.Time) (bool, error) {
	claimed, err := guard.Reserve(ctx, key, expiresAt.Add(MaxSkew))
	if err != nil {
		return false, fmt.Errorf("reserve assertion identifier: %w", err)
	}
	return claimed, nil
}

// ReplayGuard is the authoritative single-use identifier store shared by
// assertion profiles.
type ReplayGuard interface {
	MaxHold() time.Duration
	Reserve(ctx context.Context, key replay.Key, holdUntil time.Time) (bool, error)
}
