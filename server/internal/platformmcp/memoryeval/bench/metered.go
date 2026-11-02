package bench

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// ModelSpend is what one model's completions cost during a step.
type ModelSpend struct {
	Model            string  `json:"model"`
	Calls            int     `json:"calls"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`

	// Unpriced counts calls whose response reported no cost; CostUSD leaves
	// them out, so a nonzero count means the cost is a lower bound.
	Unpriced int `json:"unpriced_calls"`
}

// meteredClient counts tokens and reported cost per model for completions
// made by code the bench does not own, such as the summary generator and the
// digest job.
type meteredClient struct {
	openrouter.CompletionClient

	mu    sync.Mutex
	spend map[string]*ModelSpend
}

func newMeteredClient(client openrouter.CompletionClient) *meteredClient {
	return &meteredClient{CompletionClient: client, mu: sync.Mutex{}, spend: map[string]*ModelSpend{}}
}

func (m *meteredClient) GetCompletion(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	response, err := m.CompletionClient.GetCompletion(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("metered completion: %w", err)
	}
	if response == nil {
		return nil, errors.New("metered completion: empty response")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.spend[request.Model]
	if !ok {
		s = &ModelSpend{Model: request.Model, Calls: 0, PromptTokens: 0, CompletionTokens: 0, CostUSD: 0, Unpriced: 0}
		m.spend[request.Model] = s
	}
	s.Calls++
	s.PromptTokens += response.Usage.PromptTokens
	s.CompletionTokens += response.Usage.CompletionTokens
	if response.Usage.Cost != nil {
		s.CostUSD += *response.Usage.Cost
	} else {
		s.Unpriced++
	}
	return response, nil
}

// take returns the spend for model and resets it.
func (m *meteredClient) take(model string) ModelSpend {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.spend[model]
	if !ok {
		return ModelSpend{Model: model, Calls: 0, PromptTokens: 0, CompletionTokens: 0, CostUSD: 0, Unpriced: 0}
	}
	delete(m.spend, model)
	return *s
}
