package efficacy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// guardWindowSlack is how far past the current pass the existence guard
	// looks. Two days absorb a judge run that started before midnight UTC and
	// inserted after it.
	guardWindowSlack = 48 * time.Hour
	// publishEvaluationTimeout is the hard bound on one evaluation's whole
	// publication: transcript read, judge call, score insert and scored mark. It
	// is the 120s per evaluation ReservedClaimLease is sized against, so a batch
	// of MaxReservedClaimBatch cannot outlive the claim that owns its rows even
	// when every step in it hangs.
	publishEvaluationTimeout = 2 * time.Minute
	// transcriptPageSize is how many messages one backwards step of the
	// transcript loader reads. It is small because the loader overshoots by at
	// most one page: the page that pushes the rendering over its budget is the
	// last one read, so the page size is the whole of the memory the loader
	// spends beyond the transcript it is going to send.
	transcriptPageSize = 20
)

// modelFailureClass is the whole of what a model failure records, in last_error
// and in the log line alike. A provider's error body can quote the request back,
// and this request carries the session transcript and the skill body, so neither
// the column nor the log ever sees the cause — the row's own identifiers say
// which evaluation failed, and the judge's span carries why. It is the sentinel's
// own text so the class cannot drift from the error it classifies.
var modelFailureClass = ErrModelFailure.Error()

// sinkFailureClass is what a post-inference score-sink failure records. It is
// distinct from modelFailureClass because the judge answered and was paid for —
// the attempt is charged to bound that payment, not to blame the model.
const sinkFailureClass = "skill efficacy score sink failure"

const validationFailureClass = "skill efficacy row validation failure"

// TranscriptSource reads one chat's messages a page at a time, newest first.
// Satisfied by *chatrepo.Queries.
type TranscriptSource interface {
	CountChatMessages(ctx context.Context, arg chatrepo.CountChatMessagesParams) (int64, error)
	ListChatTranscriptMessagesPage(ctx context.Context, arg chatrepo.ListChatTranscriptMessagesPageParams) ([]chatrepo.ListChatTranscriptMessagesPageRow, error)
}

// ScoreSink is the ClickHouse side of publication: the existence guard and the
// synchronous insert. Satisfied by *telemetryrepo.Queries.
type ScoreSink interface {
	ListExistingSkillEfficacyScoreIDs(ctx context.Context, arg telemetryrepo.ListExistingSkillEfficacyScoreIDsParams) ([]string, error)
	InsertSkillEfficacyScores(ctx context.Context, rows []telemetryrepo.SkillEfficacyScore) error
}

// JudgeClient scores one session. Satisfied by *Judge.
type JudgeClient interface {
	Judge(ctx context.Context, in JudgeInput) (JudgeResult, error)
}

// PublishResult reports what one publication pass did with the reserved
// evaluations it was handed.
type PublishResult struct {
	// Loaded is the number of still-reserved evaluations the batch resolved.
	Loaded int
	// AlreadyPublished is how many of those the existence guard found in
	// ClickHouse, so they were marked scored without being judged again.
	AlreadyPublished int
	// Scored is how many evaluations ended the pass in state scored.
	Scored int
	// ModelFailures is how many took a non-terminal model failure and stayed
	// reserved with an incremented attempt count.
	ModelFailures int
	// Failed is how many terminated, either after exhausting MaxModelAttempts or
	// immediately because row validation proved a retry cannot succeed.
	Failed int
	// Retryable is how many hit an infrastructure failure that still needs
	// another pass. Post-inference sink failures charge an attempt first.
	Retryable int
}

// Publisher judges reserved evaluations and publishes their scores.
type Publisher struct {
	logger *slog.Logger
	tracer trace.Tracer
	db     *pgxpool.Pool
	chats  TranscriptSource
	scores ScoreSink
	judge  JudgeClient
	// evaluationTimeout is publishEvaluationTimeout, held on the struct so a test
	// can shorten the bound it is asserting on.
	evaluationTimeout time.Duration
}

// NewPublisher constructs a Publisher.
func NewPublisher(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, scores ScoreSink, judge JudgeClient) *Publisher {
	return &Publisher{
		logger:            logger.With(attr.SlogComponent("skill-efficacy-publisher")),
		tracer:            tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/skills/efficacy"),
		db:                db,
		chats:             chatrepo.New(db),
		scores:            scores,
		judge:             judge,
		evaluationTimeout: publishEvaluationTimeout,
	}
}

