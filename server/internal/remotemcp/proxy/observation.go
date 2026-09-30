package proxy

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/speakeasy-api/gram/tunnel/metrics"
)

// RequestObserver receives bounded categories only. It never receives a body,
// a tool name, raw user agent, request ID, credentials, or error text.
type RequestObserver func(method, clientFamily, outcome string, elapsed time.Duration)
type observationKey struct{}
type requestObservation struct {
	once           sync.Once
	record         RequestObserver
	method, client string
	started        time.Time
	id             jsonrpc.ID
}

func (o *requestObservation) finish(outcome string) {
	if o == nil {
		return
	}
	o.once.Do(func() { o.record(o.method, o.client, outcome, time.Since(o.started)) })
}
func observeRejection(ctx context.Context) {
	if o, _ := ctx.Value(observationKey{}).(*requestObservation); o != nil {
		o.finish("error")
	}
}
func observeResponse(ctx context.Context, msg jsonrpc.Message) {
	o, _ := ctx.Value(observationKey{}).(*requestObservation)
	if o == nil {
		return
	}
	response, ok := msg.(*jsonrpc.Response)
	if !ok || !jsonrpcIDsEqual(response.ID, o.id) {
		return
	}
	outcome := "success"
	if response.Error != nil {
		outcome = "error"
	} else if o.method == "tools/call" {
		var result struct {
			IsError bool `json:"isError"`
		}
		if json.Unmarshal(response.Result, &result) != nil {
			o.finish("error")
			return
		}
		if result.IsError {
			outcome = "error"
		}
	}
	o.finish(outcome)
}
func beginObservation(ctx context.Context, record RequestObserver, req *UserRequest) (context.Context, *requestObservation) {
	if record == nil || len(req.JSONRPCMessages) != 1 {
		return ctx, nil
	}
	rpc, ok := req.JSONRPCMessages[0].(*jsonrpc.Request)
	if !ok || !rpc.ID.IsValid() {
		return ctx, nil
	}
	o := &requestObservation{once: sync.Once{}, record: record, method: metrics.Method(rpc.Method), client: metrics.ClientFamily(req.UserHTTPRequest.UserAgent()), started: time.Now(), id: rpc.ID}
	record(o.method, o.client, "attempt", 0)
	return context.WithValue(ctx, observationKey{}, o), o
}
