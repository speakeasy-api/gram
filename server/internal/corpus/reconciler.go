package corpus

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/corpus/repo"
)

const (
	defaultPollInterval = 5 * time.Second
	maxPendingRows      = 10
)

// WorkflowStarter abstracts the Temporal client for starting corpus index workflows.
type WorkflowStarter interface {
	StartCorpusIndexWorkflow(ctx context.Context, params StartCorpusIndexParams) error
}

// StartCorpusIndexParams contains the parameters for starting a corpus index workflow.
type StartCorpusIndexParams struct {
	ProjectID      string
	OrganizationID string
	CommitSHA      string
	EventID        string
	TaskQueue      string
}

// Reconciler polls the corpus_publish_events outbox for pending rows and
// enqueues Temporal workflows to index the corresponding commits.
type Reconciler struct {
	db        *pgxpool.Pool
	queries   *repo.Queries
	starter   WorkflowStarter
	taskQueue string
	logger    *slog.Logger
}

// NewReconciler creates a new Reconciler.
func NewReconciler(db *pgxpool.Pool, starter WorkflowStarter, taskQueue string, logger *slog.Logger) *Reconciler {
	return &Reconciler{
		db:        db,
		queries:   repo.New(db),
		starter:   starter,
		taskQueue: taskQueue,
		logger:    logger,
	}
}

// Run starts the reconciler loop. It blocks until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context) error {
	ticker := time.NewTicker(defaultPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("reconciler stopped: %w", ctx.Err())
		case <-ticker.C:
			if err := r.ReconcileOnce(ctx); err != nil {
				r.logger.ErrorContext(ctx, "reconcile corpus outbox", attr.SlogError(err))
			}
		}
	}
}

// ReconcileOnce picks up pending outbox rows and enqueues workflows.
func (r *Reconciler) ReconcileOnce(ctx context.Context) error {
	events, err := r.queries.ListPendingPublishEvents(ctx, maxPendingRows)
	if err != nil {
		return fmt.Errorf("list pending publish events: %w", err)
	}

	for _, event := range events {
		// Transition to indexing before enqueuing to prevent double-pickup.
		_, err := r.queries.UpdatePublishEventStatus(ctx, repo.UpdatePublishEventStatusParams{
			ID:        event.ID,
			ProjectID: event.ProjectID,
			Status:    "indexing",
		})
		if err != nil {
			r.logger.ErrorContext(ctx, "update publish event status", attr.SlogError(err))
			continue
		}

		err = r.starter.StartCorpusIndexWorkflow(ctx, StartCorpusIndexParams{
			ProjectID:      event.ProjectID.String(),
			OrganizationID: event.OrganizationID,
			CommitSHA:      event.CommitSha,
			EventID:        event.ID.String(),
			TaskQueue:      r.taskQueue,
		})
		if err != nil {
			r.logger.ErrorContext(ctx, "start corpus index workflow", attr.SlogError(err))
			// Revert status to pending so it can be retried.
			_, revertErr := r.queries.UpdatePublishEventStatus(ctx, repo.UpdatePublishEventStatusParams{
				ID:        event.ID,
				ProjectID: event.ProjectID,
				Status:    "pending",
			})
			if revertErr != nil {
				r.logger.ErrorContext(ctx, "revert publish event status", attr.SlogError(revertErr))
			}
			continue
		}
	}

	return nil
}
