package analysis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	// guardWindowSlack is how far past the current pass the existence guard
	// looks. Two days absorb a judge run that started before midnight UTC and
	// inserted after it.
	guardWindowSlack = 48 * time.Hour
	// publishEvaluationTimeout is the hard bound on one evaluation's whole
	// publication: transcript read, judge call, score insert and scored mark. It
	// is the 120s per evaluation ReservedClaimLease is sized against.
	publishEvaluationTimeout = 2 * time.Minute
)

// modelFailureClass is the whole of what a model failure records, in last_error
// and in the log line alike. A provider's error body can quote the request
// back, and this request carries the session transcript, so neither the column
// nor the log ever sees the cause.
var modelFailureClass = ErrModelFailure.Error()

// sinkFailureClass is what a post-inference score-sink failure records. It is
// distinct from modelFailureClass because the judge answered and was paid for —
// the attempt is charged to bound that payment, not to blame the model.
const sinkFailureClass = "chat analysis score sink failure"

const validationFailureClass = "chat analysis row validation failure"

// ScoreSink is the ClickHouse side of publication: the existence guard, the
// synchronous insert, and the session-facts read that decorates score events.
// Satisfied by *telemetryrepo.Queries.
type ScoreSink interface {
	ListExistingChatAnalysisScoreIDs(ctx context.Context, arg telemetryrepo.ListExistingChatAnalysisScoreIDsParams) ([]string, error)
	InsertChatAnalysisScores(ctx context.Context, rows []telemetryrepo.ChatAnalysisScore) error
	GetChatSessionFactsByChatIDs(ctx context.Context, arg telemetryrepo.GetChatSessionFactsByChatIDsParams) (map[string]telemetryrepo.ChatSessionFacts, error)
}

// ScoreEventSink emits the synthetic per-session telemetry events derived from
// published work-units verdicts — the rows attribute_metrics_summaries_mv
// folds into the work-units efficiency measures. Satisfied by
// *telemetry.Logger; nil disables emission.
type ScoreEventSink interface {
	LogBulk(ctx context.Context, params []telemetry.LogParams) error
}

// WorkUnitsScoreEventURN is the gram_urn stamped on work-units score events.
// attribute_metrics_summaries_mv admits rows by this exact value; keep the two
// in sync.
const WorkUnitsScoreEventURN = "chat_analysis:work_units:score"

// scoreEventFactsWindow is how far back the session-facts read scans summary
// buckets, relative to the pass. Evaluations are enqueued shortly after a
// session's last activity, so two weeks comfortably covers reservation lag and
// retries; a session older than that simply publishes without an event.
const scoreEventFactsWindow = 14 * 24 * time.Hour

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
	// another pass.
	Retryable int
}

// Publisher judges reserved evaluations and publishes their verdicts.
type Publisher struct {
	logger *slog.Logger
	tracer trace.Tracer
	db     *pgxpool.Pool
	chats  efficacy.TranscriptSource
	scores ScoreSink
	events ScoreEventSink
	judges *Judges
	// evaluationTimeout is publishEvaluationTimeout, held on the struct so a test
	// can shorten the bound it is asserting on.
	evaluationTimeout time.Duration
}

// NewPublisher constructs a Publisher over the given judge roster. events may
// be nil, which disables work-units score event emission (scores still
// publish).
func NewPublisher(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, scores ScoreSink, events ScoreEventSink, judges *Judges) *Publisher {
	return &Publisher{
		logger:            logger.With(attr.SlogComponent("chat-analysis-publisher")),
		tracer:            tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/chat/analysis"),
		db:                db,
		chats:             chatrepo.New(db),
		scores:            scores,
		events:            events,
		judges:            judges,
		evaluationTimeout: publishEvaluationTimeout,
	}
}

