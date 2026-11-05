// Package httpstream holds what every long-lived HTTP response in Speakeasy
// shares: the reasons such a stream can end, the histogram that records them,
// and the SSE fields written when the server ends a stream on purpose.
//
// The load balancer in front of the server enforces a total response deadline,
// not an idle one, so a stream that outlives it is cut without a final event.
// Handlers end their streams before that deadline with an SSE retry field, so
// clients reconnect cleanly, and record how every stream ended so the cut-off
// stays observable.
package httpstream

import (
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultMaxAge is how long a long-lived response may stay open before the
	// server ends it. The load balancer's response deadline for streaming
	// backends is 3600s and runs from the moment it receives the request, so
	// the age is measured from handler entry and leaves five minutes for the
	// work that precedes the stream and for the final event to reach the
	// client.
	DefaultMaxAge = 55 * time.Minute

	// HeartbeatInterval is how often a stream the server owns writes a comment
	// while it has nothing else to send. Browsers, proxies and MCP client SDKs
	// give up on a silent stream well before the load balancer would; 25s
	// stays under the 30s idle bound common to them. Relayed streams are not
	// heartbeated: their upstream's keepalives pass through instead.
	HeartbeatInterval = 25 * time.Second

	// RetryMin and RetryJitter set the SSE retry delay sent when the server
	// ends a stream the client should reopen. Clients wait that long before
	// reconnecting. The jitter spreads the reconnects from one replica across
	// two seconds instead of landing them all at once.
	RetryMin    = time.Second
	RetryJitter = 2 * time.Second
)

// Heartbeat is the SSE comment written to keep a quiet stream alive. SSE
// clients discard comment lines, so it reaches no application code.
var Heartbeat = []byte(": keepalive\n\n")

// EndReason names why a long-lived HTTP response ended. It is a metric
// dimension, so the set stays closed and the values stable.
type EndReason string

const (
	// EndCompleted means the stream delivered its final event.
	EndCompleted EndReason = "completed"

	// EndClientClosed means the request's context ended before the stream
	// did: the client went away, or the load balancer cut the response.
	EndClientClosed EndReason = "client_closed"

	// EndServerShutdown means the server ended the stream because it was
	// shutting down.
	EndServerShutdown EndReason = "server_shutdown"

	// EndUpstreamClosed means the source the stream relayed ended it.
	EndUpstreamClosed EndReason = "upstream_closed"

	// EndIdleTimeout means the stream's source sent nothing for its idle
	// bound.
	EndIdleTimeout EndReason = "idle_timeout"

	// EndMaxAge means the server ended the stream because it reached its
	// maximum age.
	EndMaxAge EndReason = "max_age"

	// EndError means the stream failed for any other reason, such as a write
	// to the client failing.
	EndError EndReason = "error"
)

// RetryDelay returns the delay to send in an SSE retry field: RetryMin plus
// up to RetryJitter.
func RetryDelay() time.Duration {
	return RetryMin + time.Duration(rand.Int64N(int64(RetryJitter))) // #nosec G404 -- reconnect jitter is not security-sensitive
}

// RetryField formats the SSE retry field that asks a client to reconnect
// after delay, as a complete event. No event id is written with it: ids must
// stay unique within the client's stream and only the stream's source
// assigns them.
func RetryField(delay time.Duration) []byte {
	return []byte("retry: " + strconv.FormatInt(delay.Milliseconds(), 10) + "\n\n")
}

// Route returns the route pattern the request matched, without the method
// prefix that http.ServeMux patterns carry, so it joins with the http.route
// attribute on the server's request metrics. It is empty for a request that
// matched no pattern.
func Route(r *http.Request) string {
	if idx := strings.IndexByte(r.Pattern, '/'); idx >= 0 {
		return r.Pattern[idx:]
	}
	return ""
}