// Publish judges the given reserved evaluations and writes their scores.
//
// Publication order per evaluation is existence guard → judge → synchronous
// insert → mark scored, and the guard runs for the WHOLE batch before any judge
// call: a retry that follows a crash between insert and mark must not pay for
// inference a second time. The score id is the evaluation id, so a replayed
// insert is the same row rather than a duplicate, and the guard window starts at
// the batch's earliest evaluation created_at and always extends past the current
// pass, so it contains every insert a previous pass could have made — including
// one made for a reservation older than the slack, and one made before a stale
// reset moved the row's reservation day forward.
//
// A model failure charges the evaluation an attempt and the batch continues; the
// third one terminates that evaluation as failed and never writes a score. A
// deterministic row-validation failure terminates immediately. An infrastructure
// failure changes no state and charges no attempt — it comes back wrapping
// ErrRetryable so the caller retries the same reserved rows — with the one
// exception of a sink failure after the judge has answered, which is charged as
// well so a broken sink cannot buy the same inference forever.
//
// heartbeat, when given, is called once before each evaluation. It is what lets
// the caller's own lease on the batch stay live across a long pass and, on the
// Temporal path, what delivers a cancellation: the pass stops at the next
// evaluation boundary rather than paying for a batch a second attempt already
// owns.
func (p *Publisher) Publish(ctx context.Context, projectID uuid.UUID, ids []uuid.UUID, heartbeat func()) (PublishResult, error) {
	ctx, span := p.tracer.Start(ctx, "skill.efficacy.publish", trace.WithAttributes(
		attr.ProjectID(projectID.String()),
	))
	defer span.End()

	result := PublishResult{Loaded: 0, AlreadyPublished: 0, Scored: 0, ModelFailures: 0, Failed: 0, Retryable: 0}
	if len(ids) == 0 {
		return result, nil
	}

	queries := repo.New(p.db)
	// Project-scoped and state-scoped: rows another worker already failed or
	// scored are simply not returned, so this batch cannot act on them.
	inputs, err := queries.GetSkillEfficacyJudgeInputs(ctx, repo.GetSkillEfficacyJudgeInputsParams{
		ProjectID: projectID,
		Ids:       ids,
	})
	if err != nil {
		err = fmt.Errorf("load skill efficacy judge inputs: %w: %w", ErrRetryable, err)
		span.SetStatus(codes.Error, err.Error())
		return result, err
	}
	result.Loaded = len(inputs)
	if len(inputs) == 0 {
		return result, nil
	}

	published, err := p.alreadyPublished(ctx, projectID, inputs)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return result, err
	}

	// A batch can hold several evaluations of the same session — one per skill the
	// session activated — and they all judge the same messages, so the read and
	// the render are paid once per chat for the length of this pass.
	transcripts := make(map[uuid.UUID]Transcript, len(inputs))

	var retryable []error
	for _, input := range inputs {
		// Reported before the evaluation rather than after it, so the caller that
		// owns the batch's lease hears from the pass at least once per bounded
		// step, and a batch the owner has been told to stop working on stops here
		// instead of paying for the rest of it.
		if heartbeat != nil {
			heartbeat()
		}
		if err := ctx.Err(); err != nil {
			err = fmt.Errorf("skill efficacy publication cancelled: %w: %w", ErrRetryable, err)
			span.SetStatus(codes.Error, err.Error())
			return result, errors.Join(append(retryable, err)...)
		}

		if _, ok := published[input.ID]; ok {
			// The score is already in ClickHouse: a crash between a previous
			// insert and its mark. Finish the transition, judge nothing.
			if err := p.markScored(ctx, projectID, input.ID); err != nil {
				result.Retryable++
				retryable = append(retryable, err)
				continue
			}
			result.AlreadyPublished++
			result.Scored++
			continue
		}

		if err := p.publishEvaluation(ctx, projectID, input, transcripts, &result); err != nil {
			retryable = append(retryable, err)
		}
	}

	if len(retryable) > 0 {
		err := errors.Join(retryable...)
		span.SetStatus(codes.Error, err.Error())
		return result, err
	}

	return result, nil
}

