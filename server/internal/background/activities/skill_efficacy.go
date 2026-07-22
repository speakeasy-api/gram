package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
)

// SkillEfficacyPublisher judges a reserved batch and publishes its scores.
// Satisfied by *efficacy.Publisher.
type SkillEfficacyPublisher interface {
	Publish(ctx context.Context, projectID uuid.UUID, ids []uuid.UUID, heartbeat func()) (efficacy.PublishResult, error)
}

type EnqueueSkillEfficacyPageParams struct {
	ProjectID uuid.UUID              `json:"project_id"`
	Cursor    efficacy.EnqueueCursor `json:"cursor"`
	PageSize  int32                  `json:"page_size"`
}

type EnqueueSkillEfficacyPageResult = efficacy.EnqueuePageResult

type ReserveSkillEfficacyEvaluationsParams struct {
	ProjectID uuid.UUID              `json:"project_id"`
	Cursor    efficacy.PendingCursor `json:"cursor"`
	BatchSize int32                  `json:"batch_size"`
}

// ReserveSkillEfficacyEvaluationsResult is the batch a reservation spent the
// budget on, and where its bounded candidate walk stopped. The coordinator
// persists the cursor and hands it back, so a queue deeper than one walk is
// worked through across passes instead of scanned from the head every time.
type ReserveSkillEfficacyEvaluationsResult struct {
	IDs        []uuid.UUID            `json:"ids"`
	NextCursor efficacy.PendingCursor `json:"next_cursor"`
}

type LoadReservedSkillEfficacyEvaluationsParams struct {
	ProjectID uuid.UUID `json:"project_id"`
	BatchSize int32     `json:"batch_size"`
}

// SkillEfficacyBatch is the set of evaluations a coordinator owns for one
// publication pass. Only the ids cross the activity boundary: the rows they name
// are re-read inside the publication under the same project scope, so a batch
// that goes stale between activities cannot be acted on from a snapshot.
type SkillEfficacyBatch struct {
	IDs []uuid.UUID `json:"ids"`
}

type PublishSkillEfficacyBatchParams struct {
	ProjectID uuid.UUID   `json:"project_id"`
	IDs       []uuid.UUID `json:"ids"`
}

type PublishSkillEfficacyBatchResult = efficacy.PublishResult

type ListSkillEfficacyProjectsParams struct {
	AfterProjectID uuid.UUID `json:"after_project_id"`
	PageLimit      int32     `json:"page_limit"`
}

type ResetStaleSkillEfficacyReservationsParams struct {
	ProjectID uuid.UUID `json:"project_id"`
}

type ResetStaleSkillEfficacyReservationsResult struct {
	Reset int64 `json:"reset"`
}

type SignalSkillEfficacyCoordinatorParams struct {
	ProjectID uuid.UUID `json:"project_id"`
}

// SkillEfficacyScorer holds the activity side of the efficacy pipeline. Each
// method is one durable step a coordinator drives, and each is safe to replay:
// the enqueue is idempotent per scoring unit, the reservation is serialised on
// the organization's budget lock, and the publication is guarded on the scores
// already in ClickHouse.
type SkillEfficacyScorer struct {
	db        *pgxpool.Pool
	features  efficacy.FeatureChecker
	publisher SkillEfficacyPublisher
	signaler  efficacy.Signaler
}

func NewSkillEfficacyScorer(
	db *pgxpool.Pool,
	features efficacy.FeatureChecker,
	publisher SkillEfficacyPublisher,
	signaler efficacy.Signaler,
) *SkillEfficacyScorer {
	return &SkillEfficacyScorer{db: db, features: features, publisher: publisher, signaler: signaler}
}

// EnqueueSkillEfficacyPage turns one bounded page of reconciled activations into
// pending evaluations. The cursor it returns is what the coordinator persists
// and hands back, so a walk spans as many pages as it needs without any one
// activity holding a transaction open across them.
func (s *SkillEfficacyScorer) EnqueueSkillEfficacyPage(ctx context.Context, params EnqueueSkillEfficacyPageParams) (*EnqueueSkillEfficacyPageResult, error) {
	result, err := efficacy.EnqueuePage(ctx, s.db, s.features, params.ProjectID, params.Cursor, params.PageSize)
	if err != nil {
		return nil, fmt.Errorf("enqueue skill efficacy page: %w", err)
	}

	return &result, nil
}

// ReserveSkillEfficacyEvaluations spends the organization's budget on the next
// batch. An empty batch is a normal outcome — an unentitled organization, a
// spent cap, or an empty queue all report it — so the coordinator reads it as
// "nothing to publish now" rather than as a failure.
func (s *SkillEfficacyScorer) ReserveSkillEfficacyEvaluations(ctx context.Context, params ReserveSkillEfficacyEvaluationsParams) (*ReserveSkillEfficacyEvaluationsResult, error) {
	evaluations, next, err := efficacy.Reserve(ctx, s.db, s.features, params.ProjectID, params.Cursor, params.BatchSize)
	if err != nil {
		return nil, fmt.Errorf("reserve skill efficacy evaluations: %w", err)
	}

	return &ReserveSkillEfficacyEvaluationsResult{IDs: evaluationIDs(evaluations), NextCursor: next}, nil
}

