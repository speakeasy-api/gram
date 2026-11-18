package orgencryption

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
)

// Copies are the two stored copies of one value.
type Copies struct {
	// Platform is the platform-key ciphertext, NULL when absent.
	Platform pgtype.Text

	// Org is the organization-key ciphertext, NULL when absent.
	Org pgtype.Text
}

// ErasedCopies clears both copies. Erasing a value must always clear both, or
// a revoked value comes back from the other copy.
func ErasedCopies() Copies {
	return Copies{Platform: pgtype.Text{String: "", Valid: false}, Org: pgtype.Text{String: "", Valid: false}}
}

// Decision is the transition state a writer seals under. Writers pass
// Generation to their fenced statements.
type Decision struct {
	// OrganizationID owns the value.
	OrganizationID string

	// UseCase is the value's use case.
	UseCase UseCase

	// State decides which copies are written and read.
	State TransitionState

	// Generation is the transition generation the decision was made under.
	Generation int64

	// decidedAt bounds how long the decision may be held before sealing.
	decidedAt time.Time
}

// Cipher seals and opens values that move from the platform key to
// organization keys, following each organization's transition state:
//
//   - platform_key writes only the platform-key copy and clears the
//     organization-key copy, so it can never be older. It reads the
//     platform-key copy.
//   - dual_write writes both copies. It reads the organization-key copy when
//     present and the platform-key copy only when no organization-key copy
//     exists yet. Any failure of the organization key fails the operation;
//     nothing falls back to the platform-key copy.
//   - org_kms writes and reads only the organization-key copy.
//
// A process without organization encryption serves platform_key only and
// fails closed with ErrOrganizationEncryptionUnavailable otherwise.
type Cipher struct {
	logger      *slog.Logger
	platform    *encryption.Client
	org         *Service
	transitions *Transitions
	now         func() time.Time
}

// NewCipher builds a Cipher. org is nil in a process without organization
// encryption.
func NewCipher(logger *slog.Logger, platform *encryption.Client, org *Service, transitions *Transitions) *Cipher {
	return &Cipher{
		logger:      logger.With(attr.SlogComponent("orgencryption.cipher")),
		platform:    platform,
		org:         org,
		transitions: transitions,
		now:         time.Now,
	}
}

// Decide returns the decision to seal and open an organization's values
// under, from cached transition state.
func (c *Cipher) Decide(ctx context.Context, organizationID string, useCase UseCase) (Decision, error) {
	transition, err := c.transitions.Get(ctx, organizationID, useCase)
	if err != nil {
		return Decision{}, err
	}

	return c.decision(transition)
}

// DecideIn is Decide for a writer inside a transaction: a cache miss reads
// transition state through db instead of another pooled connection.
func (c *Cipher) DecideIn(ctx context.Context, db repo.DBTX, organizationID string, useCase UseCase) (Decision, error) {
	transition, err := c.transitions.GetIn(ctx, db, organizationID, useCase)
	if err != nil {
		return Decision{}, err
	}

	return c.decision(transition)
}

// Redecide returns a decision from transition state read again from the
// database. Writers call it after ErrFenced or ErrDecisionExpired, then seal
// the same plaintext again.
func (c *Cipher) Redecide(ctx context.Context, stale Decision) (Decision, error) {
	transition, err := c.transitions.GetFresh(ctx, stale.OrganizationID, stale.UseCase)
	if err != nil {
		return Decision{}, err
	}

	return c.decision(transition)
}

// RedecideIn is Redecide reading transition state through db, such as the
// writer's transaction.
func (c *Cipher) RedecideIn(ctx context.Context, db repo.DBTX, stale Decision) (Decision, error) {
	transition, err := c.transitions.GetFreshIn(ctx, db, stale.OrganizationID, stale.UseCase)
	if err != nil {
		return Decision{}, err
	}

	return c.decision(transition)
}

func (c *Cipher) decision(transition Transition) (Decision, error) {
	if transition.State != TransitionStatePlatformKey && c.org == nil {
		return Decision{}, fmt.Errorf("%w: use case %q is in %s", ErrOrganizationEncryptionUnavailable, transition.UseCase, transition.State)
	}

	return Decision{
		OrganizationID: transition.OrganizationID,
		UseCase:        transition.UseCase,
		State:          transition.State,
		Generation:     transition.Generation,
		decidedAt:      c.now(),
	}, nil
}

// RequireUsable checks that the decision can still seal and that the
// organization key can seal values for it. Callers run it before an
// irreversible call to a provider whose result must then be sealed, and skip
// the call when it fails. Like Encryptor.RequireUsable it reads cached state,
// so a key revoked within the cache periods can still pass.
func (c *Cipher) RequireUsable(ctx context.Context, d Decision) error {
	if err := c.checkFresh(d); err != nil {
		return err
	}
	if d.State == TransitionStatePlatformKey {
		return nil
	}

	encryptor, err := c.encryptor(d)
	if err != nil {
		return err
	}

	if err := encryptor.RequireUsable(ctx); err != nil {
		return fmt.Errorf("organization key unusable: %w", err)
	}

	return nil
}

