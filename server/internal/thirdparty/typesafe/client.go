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
	"time"
)

const Model = "jev-1.13.0"

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
}

type Evaluator interface {
	Evaluate(context.Context, json.RawMessage, map[string]Question) (Result, error)
}

type Unavailable struct{}

func (Unavailable) Evaluate(context.Context, json.RawMessage, map[string]Question) (Result, error) {
	return Result{Probabilities: nil, Model: Model, InputTokens: 0, OutputTokens: 0}, ErrUnavailable
}

type Client struct {
	httpClient *http.Client
	apiKey     string
	endpoint   string
}

func New(httpClient *http.Client, apiKey string) *Client {
	return &Client{httpClient: httpClient, apiKey: apiKey, endpoint: "https://api.typesafe.ai/v1/systemone"}
}

func (c *Client) Evaluate(ctx context.Context, state json.RawMessage, questions map[string]Question) (Result, error) {
	result := Result{Probabilities: nil, Model: Model, InputTokens: 0, OutputTokens: 0}
	if !json.Valid(state) || len(questions) == 0 {
		return result, errors.New("invalid typesafe evaluation input")
	}
	body, err := json.Marshal(struct {
		Model     string              `json:"model"`
		State     json.RawMessage     `json:"state"`
		Questions map[string]Question `json:"questions"`
	}{Model: Model, State: state, Questions: questions})
	if err != nil {
		return result, fmt.Errorf("encode typesafe request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("create typesafe request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return result, fmt.Errorf("request typesafe evaluation: %w", err)
	}
	defer res.Body.Close()
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
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return result, errors.New("invalid typesafe response JSON")
	}
	if response.Model != Model || response.Usage == nil || response.Usage.InputTokens < 0 || response.Usage.OutputTokens < 0 || len(response.Answers) != len(questions) {
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
	return Result{Probabilities: probabilities, Model: response.Model, InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens}, nil
}
