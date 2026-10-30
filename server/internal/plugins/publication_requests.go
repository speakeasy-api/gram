package plugins

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	publicationv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// PublicationRequests records refresh hints in the same transaction as the
// package-affecting change. A refresh is only needed for an existing marketplace.
// Requesting one also takes the project's package content lock shared, which is
// how turning a plugin's auto sync off serializes with those changes.
type PublicationRequests struct {
	Enabled bool
}

// ProjectPublicationRequestOutcome is the transaction-time result of requesting
// a package refresh, not evidence that a consumer delivered or published it.
type ProjectPublicationRequestOutcome string

const (
	ProjectPublicationEmissionDisabled ProjectPublicationRequestOutcome = "emission_disabled"
	ProjectPublicationNotConfigured    ProjectPublicationRequestOutcome = "not_configured"
	ProjectPublicationEnqueued         ProjectPublicationRequestOutcome = "enqueued"
)

func (r PublicationRequests) Project(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, actorID string) error {
	_, err := r.ProjectWithOutcome(ctx, tx, organizationID, projectID, actorID)
	return err
}

// ProjectWithOutcome reports what was actually written in the caller's transaction.
// A successful enqueue remains provisional until that transaction commits.
func (r PublicationRequests) ProjectWithOutcome(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, actorID string) (ProjectPublicationRequestOutcome, error) {
	return r.projectWithOutcome(ctx, tx, organizationID, projectID, actorID, false)
}

// SyncProjectWithOutcome is ProjectWithOutcome for a manual sync: the publish it
// requests also sends the pending changes of plugins whose auto sync is off, as
// the dashboard's Sync changes does.
func (r PublicationRequests) SyncProjectWithOutcome(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, actorID string) (ProjectPublicationRequestOutcome, error) {
	return r.projectWithOutcome(ctx, tx, organizationID, projectID, actorID, true)
}

func (r PublicationRequests) projectWithOutcome(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, actorID string, includePausedPlugins bool) (ProjectPublicationRequestOutcome, error) {
	// Taken whether or not emission is enabled: turning a plugin's auto sync
	// off waits on it, so the change is either seen by its check or made after
	// it.
	if err := repo.New(tx).LockPluginPackageContentShared(ctx, projectID.String()); err != nil {
		return "", fmt.Errorf("lock plugin package content: %w", err)
	}
	if !r.Enabled {
		return ProjectPublicationEmissionDisabled, nil
	}
	if organizationID == "" || projectID == uuid.Nil {
		return "", fmt.Errorf("plugin publication requires an organization and project")
	}
	var connected bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM plugin_github_connections c
		JOIN projects p ON p.id = c.project_id
		WHERE c.project_id = $1 AND p.organization_id = $2 AND p.deleted IS FALSE
	)`, projectID, organizationID).Scan(&connected); err != nil {
		return "", fmt.Errorf("check project marketplace connection: %w", err)
	}
	if !connected {
		return ProjectPublicationNotConfigured, nil
	}
	_, err := outbox.Publish(ctx, tx, organizationID, outbox.Message{
		PublicID:   uuid.Nil,
		Attributes: nil,
		Proto: publicationv1.PublicationRequested_builder{
			OrganizationId:       new(organizationID),
			ProjectId:            new(projectID.String()),
			CreatedByUserId:      new(actorID),
			IncludePausedPlugins: new(includePausedPlugins),
		}.Build(),
	})
	if err != nil {
		return "", fmt.Errorf("enqueue plugin publication: %w", err)
	}
	return ProjectPublicationEnqueued, nil
}

// LockOrganizationPluginPackageContent waits out an auto-sync turn-off check in
// every project affected by an organization-wide package change. Call it after
// all admission and plugin row locks, even when publication emission is off.
// Projects are locked by UUID so concurrent organization changes cannot invert
// their lock order.
func LockOrganizationPluginPackageContent(ctx context.Context, tx pgx.Tx, organizationID string) error {
	if organizationID == "" {
		return fmt.Errorf("plugin publication requires an organization")
	}
	projects, err := projectsrepo.New(tx).ListProjectsByOrganization(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("list organization projects for package content lock: %w", err)
	}
	for _, project := range projects {
		if err := repo.New(tx).LockPluginPackageContentShared(ctx, project.ID.String()); err != nil {
			return fmt.Errorf("lock organization plugin package content: %w", err)
		}
	}
	return nil
}

func (r PublicationRequests) Organization(ctx context.Context, tx pgx.Tx, organizationID, actorID string) error {
	if err := LockOrganizationPluginPackageContent(ctx, tx, organizationID); err != nil {
		return err
	}
	if !r.Enabled {
		return nil
	}
	_, err := outbox.Publish(ctx, tx, organizationID, outbox.Message{
		PublicID:   uuid.Nil,
		Attributes: nil,
		Proto: publicationv1.OrganizationPublicationRequested_builder{
			OrganizationId:  new(organizationID),
			CreatedByUserId: new(actorID),
		}.Build(),
	})
	if err != nil {
		return fmt.Errorf("enqueue organization plugin publication: %w", err)
	}
	return nil
}
