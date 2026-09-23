// Package typesafe is the HTTP client for TypeSafe's System One ("Jev")
// judgement API. Callers hand it an opaque state document plus a set of
// questions and get back a probability per option (choice questions) or a
// single probability (noul questions).
package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// DefaultModel is the OpenRouter model id every request is sent with; it
	// is not configurable in this version.
	DefaultModel = "typesafe/jev-latest"

	// DefaultEndpoint is the System One judgement endpoint as hosted by
	// OpenRouter, so the org's provisioned OpenRouter key pays for it.
	DefaultEndpoint = openrouter.OpenRouterBaseURL + "/v1/systemone"

	// DefaultTimeout bounds one Ask round trip. The launcher renders on
	// every keystroke, so a slow judgement is worth less than no judgement.
	DefaultTimeout = 4 * time.Second

	providerName = "typesafe"

	// maxErrorBody bounds how much of a rejected response is kept in the
	// error message.
	maxErrorBody = 512
)

var (
	// ErrMissingAPIKey is returned by Ask when it is given an empty API key; no
	// request is made.
	ErrMissingAPIKey = errors.New("typesafe: api key is not configured")

	// ErrTransport wraps failures to reach TypeSafe, including a context
	// deadline or cancellation while the request is in flight.
	ErrTransport = errors.New("typesafe: transport error")

	// ErrUpstreamStatus wraps a non-200 response from TypeSafe.
	ErrUpstreamStatus = errors.New("typesafe: unexpected upstream status")

	// ErrDecode wraps a 200 response whose body is not a valid Response.
	ErrDecode = errors.New("typesafe: decode response")
)

var (
	latencyMsKey     = attribute.Key("gram.typesafe.latency_ms")
	questionCountKey = attribute.Key("gram.typesafe.question_count")
)

// Request is the body POSTed to the judgement endpoint.
type Request struct {
	// Model selects the judge; use DefaultModel.
	Model string `json:"model"`

	// State is the opaque document the questions are asked about. It is
	// forwarded verbatim and never logged.
	State json.RawMessage `json:"state"`

	// Questions is keyed by a caller-chosen id that the answers echo back.
	Questions map[string]Question `json:"questions"`
}

// Question is one judgement to make about the state.
type Question struct {
	// Type is "choice" (pick one of Criteria, with a probability per key) or
	// "noul" (a single probability that the "true" criterion holds).
	Type string `json:"type"`

	// Instructions tells the judge what to decide.
	Instructions string `json:"instructions"`

	// Criteria describes each option. For a "noul" question it has exactly
	// the keys "true" and "false".
	Criteria map[string]string `json:"criteria"`
}

// Answer is the judgement for one question, keyed like the request.
type Answer struct {
	// Type echoes the question type.
	Type string `json:"type"`

	// Choice is the most probable criterion key of a "choice" question.
	Choice *string `json:"choice,omitempty"`

	// Probabilities maps each criterion key of a "choice" question to its
	// probability.
	Probabilities map[string]float64 `json:"probabilities,omitempty"`

	// Noul is the probability that the "true" criterion of a "noul" question
	// holds.
	Noul *float64 `json:"noul,omitempty"`
}

// Usage is the token accounting for one request.
type Usage struct {
	// InputTokens is the number of tokens in the state and questions.
	InputTokens int `json:"input_tokens"`

	// OutputTokens is the number of tokens the judge produced.
	OutputTokens int `json:"output_tokens"`
}

// Response is the decoded body of a successful judgement.
type Response struct {
	// Model is the concrete model version that answered.
	Model string `json:"model"`

	// Answers is keyed by the question ids of the request.
	Answers map[string]Answer `json:"answers"`

	// Usage is the token accounting for the request.
	Usage Usage `json:"usage"`
}

// Result pairs a decoded Response with how long the round trip took.
type Result struct {
	// Response is the decoded judgement.
	Response Response

	// Latency is the wall-clock time from sending the request to reading
	// the full response body.
	Latency time.Duration
}

// Client talks to the TypeSafe judgement API.
type Client struct {
	httpClient *guardian.HTTPClient
	endpoint   string
	timeout    time.Duration
	logger     *slog.Logger
	tracer     trace.Tracer
}

// Option customises a Client at construction time.
type Option func(*Client)

// WithEndpoint overrides the judgement endpoint URL.
func WithEndpoint(url string) Option {
	return func(c *Client) {
		if url != "" {
			c.endpoint = url
		}
	}
}

// WithTimeout overrides the per-request timeout applied inside Ask.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithTracerProvider overrides the tracer provider used for the Ask span.
// The global provider is used by default.
func WithTracerProvider(provider trace.TracerProvider) Option {
	return func(c *Client) {
		if provider != nil {
			c.tracer = provider.Tracer("github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe")
		}
	}
}

// NewClient returns a client that is always safe to call. The API key is
// supplied per Ask call because each organization pays with its own
// provisioned OpenRouter key. The httpClient comes from a guardian.Policy
// (Client or PooledClient) and must not retry: Ask makes exactly one attempt.
func NewClient(httpClient *guardian.HTTPClient, logger *slog.Logger, opts ...Option) *Client {
	if httpClient == nil {
		panic("typesafe client requires an http client")
	}
	c := &Client{
		httpClient: httpClient,
		endpoint:   DefaultEndpoint,
		timeout:    DefaultTimeout,
		logger:     logger.With(attr.SlogComponent("typesafe")),
		tracer:     otel.GetTracerProvider().Tracer("github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe"),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Ask POSTs req to the judgement endpoint with apiKey as the bearer token
// and decodes the answers. An empty apiKey fails fast with ErrMissingAPIKey
// before any network activity. The key is never logged or traced. It makes
// exactly one attempt, bounded by the client timeout or an earlier parent
// deadline, whichever comes first. Errors wrap ErrMissingAPIKey,
// ErrTransport, ErrUpstreamStatus or ErrDecode so callers can errors.Is
// them.
func (c *Client) Ask(ctx context.Context, apiKey string, req Request) (*Result, error) {
	if apiKey == "" {
		return nil, ErrMissingAPIKey
	}

	ctx, span := c.tracer.Start(ctx, "typesafe_client.ask", trace.WithAttributes(
		attr.GenAIProviderName(providerName),
		attr.GenAIRequestModel(req.Model),
		questionCountKey.Int(len(req.Questions)),
	))
	defer span.End()

	result, err := c.ask(ctx, apiKey, req)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		c.logger.WarnContext(ctx, "typesafe judgement failed", attr.SlogError(err))
		return nil, err
	}

	span.SetAttributes(
		latencyMsKey.Float64(float64(result.Latency)/float64(time.Millisecond)),
		attr.GenAIUsageInputTokens(result.Response.Usage.InputTokens),
		attr.GenAIUsageOutputTokens(result.Response.Usage.OutputTokens),
		attr.GenAIResponseModel(result.Response.Model),
	)
	return result, nil
}

func (c *Client) ask(ctx context.Context, apiKey string, req Request) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	started := time.Now()
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTransport, err)
	}
	defer func() {
		_ = httpResp.Body.Close()
	}()

	if httpResp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(httpResp.Body, maxErrorBody))
		return nil, fmt.Errorf("%w: %s: %s", ErrUpstreamStatus, httpResp.Status, bytes.TrimSpace(snippet))
	}

	var resp Response
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecode, err)
	}

	return &Result{
		Response: resp,
		Latency:  time.Since(started),
	}, nil
}