// Publish judges the given reserved evaluations and writes their verdicts.
//
// Publication order per evaluation is existence guard → judge → synchronous
// insert → mark scored, and the guard runs for the WHOLE batch before any judge
// call: a retry that follows a crash between insert and mark must not pay for
// inference a second time. The score id is the evaluation id, so every physical
// retry has the same logical event identity and analytical reads collapse it.
//
// A model failure charges the evaluation an attempt and the batch continues;
// the third one terminates that evaluation as failed and never writes a score.
// A deterministic row-validation failure — including a judge name the roster no
// longer runs — terminates immediately. An infrastructure failure changes no
// state and charges no attempt, with the one exception of a sink failure after
// the judge has answered, which is charged as well so a broken sink cannot buy
// the same inference forever.
//
// heartbeat, when given, is called once before each evaluation. It is what lets
// the caller's own lease on the batch stay live across a long pass and, on the
// Temporal path, what delivers a cancellation.
func (p *Publisher) Publish(ctx context.Context, projectID uuid.UUID, ids []uuid.UUID, heartbeat func()) (PublishResult, error) {
	ctx, span := p.tracer.Start(ctx, "chat.analysis.publish", trace.WithAttributes(
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
	inputs, err := queries.GetChatAnalysisJudgeInputs(ctx, repo.GetChatAnalysisJudgeInputsParams{
		ProjectID: projectID,
		Ids:       ids,
	})
	if err != nil {
		err = fmt.Errorf("load chat analysis judge inputs: %w: %w", ErrRetryable, err)
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

	// A batch can hold several evaluations of the same chat — one per judge — and
	// they all judge the same messages, so the read and the render are paid once
	// per chat for the length of this pass.
	transcripts := make(map[uuid.UUID]efficacy.Transcript, len(inputs))

	// Session facts decorate the work-units score events with the session's
	// identity, model, source, and usage totals. Fetched once for the whole
	// batch, best-effort: without facts an evaluation publishes normally and
	// only its score event is skipped.
	sessionFacts := p.loadScoreEventFacts(ctx, projectID, inputs, published)

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
			err = fmt.Errorf("chat analysis publication cancelled: %w: %w", ErrRetryable, err)
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

		if err := p.publishEvaluation(ctx, projectID, input, transcripts, sessionFacts, &result); err != nil {
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

// alreadyPublished asks the sink which of the batch's evaluations already have
// a score row. The batch is one project, so it is one organization, and the
// guard read pins organization_id, project_id and the batch's exact judges —
// the sink's leading ORDER BY columns.
func (p *Publisher) alreadyPublished(ctx context.Context, projectID uuid.UUID, inputs []repo.GetChatAnalysisJudgeInputsRow) (map[uuid.UUID]struct{}, error) {
	// The guard is one sink read, but it runs before any per-evaluation bound
	// exists: unbounded, a hung sink would burn the claim lease before the first
	// judge call. One evaluation's timeout is generous for one query.
	ctx, cancel := context.WithTimeout(ctx, p.evaluationTimeout)
	defer cancel()

	minCreatedAt, maxCreatedAt := guardWindow(inputs, time.Now().UTC())

	organizationID := inputs[0].OrganizationID
	ids := make([]string, 0, len(inputs))
	index := make(map[string]uuid.UUID, len(inputs))
	judgeNames := make([]string, 0, len(inputs))
	seenJudges := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if input.OrganizationID != organizationID {
			// One project belongs to one organization, so a mixed batch means the
			// rows disagree with the projects table. Retrying reads the same rows.
			return nil, fmt.Errorf("chat analysis guard window: project %s resolved evaluations across organizations", projectID)
		}
		id := input.ID.String()
		ids = append(ids, id)
		index[id] = input.ID
		if _, ok := seenJudges[input.Judge]; !ok {
			seenJudges[input.Judge] = struct{}{}
			judgeNames = append(judgeNames, input.Judge)
		}
	}

	existing, err := p.scores.ListExistingChatAnalysisScoreIDs(ctx, telemetryrepo.ListExistingChatAnalysisScoreIDsParams{
		OrganizationID: organizationID,
		ProjectID:      projectID.String(),
		Judges:         judgeNames,
		IDs:            ids,
		MinCreatedAt:   minCreatedAt,
		MaxCreatedAt:   maxCreatedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("list existing chat analysis scores: %w: %w", ErrRetryable, err)
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
// created_at to two days past now. The lower bound is the evaluation's birth
// stamp, which no transition rewrites, so it is identical on every pass and can
// never sit after a score an earlier pass inserted for that evaluation.
func guardWindow(inputs []repo.GetChatAnalysisJudgeInputsRow, now time.Time) (time.Time, time.Time) {
	var minCreated time.Time
	for _, input := range inputs {
		if created := input.EvaluationCreatedAt.Time.UTC().Truncate(24 * time.Hour); minCreated.IsZero() || created.Before(minCreated) {
			minCreated = created
		}
	}

	return minCreated, now.UTC().Add(guardWindowSlack)
}

// publishEvaluation runs one evaluation's publication under evaluationTimeout.
// Every step it covers can hang — a chat read, a judge call, a ClickHouse
// insert — and without a bound one hung step holds the batch past the lease
// that owns its rows, letting a second pass judge them concurrently.
func (p *Publisher) publishEvaluation(ctx context.Context, projectID uuid.UUID, input repo.GetChatAnalysisJudgeInputsRow, transcripts map[uuid.UUID]efficacy.Transcript, sessionFacts map[string]telemetryrepo.ChatSessionFacts, result *PublishResult) error {
	evaluationCtx, cancel := context.WithTimeout(ctx, p.evaluationTimeout)
	defer cancel()

	err := p.publishOne(evaluationCtx, projectID, input, transcripts, sessionFacts, result)
	if err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		// The bound expired while the pass's own context was still live, so the
		// hang is this evaluation's.
		return fmt.Errorf("chat analysis evaluation timed out: %w: %w", ErrRetryable, err)
	}

	return err
}

// publishOne judges one evaluation and publishes its verdict. It returns an
// error only for infrastructure failures; model failures and deterministic row
// validation failures are charged locally so the rest of the batch still runs.
func (p *Publisher) publishOne(ctx context.Context, projectID uuid.UUID, input repo.GetChatAnalysisJudgeInputsRow, transcripts map[uuid.UUID]efficacy.Transcript, sessionFacts map[string]telemetryrepo.ChatSessionFacts, result *PublishResult) error {
	judge, ok := p.judges.Get(input.Judge)
	if !ok {
		// The roster no longer runs this judge; a retry reads the same row and
		// reaches the same conclusion, so the unit terminates rather than loops.
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

	transcript, ok := transcripts[input.ChatID]
	if !ok {
		loaded, err := efficacy.LoadTranscript(ctx, p.chats, projectID, input.ChatID)
		if err != nil {
			result.Retryable++
			return fmt.Errorf("load chat analysis transcript: %w: %w", ErrRetryable, err)
		}
		transcript = loaded
		// Only a success is cached: a failed read is retryable, and caching it
		// would hand every later evaluation of the same chat the same failure.
		transcripts[input.ChatID] = transcript
	}

	judged, err := judge.Judge(ctx, JudgeInput{
		OrgID:      input.OrganizationID,
		ProjectID:  projectID.String(),
		ChatID:     input.ChatID,
		Transcript: transcript,
	})
	switch {
	case err != nil && errors.Is(err, ErrModelFailure):
		return p.recordAttempt(ctx, projectID, input, result)
	case err != nil:
		result.Retryable++
		return fmt.Errorf("judge chat analysis evaluation: %w", err)
	}

	// One row per insert: a CHECK the judge's normalization somehow let through
	// terminates only its own evaluation instead of dropping the whole batch.
	if err := p.scores.InsertChatAnalysisScores(ctx, []telemetryrepo.ChatAnalysisScore{scoreRow(projectID, input, judged)}); err != nil {
		if errors.Is(err, telemetryrepo.ErrInvalidChatAnalysisScore) {
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
		// every retry of this row buys the model call again. The failure is still
		// infrastructure and still retried, but the paid calls one evaluation can
		// cost are bounded by MaxModelAttempts exactly as a model failure's are.
		terminal, chargeErr := p.chargeAttempt(ctx, projectID, input, sinkFailureClass, MaxModelAttempts)
		switch {
		case chargeErr != nil:
			result.Retryable++
			return errors.Join(fmt.Errorf("insert chat analysis score: %w: %w", ErrRetryable, err), chargeErr)
		case terminal:
			result.Failed++
			return nil
		default:
			result.Retryable++
		}

		return fmt.Errorf("insert chat analysis score: %w: %w", ErrRetryable, err)
	}

	p.emitWorkUnitsScoreEvent(ctx, projectID, input, judged, sessionFacts)

	if err := p.markScored(ctx, projectID, input.ID); err != nil {
		result.Retryable++
		return err
	}

	result.Scored++
	return nil
}

// loadScoreEventFacts fetches session facts for the batch's not-yet-published
// work-units evaluations. Best-effort: on failure every affected evaluation
// publishes without a score event rather than failing the pass.
func (p *Publisher) loadScoreEventFacts(ctx context.Context, projectID uuid.UUID, inputs []repo.GetChatAnalysisJudgeInputsRow, published map[uuid.UUID]struct{}) map[string]telemetryrepo.ChatSessionFacts {
	if p.events == nil {
		return nil
	}

	var chatIDs []string
	for _, input := range inputs {
		if input.Judge != WorkUnitsJudgeName {
			continue
		}
		if _, ok := published[input.ID]; ok {
			continue
		}
		chatIDs = append(chatIDs, input.ChatID.String())
	}
	if len(chatIDs) == 0 {
		return nil
	}

	now := time.Now().UTC()
	facts, err := p.scores.GetChatSessionFactsByChatIDs(ctx, telemetryrepo.GetChatSessionFactsByChatIDsParams{
		ProjectID: projectID.String(),
		ChatIDs:   chatIDs,
		From:      now.Add(-scoreEventFactsWindow),
		To:        now,
	})
	if err != nil {
		p.logger.WarnContext(ctx, "failed to load session facts for work units score events", attr.SlogError(err))
		return nil
	}
	return facts
}

// emitWorkUnitsScoreEvent emits the synthetic telemetry row that feeds the
// work-units efficiency measures in attribute_metrics_summaries. Best-effort
// by design: the scores table is the source of truth, and a lost event only
// removes this session from the efficiency aggregates. Duplicate emission is
// bounded by the publication guard — a retried evaluation whose score row is
// already visible takes the alreadyPublished path and never reaches this.
func (p *Publisher) emitWorkUnitsScoreEvent(ctx context.Context, projectID uuid.UUID, input repo.GetChatAnalysisJudgeInputsRow, judged JudgeResult, sessionFacts map[string]telemetryrepo.ChatSessionFacts) {
	if p.events == nil || input.Judge != WorkUnitsJudgeName {
		return
	}

	facts, ok := sessionFacts[input.ChatID.String()]
	if !ok {
		// No telemetry summary for the session means no ingested usage: there is
		// no spend to relate the units to and no identity to attribute them to.
		p.logger.InfoContext(ctx, "no session facts; skipping work units score event", attr.SlogChatID(input.ChatID.String()))
		return
	}

	params := telemetry.LogParams{
		// Stamped with the session's end so the units land in the same buckets
		// as the session's spend, not the (later) scoring time.
		Timestamp: time.Unix(0, facts.EndTimeUnixNano).UTC(),
		ToolInfo: telemetry.ToolInfo{
			ID:             "",
			URN:            WorkUnitsScoreEventURN,
			Name:           "",
			ProjectID:      projectID.String(),
			DeploymentID:   "",
			FunctionID:     nil,
			OrganizationID: input.OrganizationID,
		},
		UserInfo: telemetry.UserInfoByEmail(facts.UserEmail),
		Attributes: map[attr.Key]any{
			attr.LogBodyKey:                  "chat_analysis.work_units_score",
			attr.GenAIConversationIDKey:      input.ChatID.String(),
			attr.GenAIResponseModelKey:       facts.Model,
			attr.HookSourceKey:               facts.HookSource,
			attr.AccountTypeKey:              facts.AccountType(),
			attr.ChatAnalysisWorkUnitsKey:    judged.Verdict.Score,
			attr.ChatAnalysisScoredCostKey:   facts.TotalCost,
			attr.ChatAnalysisScoredTokensKey: facts.TotalTokens,
		},
	}

	if err := p.events.LogBulk(ctx, []telemetry.LogParams{params}); err != nil {
		p.logger.WarnContext(ctx, "failed to emit work units score event", attr.SlogError(err), attr.SlogChatID(input.ChatID.String()))
	}
}

// recordAttempt charges a model failure to the evaluation. The query never
// returns the row to pending, so its budget slot stays spent and no second
// reservation can re-spend the unit.
func (p *Publisher) recordAttempt(ctx context.Context, projectID uuid.UUID, input repo.GetChatAnalysisJudgeInputsRow, result *PublishResult) error {
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
// terminated it.
//
// A zero-row update is benign, not an error: the row left reserved under this
// pass — a stale-reservation reset, or a concurrent terminal failure — and
// there is nothing left here to charge.
func (p *Publisher) chargeAttempt(ctx context.Context, projectID uuid.UUID, input repo.GetChatAnalysisJudgeInputsRow, class string, maxAttempts int32) (bool, error) {
	// The class, never the cause.
	row, err := repo.New(p.db).RecordChatAnalysisEvaluationAttempt(ctx, repo.RecordChatAnalysisEvaluationAttemptParams{
		LastError:   conv.ToPGText(class),
		MaxAttempts: maxAttempts,
		ProjectID:   projectID,
		ID:          input.ID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		p.logger.WarnContext(ctx, "chat analysis evaluation was no longer reserved when charging an attempt",
			attr.SlogErrorKind(class),
			attr.SlogProjectID(projectID.String()),
			attr.SlogResourceID(input.ID.String()),
		)
		return false, nil
	case err != nil:
		return false, fmt.Errorf("record chat analysis attempt: %w: %w", ErrRetryable, err)
	}

	p.logger.WarnContext(ctx, "chat analysis evaluation attempt failed",
		attr.SlogErrorKind(class),
		attr.SlogProjectID(projectID.String()),
		attr.SlogResourceID(input.ID.String()),
		attr.SlogChatID(input.ChatID.String()),
		attr.SlogRetryAttempt(int(row.Attempts)),
	)

	return row.State == StateFailed, nil
}

func (p *Publisher) markScored(ctx context.Context, projectID uuid.UUID, id uuid.UUID) error {
	marked, err := repo.New(p.db).MarkChatAnalysisEvaluationScored(ctx, repo.MarkChatAnalysisEvaluationScoredParams{
		ProjectID: projectID,
		ID:        id,
	})
	if err != nil {
		return fmt.Errorf("mark chat analysis evaluation scored: %w: %w", ErrRetryable, err)
	}
	if marked == 0 {
		// The row left reserved under us — a stale-reservation reset or a
		// concurrent terminal failure. The score stands and the guard keeps a
		// later pass from judging it again.
		p.logger.WarnContext(ctx, "chat analysis evaluation was no longer reserved when marking scored",
			attr.SlogProjectID(projectID.String()),
			attr.SlogResourceID(id.String()),
		)
	}

	return nil
}

// scoreRow builds the sink row. The score id is the evaluation id, which gives
// retries one logical event identity, and created_at is the insert-time clock,
// which the guard window always contains because its upper bound tracks the
// clock too.
func scoreRow(projectID uuid.UUID, input repo.GetChatAnalysisJudgeInputsRow, judged JudgeResult) telemetryrepo.ChatAnalysisScore {
	return telemetryrepo.ChatAnalysisScore{
		ID:                 input.ID,
		CreatedAt:          time.Now().UTC(),
		OrganizationID:     input.OrganizationID,
		ProjectID:          projectID.String(),
		ChatID:             input.ChatID.String(),
		Judge:              input.Judge,
		Score:              judged.Verdict.Score,
		Detail:             string(judged.Verdict.Detail),
		JudgeModel:         judged.Model,
		JudgePromptVersion: judged.PromptVersion,
	}
}
