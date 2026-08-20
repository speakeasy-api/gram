package assistants

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

// InterruptTurnResult reports what a stop actually stopped.
//
// The two halves are independent because a turn can be waiting in either of
// two places: queued on the thread (no runner has claimed it — typically while
// a cold runtime boots), or in flight inside the runner. A stop pressed
// mid-boot cancels a queued turn and interrupts nothing; a stop pressed
// mid-reply interrupts the runner and cancels nothing.
type InterruptTurnResult struct {
	// ThreadID is the assistant thread the chat maps to, or uuid.Nil when the
	// conversation has not produced one yet.
	ThreadID uuid.UUID
	// Interrupted reports that the runner had a live turn and cancelled it.
	Interrupted bool
	// CancelledQueued counts turns dropped from the thread's queue before any
	// runner claimed them.
	CancelledQueued int64
}

// StoppedSomething reports whether the stop had any effect — the runner
// cancelled a turn, or a queued turn was dropped. False is a normal outcome:
// the reply had already finished when the button was pressed.
func (r InterruptTurnResult) StoppedSomething() bool {
	return r.Interrupted || r.CancelledQueued > 0
}

// InterruptDashboardTurn stops whatever a dashboard conversation currently has
// generating, on behalf of the user who owns the chat.
//
// Both places a turn can live are covered, queue first:
//
//  1. Turns still queued on the thread are cancelled, so a stop pressed while a
//     cold runtime boots does not simply delay the reply until the VM is up.
//  2. The runner is asked to interrupt the turn in flight, which unwinds the
//     agent loop through agentkit's cancellation path — partial output stays in
//     the transcript rather than being discarded.
//
// Queue before runner matters: interrupting first would leave a window in which
// the loop goes idle, the thread workflow claims the next queued event, and the
// assistant starts generating again out of the very stop that was meant to end
// it.
//
// Reaching neither (no thread, no runtime, nothing queued) is success with
// StoppedSomething() false, not an error: a stop that raced the reply landing
// is the common way for a user to press this button, and it has nothing to
// report but "there was nothing left to stop".
func (s *ServiceCore) InterruptDashboardTurn(
	ctx context.Context,
	projectID, assistantID uuid.UUID,
	callerUserID string,
	chatID uuid.UUID,
) (InterruptTurnResult, error) {
	if err := s.CheckDashboardChatOwnership(ctx, projectID, chatID, callerUserID); err != nil {
		return InterruptTurnResult{}, err
	}

	queries := assistantrepo.New(s.db)

	// Dashboard threads are correlated by chat id (see SendDashboardMessage),
	// so the chat is the whole key. No thread means no turn has ever been
	// dispatched for this conversation.
	threadID, err := queries.GetAssistantThreadIDByCorrelation(ctx, assistantrepo.GetAssistantThreadIDByCorrelationParams{
		ProjectID:     projectID,
		AssistantID:   assistantID,
		CorrelationID: chatID.String(),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return InterruptTurnResult{ThreadID: uuid.Nil, Interrupted: false, CancelledQueued: 0}, nil
	case err != nil:
		return InterruptTurnResult{}, fmt.Errorf("resolve dashboard thread for interrupt: %w", err)
	}

	result := InterruptTurnResult{ThreadID: threadID, Interrupted: false, CancelledQueued: 0}

	cancelled, err := queries.CancelPendingAssistantThreadEvents(ctx, assistantrepo.CancelPendingAssistantThreadEventsParams{
		CancelledStatus: eventStatusCancelled,
		ProjectID:       projectID,
		ThreadID:        threadID,
		PendingStatus:   eventStatusPending,
	})
	if err != nil {
		return result, fmt.Errorf("cancel queued assistant turns: %w", err)
	}
	result.CancelledQueued = cancelled

	interrupted, err := s.interruptThreadRuntime(ctx, projectID, assistantID, threadID)
	if err != nil {
		return result, err
	}
	result.Interrupted = interrupted

	if result.StoppedSomething() {
		// Only the writer knows how to reach the turn stream, and a stop that
		// leaves watchers hanging is indistinguishable from one that did
		// nothing. Best-effort: a lost frame costs responsiveness, and the
		// turn is stopped either way.
		s.chatWriter.PublishTurnInterrupted(ctx, chatID)
	}

	s.logger.InfoContext(ctx, "assistant turn interrupted",
		attr.SlogAssistantID(assistantID.String()),
		attr.SlogAssistantThreadID(threadID.String()),
		attr.SlogChatID(chatID.String()),
	)
	return result, nil
}

// interruptThreadRuntime asks the thread's live runtime to stop its turn.
// Absence of a runtime is not a failure — nothing is generating — so it
// reports false rather than erroring.
func (s *ServiceCore) interruptThreadRuntime(ctx context.Context, projectID, assistantID, threadID uuid.UUID) (bool, error) {
	row, err := assistantrepo.New(s.db).GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
		ProjectID:   projectID,
		AssistantID: assistantID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("resolve assistant runtime for interrupt: %w", err)
	}

	// A runtime that is still starting has no thread task to interrupt, and
	// one that is expiring or stopping is being torn down anyway. Only an
	// active row can be holding a turn.
	if row.State != runtimeStateActive {
		return false, nil
	}

	interrupted, err := s.runtime.InterruptTurn(ctx, assistantRuntimeRecord{
		ID:                  row.ID,
		AssistantThreadID:   row.AssistantThreadID,
		AssistantID:         row.AssistantID,
		ProjectID:           row.ProjectID,
		Backend:             row.Backend,
		BackendMetadataJSON: row.BackendMetadataJson,
		State:               row.State,
		WarmUntil:           row.WarmUntil,
	}, threadID)
	if err != nil {
		return false, fmt.Errorf("interrupt assistant runtime turn: %w", err)
	}
	return interrupted, nil
}
