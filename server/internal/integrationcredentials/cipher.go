// Package integrationcredentials seals the credentials Speakeasy holds for
// third-party integrations (Slack workspaces, device integrations and network
// ingress providers) while they move from the application key to organization
// encryption.
//
// During the move a row carries two ciphertexts of the same credential:
// credentials_encrypted under the application key, which is always written,
// and credentials_org_encrypted under the organization's key, which is written
// only for organizations enrolled in
// feature.FlagOrgEncryptedIntegrationCredentials. An enrolled organization
// reads the organization ciphertext and fails closed when it does not open.
// Other organizations, and processes without organization encryption, read the
// application-key ciphertext. Turning the flag off is therefore the rollback,
// and a write while the flag is off clears the organization ciphertext so it
// is never older than the application-key one. Backfiller seals the rows an
// organization wrote before it was enrolled.
package integrationcredentials

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

// Column is the "table.column" an organization ciphertext is stored in. It is
// bound into the ciphertext, so a value copied to another column fails to
// decrypt.
type Column string

const (
	ColumnSlackDirectoryConnections Column = "slack_directory_connections.credentials_org_encrypted"
	ColumnDeviceIntegrationConfigs  Column = "device_integration_configs.credentials_org_encrypted"
	ColumnNetworkIngresses          Column = "network_ingresses.credentials_org_encrypted"
)

// ErrUnreadable means a stored credential does not decrypt: it is corrupt or
// was sealed for another organization, column or key. Retrying cannot help.
// Other errors from Open, such as an unreachable or suspended organization
// key, may clear on their own.
var ErrUnreadable = errors.New("integration credentials do not decrypt")

// ErrMissing means the row holds no credential to open.
var ErrMissing = errors.New("integration credentials are not set")

// Sealed is a credential encrypted for storage.
type Sealed struct {
	// Legacy is the application-key ciphertext, for credentials_encrypted.
	Legacy string

	// Org is the organization ciphertext, for credentials_org_encrypted. It is
	// NULL when the organization is not enrolled.
	Org pgtype.Text
}

// Stored is a credential as read from a row.
type Stored struct {
	// Legacy is credentials_encrypted.
	Legacy pgtype.Text

	// Org is credentials_org_encrypted.
	Org pgtype.Text
}

// Cipher seals and opens integration credentials.
type Cipher struct {
	logger      *slog.Logger
	legacy      *encryption.Client
	org         *orgencryption.Service
	provisioner *orgencryption.Provisioner
	features    feature.Provider
}

// New builds a Cipher. org and features may be nil, in which case every
// organization reads and writes only the application-key ciphertext.
// provisioner may be nil in a process that cannot provision keys; an enrolled
// organization's writes then fail until another process has provisioned its
// use case.
func New(logger *slog.Logger, legacy *encryption.Client, org *orgencryption.Service, provisioner *orgencryption.Provisioner, features feature.Provider) *Cipher {
	return &Cipher{
		logger:      logger.With(attr.SlogComponent("integrationcredentials")),
		legacy:      legacy,
		org:         org,
		provisioner: provisioner,
		features:    features,
	}
}

// Enrolled reports whether the organization's credentials are sealed under
// its own key in this process.
func (c *Cipher) Enrolled(ctx context.Context, organizationID string) bool {
	return c.org != nil && feature.OrgEncryptedIntegrationCredentials(ctx, c.features, organizationID)
}

// Seal encrypts a credential for storage in column of a row owned by the
// organization.
func (c *Cipher) Seal(ctx context.Context, organizationID string, column Column, plaintext []byte) (Sealed, error) {
	legacy, err := c.legacy.Encrypt(plaintext)
	if err != nil {
		return Sealed{}, fmt.Errorf("encrypt with application key: %w", err)
	}

	sealed := Sealed{Legacy: legacy, Org: pgtype.Text{String: "", Valid: false}}
	if !c.Enrolled(ctx, organizationID) {
		return sealed, nil
	}

	org, err := c.sealOrg(ctx, organizationID, column, plaintext)
	if err != nil {
		return Sealed{}, err
	}
	sealed.Org = conv.ToPGText(org)

	return sealed, nil
}

func (c *Cipher) sealOrg(ctx context.Context, organizationID string, column Column, plaintext []byte) (string, error) {
	if c.provisioner != nil {
		if err := c.provisioner.EnsureUseCase(ctx, organizationID, orgencryption.UseCaseIntegrationCredentials, orgencryption.SystemActor()); err != nil {
			return "", fmt.Errorf("provision organization encryption: %w", err)
		}
	}

	encryptor, err := c.org.For(organizationID, orgencryption.UseCaseIntegrationCredentials)
	if err != nil {
		return "", fmt.Errorf("organization encryptor: %w", err)
	}

	ciphertext, err := encryptor.Encrypt(ctx, string(column), plaintext)
	if err != nil {
		return "", fmt.Errorf("encrypt with organization key: %w", err)
	}

	return ciphertext, nil
}

// Open decrypts a credential read from column of a row owned by the
// organization. An enrolled organization's row that has an organization
// ciphertext is read from it, with no fallback to the application key.
func (c *Cipher) Open(ctx context.Context, organizationID string, column Column, stored Stored) ([]byte, error) {
	if stored.Org.Valid && c.Enrolled(ctx, organizationID) {
		return c.openOrg(ctx, organizationID, column, stored.Org.String)
	}
	if !stored.Legacy.Valid {
		return nil, ErrMissing
	}

	plaintext, err := c.legacy.Decrypt(stored.Legacy.String)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}

	return []byte(plaintext), nil
}

func (c *Cipher) openOrg(ctx context.Context, organizationID string, column Column, ciphertext string) ([]byte, error) {
	encryptor, err := c.org.For(organizationID, orgencryption.UseCaseIntegrationCredentials)
	if err != nil {
		return nil, fmt.Errorf("organization encryptor: %w", err)
	}

	plaintext, err := encryptor.Decrypt(ctx, string(column), ciphertext)
	switch {
	case errors.Is(err, orgencryption.ErrInvalidCiphertext):
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	case err != nil:
		c.logger.WarnContext(ctx, "decrypt integration credentials with organization key", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		return nil, fmt.Errorf("decrypt with organization key: %w", err)
	}

	return plaintext, nil
}
