// Package runnerkeys mints, stores and opens function runner keys: the
// per-function secret a Speakeasy Functions runner and the server use to
// authenticate each other. The server signs tool calls to the runner with it,
// the runner signs its callbacks with it, and the deploy path pushes it to the
// runner as an app secret.
//
// A functions_access row never stores its key in plaintext. It stores two
// sealed copies:
//
//   - encryption_key, the key sealed under the platform key.
//   - encryption_key_org_encrypted, the key sealed under the owning
//     organization's key.
//
// Which copies are written and read follows the organization's transition
// state for the function-runner-keys use case (package orgencryption). Moving
// a key between copies never changes the key itself, so deployed runners keep
// working through every transition. Every writer goes through Keys, and every
// statement that writes either copy lives in this package's queries.sql.
//
// Every write fences on the row's own organization_id. A row stored before
// that column existed has none, so it holds only a platform-key copy until the
// function runner key organization backfill fills the column in.
package runnerkeys

import (
	"context"
	"crypto/rand"
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
	"github.com/speakeasy-api/gram/server/internal/functions/runnerkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// Columns names the key's two sealed copies. OrgColumn is bound into every
// organization-key ciphertext, so it must never change.
var Columns = orgencryption.CopyColumns{
	UseCase:        orgencryption.UseCaseFunctionRunnerKeys,
	PlatformColumn: "functions_access.encryption_key",
	OrgColumn:      "functions_access.encryption_key_org_encrypted",
}

const (
	// keyBytes is the length of a runner key. Runners use it as an AES-256
	// key and an HS256 secret.
	keyBytes = 32

	// bearerFormat is the version of the bearer token format runners accept
	// for keys minted here.
	bearerFormat = "v01"

	// maxWriteAttempts bounds how often a write decides, seals and writes
	// again after the transition moved under it. Each retry follows a
	// committed transition, and an organization changes state a handful of
	// times in its life, so a third consecutive move means something is wrong.
	maxWriteAttempts = 3
)

var (
	// ErrNotStored means a write matched no project: it is gone, or its
	// organization changed since it was looked up.
	ErrNotStored = errors.New("runnerkeys: project is missing or moved to another organization")

	// ErrNoOrganization means the project's organization has no metadata
	// row, so there is no organization to seal a new key for.
	ErrNoOrganization = errors.New("runnerkeys: project's organization has no metadata row")
)

// Stored is a runner key as its functions_access row stores it.
type Stored struct {
	// OrganizationID is the row's organization_id, NULL for a row stored
	// before that column existed.
	OrganizationID pgtype.Text

	// Platform is encryption_key, the platform-key copy.
	Platform conv.Secret

	// Org is encryption_key_org_encrypted, the organization-key copy.
	Org pgtype.Text
}

// Keys is the one seam for function runner keys.
type Keys struct {
	logger   *slog.Logger
	cipher   *orgencryption.Cipher
	platform *encryption.Client
}

// New builds Keys over the process's cipher and platform key.
func New(logger *slog.Logger, cipher *orgencryption.Cipher, platform *encryption.Client) *Keys {
	return &Keys{
		logger:   logger.With(attr.SlogComponent("runnerkeys")),
		cipher:   cipher,
		platform: platform,
	}
}

// WithoutOrganizationEncryption builds Keys for a process without
// organization encryption, over its own transition state: organizations in
// platform_key work, and any other transition state fails closed. A process
// with organization encryption builds Keys with New over its shared cipher
// instead.
func WithoutOrganizationEncryption(logger *slog.Logger, db *pgxpool.Pool, platform *encryption.Client) *Keys {
	transitions := orgencryption.NewTransitions(logger, db, nil, nil, orgencryption.TransitionsConfig{ClearSettleDelay: 0})

	return New(logger, orgencryption.NewCipher(logger, platform, nil, transitions), platform)
}

// Open returns a runner key. It opens the copy the organization's transition
// state reads, from the process's cached transition state, so a tool call
// reads the database only on a cache miss. A row without an organization
// opens its platform-key copy.
func (k *Keys) Open(ctx context.Context, stored Stored) ([]byte, error) {
	copies := orgencryption.Copies{Platform: conv.ToPGTextEmpty(string(stored.Platform.Reveal())), Org: stored.Org}

	if !stored.OrganizationID.Valid || stored.OrganizationID.String == "" {
		if !copies.Platform.Valid {
			return nil, orgencryption.ErrMissing
		}
		key, err := k.platform.Decrypt(copies.Platform.String)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", orgencryption.ErrUnreadable, err)
		}
		return []byte(key), nil
	}

	decision, err := k.cipher.Decide(ctx, stored.OrganizationID.String, orgencryption.UseCaseFunctionRunnerKeys)
	if err != nil {
		return nil, fmt.Errorf("decide function runner key state: %w", err)
	}

	key, err := k.cipher.Open(ctx, decision, Columns, copies)
	if err != nil {
		return nil, fmt.Errorf("open function runner key: %w", err)
	}

	return key, nil
}