// Seal encrypts plaintext into the copies the decision writes to columns. It
// refuses a decision held longer than MaxDecisionHold with ErrDecisionExpired,
// and a decision with an unknown state with ErrInvalidState.
func (c *Cipher) Seal(ctx context.Context, d Decision, columns CopyColumns, plaintext []byte) (Copies, error) {
	if err := c.checkDecision(d, columns); err != nil {
		return Copies{}, err
	}

	sealed := ErasedCopies()

	if d.State != TransitionStateOrgKMS {
		platform, err := c.platform.Encrypt(plaintext)
		if err != nil {
			return Copies{}, fmt.Errorf("encrypt with platform key: %w", err)
		}
		sealed.Platform = conv.ToPGText(platform)
	}

	if d.State != TransitionStatePlatformKey {
		org, err := c.sealOrg(ctx, d, columns, plaintext)
		if err != nil {
			return Copies{}, err
		}
		sealed.Org = conv.ToPGText(org)
	}

	return sealed, nil
}

// Open decrypts the copy the decision reads. ErrUnreadable and ErrMissing are
// permanent; any other error, such as ErrKeyUnavailable or ErrSuspended, is
// a key problem that may clear on its own, so callers must not treat it as a
// reason to sign in again.
func (c *Cipher) Open(ctx context.Context, d Decision, columns CopyColumns, stored Copies) ([]byte, error) {
	if d.UseCase != columns.UseCase {
		return nil, fmt.Errorf("orgencryption: column %s belongs to use case %q, not %q", columns.OrgColumn, columns.UseCase, d.UseCase)
	}

	switch d.State {
	case TransitionStatePlatformKey:
		return c.openPlatform(stored.Platform)
	case TransitionStateDualWrite:
		if stored.Org.Valid {
			return c.openOrg(ctx, d, columns, stored.Org.String)
		}
		return c.openPlatform(stored.Platform)
	case TransitionStateOrgKMS:
		switch {
		case stored.Org.Valid:
			return c.openOrg(ctx, d, columns, stored.Org.String)
		case stored.Platform.Valid:
			return nil, fmt.Errorf("%w: only the platform-key copy remains in org_kms", ErrUnreadable)
		default:
			return nil, ErrMissing
		}
	default:
		return nil, fmt.Errorf("%w: unknown transition state %q", ErrInvalidState, d.State)
	}
}

func (c *Cipher) checkDecision(d Decision, columns CopyColumns) error {
	if d.UseCase != columns.UseCase {
		return fmt.Errorf("orgencryption: column %s belongs to use case %q, not %q", columns.OrgColumn, columns.UseCase, d.UseCase)
	}

	return c.checkFresh(d)
}

// checkFresh refuses a decision with an unknown state, so nothing writes
// copies for a state it does not understand, and a decision held longer than
// MaxDecisionHold.
func (c *Cipher) checkFresh(d Decision) error {
	if _, err := parseTransitionState(string(d.State)); err != nil {
		return err
	}
	if c.now().Sub(d.decidedAt) > MaxDecisionHold {
		return ErrDecisionExpired
	}

	return nil
}

func (c *Cipher) encryptor(d Decision) (*Encryptor, error) {
	if c.org == nil {
		return nil, ErrOrganizationEncryptionUnavailable
	}

	encryptor, err := c.org.For(d.OrganizationID, d.UseCase)
	if err != nil {
		return nil, fmt.Errorf("organization encryptor: %w", err)
	}

	return encryptor, nil
}

func (c *Cipher) sealOrg(ctx context.Context, d Decision, columns CopyColumns, plaintext []byte) (string, error) {
	encryptor, err := c.encryptor(d)
	if err != nil {
		return "", err
	}

	ciphertext, err := encryptor.Encrypt(ctx, columns.OrgColumn, plaintext)
	if err != nil {
		return "", fmt.Errorf("encrypt with organization key: %w", err)
	}

	return ciphertext, nil
}

func (c *Cipher) openOrg(ctx context.Context, d Decision, columns CopyColumns, ciphertext string) ([]byte, error) {
	encryptor, err := c.encryptor(d)
	if err != nil {
		return nil, err
	}

	plaintext, err := encryptor.Decrypt(ctx, columns.OrgColumn, ciphertext)
	switch {
	case errors.Is(err, ErrInvalidCiphertext):
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	case err != nil:
		c.logger.WarnContext(ctx, "decrypt with organization key",
			attr.SlogOrganizationID(d.OrganizationID), attr.SlogOrgEncryptionUseCase(d.UseCase), attr.SlogError(err))
		return nil, fmt.Errorf("decrypt with organization key: %w", err)
	}

	return plaintext, nil
}

func (c *Cipher) openPlatform(stored pgtype.Text) ([]byte, error) {
	if !stored.Valid {
		return nil, ErrMissing
	}

	plaintext, err := c.platform.Decrypt(stored.String)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}

	return []byte(plaintext), nil
}
