// Keeps mcp_servers.remote_session_issuer_id tracking the client bindings it
// is derived from. The column denormalises "which authorization server does
// this server's upstream authenticate against", which is otherwise only
// reachable by walking user_session_issuer -> bindings -> clients -> issuer.
// Gateway token routing needs it as a lookup key, and a join at read time
// cannot answer it per member without that walk.
//
// Every path that creates, moves, or removes a client binding has to call
// this, in its own transaction, or the column silently rots: clients and
// issuers are only ever soft-deleted, so the FK's ON DELETE SET NULL never
// fires and nothing else clears a stale value.
//
// Two rules bind every caller. Both exist because a wrong value here forwards
// a user's bearer token to the wrong third-party upstream.
//
//   - Pass the caller's own tenant scope. The ids come from join tables with
//     no tenancy column, so they are not proof of anything on their own.
//   - Take LockUserSessionIssuersForRemoteIssuerDerivation over the same ids
//     FIRST, before any row lock in the transaction. See that query's comment
//     for why the position matters and not just the lock.

package remotesessions

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// ResyncScope is the caller's own tenancy, which bounds which MCP servers a
// resync may write regardless of what ids it was handed.
type ResyncScope struct {
	organizationID string
	projectID      uuid.NullUUID
}

// ProjectResyncScope confines the resync to one project of one organization.
// Use it whenever the caller acts on a single project.
func ProjectResyncScope(organizationID string, projectID uuid.UUID) ResyncScope {
	return ResyncScope{organizationID: organizationID, projectID: conv.ToNullUUID(projectID)}
}

// OrganizationResyncScope confines the resync to one organization while
// letting it span that organization's projects. Only for callers that
// genuinely act across projects — an organization-administrator client delete,
// or an issuer migration re-pointing clients shared by several projects — and
// never as a substitute for a project scope the caller already holds.
func OrganizationResyncScope(organizationID string) ResyncScope {
	return ResyncScope{organizationID: organizationID, projectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
}

// LockUserSessionIssuersForRemoteIssuerDerivation serializes this transaction
// against every other writer that could change what those user session issuers
// derive. Locks are taken in ascending id order so two callers with
// overlapping sets cannot deadlock against each other.
//
// Call this before taking any row lock in the transaction, and over the same
// ids the later resync will be given.
//
// Takes no tenant scope and does not need one. It acquires advisory locks on a
// salted hash of each id and nothing else: no row is read, none is written,
// and a lock's existence is not observable to anyone who is not already
// contending for the same id. Locking an id from outside the caller's tenancy
// therefore costs that tenant at most the contention of one transaction that,
// on every path here, aborts a statement or two later on its own ownership
// check. What the lock protects against — a wrong derived value — is bounded
// separately, by the caller's ResyncScope. Scoping the lock query itself would
// be actively worse: a tenancy predicate yields no row for anything it rejects,
// so the lock would be silently skipped rather than refused.
func LockUserSessionIssuersForRemoteIssuerDerivation(ctx context.Context, dbtx repo.DBTX, userIssuerIDs []uuid.UUID) error {
	q := repo.New(dbtx)
	for _, id := range orderedDerivationLockIDs(userIssuerIDs) {
		if err := q.LockUserSessionIssuerForRemoteIssuerDerivation(ctx, id); err != nil {
			return fmt.Errorf("lock user session issuer %s for remote issuer derivation: %w", id, err)
		}
	}

	return nil
}

// TryLockUserSessionIssuersForRemoteIssuerDerivation is the non-blocking form,
// reporting false the moment one of the locks is held elsewhere. Only for a
// caller that must reach for a derivation lock while already holding a lock
// that other derivation-lock holders wait on, where blocking would deadlock;
// everywhere else the blocking form is correct and this one would turn routine
// contention into a spurious failure.
func TryLockUserSessionIssuersForRemoteIssuerDerivation(ctx context.Context, dbtx repo.DBTX, userIssuerIDs []uuid.UUID) (bool, error) {
	q := repo.New(dbtx)
	for _, id := range orderedDerivationLockIDs(userIssuerIDs) {
		acquired, err := q.TryLockUserSessionIssuerForRemoteIssuerDerivation(ctx, id)
		if err != nil {
			return false, fmt.Errorf("try lock user session issuer %s for remote issuer derivation: %w", id, err)
		}
		if !acquired {
			return false, nil
		}
	}

	return true, nil
}

// orderedDerivationLockIDs sorts and de-duplicates a lock set. Ascending order
// is what keeps two callers with overlapping sets from deadlocking against each
// other, and it has to hold for the try-form too: a partial acquisition there
// is still held for the rest of the transaction.
func orderedDerivationLockIDs(userIssuerIDs []uuid.UUID) []uuid.UUID {
	ordered := slices.Clone(userIssuerIDs)
	slices.SortFunc(ordered, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return slices.Compact(ordered)
}

// ResyncMCPServerRemoteSessionIssuers recomputes the denormalised issuer for
// every MCP server carrying one of userIssuerIDs that also falls inside scope.
// Safe to call with issuers that no server uses, and safe to call when nothing
// changed — the statement only writes rows whose value actually differs.
//
// Callers pass the issuers whose bindings they touched. For a detach or a
// delete that means capturing them before the rows go, since afterwards there
// is nothing left to walk. Ids outside scope are ignored rather than rejected:
// the sets are read from untenanted join tables, so a foreign id is a fact
// about the data, not a caller error.
func ResyncMCPServerRemoteSessionIssuers(ctx context.Context, dbtx mcpserversrepo.DBTX, scope ResyncScope, userIssuerIDs []uuid.UUID) error {
	// Ahead of the empty-set shortcut: a caller that forgot its scope is a bug
	// whichever set it happens to be holding, and letting the empty case
	// through would make the guarantee depend on the data rather than on the
	// call.
	if scope.organizationID == "" {
		return fmt.Errorf("resync mcp server remote session issuers: no organization scope")
	}
	if len(userIssuerIDs) == 0 {
		return nil
	}

	if _, err := mcpserversrepo.New(dbtx).ResyncMCPServerRemoteSessionIssuers(ctx, mcpserversrepo.ResyncMCPServerRemoteSessionIssuersParams{
		UserSessionIssuerIds: userIssuerIDs,
		OrganizationID:       scope.organizationID,
		ProjectID:            scope.projectID,
	}); err != nil {
		return fmt.Errorf("resync mcp server remote session issuers: %w", err)
	}
	return nil
}
