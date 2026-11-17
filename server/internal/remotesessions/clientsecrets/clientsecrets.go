// Package clientsecrets seals, opens, stores and erases the client secrets of
// remote session clients, the OAuth clients Speakeasy uses upstream.
//
// A secret has two stored copies: client_secret_encrypted under the platform
// key and client_secret_org_encrypted under the owning organization's key.
// Which copies are written and read follows the organization's transition
// state for the oauth-client-secrets use case (package orgencryption). Every
// reader and writer goes through Secrets, so they all follow the same rules,
// and every statement that writes either copy lives in
// clientsecret_queries.sql in package remotesessions.
//
// client_secret_generation is the secret's revision. It increases with every
// new, replaced or removed secret, and stays the same when the same secret is
// sealed again (backfill, a retried write, finalize, key rotation). Anything
// that needs to know the secret changed reads it instead of comparing
// ciphertexts, which change on every seal.
//
// A global client, with neither a project nor an organization, always stays
// on the platform key. A project client must carry its organization: one
// stored without it cannot be sealed for its organization, so Secrets refuses
// it with ErrUnattributed.
package clientsecrets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// Columns names the secret's two copies. OrgColumn is bound into every
// organization-key ciphertext, so it must never change.
var Columns = orgencryption.CopyColumns{
	UseCase:        orgencryption.UseCaseOAuthClientSecrets,
	PlatformColumn: "remote_session_clients.client_secret_encrypted",
	OrgColumn:      "remote_session_clients.client_secret_org_encrypted",
}

// maxWriteAttempts bounds how often a write decides, seals and writes again
// after the transition moved under it. Each retry follows a committed
// transition, and an organization changes state a handful of times in its
// life, so a third consecutive move means something is wrong.
const maxWriteAttempts = 3

var (
	// ErrUnattributed means a project client is stored without its
	// organization, so its secret cannot be sealed for or opened with the
	// organization's key.
	ErrUnattributed = errors.New("clientsecrets: project client is stored without its organization")

	// ErrClientNotFound means the client to write does not exist, is deleted,
	// or belongs to another organization.
	ErrClientNotFound = errors.New("clientsecrets: remote session client not found")
)

// Owner is who a client belongs to, which decides the key its secret is
// sealed with.
type Owner struct {
	// OrganizationID is the owning organization, NULL for a global client.
	OrganizationID pgtype.Text

	// ProjectID is the owning project, NULL for an organization or global
	// client.
	ProjectID uuid.NullUUID
}

// OwnerOf returns the owner of a client row's organization and project
// columns.
func OwnerOf(organizationID pgtype.Text, projectID uuid.NullUUID) Owner {
	return Owner{OrganizationID: organizationID, ProjectID: projectID}
}

// global reports whether the owner is a global client, which always stays
// on the platform key.
func (o Owner) global() bool {
	return !o.OrganizationID.Valid && !o.ProjectID.Valid
}

// organization returns the organization that seals the secret.
func (o Owner) organization() (string, error) {
	if !o.OrganizationID.Valid || o.OrganizationID.String == "" {
		return "", ErrUnattributed
	}

	return o.OrganizationID.String, nil
}

// Stored returns the copies a client row stores.
func Stored(platform, org pgtype.Text) orgencryption.Copies {
	return orgencryption.Copies{Platform: platform, Org: org}
}

// Has reports whether a client row stores a secret in either copy. A secret
// of the empty string is still a stored secret.
func Has(stored orgencryption.Copies) bool {
	return stored.Platform.Valid || stored.Org.Valid
}

// IsKeyProblem reports whether err from Open, Set or RequireUsable is a
// problem with a key that may clear on its own, such as an unreachable or
// suspended organization key, rather than a missing, unreadable or
// unattributable secret. Callers report a key problem as retryable and never
// as a broken client configuration.
func IsKeyProblem(err error) bool {
	if err == nil {
		return false
	}

	return !errors.Is(err, orgencryption.ErrUnreadable) &&
		!errors.Is(err, orgencryption.ErrMissing) &&
		!errors.Is(err, ErrUnattributed) &&
		!errors.Is(err, ErrClientNotFound)
}