// alreadyPublished asks the sink which of the batch's evaluations already have a
// score row. The batch is one project, so it is one organization, and the guard
// read pins organization_id, project_id and the batch's exact skill and skill
// version ids — the sink's four leading ORDER BY columns.
func (p *Publisher) alreadyPublished(ctx context.Context, projectID uuid.UUID, inputs []repo.GetSkillEfficacyJudgeInputsRow) (map[uuid.UUID]struct{}, error) {
	minCreatedAt, maxCreatedAt := guardWindow(inputs, time.Now().UTC())

	organizationID := inputs[0].OrganizationID
	ids := make([]string, 0, len(inputs))
	index := make(map[string]uuid.UUID, len(inputs))
	skillIDs := make([]string, 0, len(inputs))
	seenSkills := make(map[uuid.UUID]struct{}, len(inputs))
	skillVersionIDs := make([]string, 0, len(inputs))
	seenVersions := make(map[uuid.UUID]struct{}, len(inputs))
	for _, input := range inputs {
		if input.OrganizationID != organizationID {
			// One project belongs to one organization, so a mixed batch means the
			// rows disagree with the projects table. Retrying reads the same rows.
			return nil, fmt.Errorf("skill efficacy guard window: project %s resolved evaluations across organizations", projectID)
		}
		id := input.ID.String()
		ids = append(ids, id)
		index[id] = input.ID
		if _, ok := seenSkills[input.SkillID]; !ok {
			seenSkills[input.SkillID] = struct{}{}
			skillIDs = append(skillIDs, input.SkillID.String())
		}
		if _, ok := seenVersions[input.SkillVersionID]; !ok {
			seenVersions[input.SkillVersionID] = struct{}{}
			skillVersionIDs = append(skillVersionIDs, input.SkillVersionID.String())
		}
	}

	existing, err := p.scores.ListExistingSkillEfficacyScoreIDs(ctx, telemetryrepo.ListExistingSkillEfficacyScoreIDsParams{
		OrganizationID:  organizationID,
		ProjectID:       projectID.String(),
		SkillIDs:        skillIDs,
		SkillVersionIDs: skillVersionIDs,
		IDs:             ids,
		MinCreatedAt:    minCreatedAt,
		MaxCreatedAt:    maxCreatedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("list existing skill efficacy scores: %w: %w", ErrRetryable, err)
	}

	published := make(map[uuid.UUID]struct{}, len(existing))
	for _, id := range existing {
		if evaluationID, ok := index[id]; ok {
			published[evaluationID] = struct{}{}
		}
	}

	return published, nil
}

// guardWindow spans from the UTC midnight of the batch's earliest evaluation
// created_at to two days past now.
//
// The lower bound is the evaluation's birth stamp, which no transition rewrites,
// so it is identical on every pass and can never sit after a score an earlier
// pass inserted for that evaluation — a row cannot be scored before it exists.
// reserved_on cannot serve here: a stale reservation is reset and re-reserved on
// the current UTC day, which moves the day forward past the earlier score.
// Rounding down to midnight only widens the window, which is the safe direction.
//
// The upper bound tracks the clock because created_at is stamped at insert time,
// and every score an earlier pass could have written was stamped before this one
// started. A bound derived from the reservation instead would end before the
// score of a row reserved days ago, and the guard would miss it and pay for
// inference again.
func guardWindow(inputs []repo.GetSkillEfficacyJudgeInputsRow, now time.Time) (time.Time, time.Time) {
	var minCreated time.Time
	for _, input := range inputs {
		if created := input.EvaluationCreatedAt.Time.UTC().Truncate(24 * time.Hour); minCreated.IsZero() || created.Before(minCreated) {
			minCreated = created
		}
	}

	return minCreated, now.UTC().Add(guardWindowSlack)
}

// publishEvaluation runs one evaluation's publication under evaluationTimeout.
// Every step it covers can hang — a chat read, a judge call, a ClickHouse insert
// — and without a bound one hung step holds the batch past the lease that owns
// its rows, letting a second pass judge them concurrently. A hang is
// infrastructure: state and attempt counter are both left alone, so the caller
// retries the same reserved rows.
func (p *Publisher) publishEvaluation(ctx context.Context, projectID uuid.UUID, input repo.GetSkillEfficacyJudgeInputsRow, transcripts map[uuid.UUID]Transcript, result *PublishResult) error {
	evaluationCtx, cancel := context.WithTimeout(ctx, p.evaluationTimeout)
	defer cancel()

	err := p.publishOne(evaluationCtx, projectID, input, transcripts, result)
	if err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		// The bound expired while the pass's own context was still live, so the
		// hang is this evaluation's. The step that surfaced it cannot tell a hung
		// call from a slow one, so the classification is applied here.
		return fmt.Errorf("skill efficacy evaluation timed out: %w: %w", ErrRetryable, err)
	}

	return err
}

