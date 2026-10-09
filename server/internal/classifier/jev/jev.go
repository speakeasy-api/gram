// Package jev implements batch classification with Jev through OpenRouter's System One API.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	model            = "jev-latest"
	endpoint         = "https://openrouter.ai/api/v1/systemone"
	maxResponseBytes = 8 << 20
)

// Classifier compiles and partitions logical batches, sharing a four-request
// concurrency limit across calls. Construct it with New.
type Classifier struct {
	client   *guardian.HTTPClient
	key      conv.Secret
	endpoint string
	slots    chan struct{}
}

var _ classifier.Classifier = (*Classifier)(nil)

// Option configures a classifier at construction time.
type Option func(*Classifier)

// WithEndpoint overrides the full System One endpoint URL, including its path.
// An empty URL leaves the default endpoint unchanged. New requires HTTPS and a
// hostname, and rejects userinfo and fragments before any request can be sent.
func WithEndpoint(url string) Option {
	return func(c *Classifier) {
		if url != "" {
			c.endpoint = url
		}
	}
}

// New constructs a classifier for OpenRouter's System One API with an OpenRouter API key.
// A policy is required. Requests have a 60-second timeout and are not automatically
// retried; callers own retries using per-question failures and RetryAfter.
func New(guardianPolicy *guardian.Policy, key conv.Secret, options ...Option) (*Classifier, error) {
	c := &Classifier{
		client: guardianPolicy.PooledClient(
			guardian.WithCheckRedirect(func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }),
			guardian.WithResilience("jev", guardian.ResilienceConfig{
				Partition:       guardian.PartitionByHost(),
				Limit:           guardian.Limit{Rate: 1200, Burst: 20, Period: time.Minute},
				WaitForCapacity: true,
				Breaker:         guardian.NoBreaker(),
			}),
		),
		key:      conv.NewSecret(bytes.Clone(key.Reveal())),
		endpoint: endpoint,
		slots:    make(chan struct{}, 4),
	}
	for _, option := range options {
		option(c)
	}

	u, err := url.Parse(c.endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("jev: endpoint must be an HTTPS URL with a hostname and no userinfo or fragment")
	}

	return c, nil
}

// Classify sends the validated batch whole and splits only after HTTP 413 or
// HTTP 400 with detail.error_type=max_tokens_exceeded. An unsplittable size
// rejection sets Result.Err to classifier.ErrRequestTooLarge alongside
// outcomes, preserving successful answers. Cancellation returns known outcomes
// and usage alongside the context error.
func (c *Classifier) Classify(ctx context.Context, req *classifier.Request) classifier.Result {
	collected, err := c.classify(ctx, req)
	result := classifier.NewResult(collected.Outcomes, err)
	result.Metadata = collected.Metadata
	result.Models = collected.Models
	result.Usage = collected.Usage
	return result
}

func (c *Classifier) classify(ctx context.Context, req *classifier.Request) (classifier.Result, error) {
	var result classifier.Result
	result.Metadata = classifier.Metadata{Provider: "openrouter", Model: model, CompilerVersion: "1", AccountingVersion: "provider-usage-v1"}
	result.Usage.Complete = true

	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("jev: classify: %w", err)
	}

	if len(bytes.TrimSpace(c.key.Reveal())) == 0 {
		return result, fmt.Errorf("jev: API key is required: %w", classifier.ErrDisabled)
	}

	questions, err := compileRequest(req)
	if err != nil {
		return result, fmt.Errorf("%w: %w", classifier.ErrInvalidRequest, err)
	}

	partial := c.execute(ctx, req.Input, questions)
	result.Outcomes, result.Usage, result.Models = partial.Outcomes, partial.Usage, partial.Models
	var sizeError error
	for _, outcome := range result.Outcomes {
		if outcome.Failure != nil && outcome.Failure.Code == classifier.FailureInputTooLarge {
			sizeError = classifier.ErrRequestTooLarge
			break
		}
	}

	if err := errors.Join(sizeError, ctx.Err()); err != nil {
		return result, fmt.Errorf("jev: classify: %w", err)
	}

	return result, nil
}

type wireRequest struct {
	Model     string                  `json:"model"`
	State     classifier.Entry        `json:"state"`
	Questions map[string]wireQuestion `json:"questions"`
}

type wireUsage struct {
	Input  *int64 `json:"input_tokens"`
	Output *int64 `json:"output_tokens"`
}

type wireResponse struct {
	Model   string                `json:"model"`
	Answers map[string]wireAnswer `json:"answers"`
	Usage   wireUsage             `json:"usage"`
}

func failure(code classifier.FailureCode, message string, retryable bool) *classifier.QuestionFailure {
	return &classifier.QuestionFailure{Code: code, Message: message, Retryable: retryable, RetryAfter: nil}
}

