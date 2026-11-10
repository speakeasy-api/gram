// Package kitacp implements Kit's ACP v2 HTTP/SSE transport without extensions.
package kitacp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const MaxMessageBytes = 8 << 20

var ErrClosed = errors.New("ACP transport closed")

// TransportError preserves the network cause for errors.Is/As without exposing
// URLs or remote error text in logs. A POST outcome can still be unknown.
type TransportError struct{ cause error }

func (e *TransportError) Error() string { return "ACP HTTP transport failure (outcome may be unknown)" }
func (e *TransportError) Unwrap() error { return e.cause }

// RPCError avoids exposing remote messages or data in Error().
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("ACP RPC error %d", e.Code) }

type envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}
type reply struct {
	result json.RawMessage
	err    error
}
type pending struct {
	session string
	ch      chan reply
	cancel  context.CancelFunc
}

// Notification contains the original v2 payload and source stream.
type Notification struct {
	SessionID string
	Method    string
	Params    json.RawMessage
}
type Client struct {
	attachMu                    sync.Mutex
	http                        *guardian.HTTPClient
	endpoint, token, connection string
	done                        <-chan struct{}
	cancel                      context.CancelFunc
	mu                          sync.Mutex
	next                        uint64
	pending                     map[string]pending
	streams                     map[string]bool
	terminal                    error
	notifications               chan Notification
}

// Dial initializes a connection and opens its connection-level stream. params
// is a standard v2 initialize payload (protocolVersion, info, capabilities).
// ctx bounds setup only; CloseTransport owns the resulting transport lifetime.
// The supplied HTTP client is copied and redirects are always disabled.
func Dial(ctx context.Context, endpoint, token string, hc *guardian.HTTPClient, params any) (*Client, json.RawMessage, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "/acp/v2" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, nil, errors.New("invalid ACP endpoint: expected exact /acp/v2 URL")
	}
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return nil, nil, errors.New("invalid ACP bearer credential")
	}
	if hc == nil || hc.Transport == nil {
		return nil, nil, errors.New("ACP requires a guardian-configured HTTP client")
	}
	h := *hc
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	life, cancel := context.WithCancel(context.Background())
	c := &Client{attachMu: sync.Mutex{}, mu: sync.Mutex{}, connection: "", terminal: nil, http: &h, endpoint: endpoint, token: token, done: life.Done(), cancel: cancel, next: 1, pending: make(map[string]pending), streams: make(map[string]bool), notifications: make(chan Notification, 256)}
	body, err := marshal("initialize", params, json.RawMessage(`1`))
	if err != nil {
		cancel()
		return nil, nil, err
	}
	res, err := c.request(ctx, http.MethodPost, "", body)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		cancel()
		return nil, nil, statusError(res.StatusCode)
	}
	c.connection = res.Header.Get("Acp-Connection-Id")
	if c.connection == "" || len(c.connection) > 4096 {
		cancel()
		return nil, nil, errors.New("ACP initialize missing valid connection ID")
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, MaxMessageBytes+1))
	var msg envelope
	if err != nil || len(raw) > MaxMessageBytes || json.Unmarshal(raw, &msg) != nil || msg.JSONRPC != "2.0" || string(msg.ID) != "1" {
		cancel()
		return nil, nil, errors.New("invalid ACP initialize response")
	}
	if msg.Error != nil {
		cancel()
		return nil, nil, msg.Error
	}
	if len(msg.Result) == 0 {
		cancel()
		return nil, nil, errors.New("missing ACP initialize result")
	}
	if err := c.attach(ctx, ""); err != nil {
		// Initialize already allocated server-side connection state. Caller
		// cancellation must not prevent bounded cleanup of that connection.
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		if closeErr := c.CloseTransport(cleanup); closeErr != nil {
			return nil, nil, errors.Join(err, fmt.Errorf("close unattached ACP connection: %w", closeErr))
		}
		return nil, nil, err
	}
	return c, msg.Result, nil
}
func marshal(method string, params any, id json.RawMessage) ([]byte, error) {
	p, err := json.Marshal(params)
	if err != nil {
		return nil, errors.New("invalid ACP parameters")
	}
	b, err := json.Marshal(envelope{JSONRPC: "2.0", Method: method, Params: p, ID: id, Result: nil, Error: nil})
	if err != nil || len(b) > MaxMessageBytes {
		return nil, errors.New("ACP request exceeds message limit")
	}
	return b, nil
}
func statusError(code int) error { return fmt.Errorf("ACP HTTP status %d", code) }
func (c *Client) request(ctx context.Context, method, session string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid ACP HTTP request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.connection != "" {
		req.Header.Set("Acp-Connection-Id", c.connection)
	}
	if session != "" {
		req.Header.Set("Acp-Session-Id", session)
	}
	switch method {
	case http.MethodGet:
		req.Header.Set("Accept", "text/event-stream")
	case http.MethodPost:
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ACP request canceled: %w", ctx.Err())
		}
		return nil, &TransportError{cause: err}
	}
	return res, nil
}

