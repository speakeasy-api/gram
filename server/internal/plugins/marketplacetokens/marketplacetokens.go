// Package marketplacetokens mints, stores and opens plugin marketplace tokens:
// the URL-as-secret credentials in marketplace install URLs, which the
// marketplace proxy trades for a clone of a project's marketplace repository.
//
// A connection never stores its token in plaintext. It stores three things:
//
//   - marketplace_token_hash, the hex SHA-256 the proxy resolves an install
//     URL by. Serving a clone needs nothing else.
//   - marketplace_token_encrypted, the token sealed under the platform key.
//   - marketplace_token_org_encrypted, the token sealed under the owning
//     organization's key.
//
// The server opens a sealed copy only to show the install URL again: in the
// publish status, the Codex install script and the device agent poll. Which
// copies are written and read follows the organization's transition state for
// the marketplace-tokens use case (package orgencryption). Every writer goes
// through Tokens, and every statement that writes any of these columns lives
// in marketplacetoken_queries.sql in package plugins.
//
// Until the contract migration, a connection published before tokens were
// hashed can still hold its token in the deprecated plaintext
// marketplace_token column. Open falls back to it, nothing writes it, and
// MoveLegacy, which the marketplace token backfill runs, moves it to the hash
// and copies.
//
// Transition state is read through the database handle the caller holds. The
// organization key's configuration and data key are still loaded from the
// process's pool on a cache miss, so a writer holding a transaction can
// briefly need a second pooled connection when its organization is past
// platform_key.
package marketplacetokens

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// Columns names the token's two sealed copies. OrgColumn is bound into every
// organization-key ciphertext, so it must never change.
var Columns = orgencryption.CopyColumns{
	UseCase:        orgencryption.UseCaseMarketplaceTokens,
	PlatformColumn: "plugin_github_connections.marketplace_token_encrypted",
	OrgColumn:      "plugin_github_connections.marketplace_token_org_encrypted",
}

const (
	// tokenBytes sets the entropy of a token. 32 bytes (256 bits) is well past
	// the threshold for resisting offline guessing even at internet scale, so
	// an unsalted SHA-256 is enough to look it up by. The base64url encoding
	// without padding lands at exactly 43 characters from [A-Za-z0-9_-]; the
	// marketplace proxy's handler precheck pins that shape.
	tokenBytes = 32

	// maxWriteAttempts bounds how often a write decides, seals and writes
	// again after the transition moved under it. Each retry follows a
	// committed transition, and an organization changes state a handful of
	// times in its life, so a third consecutive move means something is wrong.
	maxWriteAttempts = 3
)

var (
	// ErrNotStored means the write matched no connection: it is gone, belongs
	// to another organization, or already holds a marketplace token.
	ErrNotStored = errors.New("marketplacetokens: connection is missing, belongs to another organization, or already holds a marketplace token")

	// ErrUnattributed means a connection has no organization_id to seal its
	// token for.
	ErrUnattributed = errors.New("marketplacetokens: connection is stored without its organization")

	// ErrEmptyToken means a write was asked to store an empty token, which
	// would hash to a value every empty install URL resolves to.
	ErrEmptyToken = errors.New("marketplacetokens: token is empty")
)

// Mint returns a new token.
func Mint() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Hash returns the hex SHA-256 of a token, the value a connection is looked
// up by.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Stored is a connection's marketplace token as its row stores it.
type Stored struct {
	// OrganizationID is the connection's organization_id, NULL for a
	// connection stored before that column existed.
	OrganizationID pgtype.Text

	// Hash is marketplace_token_hash.
	Hash pgtype.Text

	// Copies are the platform-key and organization-key copies.
	Copies orgencryption.Copies

	// Legacy is the deprecated plaintext marketplace_token column.
	Legacy pgtype.Text
}

// FromConnection returns the token a connection row stores.
func FromConnection(row repo.PluginGithubConnection) Stored {
	return Stored{
		OrganizationID: row.OrganizationID,
		Hash:           row.MarketplaceTokenHash,
		Copies:         orgencryption.Copies{Platform: row.MarketplaceTokenEncrypted, Org: row.MarketplaceTokenOrgEncrypted},
		Legacy:         row.MarketplaceToken,
	}
}

// Has reports whether the connection holds a token: a hash, or a plaintext
// token the backfill has not moved yet.
func (s Stored) Has() bool {
	return Present(s.Hash, s.Legacy)
}

// Present reports whether a connection with this hash and plaintext column
// holds a token, for callers that read only those columns.
func Present(hash, legacy pgtype.Text) bool {
	return (hash.Valid && hash.String != "") || (legacy.Valid && legacy.String != "")
}

// Tokens is the one seam for marketplace tokens.
type Tokens struct {
	logger   *slog.Logger
	cipher   *orgencryption.Cipher
	platform *encryption.Client
}

// New builds Tokens over the process's cipher and platform key.
func New(logger *slog.Logger, cipher *orgencryption.Cipher, platform *encryption.Client) *Tokens {
	return &Tokens{
		logger:   logger.With(attr.SlogComponent("marketplacetokens")),
		cipher:   cipher,
		platform: platform,
	}
}

