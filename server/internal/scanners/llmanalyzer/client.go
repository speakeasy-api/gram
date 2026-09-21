package llmanalyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// maxRetries is how many attempts beyond the first a call may make, so a
	// call issues at most maxRetries+1 requests. guardian.RetryConfig.MaxAttempts
	// maps onto retryablehttp's RetryMax, which counts retries, not attempts.
	maxRetries = 3

	// retryWaitMin and retryWaitMax bound the exponential backoff between
	// attempts. The whole call still runs under Config.Timeout.
	retryWaitMin = 100 * time.Millisecond
	retryWaitMax = time.Second

	// maxResponseBytes bounds how much of a reply is read. A verdict is a few
	// hundred bytes; the cap only guards against a misbehaving upstream.
	maxResponseBytes = 1 << 20

	// maxErrorBodyBytes bounds the upstream body snippet kept on an
	// UpstreamError for logging.
	maxErrorBodyBytes = 1024

	completionsPath = "/chat/completions"
	tracerName      = "github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
)

var (
	// ErrTimeout is returned when Config.Timeout elapsed before a reply
	// arrived. It wraps context.DeadlineExceeded.
	ErrTimeout = errors.New("risk llm: request timed out")

	// ErrEmptyCompletion is returned when the upstream replied 2xx without any
	// usable choice content.
	ErrEmptyCompletion = errors.New("risk llm: empty completion")
)

// UpstreamError is returned when the upstream answered with a non-2xx status:
// either immediately for statuses the retry policy does not retry, or after
// the retry budget ran out.
type UpstreamError struct {
	// Status is the HTTP status of the final attempt.
	Status int

	// Body is a printable, truncated snippet of the final response body. It is
	// upstream-controlled: log it, never show it to end users.
	Body string
}

func (e *UpstreamError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("risk llm: upstream status %d", e.Status)
	}
	return fmt.Sprintf("risk llm: upstream status %d: %s", e.Status, e.Body)
}

// Message is one chat turn sent to the model.
type Message struct {
	// Role is "system" or "user".
	Role string `json:"role"`

	// Content is the turn's text.
	Content string `json:"content"`
}

// Completion is the model's reply to one Complete call.
type Completion struct {
	// Content is the reply text with surrounding whitespace removed. When the
	// upstream returned an empty content field, it is the reasoning_content
	// field instead.
	Content string

	// PromptTokens is usage.prompt_tokens as reported by the upstream.
	PromptTokens int

	// CompletionTokens is usage.completion_tokens as reported by the upstream.
	CompletionTokens int

	// Model is the model name the upstream reported, falling back to the
	// configured one.
	Model string

	// Attempts is how many HTTP requests the call issued.
	Attempts int
}

// Client calls the fine-tuned risk model over the OpenAI-compatible chat
// completions API.
type Client struct {
	logger     *slog.Logger
	tracer     trace.Tracer
	metrics    *metrics
	httpClient *guardian.HTTPClient
	cfg        Config
	endpoint   string
}

