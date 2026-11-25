package evalrun

import (
	"context"
	"fmt"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// spendMeter adds up the reported cost of every completion one run makes, so
// a run that fails partway still records what it spent.
type spendMeter struct {
	openrouter.CompletionClient

	mu   sync.Mutex
	cost float64
}

func (m *spendMeter) GetCompletion(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	response, err := m.CompletionClient.GetCompletion(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("eval completion: %w", err)
	}
	if response != nil && response.Usage.Cost != nil {
		m.mu.Lock()
		m.cost += *response.Usage.Cost
		m.mu.Unlock()
	}
	return response, nil
}

func (m *spendMeter) total() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cost
}