// publishOne judges one evaluation and publishes its score. It returns an error
// only for infrastructure failures; model failures and deterministic row
// validation failures are charged locally so the rest of the batch still runs.
func (p *Publisher) publishOne(ctx context.Context, projectID uuid.UUID, input repo.GetSkillEfficacyJudgeInputsRow, transcripts map[uuid.UUID]Transcript, result *PublishResult) error {
	if input.Surface != SurfaceDev && input.Surface != SurfaceAssistant {
		terminal, err := p.chargeAttempt(ctx, projectID, input, validationFailureClass, 1)
		if err != nil {
			result.Retryable++
			return err
		}
		if terminal {
			result.Failed++
		}
		return nil
	}

	transcript, err := p.loadTranscript(ctx, projectID, input.ChatID, transcripts)
	if err != nil {
		result.Retryable++
		return err
	}

	judged, err := p.judge.Judge(ctx, JudgeInput{
		OrgID:        input.OrganizationID,
		ProjectID:    projectID.String(),
		SkillName:    input.SkillName,
		SkillURN:     urn.NewSkill(input.SkillID).String(),
		SkillContent: input.SkillContent,
		Surface:      input.Surface,
		ActivatedAt:  input.ObservedAt.Time,
		Transcript:   transcript,
	})
	switch {
	case err != nil && errors.Is(err, ErrModelFailure):
		return p.recordAttempt(ctx, projectID, input, result)
	case err != nil:
		result.Retryable++
		return fmt.Errorf("judge skill efficacy: %w", err)
	}
	normalized, err := judged.Verdict.Normalize()
	if err != nil {
		return p.recordAttempt(ctx, projectID, input, result)
	}
	judged.Verdict = normalized

	// One row per insert: a CHECK the normalizer somehow let through terminates
	// only its own evaluation instead of dropping or retrying the whole batch.
	if err := p.scores.InsertSkillEfficacyScores(ctx, []telemetryrepo.SkillEfficacyScore{scoreRow(projectID, input, judged)}); err != nil {
		if errors.Is(err, telemetryrepo.ErrInvalidSkillEfficacyScore) {
			terminal, chargeErr := p.chargeAttempt(ctx, projectID, input, validationFailureClass, 1)
			if chargeErr != nil {
				result.Retryable++
				return chargeErr
			}
			if terminal {
				result.Failed++
			}
			return nil
		}

		// The inference is already paid for and the guard has nothing to find, so
		// every retry of this row buys the model call again. A sink that stays
		// broken would charge for it forever, which is why the attempt is charged
		// here as well: the failure is still infrastructure and still retried, but
		// the paid calls one evaluation can cost are bounded by MaxModelAttempts
		// exactly as a model failure's are.
		terminal, chargeErr := p.chargeAttempt(ctx, projectID, input, sinkFailureClass, MaxModelAttempts)
		switch {
		case chargeErr != nil:
			result.Retryable++
			return errors.Join(fmt.Errorf("insert skill efficacy score: %w: %w", ErrRetryable, err), chargeErr)
		case terminal:
			result.Failed++
			return nil
		default:
			result.Retryable++
		}

		return fmt.Errorf("insert skill efficacy score: %w: %w", ErrRetryable, err)
	}

	if err := p.markScored(ctx, projectID, input.ID); err != nil {
		result.Retryable++
		return err
	}

	result.Scored++
	return nil
}

