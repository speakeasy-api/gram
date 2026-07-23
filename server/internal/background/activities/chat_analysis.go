package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
)

// ChatAnalysisPublisher judges a reserved batch and publishes its verdicts.
// Satisfied by *analysis.Publisher.
type ChatAnalysisPublisher interface {
	Publish(ctx context.Context, projectID uuid.UUID, ids []uuid.UUID, heartbeat func()) (analysis.PublishResult, error)
}

type EnqueueChatAnalysisPageParams struct {
	ProjectID uuid.UUID              `json:"project_id"`
	Cursor    analysis.EnqueueCursor `json:"cursor"`
	PageSize  int32                  `json:"page_size"`
}

type EnqueueChatAnalysisPageResult = analysis.EnqueuePageResult

type ReserveChatAnalysisEvaluationsParams struct {
	ProjectID uuid.UUID              `json:"project_id"`
	Cursor    analysis.PendingCursor `json:"cursor"`
	BatchSize int32                  `json:"batch_size"`
}

// ReserveChatAnalysisEvaluationsResult is the batch a reservation spent the
// budget on, and where its bounded candidate walk stopped.
type ReserveChatAnalysisEvaluationsResult struct {
	IDs        []uuid.UUID            `json:"ids"`
	NextCursor analysis.PendingCursor `json:"next_cursor"`
}

type LoadReservedChatAnalysisEvaluationsParams struct {
	ProjectID uuid.UUID `json:"project_id"`
	BatchSize int32     `json:"batch_size"`
}

// ChatAnalysisBatch is the set of evaluations a coordinator owns for one
// publication pass. Only the ids cross the activity boundary: the rows they
// name are re-read inside the publication under the same project scope.
type ChatAnalysisBatch struct {
	IDs []uuid.UUID `json:"ids"`
}

type PublishChatAnalysisBatchParams struct {
	ProjectID uuid.UUID   `json:"project_id"`
	IDs       []uuid.UUID `json:"ids"`
}

type PublishChatAnalysisBatchResult = analysis.PublishResult

type ListChatAnalysisProjectsParams struct {
	AfterProjectID uuid.UUID `json:"after_project_id"`
	PageLimit      int32     `json:"page_limit"`
}

type ResetStaleChatAnalysisReservationsParams struct {
	ProjectID uuid.UUID `json:"project_id"`
}

type ResetStaleChatAnalysisReservationsResult struct {
	Reset int64 `json:"reset"`
}

type SignalChatAnalysisCoordinatorParams struct {
	ProjectID uuid.UUID `json:"project_id"`
}

// ChatAnalysisScorer holds the activity side of the chat analysis pipeline.
// Each method is one durable step a coordinator drives, and each is safe to
// replay: the enqueue is idempotent per (chat, judge) unit, the reservation is
// serialised on the organization's budget lock, and the publication is guarded
// on the scores already in ClickHouse.
type ChatAnalysisScorer struct {
	db        *pgxpool.Pool
	judges    *analysis.Judges
	publisher ChatAnalysisPublisher
	signaler  analysis.Signaler
}

func NewChatAnalysisScorer(
	db *pgxpool.Pool,
	judges *analysis.Judges,
	publisher ChatAnalysisPublisher,
	signaler analysis.Signaler,
) *ChatAnalysisScorer {
	return &ChatAnalysisScorer{db: db, judges: judges, publisher: publisher, signaler: signaler}
}

// EnqueueChatAnalysisPage turns one bounded page of a project's chats into
// pending evaluations. The cursor it returns is what the coordinator persists
// and hands back.
func (s *ChatAnalysisScorer) EnqueueChatAnalysisPage(ctx context.Context, params EnqueueChatAnalysisPageParams) (*EnqueueChatAnalysisPageResult, error) {
	result, err := analysis.EnqueuePage(ctx, s.db, s.judges, params.ProjectID, params.Cursor, params.PageSize)
	if err != nil {
		return nil, fmt.Errorf("enqueue chat analysis page: %w", err)
	}

	return &result, nil
}

// ReserveChatAnalysisEvaluations spends the organization's per-judge budgets on
// the next batch. An empty batch is a normal outcome — nothing enabled, spent
// caps, or an empty queue all report it.
func (s *ChatAnalysisScorer) ReserveChatAnalysisEvaluations(ctx context.Context, params ReserveChatAnalysisEvaluationsParams) (*ReserveChatAnalysisEvaluationsResult, error) {
	evaluations, next, err := analysis.Reserve(ctx, s.db, s.judges, params.ProjectID, params.Cursor, params.BatchSize)
	if err != nil {
		return nil, fmt.Errorf("reserve chat analysis evaluations: %w", err)
	}

	return &ReserveChatAnalysisEvaluationsResult{IDs: evaluationIDsForAnalysis(evaluations), NextCursor: next}, nil
}

