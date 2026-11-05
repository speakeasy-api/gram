# Long-lived HTTP responses

The load balancer in front of the server enforces a total response deadline
on streaming backends (3600s), not an idle timeout. A response that outlives
it is cut with a broken HTTP/2 stream and no final event, whatever the backend
was sending at the time. The server therefore ends every long-lived response
itself, before the deadline, and records how each one ended.

The shared pieces live in `server/internal/httpstream`: the end reasons, the
`gram.http.stream.duration` histogram, the `retry` field written before a
server-initiated close, and the keepalive comment.

## Surfaces

| Response                                                                     | Keepalive                                               | At the maximum age                                                                                                                          | Client behaviour                                                                    |
| ---------------------------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| Remote and tunneled MCP standalone GET stream (`/mcp/{mcpSlug}`)             | Upstream's own keepalives are relayed                   | Closed with an SSE `retry` field                                                                                                            | MCP clients reopen the stream (2025-11-25 polling)                                  |
| Remote and tunneled MCP `subscriptions/listen` POST stream (MCP 2026-07-28)  | Upstream's                                              | Closed after the subscription's `resultType: "complete"` result                                                                             | Client re-subscribes                                                                |
| Remote and tunneled MCP POST stream for a request (`tools/call` and friends) | Upstream's                                              | Closed once the response has been relayed; a request still waiting on its response is left open, because closing the stream would cancel it | Client already has its answer                                                       |
| Chat turn stream (`GET /chat/turnstream`)                                    | `: keepalive` comment every 25s while the turn is quiet | Closed with an SSE `retry` field                                                                                                            | The dashboard reconnects with the last cursor it applied and replays what it missed |
| Chat completions (`POST /chat/completions`)                                  | None added; the model provider's comments are relayed   | Never closed early: a completion cannot be resumed, so cutting it would only truncate the answer                                            | n/a                                                                                 |

Tool SSE passthrough in the hosted tool gateway is buffered by every caller
before it reaches a client, so it is not a long-lived response.

## Configuration

`GRAM_STREAM_MAX_AGE` (`--stream-max-age`, default 55 minutes) is the maximum
age of a stream, measured from the handler's receipt of the request because
that is where the load balancer's deadline starts too. It must stay well under
the deadline: the five minutes of slack cover the upstream forward that
precedes an MCP stream, and the final event reaching the client. Zero or
negative leaves streams open until they end on their own.

## Metric

`gram.http.stream.duration` is a histogram in seconds with these attributes:

- `http.route`: the matched route pattern, such as `/mcp/{mcpSlug}` or
  `/chat/turnstream`.
- `http.request.method`.
- `gram.http.stream.end_reason`: `completed`, `client_closed`,
  `server_shutdown`, `upstream_closed`, `idle_timeout`, `max_age` or `error`.

A stream the load balancer cut looks to the server like the client going
away, so it is recorded as `client_closed`. The bucket boundaries straddle the
maximum age (3300) and the deadline (3600): `client_closed` samples in the
3500 to 3700 buckets are streams the rotation missed.

The remote MCP proxy keeps its own `gram.remote_mcp.proxy.stream.duration` and
`gram.remote_mcp.proxy.streams` instruments, which add the proxy's finer end
reasons and the server identity.

## Maximum duration of a synchronous tool call

A tool call must finish well inside the deadline, because nothing can rotate
a stream that is still waiting on its response.

- Hosted HTTP tools: 60 seconds per attempt (`httpToolCallTimeout` in
  `server/internal/gateway/proxy.go`).
- Speakeasy Functions: 5 minutes of execution in the runner, 6 minutes for the
  call (`functionRunnerCallTimeout`).
- Remote and tunneled MCP `tools/call`: the proxy bounds the connect and
  headers phase at 60 seconds and ends a stream that sends nothing for 60
  seconds (`DefaultNonStreamingTimeout` and `DefaultStreamingTimeout` in
  `server/internal/remotemcp/proxy`). It sets no total bound of its own. An
  upstream that keeps sending progress must still respond within the maximum
  stream age (55 minutes); past the load balancer's deadline the client loses
  the stream and never receives the response.
