// Package integrationcredentials seals and opens the credentials Speakeasy
// holds for third-party integrations: Slack workspace tokens, device
// integration (MDM and GRC) credentials, and network ingress provider OAuth
// clients.
//
// Each row stores two copies of its credential: credentials_encrypted under
// the platform key and credentials_org_encrypted under the owning
// organization's key. Which copies are written and read follows the
// organization's transition state for the integration-credentials use case
// (package orgencryption). Every reader and writer goes through Credentials,
// and every statement that writes either copy fences on the transition
// generation, so the integrations follow the same rules as every other value
// moving off the platform key.
//
// Every row belongs to one organization, so nothing here is unattributed.
package integrationcredentials

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	orgrepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
)

// The copy columns of each integration. OrgColumn is bound into every
// organization-key ciphertext, so it must never change.
var (
	SlackDirectoryConnections = orgencryption.CopyColumns{
		UseCase:        orgencryption.UseCaseIntegrationCredentials,
		PlatformColumn: "slack_directory_connections.credentials_encrypted",
		OrgColumn:      "slack_directory_connections.credentials_org_encrypted",
	}
	DeviceIntegrationConfigs = orgencryption.CopyColumns{
		UseCase:        orgencryption.UseCaseIntegrationCredentials,
		PlatformColumn: "device_integration_configs.credentials_encrypted",
		OrgColumn:      "device_integration_configs.credentials_org_encrypted",
	}
	NetworkIngresses = orgencryption.CopyColumns{
		UseCase:        orgencryption.UseCaseIntegrationCredentials,
		PlatformColumn: "network_ingresses.credentials_encrypted",
		OrgColumn:      "network_ingresses.credentials_org_encrypted",
	}
)

// maxWriteAttempts bounds how often a write decides, seals and writes again
// after the transition moved under it. An organization changes state a
// handful of times in its life, so a third consecutive move means something
// is wrong.
const maxWriteAttempts = 3

// ErrNotStored means a write matched no row while the transition stayed at
// the generation it was sealed under: the row is gone or failed the write's
// own conditions, such as an expected connection generation.
var ErrNotStored = errors.New("integrationcredentials: no row matched the write")

// Stored returns the copies a row stores.
func Stored(platform, org pgtype.Text) orgencryption.Copies {
	return orgencryption.Copies{Platform: platform, Org: org}
}

// Has reports whether a row stores a credential in either copy.
func Has(stored orgencryption.Copies) bool {
	return stored.Platform.Valid || stored.Org.Valid
}

// IsUnreadable reports whether err from Open means the stored credential is
// missing or does not decrypt, so retrying cannot help and the integration
// needs new credentials. Every other error, including an unreachable,
// suspended or revoked organization key, may clear on its own: callers treat
// it as retryable and never ask an admin to reconnect or re-enter
// credentials because of it.
func IsUnreadable(err error) bool {
	return errors.Is(err, orgencryption.ErrUnreadable) || errors.Is(err, orgencryption.ErrMissing)
}

// IsCustomerKeyProblem reports whether err is caused by the state of the
// organization's own key, such as a suspended or revoked customer-managed
// key, rather than by Speakeasy.
func IsCustomerKeyProblem(err error) bool {
	return errors.Is(err, orgencryption.ErrSuspended) || errors.Is(err, orgencryption.ErrInvalidKey)
}

// Credentials is the one seam for integration credentials.
type Credentials struct {
	logger *slog.Logger
	cipher *orgencryption.Cipher
}

// New builds Credentials over the process's shared cipher.
func New(logger *slog.Logger, cipher *orgencryption.Cipher) *Credentials {
	return &Credentials{logger: logger.With(attr.SlogComponent("integrationcredentials")), cipher: cipher}
}

// WithoutOrganizationEncryption builds Credentials for a process without
// organization encryption: organizations in platform_key work, and any other
// transition state fails closed.
func WithoutOrganizationEncryption(logger *slog.Logger, db *pgxpool.Pool, platform *encryption.Client) *Credentials {
	transitions := orgencryption.NewTransitions(logger, db, nil, nil, orgencryption.TransitionsConfig{ClearSettleDelay: 0})

	return New(logger, orgencryption.NewCipher(logger, platform, nil, transitions))
}

