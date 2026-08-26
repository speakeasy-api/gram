// Keeps mcp_servers.remote_session_issuer_id tracking the client bindings it is
// derived from, so gateway token routing can use it as a lookup key.
//
// Every path that creates, moves, or removes a client binding must call this in
// its own transaction or the column rots: clients and issuers are only ever
// soft-deleted, so the FK's ON DELETE SET NULL never fires.
//
// Two rules bind every caller, because a wrong value forwards a user's bearer
// to the wrong upstream: pass your own tenant scope, and take
// LockUserSessionIssuersForRemoteIssuerDerivation over the same ids FIRST,
// before any row lock.

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
// derive. Ascending id order so overlapping callers cannot deadlock.
//
// Call before taking any row lock, over the same ids the resync will get.
//
// Takes no tenant scope and does not need one: it locks a salted hash and
// nothing else. Scoping it would be worse — a tenancy predicate yields no row
// for what it rejects, so the lock would be silently skipped.
func LockUserSessionIssuersForRemoteIssuerDerivation(ctx context.Context, dbtx repo.DBTX, userIssuerIDs []uuid.UUID) error {
	q := repo.New(dbtx)
	for _, id := range orderedDerivationLockIDs(userIssuerIDs) {
		if err := q.LockUserSessionIssuerForRemoteIssuerDerivation(ctx, id); err != nil {
			return fmt.Errorf("lock user session issuer %s for remote issuer derivation: %w", id, err)
		}
	}

	return nil
}

// TryLockUserSessionIssuersForRemoteIssuerDerivation is the non-blocking form.
// Only for a caller that must reach for a derivation lock while holding a lock
// other derivation-lock holders wait on; elsewhere the blocking form is correct.
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
// prevents deadlock between overlapping callers, and must hold for the try-form
// too: a partial acquisition is still held for the rest of the transaction.
func orderedDerivationLockIDs(userIssuerIDs []uuid.UUID) []uuid.UUID {
	ordered := slices.Clone(userIssuerIDs)
	slices.SortFunc(ordered, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return slices.Compact(ordered)
}

// ResyncMCPServerRemoteSessionIssuers recomputes the denormalised issuer for
// every MCP server carrying one of userIssuerIDs that also falls inside scope.
// Safe with issuers no server uses, and when nothing changed — the statement
// only writes rows whose value differs.
//
// Callers pass the issuers whose bindings they touched, captured before a
// detach or delete removes them. Ids outside scope are ignored rather than
// rejected: they come from untenanted join tables.
func ResyncMCPServerRemoteSessionIssuers(ctx context.Context, dbtx mcpserversrepo.DBTX, scope ResyncScope, userIssuerIDs []uuid.UUID) error {
	// Ahead of the empty-set shortcut, so the guarantee does not depend on data.
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