// WithoutOrganizationEncryption builds Tokens for a process without
// organization encryption, over its own transition state: organizations in
// platform_key work, and any other transition state fails closed. A process
// with organization encryption builds Tokens with New over its shared cipher
// instead.
func WithoutOrganizationEncryption(logger *slog.Logger, db *pgxpool.Pool, platform *encryption.Client) *Tokens {
	transitions := orgencryption.NewTransitions(logger, db, nil, nil, orgencryption.TransitionsConfig{ClearSettleDelay: 0})

	return New(logger, orgencryption.NewCipher(logger, platform, nil, transitions), platform)
}

// Open returns a connection's token. It opens the copy the organization's
// transition state reads, and refuses a copy that does not match the stored
// hash, so a URL it returns always resolves. A connection the backfill has
// not moved yet returns its plaintext token. db is whatever the caller already
// holds (a connection or a transaction); a cache miss reads transition state
// through it. A nil db reads through the process's pool.
func (t *Tokens) Open(ctx context.Context, db repo.DBTX, stored Stored) (string, error) {
	if !stored.Hash.Valid || stored.Hash.String == "" {
		if stored.Legacy.Valid && stored.Legacy.String != "" {
			return stored.Legacy.String, nil
		}

		return "", orgencryption.ErrMissing
	}

	var plaintext []byte
	if !stored.OrganizationID.Valid || stored.OrganizationID.String == "" {
		// Every write fences on the row's own organization, so a connection
		// without one can only hold a platform-key copy.
		if !stored.Copies.Platform.Valid {
			return "", orgencryption.ErrMissing
		}
		opened, err := t.platform.Decrypt(stored.Copies.Platform.String)
		if err != nil {
			return "", fmt.Errorf("%w: %w", orgencryption.ErrUnreadable, err)
		}
		plaintext = []byte(opened)
	} else {
		decision, err := t.decide(ctx, db, stored.OrganizationID.String)
		if err != nil {
			return "", err
		}
		plaintext, err = t.cipher.Open(ctx, decision, Columns, stored.Copies)
		if err != nil {
			return "", fmt.Errorf("open marketplace token: %w", err)
		}
	}

	token := string(plaintext)
	if subtle.ConstantTimeCompare([]byte(Hash(token)), []byte(stored.Hash.String)) != 1 {
		return "", fmt.Errorf("%w: sealed marketplace token does not match its hash", orgencryption.ErrUnreadable)
	}

	return token, nil
}

// Ensure gives a connection without a token a new one. Run it in the
// transaction that upserted the connection, which holds the row's lock, so no
// other publish can give it a token in between. A connection that holds a
// token, hashed or still in plaintext, keeps it: a token is never rotated
// here.
func (t *Tokens) Ensure(ctx context.Context, tx repo.DBTX, connection repo.PluginGithubConnection) error {
	if FromConnection(connection).Has() {
		return nil
	}
	if !connection.OrganizationID.Valid || connection.OrganizationID.String == "" {
		return ErrUnattributed
	}

	token, err := Mint()
	if err != nil {
		return fmt.Errorf("mint marketplace token: %w", err)
	}

	return t.Store(ctx, tx, connection.ID, connection.OrganizationID.String, token)
}

// Store gives a connection without a token the given token: its hash and the
// copies the organization's transition state writes. It returns ErrNotStored
// when the connection is gone, belongs to another organization or already
// holds a token.
//
// When the transition moves between the decision and the write, Store decides
// again and seals the same token again.
func (t *Tokens) Store(ctx context.Context, db repo.DBTX, connectionID uuid.UUID, organizationID string, token string) error {
	if token == "" {
		return ErrEmptyToken
	}
	q := repo.New(db)
	hash := conv.ToPGText(Hash(token))

	return t.sealAndWrite(ctx, db, organizationID, token, func(copies orgencryption.Copies, generation int64) (int64, error) {
		landed, err := q.StoreMarketplaceToken(ctx, repo.StoreMarketplaceTokenParams{
			TokenHash:         hash,
			PlatformCopy:      copies.Platform,
			OrgCopy:           copies.Org,
			ID:                connectionID,
			OrganizationID:    organizationID,
			DecidedGeneration: generation,
		})
		if err != nil {
			return 0, fmt.Errorf("store marketplace token: %w", err)
		}
		return landed, nil
	})
}

// MoveLegacy moves a connection's plaintext token to its hash and the copies
// the organization's transition state writes, and clears the plaintext
// column, in one statement. Before writing, it opens the copies it sealed and
// compares them with the token, so the plaintext is never cleared for a copy
// that does not open. It reports false, and writes nothing, when the
// connection no longer holds that plaintext token, such as when another run
// moved it first.
func (t *Tokens) MoveLegacy(ctx context.Context, db repo.DBTX, connectionID uuid.UUID, organizationID string, legacy string) (bool, error) {
	if legacy == "" {
		return false, ErrEmptyToken
	}
	q := repo.New(db)
	hash := conv.ToPGText(Hash(legacy))

	err := t.sealAndWrite(ctx, db, organizationID, legacy, func(copies orgencryption.Copies, generation int64) (int64, error) {
		landed, err := q.MoveLegacyMarketplaceToken(ctx, repo.MoveLegacyMarketplaceTokenParams{
			TokenHash:         hash,
			PlatformCopy:      copies.Platform,
			OrgCopy:           copies.Org,
			ID:                connectionID,
			OrganizationID:    organizationID,
			LegacyToken:       conv.ToPGText(legacy),
			DecidedGeneration: generation,
		})
		if err != nil {
			return 0, fmt.Errorf("move plaintext marketplace token: %w", err)
		}
		return landed, nil
	})
	switch {
	case errors.Is(err, ErrNotStored):
		return false, nil
	case err != nil:
		return false, err
	default:
		return true, nil
	}
}

