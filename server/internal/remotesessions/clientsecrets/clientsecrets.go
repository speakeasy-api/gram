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
// A client row without an organization stays on the platform key: a global
// client, which has neither a project nor an organization, and a project
// client stored before project clients carried their organization. Every
// write fences on the row's own organization column, so a row without one can
// only be written under generation 0. Such a project client's secret is
// written only while its project's organization is in platform_key; once that
// organization is enrolled, Set refuses it with ErrUnattributed until the
// client's organization_id is backfilled, so no platform-key copy appears
// after finalize. The CopyStore counts such project clients holding a secret
// as unattributed, which keeps their organization from finalizing.
//
// Clearing the platform-key copies after finalize is not a new revision,
// including for a secret recorded as unreadable, whose only stored copy that
// clear removes: CopyStore writes change only the copy column they name, and
// that secret never opened.
//
// Transition state is read through the database handle the caller holds. The
// organization key's configuration and data key are still loaded from the
// process's pool on a cache miss, so a writer holding a transaction can briefly
// need a second pooled connection when its organization is past platform_key.
package clientsecrets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

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
	// ErrClientNotFound means the client to write does not exist, is deleted,
	// or belongs to another organization.
	ErrClientNotFound = errors.New("clientsecrets: remote session client not found")

	// ErrUnattributed means a project client stored without its organization
	// cannot have its secret written because its project's organization is
	// past platform_key. Its organization_id has to be backfilled first.
	ErrUnattributed = errors.New("clientsecrets: project client is stored without its organization")
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