// loadTranscript renders one chat's transcript, reusing a rendering this pass
// already made. Only a success is cached: a failed read is retryable, and
// caching it would hand every later evaluation of the same chat the same
// failure.
//
// The chat is read backwards in fixed pages rather than whole: a session has no
// bound on its length, but the rendering the judge receives does, so loading all
// of it would size the worker's memory to the longest chat any project ever
// wrote in order to discard most of it. The walk stops as soon as the rendering
// drops a message, which is sound because RenderTranscript trims oldest-first
// and every unread message is older than every message already dropped — so the
// messages it keeps are exactly the ones a full load would have kept, and only
// the omission count has to account for what was never read.
func (p *Publisher) loadTranscript(ctx context.Context, projectID uuid.UUID, chatID uuid.UUID, transcripts map[uuid.UUID]Transcript) (Transcript, error) {
	if cached, ok := transcripts[chatID]; ok {
		return cached, nil
	}

	page := chatrepo.ListChatTranscriptMessagesPageParams{
		ChatID:          chatID,
		ProjectID:       projectID,
		CursorCreatedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		CursorSeq:       pgtype.Int8{Int64: 0, Valid: false},
		CursorID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Lim:             transcriptPageSize,
	}

	unread, err := p.chats.CountChatMessages(ctx, chatrepo.CountChatMessagesParams{ChatID: chatID, ProjectID: projectID})
	if err != nil {
		return Transcript{}, fmt.Errorf("count skill efficacy transcript: %w: %w", ErrRetryable, err)
	}

	loaded := make([]TranscriptInput, 0, min(unread, int64(transcriptPageSize)))
	// Rendered up front so a chat with no messages leaves the same empty
	// rendering a whole-chat read produced rather than a zero Transcript.
	transcript := RenderTranscript(nil)
	for {
		rows, err := p.chats.ListChatTranscriptMessagesPage(ctx, page)
		if err != nil {
			return Transcript{}, fmt.Errorf("load skill efficacy transcript: %w: %w", ErrRetryable, err)
		}
		if len(rows) == 0 {
			// The cursor reached the start of the chat, so nothing is left
			// unread — including the first read of a chat with no messages.
			unread = 0
			break
		}

		unread -= int64(len(rows))
		if unread < 0 {
			unread = 0
		}
		for _, row := range rows {
			loaded = append(loaded, transcriptPageMessage(row))
		}

		oldest := rows[len(rows)-1]
		page.CursorCreatedAt = oldest.CreatedAt
		page.CursorSeq = pgtype.Int8{Int64: oldest.Seq, Valid: true}
		page.CursorID = uuid.NullUUID{UUID: oldest.ID, Valid: true}

		transcript = RenderTranscript(loaded)
		if unread == 0 || len(transcript.Messages) < len(loaded) {
			break
		}
	}

	if omitted := int64(len(loaded)-len(transcript.Messages)) + unread; omitted > 0 {
		transcript.Omitted = fmt.Sprintf(omittedMarker, omitted)
	}
	for i := range transcript.Messages {
		transcript.Messages[i].Index += int(unread)
	}

	transcripts[chatID] = transcript

	return transcript, nil
}

// transcriptPageMessage projects one page row onto the shape RenderTranscript
// reads. The page selects every column the rendering touches and nothing else,
// so the projection is field-for-field.
func transcriptPageMessage(row chatrepo.ListChatTranscriptMessagesPageRow) TranscriptInput {
	return TranscriptInput{
		ID:               row.ID,
		Seq:              row.Seq,
		CreatedAt:        row.CreatedAt,
		Role:             row.Role,
		Content:          row.Content,
		ToolCalls:        row.ToolCalls,
		ToolCallID:       row.ToolCallID,
		ToolURN:          row.ToolUrn,
		ToolOutcome:      row.ToolOutcome,
		ToolOutcomeNotes: row.ToolOutcomeNotes,
	}
}

// recordAttempt charges a model failure to the evaluation. The query never
// returns the row to pending, so its budget slot stays spent and no second
// reservation can re-spend the unit.
func (p *Publisher) recordAttempt(ctx context.Context, projectID uuid.UUID, input repo.GetSkillEfficacyJudgeInputsRow, result *PublishResult) error {
	terminal, err := p.chargeAttempt(ctx, projectID, input, modelFailureClass, MaxModelAttempts)
	if err != nil {
		result.Retryable++
		return err
	}

	if terminal {
		result.Failed++
	} else {
		result.ModelFailures++
	}

	return nil
}

