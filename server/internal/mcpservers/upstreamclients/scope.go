package upstreamclients

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ErrScopeChanged reports that the projects a writer locked before its first
// mutation no longer cover what its change reaches. The caller rolls back and
// retries the whole transaction.
var ErrScopeChanged = errors.New("the MCP servers this change reaches changed concurrently")

// scopeChanged is the error a writer returns for ErrScopeChanged.
func scopeChanged() error {
	return oops.E(oops.CodeConflict, ErrScopeChanged, "the MCP servers this change reaches changed while it was saving; retry")
}

// ProjectsConsumingIssuers is projectID with every project of the
// organization that has a live MCP server or gateway on one of userIssuerIDs.
// An organization-level issuer's bindings are shared by all of them, so a
// change to those bindings re-decides each.
func ProjectsConsumingIssuers(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) ([]uuid.UUID, error) {
	projects := []uuid.UUID{projectID}
	if len(userIssuerIDs) == 0 {
		return projects, nil
	}
	consumers, err := repo.New(db).ListProjectsConsumingUserSessionIssuers(ctx, repo.ListProjectsConsumingUserSessionIssuersParams{
		UserSessionIssuerIds: userIssuerIDs,
		OrganizationID:       organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("list projects consuming user session issuers: %w", err)
	}
	return mergeProjects(projects, consumers), nil
}

// ProjectsAssociatedWithClient is every project of the organization with a
// live MCP server associated with clientID, tombstoned associations included.
func ProjectsAssociatedWithClient(ctx context.Context, db repo.DBTX, organizationID string, clientID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := repo.New(db).ListServersAssociatedWithClient(ctx, repo.ListServersAssociatedWithClientParams{
		RemoteSessionClientID: clientID,
		OrganizationID:        conv.ToPGText(organizationID),
	})
	if err != nil {
		return nil, fmt.Errorf("list servers associated with client: %w", err)
	}
	projects := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		projects = append(projects, row.ProjectID)
	}
	return mergeProjects(nil, projects), nil
}

// RequireScope fails with ErrScopeChanged when projects is not covered by the
// projects locked before the transaction's first mutation.
func RequireScope(locked, projects []uuid.UUID) error {
	for _, projectID := range projects {
		if !slices.Contains(locked, projectID) {
			return scopeChanged()
		}
	}
	return nil
}

// LockOrganizationScope locks the writer scope of every project of the
// organization with a live MCP server and returns those projects. It is for a
// writer whose change reaches every one of them: one that changes which
// organization-level issuers are live decides whether the organization gateway
// fronts each server. Call it before the transaction's first row lock or
// mutation, and before any shared gateway-reach lock: it takes that lock
// exclusively first, so no server gains a project between discovery and the
// writer scope.
func LockOrganizationScope(ctx context.Context, tx pgx.Tx, organizationID string) ([]uuid.UUID, error) {
	if err := authclientbackfill.New(tx).LockEMAGatewayReachWriter(ctx, organizationID); err != nil {
		return nil, fmt.Errorf("lock organization gateway reach: %w", err)
	}
	projects, err := repo.New(tx).ListOrganizationProjectsWithServers(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list organization projects: %w", err)
	}
	if len(projects) == 0 {
		return nil, nil
	}
	if err := authclientbackfill.LockEMAWriterScope(ctx, tx, organizationID, projects); err != nil {
		return nil, fmt.Errorf("lock organization writer scope: %w", err)
	}
	return projects, nil
}

// ReconcileOrganizationInLockedTransaction reconciles every project of locked
// against the organization's live organization-level issuers, after a change
// to which of them are live. It fails with ErrScopeChanged when a project with
// a server appeared after the lock.
func ReconcileOrganizationInLockedTransaction(ctx context.Context, tx pgx.Tx, organizationID string, locked []uuid.UUID) error {
	q := repo.New(tx)
	projects, err := q.ListOrganizationProjectsWithServers(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("list organization projects: %w", err)
	}
	if err := RequireScope(locked, projects); err != nil {
		return err
	}
	issuers, err := q.ListOrganizationLevelUserSessionIssuerIDs(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("list organization-level user session issuers: %w", err)
	}
	for _, projectID := range projects {
		if err := ReconcileInLockedTransaction(ctx, tx, organizationID, projectID, issuers); err != nil {
			return err
		}
	}
	return nil
}

