// Package aiproviderkeys seals and opens the AI provider and model keys
// Speakeasy holds: AI integration API keys and Anthropic inference webhook
// signing secrets, customer model provider (BYOK) keys, and provisioned
// OpenRouter keys.
//
// Each row stores two copies of its key: one under the platform key and one
// under the owning organization's key. Which copies are written and read
// follows the organization's transition state for the ai-provider-keys use
// case (package orgencryption). Every reader and writer goes through Keys, and
// every statement that writes either copy fences on the transition
// generation.
//
// Every row belongs to one organization, so nothing here is unattributed.
package aiproviderkeys

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	orgrepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
)

// The copy columns of each table. OrgColumn is bound into every
// organization-key ciphertext, so it must never change.
var (
	AIIntegrationConfigs = orgencryption.CopyColumns{
		UseCase:        orgencryption.UseCaseAIProviderKeys,
		PlatformColumn: "ai_integration_configs.api_key_encrypted",
		OrgColumn:      "ai_integration_configs.api_key_org_encrypted",
	}
	ModelProviderKeys = orgencryption.CopyColumns{
		UseCase:        orgencryption.UseCaseAIProviderKeys,
		PlatformColumn: "model_provider_keys.api_key_encrypted",
		OrgColumn:      "model_provider_keys.api_key_org_encrypted",
	}
	OpenRouterAPIKeys = orgencryption.CopyColumns{
		UseCase:        orgencryption.UseCaseAIProviderKeys,
		PlatformColumn: "openrouter_api_keys.key_encrypted",
		OrgColumn:      "openrouter_api_keys.key_org_encrypted",
	}
)

// maxWriteAttempts bounds how often a write decides, seals and writes again
// after the transition moved under it. An organization changes state a
// handful of times in its life, so a third consecutive move means something
// is wrong.
const maxWriteAttempts = 3

// ErrNotStored means a write matched no row while the transition stayed at
// the generation it was sealed under: the row is gone or failed the write's
// own conditions.
var ErrNotStored = errors.New("aiproviderkeys: no row matched the write")

// Stored returns the copies a row stores.
func Stored(platform, org pgtype.Text) orgencryption.Copies {
	return orgencryption.Copies{Platform: platform, Org: org}
}

// Has reports whether a row stores a key in either copy.
func Has(stored orgencryption.Copies) bool {
	return stored.Platform.Valid || stored.Org.Valid
}

// IsUnreadable reports whether err from Open means the stored key is missing
// or does not decrypt, so retrying cannot help and the key must be entered
// again. Every other error, including an unreachable, suspended or revoked
// organization key, may clear on its own: callers treat it as retryable and
// never ask an admin to re-enter a key because of it.
func IsUnreadable(err error) bool {
	return errors.Is(err, orgencryption.ErrUnreadable) || errors.Is(err, orgencryption.ErrMissing)
}

// IsCustomerKeyProblem reports whether err is caused by the organization's
// key being suspended or invalid, which needs someone to fix the key rather
// than a retry.
func IsCustomerKeyProblem(err error) bool {
	return errors.Is(err, orgencryption.ErrSuspended) || errors.Is(err, orgencryption.ErrInvalidKey)
}

// Error converts an error from Open, Store or RequireUsable into an API
// error. msg is the public message for errors that are not about the key.
func Error(err error, msg string) *oops.ShareableError {
	switch {
	case IsCustomerKeyProblem(err):
		return oops.E(oops.CodeFailedPrecondition, err, "This organization's encryption key is suspended or invalid, so the key can't be used until it is fixed.")
	case orgencryption.IsKeyProblem(err):
		return oops.E(oops.CodeUnavailable, err, "This organization's encryption key is unavailable. Try again shortly.")
	default:
		return oops.E(oops.CodeUnexpected, err, "%s", msg)
	}
}

// Keys is the one seam for AI provider and model keys.
type Keys struct {
	logger *slog.Logger
	cipher *orgencryption.Cipher
}

// New builds Keys over the process's shared cipher.
func New(logger *slog.Logger, cipher *orgencryption.Cipher) *Keys {
	return &Keys{logger: logger.With(attr.SlogComponent("aiproviderkeys")), cipher: cipher}
}

// WithoutOrganizationEncryption builds Keys for a process without
// organization encryption: organizations in platform_key work, and any other
// transition state fails closed.
func WithoutOrganizationEncryption(logger *slog.Logger, db *pgxpool.Pool, platform *encryption.Client) *Keys {
	transitions := orgencryption.NewTransitions(logger, db, nil, nil, orgencryption.TransitionsConfig{ClearSettleDelay: 0})

	return New(logger, orgencryption.NewCipher(logger, platform, nil, transitions))
}

