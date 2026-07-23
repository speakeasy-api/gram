package analysis

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// WorkUnitsJudgeName keys the work-units judge in queue rows, settings rows
	// and score rows. The canonical constant lives in the telemetry repo so
	// score readers that cannot import this package share the same key.
	WorkUnitsJudgeName = telemetryrepo.ChatAnalysisJudgeWorkUnits

	// workUnitsModel is the same fast, cheap structured-output model the risk
	// and efficacy judges settled on. Left unconfigurable: scores are only
	// comparable across sessions when one model produced them, and the model
	// that did is recorded on every row.
	workUnitsModel = "google/gemini-3.1-flash-lite"
	// workUnitsPromptVersion is stored on every score row so a prompt change is
	// visible as a break in the series rather than as a silent shift.
	workUnitsPromptVersion = "v2"
	// workUnitsTimeout matches the efficacy judge's bound: this judge also reads
	// a whole session transcript.
	workUnitsTimeout = 60 * time.Second

	// maxWorkUnitsTextRunes caps the free-text fields the verdict stores.
	maxWorkUnitsTextRunes = 200
	// minTaskUnits and maxTaskUnits are the per-task clamp the prompt defines:
	// harm may take a task to -30, and no task exceeds 100.
	minTaskUnits = -30
	maxTaskUnits = 100
)

// workUnitsSystemPrompt is the judge's system message, verbatim from the
// work-units prompt spec. It defines the digest the judge builds from the raw
// transcript, the scoring bands, and the JSON contract WorkUnitsVerdict decodes.
//
//go:embed workunits_prompt.md
var workUnitsSystemPrompt string

// workUnitsFlags is the closed set of flags the prompt allows.
var workUnitsFlags = []string{"harm", "unverified_claims", "digest_insufficient"}

// workUnitsBands is the closed set of scoring bands the prompt defines.
var workUnitsBands = []string{"A", "B", "C", "D", "E", "F"}

// workUnitsBandRanges is each band's base-unit range from the prompt's band
// table. Harm entries carry a negated band value, so magnitude is what the
// range bounds.
var workUnitsBandRanges = map[string][2]float64{
	"A": {1, 2},
	"B": {3, 7},
	"C": {8, 15},
	"D": {16, 30},
	"E": {31, 60},
	"F": {61, 100},
}

// workUnitsModifiers and workUnitsCompletions are the snap sets Steps 3 and 4
// of the prompt allow. The literals 0.3 and 0.7 are the same float64 values
// JSON decoding produces for those tokens, so exact comparison is sound.
var (
	workUnitsModifiers   = []float64{0.5, 0.75, 1.0, 1.25, 1.5}
	workUnitsCompletions = []float64{0.0, 0.3, 0.5, 0.7, 1.0}
)

// WorkUnitsTask is one user-requested task the judge identified and scored.
type WorkUnitsTask struct {
	ID              int     `json:"id"`
	Request         string  `json:"request"`
	Band            string  `json:"band"`
	BaseUnits       float64 `json:"base_units"`
	Modifier        float64 `json:"modifier"`
	Completion      float64 `json:"completion"`
	Units           float64 `json:"units"`
	NearestExemplar string  `json:"nearest_exemplar"`
	Rationale       string  `json:"rationale"`
}

// WorkUnitsVerdict is the judge's structured answer: per-task work units plus
// the session total. SessionUnits is the verdict's headline score.
type WorkUnitsVerdict struct {
	Tasks        []WorkUnitsTask `json:"tasks"`
	SessionUnits float64         `json:"session_units"`
	Flags        []string        `json:"flags"`
}

// WorkUnitsJudge estimates how many meaningful work units a session delivered.
type WorkUnitsJudge struct {
	logger  *slog.Logger
	tracer  trace.Tracer
	client  openrouter.CompletionClient
	limiter *ratelimit.Limiter
}

var _ Judge = (*WorkUnitsJudge)(nil)

