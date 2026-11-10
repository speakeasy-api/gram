package internalsessionissuers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ErrActiveKidRequired means a compromise call must name the active key it
// saw, so repeats coalesce instead of revoking each replacement.
var ErrActiveKidRequired = errors.New("internalsessionissuers: name the active key to revoke")

// Lifecycle holds the issuer changes that need only the database: turning
// signing off and on, and containing a compromise. None of them depend on the
// rollout flag, the verified domains or KMS, so they always work.
type Lifecycle struct {
	db    *pgxpool.Pool
	audit *audit.Logger
}

func NewLifecycle(db *pgxpool.Pool, auditLogger *audit.Logger) *Lifecycle {
	return &Lifecycle{db: db, audit: auditLogger}
}

// Actor attributes interactive changes to their admin; background calls are
// system changes.
func Actor(ctx context.Context, organizationID string) (urn.Principal, *string) {
	if auth, ok := contextvalues.GetAuthContext(ctx); ok && auth != nil && auth.ActiveOrganizationID == organizationID {
		if actor, ok := contextvalues.AuthenticatedActor(ctx); ok {
			return actor, auth.Email
		}
	}
	name := "System"
	return urn.NewSystemPrincipal("internal-session-issuers"), &name
}

// KeyEvent is the audit event for an issuer key change.
func KeyEvent(ctx context.Context, organizationID string, key repo.JsonWebKey, before, after *audit.JsonWebKeySnapshot) audit.LogJsonWebKeyEvent {
	actor, displayName := Actor(ctx, organizationID)
	return audit.LogJsonWebKeyEvent{
		OrganizationID:    organizationID,
		ProjectID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Actor:             actor,
		ActorDisplayName:  displayName,
		ActorSlug:         nil,
		KeyURN:            urn.NewJsonWebKey(key.ID),
		Kid:               key.Kid,
		SetURN:            urn.NewJsonWebKeySet(key.JsonWebKeySetID),
		KeySnapshotBefore: before,
		KeySnapshotAfter:  after,

		InternalSessionIssuer: true,
	}
}

// KeySnapshot is the audited state of an issuer key.
func KeySnapshot(key repo.JsonWebKey) *audit.JsonWebKeySnapshot {
	return &audit.JsonWebKeySnapshot{Kid: key.Kid, State: key.State, ExternalKeyID: key.ExternalKeyID.String()}
}

// IssuerEvent is the audit event for enabling or disabling an issuer.
func IssuerEvent(ctx context.Context, organizationID string, issuer repo.InternalSessionIssuer) audit.LogInternalSessionIssuerEvent {
	actor, displayName := Actor(ctx, organizationID)
	return audit.LogInternalSessionIssuerEvent{OrganizationID: organizationID, Actor: actor, ActorDisplayName: displayName, ActorSlug: nil, IssuerURN: urn.NewInternalSessionIssuer(issuer.ID), Issuer: issuer.Issuer}
}

// LockIssuerKeys serializes every key write on the organization's issuer.
func LockIssuerKeys(ctx context.Context, q *repo.Queries, organizationID string) (repo.JsonWebKeySet, error) {
	set, err := q.LockOrganizationEMAIssuerForKeyWrite(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return repo.JsonWebKeySet{}, ErrNotFound
	case err != nil:
		return repo.JsonWebKeySet{}, fmt.Errorf("lock issuer: %w", err)
	}
	return set, nil
}