// platformOnly reports whether the client row carries no organization, so
// its secret stays on the platform key.
func (o Owner) platformOnly() bool {
	return !o.OrganizationID.Valid || o.OrganizationID.String == ""
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

// keyProblems are the errors that mean an organization key could not be used:
// an unreachable, revoked, suspended, disabled or misconfigured key, a process
// without organization encryption serving an enrolled organization, or
// transition state that kept moving under a write.
var keyProblems = []error{
	orgencryption.ErrKeyUnavailable,
	orgencryption.ErrSuspended,
	orgencryption.ErrNotProvisioned,
	orgencryption.ErrInvalidKey,
	orgencryption.ErrInvalidState,
	orgencryption.ErrPlatformCredentialUnusable,
	orgencryption.ErrOrganizationEncryptionUnavailable,
	orgencryption.ErrConcurrentChange,
	orgencryption.ErrFenced,
	orgencryption.ErrDecisionExpired,
}

// IsUnreadable reports whether err from Open means the stored secret itself
// is missing or does not decrypt, so retrying cannot help and the client's
// configuration needs attention. Every other error from Open, including key
// problems and database failures, may clear on its own: callers treat it as
// retryable and never as a broken client configuration.
func IsUnreadable(err error) bool {
	return errors.Is(err, orgencryption.ErrUnreadable) || errors.Is(err, orgencryption.ErrMissing)
}

// IsKeyProblem reports whether err from Open, Set or RequireUsable is a
// problem with the organization key rather than with the secret or the
// database. Callers use it to say so in their messages; deciding whether an
// error is retryable is IsUnreadable's job.
func IsKeyProblem(err error) bool {
	for _, problem := range keyProblems {
		if errors.Is(err, problem) {
			return true
		}
	}

	return false
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

// WithoutOrganizationEncryption builds Secrets for a process without
// organization encryption, over its own transition state: organizations in
// platform_key work, and any other transition state fails closed. A process
// with organization encryption builds Secrets with New over its shared
// cipher instead.
func WithoutOrganizationEncryption(logger *slog.Logger, db *pgxpool.Pool, platform *encryption.Client) *Secrets {
	transitions := orgencryption.NewTransitions(logger, db, nil, nil, orgencryption.TransitionsConfig{ClearSettleDelay: 0})

	return New(logger, orgencryption.NewCipher(logger, platform, nil, transitions), platform)
}

// Open decrypts the copy the owner's transition state reads. db is whatever
// the caller already holds (a connection or a transaction); a cache miss
// reads transition state through it. A nil db reads through the process's
// pool, for callers that hold no connection.
func (s *Secrets) Open(ctx context.Context, db repo.DBTX, owner Owner, stored orgencryption.Copies) (string, error) {
	if owner.platformOnly() {
		if !stored.Platform.Valid {
			return "", orgencryption.ErrMissing
		}

		plaintext, err := s.platform.Decrypt(stored.Platform.String)
		if err != nil {
			return "", fmt.Errorf("%w: %w", orgencryption.ErrUnreadable, err)
		}

		return plaintext, nil
	}

	decision, err := s.decide(ctx, db, owner)
	if err != nil {
		return "", err
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
// when it fails. db is whatever the caller already holds, or nil.
func (s *Secrets) RequireUsable(ctx context.Context, db repo.DBTX, owner Owner) error {
	if owner.platformOnly() {
		return s.requireUnattributedWritable(ctx, db, owner)
	}

	decision, err := s.decide(ctx, db, owner)
	if err != nil {
		return err
	}

	if err := s.cipher.RequireUsable(ctx, decision); err != nil {
		return fmt.Errorf("client secret key: %w", err)
	}

	return nil
}

// requireUnattributedWritable refuses a write to a project client stored
// without its organization once the project's organization is past
// platform_key: the write would land on the platform key, which the
// organization no longer uses or is moving off. Global clients always pass.
func (s *Secrets) requireUnattributedWritable(ctx context.Context, db repo.DBTX, owner Owner) error {
	if !owner.ProjectID.Valid {
		return nil
	}
	if db == nil {
		return fmt.Errorf("%w: no database handle to resolve its project's organization", ErrUnattributed)
	}

	organizationID, err := repo.New(db).GetRemoteSessionClientProjectOrganization(ctx, owner.ProjectID.UUID)
	if err != nil {
		return fmt.Errorf("read client project organization: %w", err)
	}

	decision, err := s.decide(ctx, db, OwnerOf(conv.ToPGText(organizationID), uuid.NullUUID{UUID: uuid.Nil, Valid: false}))
	if err != nil {
		return err
	}
	if decision.State != orgencryption.TransitionStatePlatformKey {
		return ErrUnattributed
	}

	return nil
}

// decide returns the decision for the owner's organization, reading through
// db on a cache miss, or through the process's pool when db is nil.
func (s *Secrets) decide(ctx context.Context, db repo.DBTX, owner Owner) (orgencryption.Decision, error) {
	var decision orgencryption.Decision
	var err error
	if db == nil {
		decision, err = s.cipher.Decide(ctx, owner.OrganizationID.String, orgencryption.UseCaseOAuthClientSecrets)
	} else {
		decision, err = s.cipher.DecideIn(ctx, db, owner.OrganizationID.String, orgencryption.UseCaseOAuthClientSecrets)
	}
	if err != nil {
		return orgencryption.Decision{}, fmt.Errorf("decide client secret state: %w", err)
	}

	return decision, nil
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

	if owner.platformOnly() {
		if err := s.requireUnattributedWritable(ctx, dbtx, owner); err != nil {
			return repo.RemoteSessionClient{}, err
		}

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
			ProjectID:             owner.ProjectID,
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

	organizationID := owner.OrganizationID.String
	decision, err := s.cipher.DecideIn(ctx, dbtx, organizationID, orgencryption.UseCaseOAuthClientSecrets)
	if err != nil {
		return repo.RemoteSessionClient{}, fmt.Errorf("decide client secret state: %w", err)
	}

	for range maxWriteAttempts {
		copies, err := s.cipher.Seal(ctx, decision, Columns, plaintext)
		if errors.Is(err, orgencryption.ErrDecisionExpired) {
			decision, err = s.cipher.RedecideIn(ctx, dbtx, decision)
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
			ProjectID:             owner.ProjectID,
			DecidedGeneration:     decision.Generation,
		})
		switch {
		case err == nil:
			return row, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return repo.RemoteSessionClient{}, fmt.Errorf("store client secret: %w", err)
		}

		fresh, err := s.cipher.RedecideIn(ctx, dbtx, decision)
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
		ProjectID:      owner.ProjectID,
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
	var none repo.RemoteSessionClient
	if !Has(stored) {
		return none, false, nil
	}

	plaintext, err := s.Open(ctx, dbtx, source, stored)
	if err != nil {
		return none, false, fmt.Errorf("open source client secret: %w", err)
	}

	row, err := s.Set(ctx, dbtx, targetID, target, []byte(plaintext), expiresAt)
	if err != nil {
		return none, false, err
	}

	return row, true, nil
}
