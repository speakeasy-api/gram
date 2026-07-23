package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

var (
	// ErrModelFailure marks a failure the model owns: output that does not honour
	// the response contract, or a call that ran past its timeout. The publisher
	// charges these to the evaluation's attempt counter, and the third one is
	// terminal.
	ErrModelFailure = errors.New("chat analysis judge model failure")
	// ErrRetryable marks a failure the infrastructure owns: a throttled call or a
	// transport error. The publisher leaves the evaluation reserved and its
	// attempt counter untouched, so the same row is retried.
	ErrRetryable = errors.New("chat analysis judge failure is retryable")
)

// JudgeInput is one scoring unit: a finished session's rendered transcript and
// the identifiers a verdict row needs. The transcript rendering is shared with
// the skill efficacy judge, so every session judge sees the same
// prompt-injection-hardened shape.
type JudgeInput struct {
	OrgID      string
	ProjectID  string
	ChatID     uuid.UUID
	Transcript efficacy.Transcript
}

// Verdict is a judge's normalized answer. Score is the headline metric whose
// meaning the judge defines (work units delivered, resolution likelihood, …)
// and Detail is the full structured verdict as JSON in the judge's own shape.
// Both land verbatim in the chat_analysis_scores sink, keyed by the judge's
// name.
type Verdict struct {
	Score  float64
	Detail json.RawMessage
}

// JudgeResult carries the verdict plus the attribution the score row needs.
// Cost and token counts are deliberately absent: the completion client already
// bills and records them against the chat-analysis usage source.
type JudgeResult struct {
	Verdict       Verdict
	Model         string
	PromptVersion string
}

// Judge scores one finished chat session. Implementations must return errors
// wrapping ErrModelFailure or ErrRetryable so the publisher can tell an answer
// it should charge the model for from one it should simply retry; the
// CallStructured helper classifies completion-call failures that way already.
type Judge interface {
	// Name is the judge's stable identifier. It keys queue rows, settings rows
	// and score rows, so changing it orphans all three.
	Name() string
	Judge(ctx context.Context, in JudgeInput) (JudgeResult, error)
}

// judgeNamePattern keeps judge names safe to use as settings keys, sink
// dimensions and log fields.
var judgeNamePattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Judges is the immutable judge roster the pipeline runs. Order is the
// registration order and has no behavioural weight; identity is the name.
type Judges struct {
	ordered []Judge
	byName  map[string]Judge
}

// NewJudges validates and assembles the roster. Every judge needs a unique,
// non-empty, lowercase name because the name is a persistence key.
func NewJudges(judges ...Judge) (*Judges, error) {
	byName := make(map[string]Judge, len(judges))
	ordered := make([]Judge, 0, len(judges))
	for _, judge := range judges {
		name := judge.Name()
		if !judgeNamePattern.MatchString(name) {
			return nil, fmt.Errorf("chat analysis judge name %q must match %s", name, judgeNamePattern)
		}
		if _, ok := byName[name]; ok {
			return nil, fmt.Errorf("chat analysis judge name %q registered twice", name)
		}
		byName[name] = judge
		ordered = append(ordered, judge)
	}

	return &Judges{ordered: ordered, byName: byName}, nil
}

// Names lists the roster's judge names in registration order.
func (j *Judges) Names() []string {
	names := make([]string, 0, len(j.ordered))
	for _, judge := range j.ordered {
		names = append(names, judge.Name())
	}
	return names
}

// Get resolves a judge by name.
func (j *Judges) Get(name string) (Judge, bool) {
	judge, ok := j.byName[name]
	return judge, ok
}

// StructuredCall is one structured-output judge completion: the model, the
// framing, and the response contract. Judges describe their call with this and
// hand it to CallStructured, so every judge shares the same rate limiting,
// timeout handling and failure classification.
type StructuredCall struct {
	Model        string
	SystemPrompt string
	Prompt       string
	// SchemaName names the response schema for the provider.
	SchemaName string
	// Schema is the structured-output JSON schema. Do not use minimum/maximum or
	// maxLength keywords: Anthropic routes (via Amazon Bedrock) reject those with
	// a 400. Enforce bounds in the judge's normalization instead.
	Schema map[string]any
	// Timeout bounds the call; judges reading whole transcripts should allow the
	// same 60 seconds the efficacy judge does.
	Timeout time.Duration
}