// NewClient validates cfg, applies the default timeout and max tokens, and
// builds the guardian-backed HTTP client with the retry policy the analyzer
// relies on: up to three retries on 5xx, 429 and connection errors with
// 100ms to 1s backoff, never on a context deadline.
func NewClient(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, policy *guardian.Policy, cfg Config) (*Client, error) {
	if policy == nil {
		return nil, errors.New("risk llm: guardian policy is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.Timeout = conv.Default(cfg.Timeout, DefaultTimeout)
	cfg.MaxTokens = conv.Default(cfg.MaxTokens, DefaultMaxTokens)

	logger = logger.With(attr.SlogComponent("risk-llm-analyzer"))

	// Start from the guardian defaults to keep its error handler, which
	// surfaces an exhausted budget as *RetriesExhaustedError carrying the
	// final status instead of an opaque "giving up" message.
	retry := guardian.DefaultRetryConfig()
	retry.WaitMin = retryWaitMin
	retry.WaitMax = retryWaitMax
	retry.MaxAttempts = maxRetries
	retry.CheckRetry = countingRetryPolicy
	retry.Backoff = clampedBackoff

	return &Client{
		logger:  logger,
		tracer:  tracerProvider.Tracer(tracerName),
		metrics: newMetrics(meterProvider, logger),
		// Pooled: the client lives for the process and always talks to the one
		// configured host, so each call reuses the TLS connection.
		httpClient: policy.PooledClient(guardian.WithRetryConfig(retry), guardian.WithCheckRedirect(rejectRedirect)),
		cfg:        cfg,
		endpoint:   strings.TrimRight(cfg.BaseURL, "/") + completionsPath,
	}, nil
}

// Complete sends the messages to the model and returns its reply. The call is
// bounded by Config.Timeout end to end. Failures are typed: ErrTimeout when the
// budget elapsed, *UpstreamError for a non-2xx reply, ErrEmptyCompletion for a
// 2xx reply without content; anything else is a transport or decoding error.
// Request outcome, latency, retries and token usage are recorded against info.
func (c *Client) Complete(ctx context.Context, info CallInfo, messages []Message) (Completion, error) {
	ctx, span := c.tracer.Start(ctx, "risk.llm.complete", trace.WithAttributes(
		attr.OrganizationID(info.OrgID),
		attr.OrganizationSlug(info.OrgSlug),
		attr.RiskScanMode(info.ScanMode),
		attr.RiskLLMModel(c.cfg.Model),
	))
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	counter := &attemptCounter{attempts: 0}
	ctx = context.WithValue(ctx, attemptCounterKey{}, counter)

	start := time.Now()
	completion, err := c.complete(ctx, messages)
	completion.Attempts = counter.attempts
	outcome := outcomeFor(err)

	c.metrics.RecordRequest(ctx, info, c.cfg.Model, outcome, time.Since(start))
	c.metrics.RecordRetries(ctx, info, c.cfg.Model, completion.Attempts-1)
	span.SetAttributes(
		attr.Outcome(outcome),
		attribute.Int("gram.risk.llm.attempts", completion.Attempts),
	)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "risk llm completion failed")
		c.logger.WarnContext(ctx, "risk llm completion failed",
			attr.SlogError(err),
			attr.SlogOutcome(string(outcome)),
			attr.SlogOrganizationID(info.OrgID),
			attr.SlogRiskScanMode(info.ScanMode),
		)
		return Completion{
			Content:          "",
			PromptTokens:     0,
			CompletionTokens: 0,
			Model:            c.cfg.Model,
			Attempts:         completion.Attempts,
		}, err
	}

	c.metrics.RecordTokens(ctx, info, c.cfg.Model, completion.PromptTokens, completion.CompletionTokens)
	span.SetAttributes(
		attribute.Int("gram.risk.llm.prompt_tokens", completion.PromptTokens),
		attribute.Int("gram.risk.llm.completion_tokens", completion.CompletionTokens),
	)
	return completion, nil
}

// RecordParseFailure records that a completion returned by Complete could not
// be parsed as a verdict. Parsing happens in the caller, which owns the
// decision of what to do with the failure.
func (c *Client) RecordParseFailure(ctx context.Context, info CallInfo) {
	c.metrics.RecordParseFailure(ctx, info, c.cfg.Model)
}

type chatRequest struct {
	Model              string             `json:"model"`
	Messages           []Message          `json:"messages"`
	Temperature        float64            `json:"temperature"`
	MaxTokens          int                `json:"max_tokens"`
	ChatTemplateKwargs chatTemplateKwargs `json:"chat_template_kwargs"`
}