// sealAndWrite seals token under the organization's transition state, checks
// that the copies open back to it, and runs write with them and the decided
// generation. When write lands no row, it reads the transition again: an
// unchanged generation means the row did not match and returns ErrNotStored,
// and a newer one means the transition moved, so it seals again.
func (t *Tokens) sealAndWrite(ctx context.Context, db repo.DBTX, organizationID string, token string, write func(copies orgencryption.Copies, generation int64) (int64, error)) error {
	decision, err := t.cipher.DecideIn(ctx, db, organizationID, orgencryption.UseCaseMarketplaceTokens)
	if err != nil {
		return fmt.Errorf("decide marketplace token state: %w", err)
	}

	for range maxWriteAttempts {
		copies, err := t.cipher.Seal(ctx, decision, Columns, []byte(token))
		if errors.Is(err, orgencryption.ErrDecisionExpired) {
			decision, err = t.cipher.RedecideIn(ctx, db, decision)
			if err != nil {
				return fmt.Errorf("decide marketplace token state again: %w", err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("seal marketplace token: %w", err)
		}

		opened, err := t.cipher.Open(ctx, decision, Columns, copies)
		if err != nil {
			return fmt.Errorf("open sealed marketplace token: %w", err)
		}
		if subtle.ConstantTimeCompare(opened, []byte(token)) != 1 {
			return fmt.Errorf("%w: sealed marketplace token does not open to the token", orgencryption.ErrUnreadable)
		}

		landed, err := write(copies, decision.Generation)
		if err != nil {
			return err
		}
		if landed == 1 {
			return nil
		}

		fresh, err := t.cipher.RedecideIn(ctx, db, decision)
		if err != nil {
			return fmt.Errorf("decide marketplace token state again: %w", err)
		}
		if fresh.Generation == decision.Generation {
			return ErrNotStored
		}

		t.logger.InfoContext(ctx, "marketplace token write fenced by a transition, sealing again",
			attr.SlogOrganizationID(organizationID), attr.SlogOrgEncryptionUseCase(orgencryption.UseCaseMarketplaceTokens))
		decision = fresh
	}

	return fmt.Errorf("store marketplace token: %w", orgencryption.ErrFenced)
}

// decide returns the decision for the organization, reading through db on a
// cache miss, or through the process's pool when db is nil.
func (t *Tokens) decide(ctx context.Context, db repo.DBTX, organizationID string) (orgencryption.Decision, error) {
	var decision orgencryption.Decision
	var err error
	if db == nil {
		decision, err = t.cipher.Decide(ctx, organizationID, orgencryption.UseCaseMarketplaceTokens)
	} else {
		decision, err = t.cipher.DecideIn(ctx, db, organizationID, orgencryption.UseCaseMarketplaceTokens)
	}
	if err != nil {
		return orgencryption.Decision{}, fmt.Errorf("decide marketplace token state: %w", err)
	}

	return decision, nil
}

// CustomerKeyProblem reports whether err from opening or storing a token
// means the organization's key cannot be used and the customer has to fix it:
// its encryption is suspended, or its key is unreachable, revoked, deleted,
// disabled or no longer an encryption key.
func CustomerKeyProblem(err error) bool {
	return errors.Is(err, orgencryption.ErrSuspended) || errors.Is(err, orgencryption.ErrKeyUnavailable) || errors.Is(err, orgencryption.ErrInvalidKey)
}

// OpenError is the error a handler returns when a token does not open. A key
// the customer has to fix is 412 failed_precondition: the request reached
// Speakeasy, and only fixing the key helps. The device agent then keeps its
// cached policy without reading the failure as an outage. Anything else is 500.
func OpenError(ctx context.Context, logger *slog.Logger, err error, organizationID string) error {
	if CustomerKeyProblem(err) {
		return oops.E(oops.CodeFailedPrecondition, err, "the organization's encryption key cannot open the marketplace token; fix the key to restore the marketplace URL").
			LogWarn(ctx, logger, attr.SlogOrganizationID(organizationID), attr.SlogOrgEncryptionUseCase(orgencryption.UseCaseMarketplaceTokens))
	}

	return oops.E(oops.CodeUnexpected, err, "error opening the marketplace token").
		LogError(ctx, logger, attr.SlogOrganizationID(organizationID), attr.SlogOrgEncryptionUseCase(orgencryption.UseCaseMarketplaceTokens))
}