// CallStructured performs one structured-output completion on the platform's
// internal key, drawing on the shared per-(org, model) judge rate limiter, and
// returns the raw response text plus the model that produced it. Errors wrap
// ErrModelFailure or ErrRetryable exactly as the publisher expects.
func CallStructured(ctx context.Context, logger *slog.Logger, client openrouter.CompletionClient, limiter *ratelimit.Limiter, in JudgeInput, call StructuredCall) (string, string, error) {
	// A Store outage is not a throttle: proceed rather than stall the pipeline on
	// limiter infrastructure. A real throttle is retryable — the unit keeps its
	// reservation and its attempt budget.
	switch res, err := limiter.Allow(ctx, openrouter.JudgeRateLimitKey(in.OrgID, call.Model)); {
	case err != nil:
		logger.WarnContext(ctx, "judge rate limiter unavailable, allowing call",
			attr.SlogError(err),
			attr.SlogOrganizationID(in.OrgID),
		)
	case !res.Allowed:
		logger.WarnContext(ctx, "chat analysis judge rate limited",
			attr.SlogOrganizationID(in.OrgID),
		)
		return "", "", fmt.Errorf("chat analysis judge call: %w", ErrRetryable)
	}

	strict := true
	jsonSchema := or.ChatJSONSchemaConfig{
		Name:        call.SchemaName,
		Schema:      call.Schema,
		Description: nil,
		Strict:      optionalnullable.From(&strict),
	}
	// Zero temperature keeps verdicts stable for a given transcript.
	temperature := 0.0

	callCtx, cancel := context.WithTimeout(ctx, call.Timeout)
	defer cancel()

	response, err := client.GetObjectCompletion(callCtx, openrouter.ObjectCompletionRequest{
		OrgID:        in.OrgID,
		ProjectID:    in.ProjectID,
		Model:        call.Model,
		SystemPrompt: call.SystemPrompt,
		Prompt:       call.Prompt,
		Temperature:  &temperature,
		UsageSource:  billing.ModelUsageSourceChatAnalysis,
		// Platform-initiated inference: bill the org's internal key, never the
		// customer-facing chat key's monthly cap.
		KeyType:        openrouter.KeyTypeInternal,
		KeySlot:        billing.ModelUsageSourceChatAnalysis,
		UserID:         "",
		ExternalUserID: "",
		UserEmail:      "",
		HTTPMetadata:   nil,
		JSONSchema:     &jsonSchema,
	})
	switch {
	case err != nil && errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
		// The call timeout expired while the parent context was still live: the
		// model took too long to answer, which a retry on a different route can
		// fix.
		return "", "", fmt.Errorf("chat analysis judge call timed out: %w", ErrModelFailure)
	case err != nil && (openrouter.IsHistoryCorruptionCandidate(err) || openrouter.IsBadRequest(err) || openrouter.IsContentPolicy(err)):
		// The provider rejected the request body itself: the rendered transcript
		// is malformed or past the model's context window, refused on content
		// grounds, or some other part of the payload is deterministically
		// unacceptable. A retry re-sends the same payload, so this is charged to
		// the attempt counter and terminates rather than looping.
		return "", "", fmt.Errorf("openrouter rejected chat analysis judge request: %w: %w", ErrModelFailure, err)
	case err != nil:
		return "", "", fmt.Errorf("openrouter object completion: %w: %w", ErrRetryable, err)
	case response == nil || response.Message == nil:
		return "", "", fmt.Errorf("empty chat analysis judge response: %w", ErrModelFailure)
	}

	raw := strings.TrimSpace(openrouter.GetText(*response.Message))
	if raw == "" {
		return "", "", fmt.Errorf("empty chat analysis judge content: %w", ErrModelFailure)
	}

	return raw, conv.Default(response.Model, call.Model), nil
}
