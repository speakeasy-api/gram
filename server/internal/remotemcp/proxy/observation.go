package proxy

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/tunnel/metrics"
)

// RequestObserver receives bounded categories only. It never receives a body,
// a tool name, raw user agent, request ID, credentials, or error text.
type RequestObserver func(method, clientFamily, outcome string, elapsed time.Duration)

// SessionClients remembers the MCP client each session reported at initialize,
// so requests later in the session are attributed to it rather than to their
// User-Agent, which Node-based clients leave generic.
type SessionClients interface {
	// RecordInitialize remembers what a session reported at initialize.
	// sessionID is the Mcp-Session-Id the client was answered with.
	RecordInitialize(ctx context.Context, sessionID string, params *mcp.InitializeParams)
	// ClientFamily returns the family recorded for a session, or "" when none
	// was.
	ClientFamily(ctx context.Context, sessionID string) string
}
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

// observeRejection records a request Speakeasy refused before it reached the
// MCP server. It is reported separately so policy decisions do not raise the
// server's error rate.
func observeRejection(ctx context.Context) {
	if o, _ := ctx.Value(observationKey{}).(*requestObservation); o != nil {
		o.finish("rejected")
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
func beginObservation(ctx context.Context, record RequestObserver, sessions SessionClients, req *UserRequest, initialize *InitializeRequest) (context.Context, *requestObservation) {
	if record == nil || len(req.JSONRPCMessages) != 1 {
		return ctx, nil
	}
	rpc, ok := req.JSONRPCMessages[0].(*jsonrpc.Request)
	if !ok || !rpc.ID.IsValid() {
		return ctx, nil
	}
	o := &requestObservation{once: sync.Once{}, record: record, method: metrics.Method(rpc.Method), client: observedClientFamily(ctx, sessions, req, initialize), started: time.Now(), id: rpc.ID}
	record(o.method, o.client, "attempt", 0)
	return context.WithValue(ctx, observationKey{}, o), o
}

// observedClientFamily prefers the client the MCP session reported for itself:
// an initialize request's own clientInfo, otherwise what its session recorded.
// The User-Agent is the fallback when neither names a known client.
func observedClientFamily(ctx context.Context, sessions SessionClients, req *UserRequest, initialize *InitializeRequest) string {
	reported := ""
	switch {
	case initialize != nil:
		if initialize.Params.ClientInfo != nil {
			reported = metrics.ClientFamily(initialize.Params.ClientInfo.Name)
		}
	case sessions != nil:
		if sessionID := req.UserHTTPRequest.Header.Get(McpSessionIDHeader); sessionID != "" {
			reported = sessions.ClientFamily(ctx, sessionID)
		}
	}
	return metrics.PreferredFamily(reported, metrics.ClientFamily(req.UserHTTPRequest.UserAgent()))
}
