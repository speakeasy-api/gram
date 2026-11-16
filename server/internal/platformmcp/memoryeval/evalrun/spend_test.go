package evalrun

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type pricedClient struct {
	openrouter.CompletionClient
	cost *float64
}

func (c pricedClient) GetCompletion(context.Context, openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	var response openrouter.CompletionResponse
	response.Usage.Cost = c.cost
	return &response, nil
}

// The meter keeps what a run spent so a failed run can still record it;
// unpriced calls add nothing.
func TestSpendMeterAddsReportedCost(t *testing.T) {
	t.Parallel()

	cost := 0.25
	priced := &spendMeter{CompletionClient: pricedClient{cost: &cost}, mu: sync.Mutex{}, cost: 0}
	unpriced := &spendMeter{CompletionClient: pricedClient{cost: nil}, mu: sync.Mutex{}, cost: 0}
	for range 2 {
		_, err := priced.GetCompletion(t.Context(), openrouter.CompletionRequest{})
		require.NoError(t, err)
		_, err = unpriced.GetCompletion(t.Context(), openrouter.CompletionRequest{})
		require.NoError(t, err)
	}
	require.InDelta(t, 0.5, priced.total(), 1e-9)
	require.Zero(t, unpriced.total())
}