// ClientAssociationScope is the servers associated with one client, read and
// locked before a writer removes the client, so the removal can prune their
// associations and retire their typed links in the same transaction.
type ClientAssociationScope struct {
	organizationID string
	clientID       uuid.UUID
	servers        []uuid.UUID
	projects       []uuid.UUID
}

// LockClientAssociationScope reads the servers associated with clientID and
// locks their projects' writer scope and registration gates. It also locks
// the writer scope of the projects in also, which the caller's change writes
// too. Call it before the transaction's first row lock or mutation.
func LockClientAssociationScope(ctx context.Context, tx pgx.Tx, organizationID string, clientID uuid.UUID, also []uuid.UUID) (ClientAssociationScope, error) {
	scope := ClientAssociationScope{organizationID: organizationID, clientID: clientID, servers: nil, projects: nil}
	servers, projects, err := clientServers(ctx, tx, organizationID, clientID)
	if err != nil {
		return scope, err
	}
	projects = mergeProjects(projects, also)
	scope.servers, scope.projects = servers, projects
	if len(projects) == 0 {
		return scope, nil
	}
	if err := authclientbackfill.LockEMAWriterScope(ctx, tx, organizationID, projects); err != nil {
		return scope, fmt.Errorf("lock client association writer scope: %w", err)
	}
	if err := authclientbackfill.PreflightServerAssociations(ctx, tx, organizationID, servers...); err != nil {
		return scope, fmt.Errorf("lock client association registration gates: %w", err)
	}
	return scope, nil
}

// PruneInLockedTransaction removes the associations of the client no binding
// still serves and retires the typed links derived from them. It fails with
// ErrScopeChanged when another server was associated with the client after the
// scope was locked.
func (s ClientAssociationScope) PruneInLockedTransaction(ctx context.Context, tx pgx.Tx) error {
	current, _, err := clientServers(ctx, tx, s.organizationID, s.clientID)
	if err != nil {
		return err
	}
	for _, serverID := range current {
		if !slices.Contains(s.servers, serverID) {
			return scopeChanged()
		}
	}
	if len(s.servers) == 0 {
		return nil
	}
	if _, err := repo.New(tx).PruneRemoteSessionClientAssociations(ctx, repo.PruneRemoteSessionClientAssociationsParams{
		RemoteSessionClientID: s.clientID,
		OrganizationID:        s.organizationID,
	}); err != nil {
		return fmt.Errorf("prune client associations: %w", err)
	}
	if err := authclientbackfill.SyncServerAssociations(ctx, tx, s.organizationID, s.servers...); err != nil {
		return fmt.Errorf("sync auth clients of the client's servers: %w", err)
	}
	return nil
}

func clientServers(ctx context.Context, db repo.DBTX, organizationID string, clientID uuid.UUID) ([]uuid.UUID, []uuid.UUID, error) {
	rows, err := repo.New(db).ListServersAssociatedWithClient(ctx, repo.ListServersAssociatedWithClientParams{
		RemoteSessionClientID: clientID,
		OrganizationID:        conv.ToPGText(organizationID),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list servers associated with client: %w", err)
	}
	servers := make([]uuid.UUID, 0, len(rows))
	projects := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		servers = append(servers, row.McpServerID)
		projects = append(projects, row.ProjectID)
	}
	return servers, mergeProjects(nil, projects), nil
}

func mergeProjects(into, more []uuid.UUID) []uuid.UUID {
	for _, projectID := range more {
		if !slices.Contains(into, projectID) {
			into = append(into, projectID)
		}
	}
	slices.SortFunc(into, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return into
}
