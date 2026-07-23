// Package analysis runs a roster of LLM judges over finished chat sessions.
//
// It is the generalized sibling of the skill efficacy pipeline
// (server/internal/skills/efficacy) and mirrors its shape exactly: a durable
// Postgres queue of scoring units — here (chat, judge) rather than (session,
// skill version) — is enqueued from the chats table, reserved against the
// organization's per-judge daily budget under an advisory lock, judged, and
// published to the chat_analysis_scores ClickHouse sink. Adding a new analysis
// is implementing the Judge interface and registering it in the roster;
// enabling it for an organization is a chat_analysis_settings row.
package analysis

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
)

// Evaluation lifecycle states. Spending states are exactly reserved and
// scored: both hold a budget slot, and only StateFailed and a stale-
// reservation reset give one back.
const (
	StatePending  = "pending"
	StateReserved = "reserved"
	StateFailed   = "failed"
)

const (
	// InactivityWindow is how long a chat must have gone without a new message
	// before its session is considered finished and safe to judge.
	InactivityWindow = 30 * time.Minute

	// EnqueueLookback bounds how far back the enqueue walk reaches into a
	// project's chats. The walk restarts from the head each time it exhausts the
	// queue, so the lookback is what keeps a re-walk's cost proportional to
	// recent activity rather than to the project's full history. Sessions older
	// than this when the pipeline is enabled are never analyzed.
	EnqueueLookback = 14 * 24 * time.Hour

	// StaleReservationAfter is how long a reserved evaluation may go without an
	// updated_at bump before it is treated as crashed and returned to the queue.
	StaleReservationAfter = 24 * time.Hour

	// MaxModelAttempts is the number of model attempts an evaluation gets before
	// it terminates as failed.
	MaxModelAttempts = 3

	// ReservedClaimLease is how long a LoadReserved claim owns the rows it
	// returns. Sized exactly as the efficacy lease: one evaluation is bounded at
	// two minutes, a batch holds at most MaxReservedClaimBatch of them, and the
	// lease leaves half of itself spare over the worst sequential pass.
	ReservedClaimLease = 30 * time.Minute

	// MaxEnqueuePageSize is the widest page EnqueuePage will scan in one call.
	MaxEnqueuePageSize int32 = 100

	// PendingCandidatePage is how many pending evaluations one reservation reads
	// per keyset page.
	PendingCandidatePage int32 = 100

	// MaxPendingCandidatePages bounds the candidate walk of one reservation. The
	// whole walk runs inside the organization's advisory lock, so its cost has to
	// be a function of the bound rather than of the backlog.
	MaxPendingCandidatePages = 10

	// MaxReservedClaimBatch is the largest batch Reserve hands out or
	// LoadReserved claims — both are judged under the same lease.
	MaxReservedClaimBatch int32 = 10
)

// EnqueueCursor is a position in a project's chats, which the enqueue walks
// oldest-first on the immutable (created_at, id) key. The zero value starts at
// the head. A holder stores the cursor EnqueuePage returned and hands it back
// on the next call, which is how a walk that spans more pages than one call may
// scan is resumed across process restarts and a coordinator's own retries.
type EnqueueCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// PendingCursor is a position in a project's pending evaluations, which are
// walked recent-first on the unique (observed_at, id) key. The zero value
// starts at the head of the queue, and a reservation returns it again once its
// walk has reached the end.
type PendingCursor struct {
	ObservedAt time.Time
	ID         uuid.UUID
}

// Evaluation is a queued scoring unit. It carries no verdict: score and detail
// live only in ClickHouse, PostgreSQL holds pipeline state.
type Evaluation struct {
	ID             uuid.UUID
	OrganizationID string
	ProjectID      uuid.UUID
	ChatID         uuid.UUID
	Judge          string
	ObservedAt     time.Time
	State          string
	// ReservedOn is the UTC day the evaluation spent its budget slot on, zero
	// while the row has never been reserved.
	ReservedOn time.Time
	Attempts   int32
}

// NewEvaluation projects a stored row onto the domain type.
func NewEvaluation(row repo.ChatAnalysisEvaluation) Evaluation {
	evaluation := Evaluation{
		ID:             row.ID,
		OrganizationID: row.OrganizationID,
		ProjectID:      row.ProjectID,
		ChatID:         row.ChatID,
		Judge:          row.Judge,
		ObservedAt:     row.ObservedAt.Time,
		State:          row.State,
		ReservedOn:     time.Time{},
		Attempts:       row.Attempts,
	}
	if row.ReservedOn.Valid {
		evaluation.ReservedOn = row.ReservedOn.Time
	}

	return evaluation
}

// Signaler wakes a project's chat analysis coordinator. Implementations are
// expected to be idempotent: every producer signals on each durable write, so
// the same project is woken many times over one session and a wake carries no
// payload beyond the project it names.
//
// Declared here rather than imported from the workflow layer so the producers
// depend on the analysis domain and never on the background package that runs
// the coordinator.
type Signaler interface {
	Signal(ctx context.Context, projectID uuid.UUID) error
}

// observer wakes the coordinator when a project stores new chat messages. A
// session becomes analyzable only once its transcript has gone quiet, so the
// transcript write is the event that can make an already-queued unit eligible
// — and the one that eventually stops arriving.
type observer struct {
	logger   *slog.Logger
	signaler Signaler
}

var _ chat.MessageObserver = (*observer)(nil)

// NewObserver builds the chat.MessageObserver that turns durable chat-message
// persistence into a chat analysis wake. Register it on the chat message
// writer.
func NewObserver(logger *slog.Logger, signaler Signaler) chat.MessageObserver {
	return &observer{
		logger:   logger.With(attr.SlogComponent("chat-analysis")),
		signaler: signaler,
	}
}

// OnMessagesStored implements chat.MessageObserver. The writer only calls this
// after a write that durably stored at least one row, and it dispatches on its
// own goroutine with a detached context, so blocking work is safe here. A wake
// that cannot be delivered is logged and dropped: the persistence path must not
// fail because the coordinator is unreachable, and the next write — or the
// sweep — recovers the queue.
func (o *observer) OnMessagesStored(ctx context.Context, projectID uuid.UUID) {
	if err := o.signaler.Signal(ctx, projectID); err != nil {
		o.logger.ErrorContext(ctx, "signal chat analysis coordinator on stored messages",
			attr.SlogError(err),
			attr.SlogProjectID(projectID.String()),
		)
	}
}