// chatTemplateKwargs disables the model's thinking mode. Thinking multiplied
// tail latency past the call budget during evaluation and the fine-tune never
// trained with it.
type chatTemplateKwargs struct {
	EnableThinking bool `json:"enable_thinking"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *Client) complete(ctx context.Context, messages []Message) (Completion, error) {
	body, err := json.Marshal(chatRequest{
		Model:              c.cfg.Model,
		Messages:           messages,
		Temperature:        0,
		MaxTokens:          c.cfg.MaxTokens,
		ChatTemplateKwargs: chatTemplateKwargs{EnableThinking: false},
	})
	if err != nil {
		return Completion{}, fmt.Errorf("encode risk llm request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return Completion{}, fmt.Errorf("build risk llm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Completion{}, classifyRequestError(err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Completion{}, fmt.Errorf("%w: %w", ErrTimeout, err)
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			// The read failed for a reason other than the deadline it raced;
			// keep the deadline in the chain so the outcome is timeout.
			return Completion{}, fmt.Errorf("%w: %w: %w", ErrTimeout, context.DeadlineExceeded, err)
		}
		return Completion{}, fmt.Errorf("read risk llm response: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		snippet := payload
		if len(snippet) > maxErrorBodyBytes {
			snippet = snippet[:maxErrorBodyBytes]
		}
		return Completion{}, &UpstreamError{
			Status: resp.StatusCode,
			Body:   guardian.PrintableBodySnippet(snippet),
		}
	}

	if len(bytes.TrimSpace(payload)) == 0 {
		return Completion{}, ErrEmptyCompletion
	}

	var parsed chatResponse
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return Completion{}, fmt.Errorf("decode risk llm response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Completion{}, ErrEmptyCompletion
	}

	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if content == "" {
		content = strings.TrimSpace(parsed.Choices[0].Message.ReasoningContent)
	}
	if content == "" {
		return Completion{}, ErrEmptyCompletion
	}

	return Completion{
		Content:          content,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		Model:            conv.Default(parsed.Model, c.cfg.Model),
		Attempts:         0,
	}, nil
}

// rejectRedirect hands every 3xx back to complete, which reports it as an
// *UpstreamError. The configured endpoint is the only host that may see the
// bearer token and the prompt, so a redirect is a misconfiguration, never
// something to follow.
func rejectRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

// classifyRequestError maps a transport-level failure onto the typed errors:
// an elapsed deadline becomes ErrTimeout and an exhausted retry budget that
// ended on an HTTP status becomes *UpstreamError.
func classifyRequestError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", ErrTimeout, err)
	}
	if exhausted, ok := errors.AsType[*guardian.RetriesExhaustedError](err); ok && exhausted.StatusCode != 0 {
		return &UpstreamError{Status: exhausted.StatusCode, Body: exhausted.Body}
	}
	return fmt.Errorf("risk llm request: %w", err)
}

// outcomeFor classifies a Complete error for metrics: a 429 that survived the
// retry budget is rate_limited, an elapsed deadline is timeout, any other error
// is failure.
func outcomeFor(err error) o11y.Outcome {
	if upstream, ok := errors.AsType[*UpstreamError](err); ok && upstream.Status == http.StatusTooManyRequests {
		return OutcomeRateLimited
	}
	return o11y.OutcomeFromErrorWithTimeout(err)
}

type attemptCounterKey struct{}

// attemptCounter counts the HTTP attempts of one Complete call. It travels on
// the request context because the retry policy is shared by every call.
type attemptCounter struct {
	attempts int
}

// countingRetryPolicy is retryablehttp's default policy with one addition: it
// bumps the call's attempt counter, since the policy runs exactly once per
// attempt, including the last one.
func countingRetryPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if counter, ok := ctx.Value(attemptCounterKey{}).(*attemptCounter); ok {
		counter.attempts++
	}
	shouldRetry, policyErr := retryablehttp.DefaultRetryPolicy(ctx, resp, err)
	if policyErr != nil {
		return shouldRetry, fmt.Errorf("risk llm retry policy: %w", policyErr)
	}
	return shouldRetry, nil
}

// clampedBackoff is retryablehttp's default backoff with the Retry-After
// header clamped to [retryWaitMin, retryWaitMax]. The default honors the
// header verbatim, so a 429 asking for a minute would burn the whole call
// budget on one wait, and a zero or past Retry-After would retry with no
// pause at all.
func clampedBackoff(minWait, maxWait time.Duration, attemptNum int, resp *http.Response) time.Duration {
	return max(minWait, min(retryablehttp.DefaultBackoff(minWait, maxWait, attemptNum, resp), maxWait))
}
