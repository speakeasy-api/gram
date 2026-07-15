package aiintegrations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SyncProgress describes how far a sync pipeline run got before failing.
// Implementations must be JSON-serializable so the progress can travel in
// Temporal application error details.
type SyncProgress interface {
	String() string
}

// SyncStageError attributes a sync pipeline failure to a named stage.
type SyncStageError struct {
	Stage string `json:"stage"`
	Err   error  `json:"-"`
}

// SyncError accumulates the per-stage failures of one sync pipeline run
// together with the progress the run made before failing. Its message
// carries every stage failure and the progress summary so failures surfaced
// in the Temporal UI describe the whole run, not just the first error.
type SyncError struct {
	// Op names the sync operation, e.g. "sync anthropic compliance".
	Op string
	// Stages holds the stage failures, with cancellation noise from
	// sibling-stage teardown removed.
	Stages   []SyncStageError
	Progress SyncProgress
}

// newSyncError combines the per-stage errors of a pipeline run into one
// SyncError. Nil stage errors are skipped. Context cancellation errors are
// dropped when at least one stage failed for a real reason, because errgroup
// cancels the remaining stages after the first failure and their context
// errors carry no signal. At least one stage error must be non-nil.
func newSyncError(op string, progress SyncProgress, stages ...SyncStageError) error {
	failed := make([]SyncStageError, 0, len(stages))
	for _, stage := range stages {
		if stage.Err != nil {
			failed = append(failed, stage)
		}
	}

	signal := make([]SyncStageError, 0, len(failed))
	for _, stage := range failed {
		if !errors.Is(stage.Err, context.Canceled) {
			signal = append(signal, stage)
		}
	}
	if len(signal) > 0 {
		failed = signal
	}

	return &SyncError{Op: op, Stages: failed, Progress: progress}
}

func (e *SyncError) Error() string {
	var sb strings.Builder
	sb.WriteString(e.Op)
	for i, stage := range e.Stages {
		if i == 0 {
			sb.WriteString(": ")
		} else {
			sb.WriteString("; ")
		}
		sb.WriteString("[")
		sb.WriteString(stage.Stage)
		sb.WriteString("] ")
		sb.WriteString(stage.Err.Error())
	}
	if e.Progress != nil {
		sb.WriteString(" (progress: ")
		sb.WriteString(e.Progress.String())
		sb.WriteString(")")
	}
	return sb.String()
}

// Unwrap exposes the stage errors so errors.Is/errors.As keep working
// against the underlying causes, e.g. provider HTTP errors.
func (e *SyncError) Unwrap() []error {
	errs := make([]error, 0, len(e.Stages))
	for _, stage := range e.Stages {
		errs = append(errs, stage.Err)
	}
	return errs
}

// ComplianceSyncProgress records how far an Anthropic compliance import run
// got before it stopped.
type ComplianceSyncProgress struct {
	FirstSync           bool `json:"first_sync"`
	ActivityPages       int  `json:"activity_pages"`
	ChatActivities      int  `json:"chat_activities"`
	ChatsImported       int  `json:"chats_imported"`
	MessagePagesFetched int  `json:"message_pages_fetched"`
	MessagePagesWritten int  `json:"message_pages_written"`
	// CursorReached is the activities pagination token discovery got to; it
	// shows how far the feed walk progressed regardless of durability.
	CursorReached string `json:"cursor_reached,omitempty"`
	// CursorPersisted is the last activities pagination token durably
	// written to the sync state during the run; retries resume from it.
	CursorPersisted string `json:"cursor_persisted,omitempty"`
}

func (p ComplianceSyncProgress) String() string {
	return fmt.Sprintf(
		"first_sync=%t activity_pages=%d chat_activities=%d chats_imported=%d message_pages_fetched=%d message_pages_written=%d cursor_reached=%q cursor_persisted=%q",
		p.FirstSync, p.ActivityPages, p.ChatActivities, p.ChatsImported, p.MessagePagesFetched, p.MessagePagesWritten, p.CursorReached, p.CursorPersisted,
	)
}

// CursorUsageSyncProgress records how far a Cursor usage poll run got before
// it stopped.
type CursorUsageSyncProgress struct {
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	UsagePages  int       `json:"usage_pages"`
	UsageEvents int       `json:"usage_events"`
}

func (p CursorUsageSyncProgress) String() string {
	// RFC3339Nano keeps the millisecond watermark advance on the window
	// start visible instead of truncating it to whole seconds.
	return fmt.Sprintf(
		"window=%s..%s usage_pages=%d usage_events=%d",
		p.WindowStart.Format(time.RFC3339Nano), p.WindowEnd.Format(time.RFC3339Nano), p.UsagePages, p.UsageEvents,
	)
}
