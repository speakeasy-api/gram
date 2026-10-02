package plugins

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	publicationv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

type PublicationSignal func(context.Context, uuid.UUID, string) error

type PublicationHandler struct {
	logger   *slog.Logger
	validate func(context.Context, string, uuid.UUID) (bool, error)
	signal   PublicationSignal
}

func NewPublicationHandler(logger *slog.Logger, db *pgxpool.Pool, signal PublicationSignal) *PublicationHandler {
	return &PublicationHandler{
		logger: logger,
		validate: func(ctx context.Context, organizationID string, projectID uuid.UUID) (bool, error) {
			var connected bool
			err := db.QueryRow(ctx, `SELECT EXISTS (
				SELECT 1
				FROM plugin_github_connections c
				JOIN projects p ON p.id = c.project_id
				WHERE c.project_id = $1
				  AND p.organization_id = $2
				  AND p.deleted IS FALSE
			)`, projectID, organizationID).Scan(&connected)
			if err != nil {
				return false, fmt.Errorf("check plugin marketplace connection: %w", err)
			}
			return connected, nil
		},
		signal: signal,
	}
}

func (h *PublicationHandler) HandleBatchWithResult(ctx context.Context, messages []streams.BatchMessage[*publicationv1.PublicationRequested]) error {
	type key struct {
		organizationID string
		projectID      uuid.UUID
	}
	results := make(map[key]error)
	for _, message := range messages {
		request := message.Message
		organizationID := request.GetOrganizationId()
		projectID, err := uuid.Parse(request.GetProjectId())
		if organizationID == "" || err != nil || projectID == uuid.Nil {
			h.logger.WarnContext(ctx, "discard invalid plugin publication request", attr.SlogError(fmt.Errorf("invalid organization or project id")))
			continue
		}

		requestKey := key{organizationID: organizationID, projectID: projectID}
		if previous, ok := results[requestKey]; ok {
			if previous != nil {
				message.Fail(previous)
			}
			continue
		}

		connected, err := h.validate(ctx, organizationID, projectID)
		if err != nil {
			err = fmt.Errorf("validate plugin publication request: %w", err)
			results[requestKey] = err
			message.Fail(err)
			continue
		}
		if !connected {
			results[requestKey] = nil
			continue
		}

		if h.signal == nil {
			err = fmt.Errorf("plugin publication signal is not configured")
			results[requestKey] = err
			message.Fail(err)
			continue
		}
		err = h.signal(ctx, projectID, request.GetCreatedByUserId())
		if err != nil {
			err = fmt.Errorf("signal plugin publication: %w", err)
			results[requestKey] = err
			message.Fail(err)
			continue
		}
		results[requestKey] = nil
	}
	return nil
}

var _ streams.BatchResultHandler[*publicationv1.PublicationRequested] = (*PublicationHandler)(nil)