// Notifications must be drained concurrently with Call, including during replay.
// A full buffer backpressures its SSE reader without dropping updates. Consumers
// must not wait for a Call response before continuing to drain: the response can
// follow notifications on the same stream. The channel is not closed; select on
// Done too.
func (c *Client) Notifications() <-chan Notification { return c.notifications }
func (c *Client) Done() <-chan struct{}              { return c.done }
func (c *Client) Err() error                         { c.mu.Lock(); defer c.mu.Unlock(); return c.terminal }

// AttachSession opens SSE before resume or prompt, or after session/new.
// ctx bounds stream establishment only. Successful calls are idempotent.
func (c *Client) AttachSession(ctx context.Context, session string) error {
	if session == "" || len(session) > 4096 || strings.ContainsAny(session, "\r\n") {
		return errors.New("invalid ACP session ID")
	}
	return c.attach(ctx, session)
}
func (c *Client) attach(ctx context.Context, session string) error {
	c.attachMu.Lock()
	defer c.attachMu.Unlock()
	c.mu.Lock()
	if c.terminal != nil {
		err := c.terminal
		c.mu.Unlock()
		return err
	}
	if c.streams[session] {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	ready := make(chan error, 1)
	go c.serveStream(ctx, session, ready)
	return <-ready
}

// The stream goroutine owns the HTTP response from acquisition through close.
func (c *Client) serveStream(ctx context.Context, session string, ready chan<- error) {
	streamCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	c.cancelWhenClosed(streamCtx, cancel)
	stop := context.AfterFunc(ctx, cancel)
	res, err := c.request(streamCtx, http.MethodGet, session, nil)
	stopped := stop()
	if err != nil {
		ready <- err
		return
	}
	defer func() { _ = res.Body.Close() }()
	if !stopped || ctx.Err() != nil {
		ready <- context.Canceled
		return
	}
	typ, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if res.StatusCode != http.StatusOK || typ != "text/event-stream" {
		ready <- errors.New("ACP event stream rejected")
		return
	}
	c.mu.Lock()
	if c.terminal != nil {
		err := c.terminal
		c.mu.Unlock()
		ready <- err
		return
	}
	c.streams[session] = true
	c.mu.Unlock()
	ready <- nil
	c.fail(c.readSSE(res.Body, session))
}

func (c *Client) cancelWhenClosed(ctx context.Context, cancel context.CancelFunc) {
	go func() {
		select {
		case <-c.done:
			cancel()
		case <-ctx.Done():
		}
	}()
}

// Call correlates an ordinary JSON-RPC request with its SSE response. It never
// retries. Context cancellation stops waiting, not accepted server work.
func (c *Client) Call(ctx context.Context, method string, params any, session string) (json.RawMessage, error) {
	if method == "initialize" || method == "session/cancel" {
		return nil, errors.New("use Dial or Cancel for this ACP method")
	}
	c.mu.Lock()
	if c.terminal != nil {
		err := c.terminal
		c.mu.Unlock()
		return nil, err
	}
	if !c.streams[session] || ((method == "session/resume" || method == "session/prompt") && session == "") {
		c.mu.Unlock()
		return nil, errors.New("ACP session stream must be attached first")
	}
	c.next++
	id := strconv.FormatUint(c.next, 10)
	ch := make(chan reply, 1)
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.pending[id] = pending{session: session, ch: ch, cancel: cancel}
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	body, err := marshal(method, params, json.RawMessage(id))
	if err != nil {
		return nil, err
	}
	res, err := c.request(reqCtx, http.MethodPost, session, body)
	if err != nil {
		return nil, err
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		return nil, statusError(res.StatusCode)
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("ACP request canceled: %w", ctx.Err())
	case <-c.done:
		return nil, c.Err()
	}
}

// Cancel sends standard v2 session/cancel, a notification without a request ID.
func (c *Client) Cancel(ctx context.Context, session string) error {
	c.mu.Lock()
	attached := c.streams[session]
	err := c.terminal
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if session == "" || !attached {
		return errors.New("ACP session stream must be attached first")
	}
	body, err := marshal("session/cancel", struct {
		SessionID string `json:"sessionId"`
	}{session}, nil)
	if err != nil {
		return err
	}
	res, err := c.request(ctx, http.MethodPost, session, body)
	if err != nil {
		return err
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		return statusError(res.StatusCode)
	}
	return nil
}

// CloseTransport tears down only this connection; never session work or history.
func (c *Client) CloseTransport(ctx context.Context) error {
	c.fail(ErrClosed)
	res, err := c.request(ctx, http.MethodDelete, "", nil)
	if err != nil {
		return err
	}
	_ = res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return statusError(res.StatusCode)
	}
	return nil
}
func (c *Client) fail(err error) {
	if err == nil {
		err = errors.New("ACP event stream ended")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.terminal != nil {
		return
	}
	c.terminal = err
	c.cancel()
	for _, p := range c.pending {
		if p.cancel != nil {
			p.cancel()
		}
		select {
		case p.ch <- reply{err: err, result: nil}:
		default:
		}
	}
}

// readSSE handles multiline data and comments, ignoring IDs because Kit does not
// promise Last-Event-ID replay. Bounds apply to whole frames, not just lines.
func (c *Client) readSSE(r io.Reader, session string) error {
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 4096), MaxMessageBytes)
	var data []byte
	size := 0
	for scan.Scan() {
		line := scan.Text()
		size += len(line) + 1
		if size > MaxMessageBytes {
			return errors.New("ACP SSE frame exceeds limit")
		}
		if line == "" {
			if len(data) > 0 {
				if err := c.dispatch(bytes.TrimSuffix(data, []byte{'\n'}), session); err != nil {
					return err
				}
			}
			data = nil
			size = 0
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		if field == "data" {
			data = append(data, value...)
			data = append(data, '\n')
		}
	}
	if scan.Err() != nil {
		return errors.New("ACP SSE read failed or frame exceeds limit")
	}
	return errors.New("ACP event stream ended")
}
func (c *Client) dispatch(data []byte, session string) error {
	var m envelope
	if json.Unmarshal(data, &m) != nil || m.JSONRPC != "2.0" {
		return errors.New("invalid ACP SSE JSON-RPC message")
	}
	if m.Method != "" {
		if len(m.ID) > 0 {
			return errors.New("unsupported ACP server request")
		}
		select {
		case c.notifications <- Notification{session, m.Method, m.Params}:
			return nil
		case <-c.done:
			return ErrClosed
		}
	}
	if len(m.ID) == 0 || (m.Error == nil && len(m.Result) == 0) {
		return errors.New("invalid ACP SSE response")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.pending[string(m.ID)]
	if !ok {
		return nil
	}
	if p.session != session {
		return errors.New("ACP response arrived on wrong stream")
	}
	var err error
	if m.Error != nil {
		err = m.Error
	}
	select {
	case p.ch <- reply{m.Result, err}:
	default:
	}
	return nil
}