// decide returns the organization's decision, reading transition state
// through db on a cache miss, or through the process's pool when db is nil.
func (c *Credentials) decide(ctx context.Context, db orgrepo.DBTX, organizationID string) (orgencryption.Decision, error) {
	var decision orgencryption.Decision
	var err error
	if db == nil {
		decision, err = c.cipher.Decide(ctx, organizationID, orgencryption.UseCaseIntegrationCredentials)
	} else {
		decision, err = c.cipher.DecideIn(ctx, db, organizationID, orgencryption.UseCaseIntegrationCredentials)
	}
	if err != nil {
		return orgencryption.Decision{}, fmt.Errorf("decide integration credentials state: %w", err)
	}

	return decision, nil
}

// Open decrypts the copy the organization's transition state reads. db is
// whatever the caller already holds, or nil.
func (c *Credentials) Open(ctx context.Context, db orgrepo.DBTX, organizationID string, columns orgencryption.CopyColumns, stored orgencryption.Copies) ([]byte, error) {
	decision, err := c.decide(ctx, db, organizationID)
	if err != nil {
		return nil, err
	}

	plaintext, err := c.cipher.Open(ctx, decision, columns, stored)
	if err != nil {
		return nil, fmt.Errorf("open integration credentials: %w", err)
	}

	return plaintext, nil
}

// RequireUsable checks that a credential could be sealed for the
// organization now. Callers run it before an irreversible provider call whose
// result must then be stored, such as a token refresh that rotates the
// refresh token, and skip the call when it fails.
func (c *Credentials) RequireUsable(ctx context.Context, db orgrepo.DBTX, organizationID string) error {
	decision, err := c.decide(ctx, db, organizationID)
	if err != nil {
		return err
	}

	if err := c.cipher.RequireUsable(ctx, decision); err != nil {
		return fmt.Errorf("integration credentials key: %w", err)
	}

	return nil
}

// Write runs one fenced statement that stores copies sealed under
// generation, and reports whether it matched a row.
type Write func(ctx context.Context, copies orgencryption.Copies, generation int64) (bool, error)

// Store seals plaintext for the organization and stores it with write. When
// write matches no row because the transition moved, Store decides again,
// seals the same plaintext again and retries; it never asks the caller to
// repeat whatever produced the credential. When write matches no row and the
// transition did not move, Store returns ErrNotStored. db is whatever the
// caller already holds, or nil.
func (c *Credentials) Store(ctx context.Context, db orgrepo.DBTX, organizationID string, columns orgencryption.CopyColumns, plaintext []byte, write Write) error {
	decision, err := c.decide(ctx, db, organizationID)
	if err != nil {
		return err
	}

	for range maxWriteAttempts {
		copies, err := c.cipher.Seal(ctx, decision, columns, plaintext)
		if errors.Is(err, orgencryption.ErrDecisionExpired) {
			if decision, err = c.redecide(ctx, db, decision); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("seal integration credentials: %w", err)
		}

		landed, err := write(ctx, copies, decision.Generation)
		if err != nil {
			return fmt.Errorf("store integration credentials: %w", err)
		}
		if landed {
			return nil
		}

		fresh, err := c.redecide(ctx, db, decision)
		if err != nil {
			return err
		}
		if fresh.Generation == decision.Generation {
			return ErrNotStored
		}

		c.logger.InfoContext(ctx, "integration credentials write fenced by a transition, sealing again",
			attr.SlogOrganizationID(organizationID), attr.SlogOrgEncryptionUseCase(orgencryption.UseCaseIntegrationCredentials))
		decision = fresh
	}

	return fmt.Errorf("store integration credentials: %w", orgencryption.ErrFenced)
}

func (c *Credentials) redecide(ctx context.Context, db orgrepo.DBTX, stale orgencryption.Decision) (orgencryption.Decision, error) {
	var decision orgencryption.Decision
	var err error
	if db == nil {
		decision, err = c.cipher.Redecide(ctx, stale)
	} else {
		decision, err = c.cipher.RedecideIn(ctx, db, stale)
	}
	if err != nil {
		return orgencryption.Decision{}, fmt.Errorf("decide integration credentials state again: %w", err)
	}

	return decision, nil
}

// DBTX is a connection or transaction that sqlc queries run on.
type DBTX = orgrepo.DBTX
