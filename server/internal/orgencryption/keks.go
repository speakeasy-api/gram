package orgencryption

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

// managedKeyIDPrefix prefixes the KMS key id of every managed KEK.
const managedKeyIDPrefix = "orgkek"

// KeyConfig locates Speakeasy's side of organization encryption.
type KeyConfig struct {
	// KeyRing is the Speakeasy-owned GCP KMS key ring managed KEKs are minted
	// in, fully qualified.
	KeyRing string

	// PlatformCredentialID is the platform-tier gcp_iam credential every
	// process reaches managed KEKs with.
	PlatformCredentialID uuid.UUID

	// PlatformServiceAccount is the service account the platform credential
	// must impersonate. Managed KEKs grant it, and customers grant it on their
	// own KEKs. A credential impersonating anything else is refused.
	PlatformServiceAccount string
}

func (c KeyConfig) validate() error {
	if err := gcpkms.ValidateKeyRingName(c.KeyRing); err != nil {
		return fmt.Errorf("organization encryption key ring: %w", err)
	}
	if c.PlatformCredentialID == uuid.Nil {
		return errors.New("organization encryption platform credential id is required")
	}
	if strings.TrimSpace(c.PlatformServiceAccount) == "" {
		return errors.New("organization encryption platform service account is required")
	}

	return nil
}

// managedKeyPrefix is the resource name prefix every managed KEK must have.
// A managed key outside it is refused, so a copied or tampered database
// cannot point the platform identity at an arbitrary key.
func (c KeyConfig) managedKeyPrefix() string {
	return c.KeyRing + "/cryptoKeys/" + managedKeyIDPrefix + "-"
}

// keyResolver opens KMS clients for KEKs.
type keyResolver struct {
	cfg      KeyConfig
	identity *gcpauth.Identity
	clients  gcpkms.EncryptionClientFactory
}

// openKey is a usable KEK. The caller must Close it.
type openKey struct {
	id           uuid.UUID
	resourceName string
	managed      bool
	client       gcpkms.EncryptionClient
}

func (k *openKey) Close() error {
	if err := k.client.Close(); err != nil {
		return fmt.Errorf("close kms client: %w", err)
	}

	return nil
}

// open validates that keyID can serve as one of the organization's KEKs and
// opens a client authenticated as the identity that reaches it.
func (r *keyResolver) open(ctx context.Context, logger *slog.Logger, q *repo.Queries, organizationID string, keyID uuid.UUID) (*openKey, error) {
	row, err := q.GetEncryptionKeyForUse(ctx, repo.GetEncryptionKeyForUseParams{ID: keyID, OrganizationID: organizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("%w: key not found", ErrInvalidKey)
	case err != nil:
		return nil, fmt.Errorf("load key encryption key: %w", err)
	}

	resourceName, err := r.checkKey(row)
	if err != nil {
		return nil, err
	}

	credential, problem, detail, err := r.identity.ScreenStoredCredential(ctx, logger, gcpauth.StoredCredential{
		Present:                   row.CredentialID.Valid,
		ImpersonateServiceAccount: row.ImpersonateServiceAccount.String,
		HasWifConfig:              row.WifPoolID.Valid || row.WifProviderID.Valid || row.WifProjectNumber.Valid,
		SkipProjectVerification:   row.SkipProjectVerification.Bool,
	})
	if err != nil {
		return nil, fmt.Errorf("screen key encryption key credential: %w", err)
	}
	if problem != "" {
		// A managed key is reached only through the pinned platform credential
		// (checkKey), so a screening problem there is Speakeasy's configuration
		// drift: it must not suspend the organization.
		if row.Managed {
			return nil, fmt.Errorf("%w: %s", ErrPlatformCredentialUnusable, detail)
		}
		return nil, fmt.Errorf("%w: %s", ErrInvalidKey, detail)
	}

	tokenSource, err := r.identity.TokenSource(ctx, credential)
	if err != nil {
		return nil, fmt.Errorf("build key encryption key token source: %w", err)
	}

	client, err := r.clients(ctx, tokenSource)
	if err != nil {
		return nil, fmt.Errorf("build key encryption key kms client: %w", err)
	}

	return &openKey{id: row.ID, resourceName: resourceName, managed: row.Managed, client: client}, nil
}

// checkKey enforces what makes a key usable as a KEK and returns its KMS
// resource name.
func (r *keyResolver) checkKey(row repo.GetEncryptionKeyForUseRow) (string, error) {
	switch {
	case row.Deleted:
		return "", fmt.Errorf("%w: key was deleted", ErrInvalidKey)
	case row.Purpose != externalKeyPurposeEncrypt:
		return "", fmt.Errorf("%w: key purpose is %q, not %q", ErrInvalidKey, row.Purpose, externalKeyPurposeEncrypt)
	case row.Provider != externalKeyProviderGcpKms:
		return "", fmt.Errorf("%w: only GCP KMS keys can encrypt organization data", ErrInvalidKey)
	case !row.ResourceName.Valid:
		return "", fmt.Errorf("%w: key is missing its GCP resource name", ErrInvalidKey)
	}

	if err := gcpkms.ValidateKeyName(row.ResourceName.String); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}

	if row.Managed {
		if err := r.checkManagedResource(row.ResourceName.String); err != nil {
			return "", err
		}
		if row.KmsDisabledAt.Valid {
			return "", fmt.Errorf("%w: managed key was disabled", ErrInvalidKey)
		}
		// The row names the credential, but only the configured platform
		// credential may reach a managed key.
		if !row.CredentialID.Valid || row.CredentialID.UUID != r.cfg.PlatformCredentialID {
			return "", fmt.Errorf("%w: managed key does not use the configured platform credential", ErrPlatformCredentialUnusable)
		}
		if !strings.EqualFold(strings.TrimSpace(row.ImpersonateServiceAccount.String), r.cfg.PlatformServiceAccount) {
			return "", fmt.Errorf("%w: managed key credential does not impersonate the platform service account", ErrPlatformCredentialUnusable)
		}
	}

	return row.ResourceName.String, nil
}

// checkManagedResource refuses a managed key outside the configured key ring.
func (r *keyResolver) checkManagedResource(resourceName string) error {
	if !strings.HasPrefix(resourceName, r.cfg.managedKeyPrefix()) {
		return fmt.Errorf("%w: managed key is outside the configured key ring", ErrInvalidKey)
	}

	return nil
}

// wrap encrypts a DEK under the KEK.
func (k *openKey) wrap(ctx context.Context, organizationID string, dataKeyID uuid.UUID, useCase string, dataKey []byte) (*gcpkms.EncryptResult, error) {
	result, err := k.client.Encrypt(ctx, k.resourceName, dataKey, wrapAssociatedData(organizationID, dataKeyID, useCase))
	if err != nil {
		return nil, fmt.Errorf("%w: wrap data encryption key: %w", ErrKeyUnavailable, err)
	}

	return result, nil
}

// unwrap decrypts a DEK the KEK wrapped.
func (k *openKey) unwrap(ctx context.Context, organizationID string, dataKeyID uuid.UUID, useCase string, wrapped []byte) ([]byte, error) {
	dataKey, err := k.client.Decrypt(ctx, k.resourceName, wrapped, wrapAssociatedData(organizationID, dataKeyID, useCase))
	if err != nil {
		return nil, fmt.Errorf("%w: unwrap data encryption key: %w", ErrKeyUnavailable, err)
	}

	return dataKey, nil
}