// chargeAttempt charges one attempt to the evaluation and reports whether that
// terminated it. The row is never returned to pending, so its budget slot stays
// spent and no second reservation can re-spend the unit.
//
// A zero-row update is benign, not an error: the row left reserved under this
// pass — a stale-reservation reset, or a concurrent terminal failure — and there
// is nothing left here to charge. Failing the call would fail the whole batch
// over one row another owner has already accounted for.
func (p *Publisher) chargeAttempt(ctx context.Context, projectID uuid.UUID, input repo.GetSkillEfficacyJudgeInputsRow, class string, maxAttempts int32) (bool, error) {
	// The class, never the cause.
	row, err := repo.New(p.db).RecordSkillEfficacyEvaluationAttempt(ctx, repo.RecordSkillEfficacyEvaluationAttemptParams{
		LastError:   conv.ToPGText(class),
		MaxAttempts: maxAttempts,
		ProjectID:   projectID,
		ID:          input.ID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		p.logger.WarnContext(ctx, "skill efficacy evaluation was no longer reserved when charging an attempt",
			attr.SlogErrorKind(class),
			attr.SlogProjectID(projectID.String()),
			attr.SlogResourceID(input.ID.String()),
		)
		return false, nil
	case err != nil:
		return false, fmt.Errorf("record skill efficacy attempt: %w: %w", ErrRetryable, err)
	}

	p.logger.WarnContext(ctx, "skill efficacy evaluation attempt failed",
		attr.SlogErrorKind(class),
		attr.SlogProjectID(projectID.String()),
		attr.SlogResourceID(input.ID.String()),
		attr.SlogResourceURN(urn.NewSkill(input.SkillID).String()),
		attr.SlogSessionID(input.SessionID),
		attr.SlogChatID(input.ChatID.String()),
		attr.SlogRetryAttempt(int(row.Attempts)),
	)

	return row.State == StateFailed, nil
}

func (p *Publisher) markScored(ctx context.Context, projectID uuid.UUID, id uuid.UUID) error {
	marked, err := repo.New(p.db).MarkSkillEfficacyEvaluationScored(ctx, repo.MarkSkillEfficacyEvaluationScoredParams{
		ProjectID: projectID,
		ID:        id,
	})
	if err != nil {
		return fmt.Errorf("mark skill efficacy evaluation scored: %w: %w", ErrRetryable, err)
	}
	if marked == 0 {
		// The row left reserved under us — a stale-reservation reset or a
		// concurrent terminal failure. The score stands and the guard keeps a
		// later pass from judging it again.
		p.logger.WarnContext(ctx, "skill efficacy evaluation was no longer reserved when marking scored",
			attr.SlogProjectID(projectID.String()),
			attr.SlogResourceID(id.String()),
		)
	}

	return nil
}

// scoreRow builds the sink row. The score id is the evaluation id, which makes
// publication idempotent across retries, and created_at is the insert-time
// clock, which the guard window always contains because its upper bound tracks
// the clock too.
func scoreRow(projectID uuid.UUID, input repo.GetSkillEfficacyJudgeInputsRow, judged JudgeResult) telemetryrepo.SkillEfficacyScore {
	return telemetryrepo.SkillEfficacyScore{
		ID:                 input.ID,
		CreatedAt:          time.Now().UTC(),
		OrganizationID:     input.OrganizationID,
		ProjectID:          projectID.String(),
		SessionID:          input.SessionID,
		SkillID:            input.SkillID,
		SkillVersionID:     input.SkillVersionID,
		CanonicalSHA256:    input.CanonicalSha256,
		Surface:            input.Surface,
		TraceID:            nil,
		GramChatID:         input.ChatID.String(),
		Score:              judged.Verdict.Score,
		Rationale:          judged.Verdict.Rationale,
		EstTurnsSaved:      judged.Verdict.EstTurnsSaved,
		EstMinutesSaved:    judged.Verdict.EstMinutesSaved,
		ROIConfidence:      judged.Verdict.ROIConfidence,
		Flags:              judged.Verdict.Flags,
		JudgeModel:         judged.Model,
		JudgePromptVersion: judged.PromptVersion,
	}
}