// LoadReservedSkillEfficacyEvaluations claims reserved evaluations whose owner
// is gone. It is the crash-recovery path: a batch this coordinator reserved is
// published from the reservation's own return value, so anything this claims was
// left behind by a worker that died mid-pass.
func (s *SkillEfficacyScorer) LoadReservedSkillEfficacyEvaluations(ctx context.Context, params LoadReservedSkillEfficacyEvaluationsParams) (*SkillEfficacyBatch, error) {
	evaluations, err := efficacy.LoadReserved(ctx, s.db, params.ProjectID, params.BatchSize)
	if err != nil {
		return nil, fmt.Errorf("load reserved skill efficacy evaluations: %w", err)
	}

	return &SkillEfficacyBatch{IDs: evaluationIDs(evaluations)}, nil
}

// PublishSkillEfficacyBatch judges the batch and writes its scores. This is the
// only activity that calls a model, which is why it runs on the dedicated
// efficacy task queue.
//
// A model failure never surfaces here: it is charged to its own evaluation's
// attempt counter inside the publication and reported through the result, so one
// bad session cannot fail the batch or the workflow. What does surface is
// infrastructure, and it comes back retryable so Temporal re-runs the pass
// against the same reserved rows. Anything the domain does not class as
// infrastructure is deterministic — a retry would read the same rows and reach
// the same conclusion — so it is raised non-retryable instead of burning the
// policy's attempts on it.
func (s *SkillEfficacyScorer) PublishSkillEfficacyBatch(ctx context.Context, params PublishSkillEfficacyBatchParams) (*PublishSkillEfficacyBatchResult, error) {
	// One heartbeat per evaluation. A batch is many minutes of paid model calls,
	// and without it a start-to-close timeout starts a second attempt against the
	// same reserved rows while this one is still judging them — the server's
	// cancellation only reaches a worker that heartbeats, and the domain only
	// stops at an evaluation boundary if something cancels its context.
	var heartbeat func()
	if activity.IsActivity(ctx) {
		heartbeat = func() { activity.RecordHeartbeat(ctx) }
	}
	result, err := s.publisher.Publish(ctx, params.ProjectID, params.IDs, heartbeat)
	switch {
	case err != nil && errors.Is(err, efficacy.ErrRetryable):
		return nil, fmt.Errorf("publish skill efficacy batch: %w", err)
	case err != nil:
		return nil, temporal.NewNonRetryableApplicationError("publish skill efficacy batch", "skill_efficacy_publish_error", err)
	case result.ModelFailures > 0:
		return nil, fmt.Errorf("retry %d skill efficacy model failures: %w", result.ModelFailures, efficacy.ErrRetryable)
	}

	return &result, nil
}

// ListSkillEfficacyProjects returns the next page of projects holding efficacy
// work the pipeline has not finished, including ones whose only remaining work
// is a reservation its owner never came back for — which each row names, so the
// sweep only pays for the reset on the projects that have one.
func (s *SkillEfficacyScorer) ListSkillEfficacyProjects(ctx context.Context, params ListSkillEfficacyProjectsParams) ([]efficacy.PendingWorkProject, error) {
	projects, err := efficacy.PendingWorkProjects(ctx, s.db, params.AfterProjectID, efficacy.StaleReservationAfter, params.PageLimit)
	if err != nil {
		return nil, fmt.Errorf("list projects with pending skill efficacy work: %w", err)
	}

	return projects, nil
}

// ResetStaleSkillEfficacyReservations returns a project's abandoned reservations
// to the queue, re-opening the budget slot each one held. Staleness is the
// domain's own threshold, orders of magnitude above the publication bound, so a
// reset can only reach a row whose owner is genuinely gone.
func (s *SkillEfficacyScorer) ResetStaleSkillEfficacyReservations(ctx context.Context, params ResetStaleSkillEfficacyReservationsParams) (*ResetStaleSkillEfficacyReservationsResult, error) {
	reset, err := efficacy.ResetStaleReservations(ctx, s.db, params.ProjectID, efficacy.StaleReservationAfter)
	if err != nil {
		return nil, fmt.Errorf("reset stale skill efficacy reservations: %w", err)
	}

	return &ResetStaleSkillEfficacyReservationsResult{Reset: reset}, nil
}

// SignalSkillEfficacyCoordinator wakes the project's coordinator, starting it if
// no run is live. Signalling from an activity rather than starting a child keeps
// a sweep that finds the same project on consecutive ticks from queueing a
// second coordinator behind the first.
func (s *SkillEfficacyScorer) SignalSkillEfficacyCoordinator(ctx context.Context, params SignalSkillEfficacyCoordinatorParams) error {
	if err := s.signaler.Signal(ctx, params.ProjectID); err != nil {
		return fmt.Errorf("signal skill efficacy coordinator: %w", err)
	}

	return nil
}

func evaluationIDs(evaluations []efficacy.Evaluation) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(evaluations))
	for _, evaluation := range evaluations {
		ids = append(ids, evaluation.ID)
	}

	return ids
}