// NewWorkUnitsJudge constructs the judge. Pass the limiter from
// openrouter.NewJudgeRateLimiter so its calls draw from the same bucket as
// every other judge spending the org's key on the same model.
func NewWorkUnitsJudge(logger *slog.Logger, tracerProvider trace.TracerProvider, client openrouter.CompletionClient, limiter *ratelimit.Limiter) *WorkUnitsJudge {
	return &WorkUnitsJudge{
		logger:  logger.With(attr.SlogComponent("work-units-judge")),
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/chat/analysis"),
		client:  client,
		limiter: limiter,
	}
}

func (j *WorkUnitsJudge) Name() string {
	return WorkUnitsJudgeName
}

// workUnitsPromptPayload is the judge's user turn. Structured JSON rather than
// prose means hostile transcript text is always a quoted string in a known
// field and can never forge a digest section.
type workUnitsPromptPayload struct {
	Transcript efficacy.Transcript `json:"transcript"`
}

// Judge scores one session. Errors wrap ErrModelFailure or ErrRetryable so the
// publisher can tell an answer it should charge the model for from one it
// should simply retry.
func (j *WorkUnitsJudge) Judge(ctx context.Context, in JudgeInput) (JudgeResult, error) {
	ctx, span := j.tracer.Start(ctx, "chat.analysis.work_units.judge", trace.WithAttributes(
		attr.OrganizationID(in.OrgID),
		attr.ProjectID(in.ProjectID),
	))
	defer span.End()

	prompt, err := json.Marshal(workUnitsPromptPayload{Transcript: in.Transcript})
	if err != nil {
		err = fmt.Errorf("marshal work units judge payload: %w", err)
		span.SetStatus(codes.Error, err.Error())
		return JudgeResult{}, err
	}

	raw, model, err := CallStructured(ctx, j.logger, j.client, j.limiter, in, StructuredCall{
		Model:        workUnitsModel,
		SystemPrompt: workUnitsSystemPrompt,
		Prompt:       string(prompt),
		SchemaName:   "work_units_verdict",
		Schema:       workUnitsSchema(),
		Timeout:      workUnitsTimeout,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		j.logger.WarnContext(ctx, "work units judge call failed",
			attr.SlogError(err),
			attr.SlogOrganizationID(in.OrgID),
			attr.SlogProjectID(in.ProjectID),
		)
		return JudgeResult{}, err
	}

	verdict, err := ParseWorkUnitsVerdict(raw)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return JudgeResult{}, err
	}

	detail, err := json.Marshal(verdict)
	if err != nil {
		err = fmt.Errorf("marshal work units verdict: %w", err)
		span.SetStatus(codes.Error, err.Error())
		return JudgeResult{}, err
	}

	span.SetAttributes(attribute.Float64("chat.analysis.work_units.session_units", verdict.SessionUnits))

	return JudgeResult{
		Verdict:       Verdict{Score: verdict.SessionUnits, Detail: detail},
		Model:         model,
		PromptVersion: workUnitsPromptVersion,
	}, nil
}

// ParseWorkUnitsVerdict decodes the judge's raw structured output and
// normalizes it. Unparseable output is a model failure: the model returned
// something outside the contract it was given, and a retry can produce a
// different answer.
func ParseWorkUnitsVerdict(raw string) (WorkUnitsVerdict, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()

	var v WorkUnitsVerdict
	if err := decoder.Decode(&v); err != nil {
		return WorkUnitsVerdict{}, fmt.Errorf("parse work units verdict: %w: %w", ErrModelFailure, err)
	}
	// One JSON value and nothing after it: a valid prefix followed by prose or a
	// second value is outside the contract, not a verdict with an appendix.
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return WorkUnitsVerdict{}, fmt.Errorf("parse work units verdict: trailing content after JSON: %w", ErrModelFailure)
	}

	return v.Normalize()
}

