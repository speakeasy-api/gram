package corpus

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
	starter   WorkflowStarter
	taskQueue string
	logger    *slog.Logger
}

// NewReconciler creates a new Reconciler.
func NewReconciler(db *pgxpool.Pool, starter WorkflowStarter, taskQueue string, logger *slog.Logger) *Reconciler {
	panic("not implemented")
}

// Run starts the reconciler loop. It blocks until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context) error {
	_ = time.Second
	panic("not implemented")
}

// ReconcileOnce picks up pending outbox rows and enqueues workflows.
func (r *Reconciler) ReconcileOnce(ctx context.Context) error {
	panic("not implemented")
}
