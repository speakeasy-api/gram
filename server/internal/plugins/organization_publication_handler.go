package plugins

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	publicationv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

const organizationPublicationPageSize = 100

type OrganizationPublicationHandler struct {
	logger *slog.Logger
	db     *pgxpool.Pool
}

func NewOrganizationPublicationHandler(logger *slog.Logger, db *pgxpool.Pool) *OrganizationPublicationHandler {
	return &OrganizationPublicationHandler{logger: logger, db: db}
}

func (h *OrganizationPublicationHandler) HandleBatchWithResult(ctx context.Context, messages []streams.BatchMessage[*publicationv1.OrganizationPublicationRequested]) error {
	type key struct {
		organizationID string
		afterProjectID uuid.UUID
	}
	results := make(map[key]error)
	for _, message := range messages {
		request := message.Message
		organizationID := request.GetOrganizationId()
		cursor := uuid.Nil
		if raw := request.GetAfterProjectId(); raw != "" {
			parsed, err := uuid.Parse(raw)
			if err != nil || parsed == uuid.Nil {
				h.logger.WarnContext(ctx, "discard invalid organization plugin publication request", attr.SlogError(fmt.Errorf("invalid project cursor")))
				continue
			}
			cursor = parsed
		}
		if organizationID == "" {
			h.logger.WarnContext(ctx, "discard invalid organization plugin publication request", attr.SlogError(fmt.Errorf("missing organization")))
			continue
		}

		requestKey := key{organizationID: organizationID, afterProjectID: cursor}
		if previous, ok := results[requestKey]; ok {
			if previous != nil {
				message.Fail(previous)
			}
			continue
		}
		err := h.publishPage(ctx, organizationID, request.GetCreatedByUserId(), cursor)
		results[requestKey] = err
		if err != nil {
			message.Fail(err)
		}
	}
	return nil
}

func (h *OrganizationPublicationHandler) publishPage(ctx context.Context, organizationID, actorID string, cursor uuid.UUID) error {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin organization publication page: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `SELECT c.project_id
		FROM plugin_github_connections c
		JOIN projects p ON p.id = c.project_id
		WHERE p.organization_id = $1
		  AND p.deleted IS FALSE
		  AND c.project_id > $2
		ORDER BY c.project_id ASC
		LIMIT $3`, organizationID, cursor, organizationPublicationPageSize+1)
	if err != nil {
		return fmt.Errorf("list organization marketplace projects: %w", err)
	}
	projectIDs := make([]uuid.UUID, 0, organizationPublicationPageSize+1)
	for rows.Next() {
		var projectID uuid.UUID
		if err := rows.Scan(&projectID); err != nil {
			rows.Close()
			return fmt.Errorf("scan organization marketplace project: %w", err)
		}
		projectIDs = append(projectIDs, projectID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read organization marketplace projects: %w", err)
	}

	hasContinuation := len(projectIDs) > organizationPublicationPageSize
	if hasContinuation {
		projectIDs = projectIDs[:organizationPublicationPageSize]
	}
	for _, projectID := range projectIDs {
		if _, err := outbox.Publish(ctx, tx, organizationID, outbox.Message{PublicID: uuid.Nil, Attributes: nil, Proto: publicationv1.PublicationRequested_builder{
			OrganizationId:  new(organizationID),
			ProjectId:       new(projectID.String()),
			CreatedByUserId: new(actorID),
		}.Build()}); err != nil {
			return fmt.Errorf("enqueue project publication for %s: %w", projectID, err)
		}
	}
	if hasContinuation {
		lastProjectID := projectIDs[len(projectIDs)-1].String()
		if _, err := outbox.Publish(ctx, tx, organizationID, outbox.Message{PublicID: uuid.Nil, Attributes: nil, Proto: publicationv1.OrganizationPublicationRequested_builder{
			OrganizationId:  new(organizationID),
			CreatedByUserId: new(actorID),
			AfterProjectId:  new(lastProjectID),
		}.Build()}); err != nil {
			return fmt.Errorf("enqueue organization publication continuation: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit organization publication page: %w", err)
	}
	return nil
}

var _ streams.BatchResultHandler[*publicationv1.OrganizationPublicationRequested] = (*OrganizationPublicationHandler)(nil)
