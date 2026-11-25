package hooksrollout

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooksrollout/repo"
)

// A staged rollout clears a growing share of the organizations that follow the
// default pin for a newer hooks generator version. Stages advance on time
// alone: after StageSoak at one share the next share applies, and the last
// stage moves the default pin itself. Staff pause it from the admin dashboard
// when a hooks alert fires.

// StageState is where a staged rollout stands.
type StageState string

const (
	StageRunning   StageState = "running"
	StagePaused    StageState = "paused"
	StageCompleted StageState = "completed"
	StageCancelled StageState = "cancelled"
)

// StageSoak is how long a stage runs before the next one starts.
const StageSoak = 24 * time.Hour

// StageWorkflowActor is the set_by of changes the rollout schedule makes.
const StageWorkflowActor = "hooks-rollout-schedule"

// stagePercents are the shares a staged rollout steps through. The first stage
// clears no one beyond the canary organization; reaching 100 completes the
// rollout by moving the default pin.
var stagePercents = []int{0, 25, 100}

var (
	ErrNoActiveStage      = errors.New("no staged hooks rollout is running or paused")
	ErrStageNotRunning    = errors.New("the staged hooks rollout is not running")
	ErrStageNotPaused     = errors.New("the staged hooks rollout is not paused")
	ErrStageVersionBehind = errors.New("version must be newer than the default pin")
	ErrStageChanged       = errors.New("the staged hooks rollout changed since it was read")
)

// Stage is the current state of a staged rollout.
type Stage struct {
	// Revision increases with every change, so a caller can tell whether the
	// rollout it showed an operator is still the current one.
	Revision int64
	Version  int
	Percent  int
	State    StageState
	Reason   string
	SetBy    string
	SetAt    time.Time
}

// Active reports whether the rollout still clears organizations beyond the
// default pin.
func (s *Stage) Active() bool {
	return s != nil && (s.State == StageRunning || s.State == StagePaused)
}

// NextAdvance is when the running stage moves on, zero when it won't.
func (s *Stage) NextAdvance() time.Time {
	if s == nil || s.State != StageRunning {
		return time.Time{}
	}
	return s.SetAt.Add(StageSoak)
}

// InStage reports whether the organization falls within the first percent of
// organizations. The bucket is a stable hash of the id, so an organization
// stays cleared as the share grows.
func InStage(organizationID string, percent int) bool {
	sum := sha256.Sum256([]byte(organizationID))
	return int(binary.BigEndian.Uint64(sum[:8])%100) < percent
}

func nextPercent(percent int) int {
	for _, p := range stagePercents {
		if p > percent {
			return p
		}
	}
	return 100
}