// SetDisabled turns signing off or on and audits a real change. Keys stay
// published either way.
func (l *Lifecycle) SetDisabled(ctx context.Context, organizationID string, disabled bool) error {
	tx, err := l.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin issuer update: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	if _, err := LockIssuerKeys(ctx, q, organizationID); err != nil {
		return err
	}
	before, err := q.GetOrganizationInternalSessionIssuer(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("read issuer: %w", err)
	}
	if before.DisabledAt.Valid == disabled {
		return nil
	}
	after, err := q.SetInternalSessionIssuerDisabled(ctx, repo.SetInternalSessionIssuerDisabledParams{Disabled: disabled, OrganizationID: organizationID})
	if err != nil {
		return fmt.Errorf("update issuer: %w", err)
	}
	if disabled {
		err = l.audit.LogInternalSessionIssuerDisable(ctx, tx, IssuerEvent(ctx, organizationID, after))
	} else {
		err = l.audit.LogInternalSessionIssuerEnable(ctx, tx, IssuerEvent(ctx, organizationID, after))
	}
	if err != nil {
		return fmt.Errorf("audit issuer update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit issuer update: %w", err)
	}
	return nil
}

// Containment is the outcome of Contain.
type Containment struct {
	// Revoked are the keys this call withdrew.
	Revoked []repo.JsonWebKey
	// Coalesced means activeKid was already replaced; nothing changed.
	Coalesced bool
}

// Contain revokes every live issuer key (active, pending and retired) in one
// transaction, so the issuer publishes nothing and cannot sign until a
// replacement is activated. activeKid names the active key the caller saw:
// if an earlier compromise call already revoked it and minted the current
// active key, this one changes nothing. A keyless issuer needs no kid.
func (l *Lifecycle) Contain(ctx context.Context, organizationID string, activeKid *string) (Containment, error) {
	none := Containment{Revoked: nil, Coalesced: false}
	tx, err := l.db.Begin(ctx)
	if err != nil {
		return none, fmt.Errorf("begin issuer containment: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	set, err := LockIssuerKeys(ctx, q, organizationID)
	if err != nil {
		return none, err
	}
	keys, err := q.ListJsonWebKeys(ctx, repo.ListJsonWebKeysParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return none, fmt.Errorf("load issuer keys: %w", err)
	}
	var active *repo.JsonWebKey
	for i := range keys {
		if keys[i].State == "active" {
			active = &keys[i]
		}
	}
	switch {
	case active != nil && (activeKid == nil || *activeKid == ""):
		return none, ErrActiveKidRequired
	case active != nil && *activeKid != active.Kid:
		all, err := q.ListJsonWebKeys(ctx, repo.ListJsonWebKeysParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, IncludeRevoked: true})
		if err != nil {
			return none, fmt.Errorf("load issuer key history: %w", err)
		}
		i := slices.IndexFunc(all, func(k repo.JsonWebKey) bool { return k.Kid == *activeKid })
		if i < 0 {
			return none, ErrUnknownKid
		}
		// A repeat is coalesced only when the named key is revoked and the
		// active key was minted after that revocation, as a compromise
		// replacement is. A key the sweep revoked after a routine rotation
		// leaves the pre-compromise successor active, so that is contained.
		if all[i].State == "revoked" && all[i].RevokedAt.Valid && active.CreatedAt.Valid && active.CreatedAt.Time.After(all[i].RevokedAt.Time) {
			return Containment{Revoked: nil, Coalesced: true}, nil
		}
	}
	revoked := make([]repo.JsonWebKey, 0, len(keys))
	for _, key := range keys {
		row, err := q.RevokeJsonWebKey(ctx, repo.RevokeJsonWebKeyParams{ID: key.ID, OrganizationID: organizationID})
		if err != nil {
			return none, fmt.Errorf("revoke issuer key: %w", err)
		}
		if err := l.audit.LogJsonWebKeyRevoke(ctx, tx, KeyEvent(ctx, organizationID, row, KeySnapshot(key), KeySnapshot(row))); err != nil {
			return none, fmt.Errorf("audit issuer key revocation: %w", err)
		}
		revoked = append(revoked, row)
	}
	if err := tx.Commit(ctx); err != nil {
		return none, fmt.Errorf("commit issuer containment: %w", err)
	}
	return Containment{Revoked: revoked, Coalesced: false}, nil
}
