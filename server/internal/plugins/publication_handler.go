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

type PublicationHandler struct {
	logger   *slog.Logger
	validate func(context.Context, string, uuid.UUID) (bool, error)
	signaler PluginSyncSignaler
}

func NewPublicationHandler(logger *slog.Logger, db *pgxpool.Pool, signaler PluginSyncSignaler) *PublicationHandler {
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
		signaler: signaler,
	}
}

func (h *PublicationHandler) HandleBatchWithResult(ctx context.Context, messages []streams.BatchMessage[*publicationv1.PublicationRequested]) error {
	// Each project is signalled once per batch, so a manual sync must not be
	// lost to an ordinary request for the same project that came first, and
	// the sync is attributed to whoever asked for it.
	syncActors := make(map[publicationRequestKey]string)
	for _, message := range messages {
		requestKey, ok := publicationRequestKeyOf(message.Message)
		if _, seen := syncActors[requestKey]; ok && !seen && message.Message.GetIncludePausedPlugins() {
			syncActors[requestKey] = message.Message.GetCreatedByUserId()
		}
	}

	results := make(map[publicationRequestKey]error)
	for _, message := range messages {
		request := message.Message
		requestKey, ok := publicationRequestKeyOf(request)
		if !ok {
			h.logger.WarnContext(ctx, "discard invalid plugin publication request", attr.SlogError(fmt.Errorf("invalid organization or project id")))
			continue
		}
		if previous, ok := results[requestKey]; ok {
			if previous != nil {
				message.Fail(previous)
			}
			continue
		}

		connected, err := h.validate(ctx, requestKey.organizationID, requestKey.projectID)
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

		if h.signaler == nil {
			err = fmt.Errorf("plugin publication signal is not configured")
			results[requestKey] = err
			message.Fail(err)
			continue
		}
		signal, actor := h.signaler.SignalPluginPublish, request.GetCreatedByUserId()
		if syncActor, ok := syncActors[requestKey]; ok {
			signal, actor = h.signaler.SignalPluginSync, syncActor
		}
		if err := signal(ctx, requestKey.projectID, actor); err != nil {
			err = fmt.Errorf("signal plugin publication: %w", err)
			results[requestKey] = err
			message.Fail(err)
			continue
		}
		results[requestKey] = nil
	}
	return nil
}

type publicationRequestKey struct {
	organizationID string
	projectID      uuid.UUID
}

func publicationRequestKeyOf(request *publicationv1.PublicationRequested) (publicationRequestKey, bool) {
	projectID, err := uuid.Parse(request.GetProjectId())
	if request.GetOrganizationId() == "" || err != nil || projectID == uuid.Nil {
		return publicationRequestKey{organizationID: "", projectID: uuid.Nil}, false
	}
	return publicationRequestKey{organizationID: request.GetOrganizationId(), projectID: projectID}, true
}

var _ streams.BatchResultHandler[*publicationv1.PublicationRequested] = (*PublicationHandler)(nil)