// Beginner opens transactions. *pgxpool.Pool satisfies it.
type Beginner interface {
	repo.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// StageStore reads and changes the staged rollout.
type StageStore struct {
	db Beginner
}

func NewStageStore(db Beginner) *StageStore {
	return &StageStore{db: db}
}

// Current reads the current stage, nil when no rollout was ever staged.
func (s *StageStore) Current(ctx context.Context) (*Stage, error) {
	return currentStage(ctx, repo.New(s.db))
}

func currentStage(ctx context.Context, q *repo.Queries) (*Stage, error) {
	row, err := q.GetCurrentHooksRolloutStage(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("get current hooks rollout stage: %w", err)
	}
	return &Stage{
		Revision: row.Seq,
		Version:  int(row.Version),
		Percent:  int(row.Percent),
		State:    StageState(row.State),
		Reason:   conv.FromPGTextOrEmpty[string](row.Reason),
		SetBy:    row.SetBy,
		SetAt:    row.CreatedAt.Time,
	}, nil
}

// Revision is the rollout's current revision, 0 when none was ever staged.
func (s *Stage) CurrentRevision() int64 {
	if s == nil {
		return 0
	}
	return s.Revision
}

// The mutations below take the revision the caller last read and fail with
// ErrStageChanged when the rollout has moved on since, so an operator never
// acts on a rollout they weren't shown.

// Start begins a staged rollout of version at the first stage. It replaces any
// active rollout of an older version.
func (s *StageStore) Start(ctx context.Context, revision int64, version int, setBy string) error {
	return s.change(ctx, revision, func(q *repo.Queries, current *Stage) error {
		defaultPin, err := defaultPin(ctx, q)
		if err != nil {
			return err
		}
		if defaultPin != nil && version <= defaultPin.Version {
			return ErrStageVersionBehind
		}
		if current.Active() && current.Version == version {
			return nil
		}
		return insertStage(ctx, q, Stage{Revision: 0, Version: version, Percent: stagePercents[0], State: StageRunning, Reason: "", SetBy: setBy, SetAt: time.Time{}})
	})
}

// Pause holds the rollout at its current share until it is resumed.
func (s *StageStore) Pause(ctx context.Context, revision int64, reason, setBy string) error {
	return s.change(ctx, revision, func(q *repo.Queries, current *Stage) error {
		if !current.Active() {
			return ErrNoActiveStage
		}
		if current.State != StageRunning {
			return ErrStageNotRunning
		}
		return insertStage(ctx, q, Stage{Revision: 0, Version: current.Version, Percent: current.Percent, State: StagePaused, Reason: reason, SetBy: setBy, SetAt: time.Time{}})
	})
}

// Resume restarts a paused rollout. The soak at the current share starts over.
func (s *StageStore) Resume(ctx context.Context, revision int64, setBy string) error {
	return s.change(ctx, revision, func(q *repo.Queries, current *Stage) error {
		if !current.Active() {
			return ErrNoActiveStage
		}
		if current.State != StagePaused {
			return ErrStageNotPaused
		}
		return insertStage(ctx, q, Stage{Revision: 0, Version: current.Version, Percent: current.Percent, State: StageRunning, Reason: "", SetBy: setBy, SetAt: time.Time{}})
	})
}

// Cancel ends the rollout. Organizations it cleared go back to the default pin.
func (s *StageStore) Cancel(ctx context.Context, revision int64, reason, setBy string) error {
	return s.change(ctx, revision, func(q *repo.Queries, current *Stage) error {
		if !current.Active() {
			return ErrNoActiveStage
		}
		return insertStage(ctx, q, Stage{Revision: 0, Version: current.Version, Percent: current.Percent, State: StageCancelled, Reason: reason, SetBy: setBy, SetAt: time.Time{}})
	})
}

// Advance moves a running rollout to its next stage once the soak has passed.
// The last stage moves the default pin to the rollout's version and completes
// it. It reports the stage it moved to, nil when nothing changed.
func (s *StageStore) Advance(ctx context.Context, now time.Time) (*Stage, error) {
	var advanced *Stage
	err := withStageLock(ctx, s.db, func(q *repo.Queries) error {
		current, err := currentStage(ctx, q)
		if err != nil {
			return err
		}
		if current == nil || current.State != StageRunning || now.Before(current.NextAdvance()) {
			return nil
		}
		next := Stage{Revision: 0, Version: current.Version, Percent: nextPercent(current.Percent), State: StageRunning, Reason: "", SetBy: StageWorkflowActor, SetAt: time.Time{}}
		if next.Percent >= 100 {
			next.State = StageCompleted
			pin, err := defaultPin(ctx, q)
			if err != nil {
				return err
			}
			if pin == nil || pin.Version < next.Version {
				if err := insertDefaultPin(ctx, q, next.Version, StageWorkflowActor); err != nil {
					return err
				}
			}
		}
		if err := insertStage(ctx, q, next); err != nil {
			return err
		}
		advanced = &next
		return nil
	})
	if err != nil {
		return nil, err
	}
	return advanced, nil
}

func (s *StageStore) change(ctx context.Context, revision int64, fn func(q *repo.Queries, current *Stage) error) error {
	return withStageLock(ctx, s.db, func(q *repo.Queries) error {
		current, err := currentStage(ctx, q)
		if err != nil {
			return err
		}
		if current.CurrentRevision() != revision {
			return ErrStageChanged
		}
		return fn(q, current)
	})
}

// withStageLock runs fn in a transaction holding the lock that serializes
// stage changes and default pin moves. Inside a caller's transaction it nests
// as a savepoint.
func withStageLock(ctx context.Context, db Beginner, fn func(q *repo.Queries) error) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin hooks rollout change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := repo.New(tx)
	if err := q.LockHooksRolloutStages(ctx); err != nil {
		return fmt.Errorf("lock hooks rollout stages: %w", err)
	}
	if err := fn(q); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit hooks rollout change: %w", err)
	}
	return nil
}

func insertStage(ctx context.Context, q *repo.Queries, stage Stage) error {
	if _, err := q.InsertHooksRolloutStage(ctx, repo.InsertHooksRolloutStageParams{
		Version: conv.SafeInt32(stage.Version),
		Percent: conv.SafeInt32(stage.Percent),
		State:   string(stage.State),
		Reason:  conv.ToPGTextEmpty(stage.Reason),
		SetBy:   stage.SetBy,
	}); err != nil {
		return fmt.Errorf("insert hooks rollout stage: %w", err)
	}
	return nil
}

func insertDefaultPin(ctx context.Context, q *repo.Queries, version int, setBy string) error {
	if _, err := q.InsertHooksRolloutPin(ctx, repo.InsertHooksRolloutPinParams{
		OrganizationID: pgtype.Text{String: "", Valid: false},
		Version:        pgtype.Int4{Int32: conv.SafeInt32(version), Valid: true},
		SetBy:          setBy,
	}); err != nil {
		return fmt.Errorf("insert default hooks rollout pin: %w", err)
	}
	return nil
}