// LoadReservedChatAnalysisEvaluations claims reserved evaluations whose owner
// is gone — the crash-recovery path.
func (s *ChatAnalysisScorer) LoadReservedChatAnalysisEvaluations(ctx context.Context, params LoadReservedChatAnalysisEvaluationsParams) (*ChatAnalysisBatch, error) {
	evaluations, err := analysis.LoadReserved(ctx, s.db, params.ProjectID, params.BatchSize)
	if err != nil {
		return nil, fmt.Errorf("load reserved chat analysis evaluations: %w", err)
	}

	return &ChatAnalysisBatch{IDs: evaluationIDsForAnalysis(evaluations)}, nil
}

// PublishChatAnalysisBatch judges the batch and writes its verdicts. This is
// the only activity that calls a model, which is why it runs on the dedicated
// judged-publication task queue.
//
// A model failure never surfaces here: it is charged to its own evaluation's
// attempt counter inside the publication and reported through the result.
// What does surface is infrastructure, and it comes back retryable so Temporal
// re-runs the pass against the same reserved rows.
func (s *ChatAnalysisScorer) PublishChatAnalysisBatch(ctx context.Context, params PublishChatAnalysisBatchParams) (*PublishChatAnalysisBatchResult, error) {
	// One heartbeat per evaluation: the server's cancellation only reaches a
	// worker that heartbeats, and the domain only stops at an evaluation
	// boundary if something cancels its context.
	var heartbeat func()
	if activity.IsActivity(ctx) {
		heartbeat = func() { activity.RecordHeartbeat(ctx) }
	}
	result, err := s.publisher.Publish(ctx, params.ProjectID, params.IDs, heartbeat)
	switch {
	case err != nil && errors.Is(err, analysis.ErrRetryable):
		return nil, fmt.Errorf("publish chat analysis batch: %w", err)
	case err != nil:
		return nil, temporal.NewNonRetryableApplicationError("publish chat analysis batch", "chat_analysis_publish_error", err)
	case result.ModelFailures > 0:
		return nil, fmt.Errorf("retry %d chat analysis model failures: %w", result.ModelFailures, analysis.ErrRetryable)
	}

	return &result, nil
}

// ListChatAnalysisProjects returns the next page of projects holding analysis
// work the pipeline has not finished.
func (s *ChatAnalysisScorer) ListChatAnalysisProjects(ctx context.Context, params ListChatAnalysisProjectsParams) ([]analysis.PendingWorkProject, error) {
	projects, err := analysis.PendingWorkProjects(ctx, s.db, params.AfterProjectID, analysis.StaleReservationAfter, params.PageLimit)
	if err != nil {
		return nil, fmt.Errorf("list projects with pending chat analysis work: %w", err)
	}

	return projects, nil
}

// ResetStaleChatAnalysisReservations returns a project's abandoned reservations
// to the queue, re-opening the budget slot each one held.
func (s *ChatAnalysisScorer) ResetStaleChatAnalysisReservations(ctx context.Context, params ResetStaleChatAnalysisReservationsParams) (*ResetStaleChatAnalysisReservationsResult, error) {
	reset, err := analysis.ResetStaleReservations(ctx, s.db, params.ProjectID, analysis.StaleReservationAfter)
	if err != nil {
		return nil, fmt.Errorf("reset stale chat analysis reservations: %w", err)
	}

	return &ResetStaleChatAnalysisReservationsResult{Reset: reset}, nil
}

// SignalChatAnalysisCoordinator wakes the project's coordinator, starting it if
// no run is live. Signalling from an activity rather than starting a child
// keeps a sweep that finds the same project on consecutive ticks from queueing
// a second coordinator behind the first.
func (s *ChatAnalysisScorer) SignalChatAnalysisCoordinator(ctx context.Context, params SignalChatAnalysisCoordinatorParams) error {
	if err := s.signaler.Signal(ctx, params.ProjectID); err != nil {
		return fmt.Errorf("signal chat analysis coordinator: %w", err)
	}

	return nil
}

func evaluationIDsForAnalysis(evaluations []analysis.Evaluation) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(evaluations))
	for _, evaluation := range evaluations {
		ids = append(ids, evaluation.ID)
	}

	return ids
}
