package bench

import (
	"context"
	"fmt"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// SpendCapError is the cause of a run stopped by its spend cap.
type SpendCapError struct {
	CapUSD   float64
	SpentUSD float64
}

func (e *SpendCapError) Error() string {
	return fmt.Sprintf("spend cap of $%.2f reached ($%.2f spent), so the run stopped early", e.CapUSD, e.SpentUSD)
}

// WithSpendCap wraps client so a run stops once the cost its completions
// report reaches capUSD. Reaching the cap cancels the returned context with a
// *SpendCapError cause, and later calls fail without reaching the model.
// Calls already in flight finish, so spend can pass the cap by up to one call
// per concurrent question. Unpriced calls add nothing. A cap of zero or less
// disables it. Call stop to release the context.
func WithSpendCap(ctx context.Context, client openrouter.CompletionClient, capUSD float64) (capped context.Context, cappedClient openrouter.CompletionClient, stop context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(ctx)
	stop = func() { cancel(nil) }
	if capUSD <= 0 {
		return ctx, client, stop
	}
	return ctx, &spendCapClient{CompletionClient: client, capUSD: capUSD, cancel: cancel, mu: sync.Mutex{}, spent: 0, reached: nil}, stop
}

type spendCapClient struct {
	openrouter.CompletionClient

	capUSD float64
	cancel context.CancelCauseFunc

	mu      sync.Mutex
	spent   float64
	reached *SpendCapError
}

func (c *spendCapClient) GetCompletion(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	c.mu.Lock()
	reached := c.reached
	c.mu.Unlock()
	if reached != nil {
		return nil, reached
	}
	response, err := c.CompletionClient.GetCompletion(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("capped completion: %w", err)
	}
	if response == nil || response.Usage.Cost == nil {
		return response, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.spent += *response.Usage.Cost
	if c.reached == nil && c.spent >= c.capUSD {
		c.reached = &SpendCapError{CapUSD: c.capUSD, SpentUSD: c.spent}
		c.cancel(c.reached)
	}
	return response, nil
}
