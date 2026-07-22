package efficacy

import (
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

// Evaluation lifecycle states. Spending states are exactly reserved and
// scored: both hold a budget slot, and only StateFailed and a stale-
// reservation reset give one back.
const (
	StatePending  = "pending"
	StateReserved = "reserved"
	StateFailed   = "failed"
)

// Scoring surfaces, mirroring the CASE in ListPendingSkillObservations:
// assistant producers map to SurfaceAssistant, every other producer to
// SurfaceDev.
const (
	SurfaceAssistant = "assistant"
	SurfaceDev       = "dev"
)

const (
	// InactivityWindow is how long a chat must have gone without a new message
	// before its session is considered finished and safe to score.
	InactivityWindow = 30 * time.Minute

	// StaleReservationAfter is how long a reserved evaluation may go without an
	// updated_at bump before it is treated as crashed and returned to the queue.
	// Orders of magnitude above the judge timeout, so a reset can only hit a row
	// whose owner is genuinely gone.
	StaleReservationAfter = 24 * time.Hour

	// MaxModelAttempts is the number of model attempts an evaluation gets before
	// it terminates as failed.
	MaxModelAttempts = 3

	// ReservedClaimLease is how long a LoadReserved claim owns the rows it
	// returns: a second claim raised inside the lease selects nothing, so two
	// publication passes cannot judge the same evaluation at the same time.
	//
	// The lease has to outlast the worst sequential pass over a claimed batch.
	// One judge call is bounded by judgeTimeout (60s) and the rest of an
	// evaluation — transcript read, limiter wait, score insert, scored mark — is
	// allowed as much again, so an evaluation costs at most 120s.
	// MaxReservedClaimBatch of them cost at most 10 * 120s = 20m, which this
	// leaves half of itself to spare over.
	ReservedClaimLease = 30 * time.Minute

	// MaxEnqueuePageSize is the widest page EnqueuePage will scan in one call.
	// A page is one transaction — scan, insert, confirm, stamp — so the bound is
	// on how many activations that transaction may hold locked at once, and it is
	// unrelated to the daily budget: enqueueing spends nothing, and a coordinator
	// reaches the rest of the queue by chaining NextCursor.
	MaxEnqueuePageSize int32 = 100

	// PendingCandidatePage is how many pending evaluations one reservation reads
	// per keyset page. It is unrelated to the batch a reservation hands out: the
	// page bounds how much of the queue is examined per round trip, and a pass
	// keeps paging until its batch is full or the queue runs out, so a capped
	// skill can never hold the head against the skills behind it.
	PendingCandidatePage int32 = 100

	// MaxPendingCandidatePages bounds the candidate walk of one reservation. The
	// whole walk runs inside the organization's advisory lock, which serialises
	// every other reserver in that organization behind it, so its cost has to be
	// a function of the bound rather than of the backlog. A walk cut off here
	// returns the cursor it stopped at and the next pass resumes from it, so the
	// bound moves work forward instead of hiding it.
	MaxPendingCandidatePages = 10

	// MaxReservedClaimBatch is the largest batch Reserve hands out or
	// LoadReserved claims — both are judged under the same lease — and it is
	// what keeps a claimed batch inside ReservedClaimLease. A backlog larger
	// than this — an organization's day is up to DefaultOrgDailyCap evaluations
	// — is drained by claiming again, not by claiming wider.
	MaxReservedClaimBatch int32 = 10
)

// Settings are the effective per-organization scoring budgets. A cap of 0 means
// that grain is off; Enabled false means the organization is off entirely.
type Settings struct {
	Enabled          bool
	PerSkillDailyCap int32
	OrgDailyCap      int32
	NewVersionBurst  int32
}

// EnqueueCursor is a position in a project's pending activations, which are
// ordered on the unique (seen_at, id) key. The zero value starts at the head of
// the queue. A holder does not construct one otherwise: it stores the cursor
// EnqueuePage returned and hands that back on the next call, which is how a
// walk that spans more pages than one call may scan is resumed — across
// process restarts and across a coordinator's own retries.
type EnqueueCursor struct {
	SeenAt time.Time
	ID     uuid.UUID
}

// PendingCursor is a position in a project's pending evaluations, which are
// walked recent-first on the unique (observed_at, id) key. The zero value
// starts at the head of the queue, and a reservation returns it again once its
// walk has reached the end — so a queue whose head is entirely capped is walked
// through in bounded steps and then started over, which is what stops the tail
// starving while the head cannot be admitted.
type PendingCursor struct {
	ObservedAt time.Time
	ID         uuid.UUID
}

// Candidate is one scoring unit — (session, surface, skill version) — assembled
// from the activations that share it. ObservationIDs carries every activation
// folded into the unit so the enqueue pass can stamp them once the unit is
// confirmed present.
//
// UserID and UserEmail are the actor the folded activations attributed the unit
// to, and they are what binds a dev unit to its chat: a dev session id comes
// from the client, so the chat is only that actor's to score when one of them
// matches it. Either may be empty — an activation carries whichever the ingest
// path resolved — and a dev unit with both empty matches no chat at all.
// Assistant activations carry neither and are bound by their server-generated
// session id instead.
type Candidate struct {
	SessionID       string
	Surface         string
	UserID          string
	UserEmail       string
	ChatID          uuid.UUID
	SkillID         uuid.UUID
	SkillVersionID  uuid.UUID
	CanonicalSha256 string
	ObservedAt      time.Time
	ObservationIDs  []uuid.UUID
}

// Evaluation is a queued scoring unit. It carries no verdict: score, rationale
// and ROI live only in ClickHouse, PostgreSQL holds pipeline state.
type Evaluation struct {
	ID              uuid.UUID
	OrganizationID  string
	ProjectID       uuid.UUID
	Surface         string
	SessionID       string
	ChatID          uuid.UUID
	SkillID         uuid.UUID
	SkillVersionID  uuid.UUID
	CanonicalSha256 string
	ObservedAt      time.Time
	State           string
	// ReservedOn is the UTC day the evaluation spent its budget slot on, zero
	// while the row has never been reserved.
	ReservedOn time.Time
	Attempts   int32
}

// NewEvaluation projects a stored row onto the domain type.
func NewEvaluation(row repo.SkillEfficacyEvaluation) Evaluation {
	evaluation := Evaluation{
		ID:              row.ID,
		OrganizationID:  row.OrganizationID,
		ProjectID:       row.ProjectID,
		Surface:         row.Surface,
		SessionID:       row.SessionID,
		ChatID:          row.ChatID,
		SkillID:         row.SkillID,
		SkillVersionID:  row.SkillVersionID,
		CanonicalSha256: row.CanonicalSha256,
		ObservedAt:      row.ObservedAt.Time,
		State:           row.State,
		ReservedOn:      time.Time{},
		Attempts:        row.Attempts,
	}
	if row.ReservedOn.Valid {
		evaluation.ReservedOn = row.ReservedOn.Time
	}

	return evaluation
}