// Secrets is the one seam for client secrets.
type Secrets struct {
	logger   *slog.Logger
	cipher   *orgencryption.Cipher
	platform *encryption.Client
}

// New builds Secrets over the process's cipher and platform key.
func New(logger *slog.Logger, cipher *orgencryption.Cipher, platform *encryption.Client) *Secrets {
	return &Secrets{
		logger:   logger.With(attr.SlogComponent("clientsecrets")),
		cipher:   cipher,
		platform: platform,
	}
}

// Open decrypts the copy the owner's transition state reads.
func (s *Secrets) Open(ctx context.Context, owner Owner, stored orgencryption.Copies) (string, error) {
	if owner.global() {
		if !stored.Platform.Valid {
			return "", orgencryption.ErrMissing
		}

		plaintext, err := s.platform.Decrypt(stored.Platform.String)
		if err != nil {
			return "", fmt.Errorf("%w: %w", orgencryption.ErrUnreadable, err)
		}

		return plaintext, nil
	}

	organizationID, err := owner.organization()
	if err != nil {
		return "", err
	}

	decision, err := s.cipher.Decide(ctx, organizationID, orgencryption.UseCaseOAuthClientSecrets)
	if err != nil {
		return "", fmt.Errorf("decide client secret state: %w", err)
	}

	plaintext, err := s.cipher.Open(ctx, decision, Columns, stored)
	if err != nil {
		return "", fmt.Errorf("open client secret: %w", err)
	}

	return string(plaintext), nil
}

// RequireUsable checks that a secret could be sealed for the owner now.
// Callers run it before an irreversible call to a provider whose secret must
// then be stored, such as a dynamic client registration, and skip the call
// when it fails.
func (s *Secrets) RequireUsable(ctx context.Context, owner Owner) error {
	if owner.global() {
		return nil
	}

	organizationID, err := owner.organization()
	if err != nil {
		return err
	}

	decision, err := s.cipher.Decide(ctx, organizationID, orgencryption.UseCaseOAuthClientSecrets)
	if err != nil {
		return fmt.Errorf("decide client secret state: %w", err)
	}

	if err := s.cipher.RequireUsable(ctx, decision); err != nil {
		return fmt.Errorf("client secret key: %w", err)
	}

	return nil
}