func (c *Classifier) execute(ctx context.Context, state classifier.Entry, questions []plannedQuestion) classifier.Result {
	var result classifier.Result
	result.Usage.Complete = true
	if ctx.Err() != nil {
		return result
	}
	wire, failed, tooLarge, attempted := c.send(ctx, state, questions)
	if attempted {
		result.Usage.Complete = wire.Usage.Input != nil && wire.Usage.Output != nil && *wire.Usage.Input >= 0 && *wire.Usage.Output >= 0
		if wire.Usage.Input != nil && *wire.Usage.Input >= 0 {
			result.Usage.InputTokens = *wire.Usage.Input
		}
		if wire.Usage.Output != nil && *wire.Usage.Output >= 0 {
			result.Usage.OutputTokens = *wire.Usage.Output
		}
	}
	if !attempted && ctx.Err() != nil {
		return result
	}
	if wire.Model != "" {
		result.Models = []string{wire.Model}
	}
	if tooLarge && len(questions) > 1 {
		// Only explicit size rejection triggers subdivision, never arbitrary validation errors.
		// Both halves are non-empty and strictly smaller; singleton rejections
		// terminate below. An N-question batch makes at most 2N-1 attempts.
		middle := len(questions) / 2
		for _, part := range [][]plannedQuestion{questions[:middle], questions[middle:]} {
			child := c.execute(ctx, state, part)
			result.Outcomes = append(result.Outcomes, child.Outcomes...)
			result.Usage.InputTokens += child.Usage.InputTokens
			result.Usage.OutputTokens += child.Usage.OutputTokens
			result.Usage.Complete = result.Usage.Complete && child.Usage.Complete
			for _, version := range child.Models {
				if !slices.Contains(result.Models, version) {
					result.Models = append(result.Models, version)
				}
			}
		}
		return result
	}
	if failed == nil {
		for key := range wire.Answers {
			found := false
			for _, q := range questions {
				if key == strconv.Itoa(q.index) {
					found = true
					break
				}
			}
			if !found {
				failed = failure(classifier.FailureInvalidResponse, "Provider returned an unknown question key", false)
				break
			}
		}
	}
	for _, q := range questions {
		outcome := classifier.QuestionOutcome{Key: q.question.Key, Answer: nil, Failure: failed}
		if failed == nil {
			outcome.Answer = decodeAnswer(q, wire.Answers[strconv.Itoa(q.index)])
			if outcome.Answer == nil {
				outcome.Failure = failure(classifier.FailureInvalidResponse, "Provider returned an invalid or missing answer", false)
			}
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	return result
}

func (c *Classifier) send(ctx context.Context, state classifier.Entry, questions []plannedQuestion) (wireResponse, *classifier.QuestionFailure, bool, bool) {
	var response wireResponse
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return response, failure(classifier.FailureProviderUnavailable, "Provider admission wait interrupted", true), false, false
	}
	request := wireRequest{Model: model, State: state, Questions: make(map[string]wireQuestion, len(questions))}
	for _, q := range questions {
		request.Questions[strconv.Itoa(q.index)] = q.wire
	}

	body, err := json.Marshal(request)
	if err != nil {
		return response, failure(classifier.FailureProviderRejected, "Cannot encode provider request", false), false, false
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return response, failure(classifier.FailureProviderRejected, "Cannot construct provider request", false), false, false
	}

	httpReq.Header.Set("Authorization", "Bearer "+string(c.key.Reveal()))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		if denial, ok := errors.AsType[*guardian.ResilienceError](err); ok && errors.Is(err, guardian.ErrRateLimited) {
			failed := failure(classifier.FailureRateLimited, "Provider admission rate limited", true)
			if denial.RetryAfter > 0 {
				failed.RetryAfter = new(denial.RetryAfter)
			}
			return response, failed, false, false
		}

		return response, failure(classifier.FailureProviderUnavailable, "Provider request failed", true), false, true
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		tooLarge := resp.StatusCode == http.StatusRequestEntityTooLarge
		if resp.StatusCode == http.StatusBadRequest {
			// Direct Jev probes identify this exact response as context overflow.
			// Other 400 bodies remain ordinary rejections; never match free text.
			var rejection struct {
				Detail struct {
					ErrorType string `json:"error_type"`
				} `json:"detail"`
			}
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
			tooLarge = readErr == nil && len(body) <= maxResponseBytes &&
				json.Unmarshal(body, &rejection) == nil && rejection.Detail.ErrorType == "max_tokens_exceeded"
		}
		code, retryable := classifier.FailureProviderRejected, false
		switch {
		case tooLarge:
			code = classifier.FailureInputTooLarge
		case resp.StatusCode == http.StatusTooManyRequests:
			code, retryable = classifier.FailureRateLimited, true
		case resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout:
			code, retryable = classifier.FailureProviderUnavailable, true
		}
		failed := failure(code, fmt.Sprintf("Provider returned HTTP %d", resp.StatusCode), retryable)
		if retryable {
			failed.RetryAfter = retryAfter(resp.Header.Get("Retry-After"))
		}
		return response, failed, tooLarge, true
	}

	body, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return response, failure(classifier.FailureProviderUnavailable, "Cannot read provider response", true), false, true
	}

	if len(body) > maxResponseBytes || json.Unmarshal(body, &response) != nil {
		var empty wireResponse
		return empty, failure(classifier.FailureInvalidResponse, "Provider response is malformed or too large", false), false, true
	}
	return response, nil, false, true
}

func retryAfter(value string) *time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 && seconds <= int64((1<<63-1)/time.Second) {
		delay := time.Duration(seconds) * time.Second
		return &delay
	}

	if date, err := http.ParseTime(value); err == nil {
		delay := max(time.Until(date), 0)
		return &delay
	}

	return nil
}