// Normalize forces the verdict inside the prompt's own contract: per-task
// units recomputed from the factors the judge reported — round(base_units ×
// modifier × completion), clamped to [-30, 100] — so the stored score can
// never disagree with the arithmetic behind it, the session total recomputed
// as the tasks' sum, flags restricted to the allowed set, and free text
// capped. The structured-output schema already requires every field, so the
// shape checks here are defense in depth against a model that returned empty
// or null anyway. A non-finite number, a modifier or completion outside its
// snap set, or a base outside the task's band is unfixable — repairing any of
// them would invent a score the judge never gave — so each is reported as a
// model failure.
func (v WorkUnitsVerdict) Normalize() (WorkUnitsVerdict, error) {
	if v.Tasks == nil {
		return WorkUnitsVerdict{}, fmt.Errorf("work units verdict is missing its tasks: %w", ErrModelFailure)
	}

	tasks := make([]WorkUnitsTask, 0, len(v.Tasks))
	total := 0.0
	for _, task := range v.Tasks {
		if !slices.Contains(workUnitsBands, task.Band) {
			return WorkUnitsVerdict{}, fmt.Errorf("work units verdict band %q is not a scoring band: %w", task.Band, ErrModelFailure)
		}
		for _, value := range []float64{task.BaseUnits, task.Modifier, task.Completion, task.Units} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return WorkUnitsVerdict{}, fmt.Errorf("work units verdict number is not finite: %w", ErrModelFailure)
			}
		}
		// Recomputing units from the factors only helps if the factors themselves
		// sit inside the contract: an out-of-set modifier or completion, or a base
		// outside the task's band, would let a wayward answer inflate or invert
		// the score through the arithmetic.
		if !slices.Contains(workUnitsModifiers, task.Modifier) {
			return WorkUnitsVerdict{}, fmt.Errorf("work units verdict modifier %v is not an allowed value: %w", task.Modifier, ErrModelFailure)
		}
		if !slices.Contains(workUnitsCompletions, task.Completion) {
			return WorkUnitsVerdict{}, fmt.Errorf("work units verdict completion %v is not an allowed value: %w", task.Completion, ErrModelFailure)
		}
		if bounds := workUnitsBandRanges[task.Band]; math.Abs(task.BaseUnits) < bounds[0] || math.Abs(task.BaseUnits) > bounds[1] {
			return WorkUnitsVerdict{}, fmt.Errorf("work units verdict base units %v are outside band %s: %w", task.BaseUnits, task.Band, ErrModelFailure)
		}

		task.Units = math.Round(max(minTaskUnits, min(maxTaskUnits, task.BaseUnits*task.Modifier*task.Completion)))
		task.Request = truncateText(task.Request)
		task.Rationale = truncateText(task.Rationale)
		task.NearestExemplar = truncateText(task.NearestExemplar)
		tasks = append(tasks, task)
		total += task.Units
	}

	var flags []string
	for _, flag := range v.Flags {
		if slices.Contains(workUnitsFlags, flag) && !slices.Contains(flags, flag) {
			flags = append(flags, flag)
		}
	}

	return WorkUnitsVerdict{
		Tasks: tasks,
		// Recomputed, never trusted: the stored headline must equal the sum of
		// the clamped per-task units.
		SessionUnits: total,
		Flags:        flags,
	}, nil
}

func truncateText(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= maxWorkUnitsTextRunes {
		return s
	}
	return string([]rune(s)[:maxWorkUnitsTextRunes])
}

// workUnitsSchema is the judge's structured-output JSON schema. Deliberately no
// minimum/maximum on the numbers and no maxLength on the strings: Anthropic
// routes (via Amazon Bedrock) reject those keywords with a 400. Those bounds
// are enforced by Normalize instead.
func workUnitsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tasks": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":               map[string]any{"type": "number"},
						"request":          map[string]any{"type": "string"},
						"band":             map[string]any{"type": "string", "enum": workUnitsBands},
						"base_units":       map[string]any{"type": "number"},
						"modifier":         map[string]any{"type": "number"},
						"completion":       map[string]any{"type": "number"},
						"units":            map[string]any{"type": "number"},
						"nearest_exemplar": map[string]any{"type": "string"},
						"rationale":        map[string]any{"type": "string"},
					},
					"required":             []string{"id", "request", "band", "base_units", "modifier", "completion", "units", "nearest_exemplar", "rationale"},
					"additionalProperties": false,
				},
			},
			"session_units": map[string]any{"type": "number"},
			"flags": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string", "enum": workUnitsFlags},
			},
		},
		// Strict structured output requires every declared property to be
		// required.
		"required":             []string{"tasks", "session_units", "flags"},
		"additionalProperties": false,
	}
}