// Set stores plaintext as the client's new or replaced secret, with the
// expiry the issuer reported for it, and returns the updated row. It counts a
// new revision and clears the client's upstream rejection marker. Run it in
// the same transaction as the write that created or changed the client.
//
// When the transition moves between the decision and the write, Set decides
// again and seals the same plaintext again; it never asks the caller to
// repeat whatever produced the secret.
func (s *Secrets) Set(ctx context.Context, dbtx repo.DBTX, clientID uuid.UUID, owner Owner, plaintext []byte, expiresAt pgtype.Timestamptz) (repo.RemoteSessionClient, error) {
	q := repo.New(dbtx)

	if owner.global() {
		platform, err := s.platform.Encrypt(plaintext)
		if err != nil {
			return repo.RemoteSessionClient{}, fmt.Errorf("encrypt client secret with platform key: %w", err)
		}

		row, err := q.SetRemoteSessionClientSecret(ctx, repo.SetRemoteSessionClientSecretParams{
			PlatformCopy:          conv.ToPGText(platform),
			OrgCopy:               pgtype.Text{String: "", Valid: false},
			ClientSecretExpiresAt: expiresAt,
			ID:                    clientID,
			OrganizationID:        owner.OrganizationID,
			DecidedGeneration:     0,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.RemoteSessionClient{}, ErrClientNotFound
		}
		if err != nil {
			return repo.RemoteSessionClient{}, fmt.Errorf("store client secret: %w", err)
		}

		return row, nil
	}

	organizationID, err := owner.organization()
	if err != nil {
		return repo.RemoteSessionClient{}, err
	}

	decision, err := s.cipher.Decide(ctx, organizationID, orgencryption.UseCaseOAuthClientSecrets)
	if err != nil {
		return repo.RemoteSessionClient{}, fmt.Errorf("decide client secret state: %w", err)
	}

	for range maxWriteAttempts {
		copies, err := s.cipher.Seal(ctx, decision, Columns, plaintext)
		if errors.Is(err, orgencryption.ErrDecisionExpired) {
			decision, err = s.cipher.Redecide(ctx, decision)
			if err != nil {
				return repo.RemoteSessionClient{}, fmt.Errorf("decide client secret state again: %w", err)
			}
			continue
		}
		if err != nil {
			return repo.RemoteSessionClient{}, fmt.Errorf("seal client secret: %w", err)
		}

		row, err := q.SetRemoteSessionClientSecret(ctx, repo.SetRemoteSessionClientSecretParams{
			PlatformCopy:          copies.Platform,
			OrgCopy:               copies.Org,
			ClientSecretExpiresAt: expiresAt,
			ID:                    clientID,
			OrganizationID:        owner.OrganizationID,
			DecidedGeneration:     decision.Generation,
		})
		switch {
		case err == nil:
			return row, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return repo.RemoteSessionClient{}, fmt.Errorf("store client secret: %w", err)
		}

		fresh, err := s.cipher.Redecide(ctx, decision)
		if err != nil {
			return repo.RemoteSessionClient{}, fmt.Errorf("decide client secret state again: %w", err)
		}
		if fresh.Generation == decision.Generation {
			return repo.RemoteSessionClient{}, ErrClientNotFound
		}

		s.logger.InfoContext(ctx, "client secret write fenced by a transition, sealing again",
			attr.SlogOrganizationID(organizationID), attr.SlogOrgEncryptionUseCase(orgencryption.UseCaseOAuthClientSecrets))
		decision = fresh
	}

	return repo.RemoteSessionClient{}, fmt.Errorf("store client secret: %w", orgencryption.ErrFenced)
}

// Clear removes the client's secret, both copies, and returns the updated
// row. It counts a new revision. Erasing is correct under any transition
// state, so it needs no decision.
func (s *Secrets) Clear(ctx context.Context, dbtx repo.DBTX, clientID uuid.UUID, owner Owner) (repo.RemoteSessionClient, error) {
	row, err := repo.New(dbtx).ClearRemoteSessionClientSecret(ctx, repo.ClearRemoteSessionClientSecretParams{
		ID:             clientID,
		OrganizationID: owner.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return repo.RemoteSessionClient{}, ErrClientNotFound
	}
	if err != nil {
		return repo.RemoteSessionClient{}, fmt.Errorf("clear client secret: %w", err)
	}

	return row, nil
}

// Copy stores the secret of the source client as the target client's
// secret, opening it with the source's key and sealing it for the target's.
// A ciphertext is bound to its organization and column, so a copy between
// clients is always opened and sealed again, never copied as stored. Copy
// does nothing when the source has no secret.
func (s *Secrets) Copy(ctx context.Context, dbtx repo.DBTX, source Owner, stored orgencryption.Copies, targetID uuid.UUID, target Owner, expiresAt pgtype.Timestamptz) (repo.RemoteSessionClient, bool, error) {
	if !Has(stored) {
		return repo.RemoteSessionClient{}, false, nil
	}

	plaintext, err := s.Open(ctx, source, stored)
	if err != nil {
		return repo.RemoteSessionClient{}, false, fmt.Errorf("open source client secret: %w", err)
	}

	row, err := s.Set(ctx, dbtx, targetID, target, []byte(plaintext), expiresAt)
	if err != nil {
		return repo.RemoteSessionClient{}, false, err
	}

	return row, true, nil
}
