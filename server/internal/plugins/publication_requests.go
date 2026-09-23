package plugins

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	publicationv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	"github.com/speakeasy-api/gram/server/internal/outbox"
)

// PublicationRequests records refresh hints in the same transaction as the
// package-affecting change. A refresh is only needed for an existing marketplace.
type PublicationRequests struct {
	Enabled bool
}

func (r PublicationRequests) Project(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, actorID string) error {
	if !r.Enabled {
		return nil
	}
	if organizationID == "" || projectID == uuid.Nil {
		return fmt.Errorf("plugin publication requires an organization and project")
	}
	var connected bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM plugin_github_connections c
		JOIN projects p ON p.id = c.project_id
		WHERE c.project_id = $1 AND p.organization_id = $2 AND p.deleted IS FALSE
	)`, projectID, organizationID).Scan(&connected); err != nil {
		return fmt.Errorf("check project marketplace connection: %w", err)
	}
	if !connected {
		return nil
	}
	_, err := outbox.Publish(ctx, tx, organizationID, outbox.Message{
		PublicID:   uuid.Nil,
		Attributes: nil,
		Proto: publicationv1.PublicationRequested_builder{
			OrganizationId:  new(organizationID),
			ProjectId:       new(projectID.String()),
			CreatedByUserId: new(actorID),
		}.Build(),
	})
	if err != nil {
		return fmt.Errorf("enqueue plugin publication: %w", err)
	}
	return nil
}

func (r PublicationRequests) Organization(ctx context.Context, tx pgx.Tx, organizationID, actorID string) error {
	if !r.Enabled {
		return nil
	}
	if organizationID == "" {
		return fmt.Errorf("plugin publication requires an organization")
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
