package analysis

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
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
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// WorkUnitsJudgeName keys the work-units judge in queue rows, settings rows
	// and score rows.
	WorkUnitsJudgeName = "work_units"

	// workUnitsModel is the same fast, cheap structured-output model the risk
	// and efficacy judges settled on. Left unconfigurable: scores are only
	// comparable across sessions when one model produced them, and the model
	// that did is recorded on every row.
	workUnitsModel = "google/gemini-3.1-flash-lite"
	// workUnitsPromptVersion is stored on every score row so a prompt change is
	// visible as a break in the series rather than as a silent shift.
	workUnitsPromptVersion = "v1"
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

	return v.Normalize()
}

// Normalize forces the verdict inside the prompt's own contract: per-task units
// clamped to [-30, 100] and rounded, the session total recomputed as their sum
// so the headline score always agrees with the tasks it summarises, flags
// restricted to the allowed set, and free text capped. A non-finite number is
// the one unfixable case — clamping NaN would invent a score the judge never
// gave — so it is reported as a model failure.
func (v WorkUnitsVerdict) Normalize() (WorkUnitsVerdict, error) {
	tasks := make([]WorkUnitsTask, 0, len(v.Tasks))
	total := 0.0
	for _, task := range v.Tasks {
		for _, value := range []float64{task.BaseUnits, task.Modifier, task.Completion, task.Units} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return WorkUnitsVerdict{}, fmt.Errorf("work units verdict number is not finite: %w", ErrModelFailure)
			}
		}

		task.Units = math.Round(max(minTaskUnits, min(maxTaskUnits, task.Units)))
		task.Request = truncateText(task.Request)
		task.Rationale = truncateText(task.Rationale)
		task.Band = truncateText(task.Band)
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
						"band":             map[string]any{"type": "string", "enum": []string{"A", "B", "C", "D", "E", "F"}},
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