// Create mints a runner key for each of a deployment's functions and stores
// the copies the organization's transition state writes, one statement per
// key, sealed for the project's organization. It returns the functions_access
// row ids in the order of functionIDs.
//
// It takes the pool rather than a transaction because it seals before each
// write, and an organization key may wait on KMS to unwrap its data key. If it
// fails part way, the keys it stored stay, and calling it again for the
// remaining functions completes the deployment.
func (k *Keys) Create(ctx context.Context, db *pgxpool.Pool, projectID, deploymentID uuid.UUID, functionIDs []uuid.UUID) ([]uuid.UUID, error) {
	q := repo.New(db)
	organizationID, err := q.GetProjectOrganization(ctx, projectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrNoOrganization
	case err != nil:
		return nil, fmt.Errorf("get project organization: %w", err)
	}

	ids := make([]uuid.UUID, 0, len(functionIDs))
	for _, functionID := range functionIDs {
		id, err := k.create(ctx, q, repo.CreateRunnerKeyParams{
			DeploymentID:      deploymentID,
			FunctionID:        functionID,
			PlatformCopy:      nil,
			OrgCopy:           pgtype.Text{String: "", Valid: false},
			BearerFormat:      conv.ToPGText(bearerFormat),
			ProjectID:         projectID,
			OrganizationID:    organizationID,
			DecidedGeneration: 0,
		})
		if err != nil {
			return ids, fmt.Errorf("create runner key for function %s: %w", functionID, err)
		}
		ids = append(ids, id)
	}

	return ids, nil
}

// create mints one key and stores it with params, filling in its copies and
// the decided generation. When the transition moves between the decision and
// the write, it decides again and seals the same key again.
func (k *Keys) create(ctx context.Context, q *repo.Queries, params repo.CreateRunnerKeyParams) (uuid.UUID, error) {
	key := make([]byte, keyBytes)
	if _, err := rand.Read(key); err != nil {
		return uuid.Nil, fmt.Errorf("generate function runner key: %w", err)
	}

	decision, err := k.cipher.Decide(ctx, params.OrganizationID, orgencryption.UseCaseFunctionRunnerKeys)
	if err != nil {
		return uuid.Nil, fmt.Errorf("decide function runner key state: %w", err)
	}

	for range maxWriteAttempts {
		copies, err := k.cipher.Seal(ctx, decision, Columns, key)
		if errors.Is(err, orgencryption.ErrDecisionExpired) {
			if decision, err = k.cipher.Redecide(ctx, decision); err != nil {
				return uuid.Nil, fmt.Errorf("decide function runner key state again: %w", err)
			}
			continue
		}
		if err != nil {
			return uuid.Nil, fmt.Errorf("seal function runner key: %w", err)
		}

		params.PlatformCopy = nil
		if copies.Platform.Valid {
			params.PlatformCopy = []byte(copies.Platform.String)
		}
		params.OrgCopy = copies.Org
		params.DecidedGeneration = decision.Generation

		id, err := q.CreateRunnerKey(ctx, params)
		switch {
		case err == nil:
			return id, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return uuid.Nil, fmt.Errorf("store function runner key: %w", err)
		}

		fresh, err := k.cipher.Redecide(ctx, decision)
		if err != nil {
			return uuid.Nil, fmt.Errorf("decide function runner key state again: %w", err)
		}
		if fresh.Generation == decision.Generation {
			return uuid.Nil, ErrNotStored
		}

		k.logger.InfoContext(ctx, "function runner key write fenced by a transition, sealing again",
			attr.SlogOrganizationID(params.OrganizationID), attr.SlogOrgEncryptionUseCase(orgencryption.UseCaseFunctionRunnerKeys))
		decision = fresh
	}

	return uuid.Nil, fmt.Errorf("store function runner key: %w", orgencryption.ErrFenced)
}

// Error converts an error from Open or Create into an API error with msg as
// its public message. An organization key the customer has to fix is
// failed_precondition: only fixing the key helps. A key that is missing or
// does not decrypt, or a project without an organization, is an invariant
// violation that a retry cannot fix. Anything else, such as a database or
// transition state error, is unexpected and may clear on retry.
func Error(ctx context.Context, logger *slog.Logger, err error, msg string) error {
	switch {
	case errors.Is(err, orgencryption.ErrSuspended), errors.Is(err, orgencryption.ErrKeyUnavailable), errors.Is(err, orgencryption.ErrInvalidKey):
		return oops.E(oops.CodeFailedPrecondition, err, "the organization's encryption key cannot be used for Speakeasy Functions credentials; fix the key to restore function tool calls").
			LogWarn(ctx, logger, attr.SlogOrgEncryptionUseCase(orgencryption.UseCaseFunctionRunnerKeys))
	case errors.Is(err, orgencryption.ErrMissing), errors.Is(err, orgencryption.ErrUnreadable), errors.Is(err, ErrNoOrganization):
		return oops.E(oops.CodeInvariantViolation, err, "%s", msg).LogError(ctx, logger)
	default:
		return oops.E(oops.CodeUnexpected, err, "%s", msg).LogError(ctx, logger)
	}
}
