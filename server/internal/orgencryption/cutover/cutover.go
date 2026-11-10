// Package cutover moves stored secrets from the application key in package
// encryption to organization encryption one organization at a time.
//
// During the move every row keeps its application-key ciphertext and gains an
// organization ciphertext of the same secret in a separate column. A feature
// flag, evaluated per organization, decides which copy a read trusts:
//
//   - Off, every read and write uses the application key only, and writes
//     leave the organization column empty so it can never hold a stale value.
//   - On, writes seal both copies, reads open the organization copy and fail
//     closed when it does not open, and a row with no organization copy yet
//     gains one on its next read.
//
// Turning the flag off is the rollback: reads return to the application-key
// copy, which every row still holds. Once every organization is moved and
// verified, the application-key copies are destroyed and this package's
// consumers read the organization column alone.
package cutover

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// Ciphertexts is one secret as it is stored during the move.
type Ciphertexts struct {
	// Legacy is the secret sealed with the application key. Every row keeps
	// it until the application-key copies are destroyed.
	Legacy string

	// Org is the secret sealed with organization encryption, or invalid when
	// the organization is not moved yet or sealing it failed.
	Org pgtype.Text
}

// Config selects what a Sealer moves.
type Config struct {
	// UseCase is the organization encryption use case the secrets belong to.
	UseCase orgencryption.UseCase

	// Flag moves an organization when it is on for the organization id,
	// evaluated locally with no groups.
	Flag feature.Flag
}

// OrgEncryption is the organization encryption secrets move to. A Sealer
// built without it, or with any field nil, uses the application key alone.
type OrgEncryption struct {
	// Service seals and opens organization ciphertexts.
	Service *orgencryption.Service

	// Provisioner creates the use case's data encryption key the first time
	// an organization seals a value.
	Provisioner *orgencryption.Provisioner

	// Flags evaluates Config.Flag.
	Flags feature.Provider
}

// Sealer seals and opens secrets for one use case during the move.
type Sealer struct {
	logger *slog.Logger
	legacy *encryption.Client
	org    *OrgEncryption
	cfg    Config
}

// New builds a Sealer. org may be nil, as it is wherever organization
// encryption is not configured; the Sealer then never moves an organization.
func New(logger *slog.Logger, legacy *encryption.Client, cfg Config, org *OrgEncryption) *Sealer {
	if org != nil && (org.Service == nil || org.Provisioner == nil || org.Flags == nil) {
		org = nil
	}

	return &Sealer{
		logger: logger.With(attr.SlogComponent("orgencryption-cutover")),
		legacy: legacy,
		org:    org,
		cfg:    cfg,
	}
}

// Backfill stores the organization ciphertext Open sealed for a row that had
// none. It must leave the row alone when its application-key ciphertext no
// longer equals the one Open read, so a concurrent write is never undone.
type Backfill func(ctx context.Context, org string) error

