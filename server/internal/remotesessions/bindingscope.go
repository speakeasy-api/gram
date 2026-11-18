package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// bindingWriteScope is every project a change to one client's issuer bindings
// re-decides: the caller's, each project with a server or gateway on one of
// the issuers, and each project with a server associated with the client. An
// organization-level issuer's bindings are shared by servers in every project,
// so a change made from one project re-decides all of them.
type bindingWriteScope struct {
	organizationID string

	// clientID is the client whose bindings change, or uuid.Nil for one this
	// transaction creates.
	clientID uuid.UUID

	projects []uuid.UUID
}

// lockBindingWriteScope reads the projects a binding change on userIssuerIDs
// reaches and locks their writer scope. Call it before the transaction's first
// row lock or mutation.
func lockBindingWriteScope(ctx context.Context, tx pgx.Tx, organizationID string, projectID, clientID uuid.UUID, userIssuerIDs []uuid.UUID) (bindingWriteScope, error) {
	scope := bindingWriteScope{organizationID: organizationID, clientID: clientID, projects: nil}
	projects, err := scope.reach(ctx, tx, projectID, userIssuerIDs)
	if err != nil {
		return scope, err
	}
	scope.projects = projects
	if err := authclientbackfill.LockEMAWriterScope(ctx, tx, organizationID, projects); err != nil {
		return scope, fmt.Errorf("lock client binding writer scope: %w", err)
	}
	return scope, nil
}

func (s bindingWriteScope) reach(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, userIssuerIDs []uuid.UUID) ([]uuid.UUID, error) {
	projects, err := upstreamclients.ProjectsConsumingIssuers(ctx, tx, s.organizationID, projectID, userIssuerIDs)
	if err != nil {
		return nil, fmt.Errorf("list projects sharing the client's issuers: %w", err)
	}
	if s.clientID == uuid.Nil {
		return projects, nil
	}
	associated, err := upstreamclients.ProjectsAssociatedWithClient(ctx, tx, s.organizationID, s.clientID)
	if err != nil {
		return nil, fmt.Errorf("list projects associated with the client: %w", err)
	}
	for _, id := range associated {
		if !slices.Contains(projects, id) {
			projects = append(projects, id)
		}
	}
	return projects, nil
}

// resync derives and reconciles every project of the scope for userIssuerIDs,
// and offers captured on each issuer's only consumer in it. It fails with
// upstreamclients.ErrScopeChanged when the change now reaches a project the
// scope did not lock.
func (s bindingWriteScope) resync(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, userIssuerIDs []uuid.UUID, captured uuid.UUID) error {
	current, err := s.reach(ctx, tx, projectID, userIssuerIDs)
	if err != nil {
		return err
	}
	if err := upstreamclients.RequireScope(s.projects, current); err != nil {
		return err //nolint:wrapcheck // The conflict passes through to the caller.
	}
	for _, id := range s.projects {
		if err := ResyncMCPServerRemoteSessionIssuers(ctx, tx, s.organizationID, id, userIssuerIDs); err != nil {
			return err
		}
		if captured == uuid.Nil {
			continue
		}
		if err := upstreamclients.CaptureSoleConsumers(ctx, tx, s.organizationID, id, userIssuerIDs, []uuid.UUID{captured}); err != nil {
			return fmt.Errorf("capture client on its issuers' servers: %w", err)
		}
	}
	return nil
}

// bindingSyncError is the response to a failed resync: a scope that changed
// concurrently asks the caller to retry, and anything else is ours.
func bindingSyncError(ctx context.Context, logger *slog.Logger, err error) error {
	if errors.Is(err, upstreamclients.ErrScopeChanged) {
		return err
	}
	return oops.E(oops.CodeUnexpected, err, "synchronize client binding").LogError(ctx, logger)
}
