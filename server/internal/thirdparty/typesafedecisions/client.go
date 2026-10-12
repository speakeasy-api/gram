// Package typesafedecisions provides bounded, typed judgments from Jev.
package typesafedecisions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	sdk "github.com/OpenRouterTeam/go-sdk"
	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/retry"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Model pins the OpenRouter Jev release; responses may include a build suffix.
const Model = "typesafe/jev-1.13"

var ErrUnavailable = errors.New("typesafe is not configured")

// Question defines a single binary semantic condition.
type Question struct {
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
	Type         string            `json:"type"`
}

// Result contains probabilities, not generative-model confidence scores.
type Result struct {
	Probabilities map[string]float64
	Model         string
	InputTokens   int
	OutputTokens  int

	// CostUSD is the cost reported by OpenRouter for this request.
	CostUSD float64
}

type Evaluator interface {
	Evaluate(context.Context, string, json.RawMessage, map[string]Question) (Result, error)
}

type Unavailable struct{}

func (Unavailable) Evaluate(context.Context, string, json.RawMessage, map[string]Question) (Result, error) {
	return Result{Probabilities: nil, Model: Model, InputTokens: 0, OutputTokens: 0, CostUSD: 0}, ErrUnavailable
}

// Client evaluates Jev through OpenRouter's Decisions API using an internal
// organization key. The resolver also supports a fixed development key.
type Client struct {
	httpClient *guardian.HTTPClient
	resolveKey func(context.Context, string) (string, error)
	endpoint   string
}

func New(httpClient *guardian.HTTPClient, resolveKey func(context.Context, string) (string, error)) *Client {
	return &Client{httpClient: httpClient, resolveKey: resolveKey, endpoint: "https://openrouter.ai/api/alpha/decisions"}
}

func (c *Client) Evaluate(ctx context.Context, orgID string, state json.RawMessage, questions map[string]Question) (Result, error) {
	result := Result{Probabilities: nil, Model: Model, InputTokens: 0, OutputTokens: 0, CostUSD: 0}
	if !json.Valid(state) || len(questions) == 0 {
		return result, errors.New("invalid typesafe evaluation input")
	}
	if c.resolveKey == nil || c.httpClient == nil {
		return result, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	apiKey, err := c.resolveKey(ctx, orgID)
	if err != nil {
		return result, fmt.Errorf("resolve Jev OpenRouter key: %w", err)
	}
	if apiKey == "" || apiKey == "unset" {
		return result, ErrUnavailable
	}
	sdkQuestions := make(map[string]components.Questions, len(questions))
	for id, question := range questions {
		if question.Type != "noul" {
			return result, errors.New("unsupported typesafe question type")
		}
		criteria := &components.DecisionsNoulQuestionCriteria{True: components.CreateTrueStr(""), False: components.CreateFalseStr("")}
		for label, description := range question.Criteria {
			switch label {
			case "true":
				value := components.CreateTrueStr(description)
				criteria.True = value
			case "false":
				value := components.CreateFalseStr(description)
				criteria.False = value
			default:
				return result, errors.New("invalid typesafe noul criterion")
			}
		}
		if len(question.Criteria) == 0 {
			criteria = nil
		}
		sdkQuestions[id] = components.CreateQuestionsNoul(components.DecisionsNoulQuestion{
			Type:         components.DecisionsNoulQuestionTypeNoul,
			Instructions: components.CreateDecisionsNoulQuestionInstructionsStr(question.Instructions),
			Criteria:     criteria,
		})
	}
	// Preserve JSON numbers in structured state rather than rounding through float64.
	decoder := json.NewDecoder(bytes.NewReader(state))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return result, errors.New("invalid typesafe state")
	}
	var sdkState components.State
	switch value := value.(type) {
	case string:
		sdkState = components.CreateStateStr(value)
	case map[string]any:
		sdkState = components.CreateStateMapOfAny(value)
	case []any:
		sdkState = components.CreateStateArrayOfAny(value)
	default:
		return result, errors.New("invalid typesafe state")
	}
	transport := &boundedClient{client: c.httpClient, endpoint: c.endpoint, err: nil}
	client := sdk.New(sdk.WithClient(transport), sdk.WithSecurity(apiKey),
		sdk.WithRetryConfig(retry.Config{Strategy: "none", Backoff: nil, RetryConnectionErrors: false}))
	response, err := client.Alpha.Decisions.Create(ctx, components.DecisionsRequest{
		Model: Model, State: sdkState, Questions: sdkQuestions,
		Provider: nil, SessionID: nil, Trace: nil, User: nil,
	})
	if err != nil {
		if transport.err != nil {
			return result, transport.err
		}
		if ctx.Err() != nil {
			return result, fmt.Errorf("request typesafe evaluation: %w", ctx.Err())
		}
		// SDK decoding errors can include provider response bodies.
		return result, errors.New("invalid typesafe response JSON")
	}
	if (response.Model != Model && !strings.HasPrefix(response.Model, Model+"-")) || response.Usage.Cost == nil || math.IsNaN(*response.Usage.Cost) || math.IsInf(*response.Usage.Cost, 0) || *response.Usage.Cost < 0 || response.Usage.InputTokens < 0 || response.Usage.OutputTokens < 0 || len(response.Answers) != len(questions) {
		return result, errors.New("invalid typesafe response metadata")
	}
	probabilities := make(map[string]float64, len(questions))
	for id := range questions {
		answer, ok := response.Answers[id]
		if !ok || answer.Type != components.AnswersTypeNoul || answer.DecisionsNoulAnswer == nil {
			return result, errors.New("invalid typesafe probability")
		}
		probability := answer.DecisionsNoulAnswer.Noul
		if math.IsNaN(probability) || math.IsInf(probability, 0) || probability < 0 || probability > 1 {
			return result, errors.New("invalid typesafe probability")
		}
		probabilities[id] = probability
	}
	return Result{Probabilities: probabilities, Model: response.Model, InputTokens: int(response.Usage.InputTokens), OutputTokens: int(response.Usage.OutputTokens), CostUSD: *response.Usage.Cost}, nil
}

// boundedClient keeps provider bodies bounded and private before SDK decoding.
// It is scoped to one evaluation, whose SDK retries are disabled.
type boundedClient struct {
	client   *guardian.HTTPClient
	endpoint string
	err      error
}

func (c *boundedClient) Do(req *http.Request) (*http.Response, error) {
	response, err := c.do(req)
	c.err = err
	return response, err
}

func (c *boundedClient) do(req *http.Request) (*http.Response, error) {
	// Preserve the SDK request while allowing a local endpoint in tests.
	endpoint, err := req.URL.Parse(c.endpoint)
	if err != nil {
		return nil, errors.New("invalid typesafe endpoint")
	}
	req.URL = endpoint
	res, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request typesafe evaluation: %w", err)
	}
	originalBody := res.Body
	defer o11y.NoLogDefer(func() error { return originalBody.Close() })
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("typesafe HTTP status %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("read typesafe response: %w", err)
	}
	if len(raw) > 1<<20 {
		return nil, errors.New("typesafe response exceeds limit")
	}
	// The SDK represents noul as float64: missing/null must not become a safe zero.
	var presence struct {
		Answers map[string]struct {
			Noul *float64 `json:"noul"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(raw, &presence); err != nil {
		return nil, errors.New("invalid typesafe response JSON")
	}
	for _, answer := range presence.Answers {
		if answer.Noul == nil {
			return nil, errors.New("invalid typesafe probability")
		}
	}
	res.Body = io.NopCloser(bytes.NewReader(raw))
	return res, nil
}
