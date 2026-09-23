// Package typesafe provides bounded, typed judgments from Jev.
package typesafe

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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	apiKey, err := c.resolveKey(ctx, orgID)
	if err != nil {
		return result, fmt.Errorf("resolve Jev OpenRouter key: %w", err)
	}
	if apiKey == "" || apiKey == "unset" {
		return result, ErrUnavailable
	}
	body, err := json.Marshal(struct {
		Model     string              `json:"model"`
		State     json.RawMessage     `json:"state"`
		Questions map[string]Question `json:"questions"`
	}{Model: Model, State: state, Questions: questions})
	if err != nil {
		return result, fmt.Errorf("encode typesafe request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("create typesafe request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return result, fmt.Errorf("request typesafe evaluation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		// Provider error bodies may echo sensitive state or credentials.
		return result, fmt.Errorf("typesafe HTTP status %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		return result, fmt.Errorf("read typesafe response: %w", err)
	}
	if len(raw) > 1<<20 {
		return result, errors.New("typesafe response exceeds limit")
	}
	var response struct {
		Model   string `json:"model"`
		Answers map[string]struct {
			Type string   `json:"type"`
			Noul *float64 `json:"noul"`
		} `json:"answers"`
		Usage *struct {
			InputTokens  int      `json:"input_tokens"`
			OutputTokens int      `json:"output_tokens"`
			Cost         *float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return result, errors.New("invalid typesafe response JSON")
	}
	if (response.Model != Model && !strings.HasPrefix(response.Model, Model+"-")) || response.Usage == nil || response.Usage.Cost == nil || math.IsNaN(*response.Usage.Cost) || math.IsInf(*response.Usage.Cost, 0) || *response.Usage.Cost < 0 || response.Usage.InputTokens < 0 || response.Usage.OutputTokens < 0 || len(response.Answers) != len(questions) {
		return result, errors.New("invalid typesafe response metadata")
	}
	probabilities := make(map[string]float64, len(questions))
	for id := range questions {
		answer, ok := response.Answers[id]
		if !ok || answer.Type != "noul" || answer.Noul == nil || math.IsNaN(*answer.Noul) || math.IsInf(*answer.Noul, 0) || *answer.Noul < 0 || *answer.Noul > 1 {
			return result, errors.New("invalid typesafe probability")
		}
		probabilities[id] = *answer.Noul
	}
	return Result{Probabilities: probabilities, Model: response.Model, InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens, CostUSD: *response.Usage.Cost}, nil
}
