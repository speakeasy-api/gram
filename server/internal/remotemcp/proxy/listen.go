package proxy

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// listenResultTypeComplete is the resultType of the response that ends a
// subscriptions/listen stream gracefully (MCP 2026-07-28, Subscriptions,
// Graceful Closure).
const listenResultTypeComplete = "complete"

// listenCompletion is the JSON-RPC response that ends a subscriptions/listen
// stream gracefully.
type listenCompletion struct {
	// JSONRPC is always "2.0".
	JSONRPC string `json:"jsonrpc"`

	// ID is the id of the subscriptions/listen request.
	ID any `json:"id"`

	// Result marks the subscription complete.
	Result listenCompletionResult `json:"result"`
}

// listenCompletionResult is the result of a gracefully ended subscription.
type listenCompletionResult struct {
	// ResultType is always [listenResultTypeComplete].
	ResultType string `json:"resultType"`

	// Meta names the subscription that ended.
	Meta listenCompletionMeta `json:"_meta"`
}

// listenCompletionMeta is the _meta of a message on a listen stream.
type listenCompletionMeta struct {
	// SubscriptionID is the JSON-RPC id of the subscriptions/listen request.
	SubscriptionID any `json:"io.modelcontextprotocol/subscriptionId"`
}

// listenCompletionEvent builds the SSE event that ends the subscription
// opened by the subscriptions/listen request with the given id. MCP 2026-07-28
// asks a server that ends a subscription on its own initiative, for example
// during shutdown, to send this result before closing the stream. It does not
// send notifications/cancelled: the schema limits server-sent cancellation to
// stdio.
func listenCompletionEvent(id jsonrpc.ID) ([]byte, error) {
	payload, err := json.Marshal(listenCompletion{
		JSONRPC: "2.0",
		ID:      id.Raw(),
		Result: listenCompletionResult{
			ResultType: listenResultTypeComplete,
			Meta:       listenCompletionMeta{SubscriptionID: id.Raw()},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal subscriptions/listen completion: %w", err)
	}
	return formatSSEDataEvent(payload), nil
}
