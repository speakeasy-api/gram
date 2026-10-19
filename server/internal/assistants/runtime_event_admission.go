package assistants

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
)

var errRuntimeAuthorityRetired = errors.New("assistant runtime authority is no longer active")

// claimRuntimeEvent serializes event admission with selection changes. A runtime
// loaded before a switch must not claim an event ingested after that switch.
// The processing row keeps switching busy after this short transaction commits.
func (s *ServiceCore) claimRuntimeEvent(ctx context.Context, runtime assistantRuntimeRecord, threadID uuid.UUID) (assistantThreadEventRecord, bool, error) {
	var zero assistantThreadEventRecord
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, false, fmt.Errorf("begin runtime event admission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := assistantrepo.New(tx)
	if err := queries.AcquireAssistantAdvisoryLock(ctx, runtime.AssistantID.String()); err != nil {
		return zero, false, fmt.Errorf("lock runtime event admission: %w", err)
	}
	row, err := queries.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ID: runtime.ID, ProjectID: runtime.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, false, errRuntimeAuthorityRetired
	}
	if err != nil {
		return zero, false, fmt.Errorf("read event runtime authority: %w", err)
	}
	if row.Deleted || row.Ended || row.AssistantID != runtime.AssistantID || row.RuntimeVersion != runtime.RuntimeVersion || row.Backend != runtime.Backend || row.State != runtimeStateActive {
		return zero, false, errRuntimeAuthorityRetired
	}
	event, ok, err := claimNextPendingEventWithQueries(ctx, queries, runtime.ProjectID, threadID)
	if err != nil {
		return zero, false, err
	}
	if ok && event.AssistantID != runtime.AssistantID {
		return zero, false, errRuntimeAuthorityRetired
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, false, fmt.Errorf("commit runtime event admission: %w", err)
	}
	return event, ok, nil
}