// Moved reports whether the organization's secrets are being moved.
func (s *Sealer) Moved(ctx context.Context, organizationID string) bool {
	if s.org == nil || organizationID == "" {
		return false
	}

	on, err := s.org.Flags.IsFlagEnabledLocal(ctx, s.cfg.Flag, organizationID, nil, nil)
	if err != nil {
		s.logger.WarnContext(ctx, "evaluate organization encryption cutover flag", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		return false
	}

	return on
}

// Seal encrypts plaintext for storage. column is the "table.column" the
// organization ciphertext is stored in; it is bound into that ciphertext.
//
// The application-key ciphertext is always produced, and failing to produce
// it is the only error. When the organization is moved but its ciphertext
// cannot be sealed, the failure is logged and Org is left invalid: the row
// stays readable through the application-key copy and gains the organization
// copy on a later read.
func (s *Sealer) Seal(ctx context.Context, organizationID string, column string, plaintext []byte) (Ciphertexts, error) {
	sealed, err := s.SealApplicationKey(plaintext)
	if err != nil {
		return Ciphertexts{}, err
	}

	if !s.Moved(ctx, organizationID) {
		return sealed, nil
	}

	org, err := s.sealOrg(ctx, organizationID, column, plaintext)
	if err != nil {
		s.logger.WarnContext(ctx, "seal with organization encryption; keeping the application-key copy only", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		return sealed, nil
	}
	sealed.Org = conv.ToPGText(org)

	return sealed, nil
}

// SealApplicationKey encrypts plaintext with the application key alone, for a
// write that must not reach organization encryption, such as one made while
// holding a lock that organization encryption's own database reads could
// wait on. The row gains its organization copy on its next read through Open.
func (s *Sealer) SealApplicationKey(plaintext []byte) (Ciphertexts, error) {
	legacy, err := s.legacy.Encrypt(plaintext)
	if err != nil {
		return Ciphertexts{}, fmt.Errorf("seal with application key: %w", err)
	}

	return Ciphertexts{Legacy: legacy, Org: pgtype.Text{String: "", Valid: false}}, nil
}

// Open decrypts a stored secret. column must match the one Seal was given.
//
// For a moved organization a row with an organization ciphertext is opened
// with it alone, and a failure is returned rather than falling back, so a
// suspended or revoked key stops reads. A row without one is opened with the
// application key and, when backfill is set, sealed for the organization and
// handed to backfill; a backfill failure is logged and does not fail the read.
func (s *Sealer) Open(ctx context.Context, organizationID string, column string, stored Ciphertexts, backfill Backfill) (string, error) {
	moved := s.Moved(ctx, organizationID)
	if moved && stored.Org.Valid {
		encryptor, err := s.org.Service.For(organizationID, s.cfg.UseCase)
		if err != nil {
			return "", fmt.Errorf("open organization encryption: %w", err)
		}

		plaintext, err := encryptor.Decrypt(ctx, column, stored.Org.String)
		if err != nil {
			return "", fmt.Errorf("decrypt with organization encryption: %w", err)
		}

		return string(plaintext), nil
	}

	plaintext, err := s.legacy.Decrypt(stored.Legacy)
	if err != nil {
		return "", fmt.Errorf("decrypt with application key: %w", err)
	}

	if moved && backfill != nil {
		s.backfill(ctx, organizationID, column, []byte(plaintext), backfill)
	}

	return plaintext, nil
}

func (s *Sealer) backfill(ctx context.Context, organizationID string, column string, plaintext []byte, backfill Backfill) {
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))

	org, err := s.sealOrg(ctx, organizationID, column, plaintext)
	if err != nil {
		logger.WarnContext(ctx, "seal with organization encryption for backfill", attr.SlogError(err))
		return
	}

	if err := backfill(ctx, org); err != nil {
		logger.WarnContext(ctx, "store organization encryption backfill", attr.SlogError(err))
	}
}

// sealOrg encrypts with organization encryption, provisioning the use case's
// data encryption key the first time the organization needs it.
func (s *Sealer) sealOrg(ctx context.Context, organizationID string, column string, plaintext []byte) (string, error) {
	encryptor, err := s.org.Service.For(organizationID, s.cfg.UseCase)
	if err != nil {
		return "", fmt.Errorf("open organization encryption: %w", err)
	}

	ciphertext, err := encryptor.Encrypt(ctx, column, plaintext)
	if errors.Is(err, orgencryption.ErrNotProvisioned) {
		if err := s.org.Provisioner.EnsureUseCase(ctx, organizationID, s.cfg.UseCase, orgencryption.SystemActor()); err != nil {
			return "", fmt.Errorf("provision organization encryption: %w", err)
		}
		ciphertext, err = encryptor.Encrypt(ctx, column, plaintext)
	}
	if err != nil {
		return "", fmt.Errorf("encrypt with organization encryption: %w", err)
	}

	return ciphertext, nil
}

// AIProviderKeys moves AI provider and model credentials: AI integration API
// keys and webhook signing secrets, customer model provider keys, and
// provisioned OpenRouter keys.
var AIProviderKeys = Config{
	UseCase: orgencryption.UseCaseAIProviderKeys,
	Flag:    feature.FlagAIProviderKeysOrgEncryption,
}