// decide returns the organization's decision, reading transition state
// through db on a cache miss, or through the process's pool when db is nil.
func (k *Keys) decide(ctx context.Context, db orgrepo.DBTX, organizationID string) (orgencryption.Decision, error) {
	var decision orgencryption.Decision
	var err error
	if db == nil {
		decision, err = k.cipher.Decide(ctx, organizationID, orgencryption.UseCaseAIProviderKeys)
	} else {
		decision, err = k.cipher.DecideIn(ctx, db, organizationID, orgencryption.UseCaseAIProviderKeys)
	}
	if err != nil {
		return orgencryption.Decision{}, fmt.Errorf("decide ai provider keys state: %w", err)
	}

	return decision, nil
}

// Open decrypts the copy the organization's transition state reads. db is
// whatever the caller already holds, or nil.
func (k *Keys) Open(ctx context.Context, db orgrepo.DBTX, organizationID string, columns orgencryption.CopyColumns, stored orgencryption.Copies) ([]byte, error) {
	decision, err := k.decide(ctx, db, organizationID)
	if err != nil {
		return nil, err
	}

	plaintext, err := k.cipher.Open(ctx, decision, columns, stored)
	if err != nil {
		return nil, fmt.Errorf("open ai provider key: %w", err)
	}

	return plaintext, nil
}

// RequireUsable checks that a key could be sealed for the organization now.
// Callers run it before an irreversible provider call whose result must then
// be stored, such as minting an upstream OpenRouter key, and skip the call
// when it fails.
func (k *Keys) RequireUsable(ctx context.Context, db orgrepo.DBTX, organizationID string) error {
	decision, err := k.decide(ctx, db, organizationID)
	if err != nil {
		return err
	}

	if err := k.cipher.RequireUsable(ctx, decision); err != nil {
		return fmt.Errorf("ai provider keys key: %w", err)
	}

	return nil
}

// Write runs one fenced statement that stores copies sealed under
// generation, and reports whether it matched a row.
type Write func(ctx context.Context, copies orgencryption.Copies, generation int64) (bool, error)

// Store seals plaintext for the organization and stores it with write. When
// write matches no row because the transition moved, Store decides again,
// seals the same plaintext again and retries; it never asks the caller to
// repeat whatever produced the key. When write matches no row and the
// transition did not move, Store returns ErrNotStored. db is whatever the
// caller already holds, or nil.
func (k *Keys) Store(ctx context.Context, db orgrepo.DBTX, organizationID string, columns orgencryption.CopyColumns, plaintext []byte, write Write) error {
	decision, err := k.decide(ctx, db, organizationID)
	if err != nil {
		return err
	}

	for range maxWriteAttempts {
		copies, err := k.cipher.Seal(ctx, decision, columns, plaintext)
		if errors.Is(err, orgencryption.ErrDecisionExpired) {
			if decision, err = k.redecide(ctx, db, decision); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("seal ai provider key: %w", err)
		}

		landed, err := write(ctx, copies, decision.Generation)
		if err != nil {
			return fmt.Errorf("store ai provider key: %w", err)
		}
		if landed {
			return nil
		}

		fresh, err := k.redecide(ctx, db, decision)
		if err != nil {
			return err
		}
		if fresh.Generation == decision.Generation {
			return ErrNotStored
		}

		k.logger.InfoContext(ctx, "ai provider key write fenced by a transition, sealing again",
			attr.SlogOrganizationID(organizationID), attr.SlogOrgEncryptionUseCase(orgencryption.UseCaseAIProviderKeys))
		decision = fresh
	}

	return fmt.Errorf("store ai provider key: %w", orgencryption.ErrFenced)
}

func (k *Keys) redecide(ctx context.Context, db orgrepo.DBTX, stale orgencryption.Decision) (orgencryption.Decision, error) {
	var decision orgencryption.Decision
	var err error
	if db == nil {
		decision, err = k.cipher.Redecide(ctx, stale)
	} else {
		decision, err = k.cipher.RedecideIn(ctx, db, stale)
	}
	if err != nil {
		return orgencryption.Decision{}, fmt.Errorf("decide ai provider keys state again: %w", err)
	}

	return decision, nil
}

// DBTX is a connection or transaction that sqlc queries run on.
type DBTX = orgrepo.DBTX
