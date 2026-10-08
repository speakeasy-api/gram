package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultStdioMaxSessions = 16
	defaultStdioIdleTimeout = 30 * time.Minute
	// stdioInitializeTimeout bounds how long a freshly spawned server may take to answer initialize.
	stdioInitializeTimeout = 60 * time.Second
	// stdioShutdownGrace is how long a server gets after stdin closes before SIGTERM, then again before SIGKILL.
	stdioShutdownGrace = 3 * time.Second
	// stdioExitDrain bounds how long stdout may stay open after the process exits (e.g. held by a grandchild).
	stdioExitDrain       = 2 * time.Second
	stdioMaxRequestBytes = 32 << 20
	stdioBacklogLimit    = 256
	stdioStreamBuffer    = 64

	headerMCPSessionID = "Mcp-Session-Id"

	rpcCodeParseError     = -32700
	rpcCodeInvalidRequest = -32600
	rpcCodeServerError    = -32000
	rpcCodeSessionMissing = -32001
)

var (
	errStdioAtCapacity    = errors.New("stdio session capacity reached")
	errStdioBridgeClosed  = errors.New("stdio bridge closed")
	errStdioSessionClosed = errors.New("stdio session closed")
	errStdioDuplicateID   = errors.New("duplicate in-flight JSON-RPC id")
)

// stdioBridge serves MCP Streamable HTTP by spawning one stdio MCP server
// process per MCP session. Messages are relayed verbatim so Gram's proxy sees
// the server's own JSON-RPC payloads.
type stdioBridge struct {
	command     string
	env         []string
	maxSessions int
	idleTimeout time.Duration
	logger      *slog.Logger

	mu       sync.Mutex
	sessions map[string]*stdioSession
	closed   bool
}

func newStdioBridge(command string, maxSessions int, idleTimeout time.Duration, logger *slog.Logger) *stdioBridge {
	if maxSessions <= 0 {
		maxSessions = defaultStdioMaxSessions
	}
	if idleTimeout <= 0 {
		idleTimeout = defaultStdioIdleTimeout
	}
	return &stdioBridge{
		command:     command,
		env:         childEnv(os.Environ()),
		maxSessions: maxSessions,
		idleTimeout: idleTimeout,
		logger:      logger,
		sessions:    make(map[string]*stdioSession),
		closed:      false,
	}
}

// childEnv strips the agent's own TUNNEL_* settings so the tunnel key never
// reaches the MCP server process.
func childEnv(environ []string) []string {
	env := make([]string, 0, len(environ))
	for _, kv := range environ {
		if strings.HasPrefix(kv, "TUNNEL_") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func (b *stdioBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// MCP traffic arrives at the gateway root. Anything else (e.g. OAuth
	// back-channel paths) has no stdio equivalent.
	if r.URL.Path != "/" && r.URL.Path != "" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPost:
		b.handlePost(w, r)
	case http.MethodGet:
		b.handleGet(w, r)
	case http.MethodDelete:
		b.handleDelete(w, r)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (b *stdioBridge) handlePost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, stdioMaxRequestBytes+1))
	if err != nil {
		writeRPCError(w, http.StatusBadRequest, nil, rpcCodeParseError, "failed to read request body")
		return
	}
	if len(body) > stdioMaxRequestBytes {
		writeRPCError(w, http.StatusRequestEntityTooLarge, nil, rpcCodeInvalidRequest, "request body too large")
		return
	}
	msgs, batch, err := parseRPCPayload(body)
	if err != nil {
		writeRPCError(w, http.StatusBadRequest, nil, rpcCodeParseError, "invalid JSON-RPC payload")
		return
	}

	sid := r.Header.Get(headerMCPSessionID)
	if sid == "" {
		b.handleInitialize(w, r, msgs, batch)
		return
	}
	sess := b.session(sid)
	if sess == nil {
		writeRPCError(w, http.StatusNotFound, nil, rpcCodeSessionMissing, "session not found")
		return
	}
	release := sess.acquire()
	defer release()

	ids := requestIDs(msgs)
	if len(ids) == 0 {
		if err := sess.send(msgs); err != nil {
			writeRPCError(w, http.StatusNotFound, nil, rpcCodeSessionMissing, "session not found")
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	sse := acceptsSSE(r)
	stream, err := sess.openStream(ids, sse)
	if err != nil {
		if errors.Is(err, errStdioDuplicateID) {
			writeRPCError(w, http.StatusBadRequest, nil, rpcCodeInvalidRequest, err.Error())
			return
		}
		writeRPCError(w, http.StatusNotFound, nil, rpcCodeSessionMissing, "session not found")
		return
	}
	defer sess.closeStream(stream)

	if err := sess.send(msgs); err != nil {
		writeRPCError(w, http.StatusNotFound, nil, rpcCodeSessionMissing, "session not found")
		return
	}

	if sse {
		writeSSEHeaders(w, sid)
		for msg := range stream.until(r.Context(), sess, len(ids)) {
			if err := writeSSEEvent(w, msg); err != nil {
				return
			}
		}
		return
	}

	responses := make([]json.RawMessage, 0, len(ids))
	for msg := range stream.until(r.Context(), sess, len(ids)) {
		responses = append(responses, msg)
	}
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set(headerMCPSessionID, sid)
	writeJSONResponses(w, responses, batch)
}

func (b *stdioBridge) handleInitialize(w http.ResponseWriter, r *http.Request, msgs []rpcMessage, batch bool) {
	if batch || len(msgs) != 1 || msgs[0].method != "initialize" || msgs[0].id == "" {
		writeRPCError(w, http.StatusBadRequest, nil, rpcCodeInvalidRequest, "Bad Request: Mcp-Session-Id header is required")
		return
	}

	sess, err := b.start()
	switch {
	case errors.Is(err, errStdioAtCapacity):
		w.Header().Set("Retry-After", "5")
		writeRPCError(w, http.StatusServiceUnavailable, msgs[0].rawID, rpcCodeServerError, "MCP server is at capacity")
		return
	case err != nil:
		b.logger.Warn("tunnel stdio server failed to start", slog.Any("error", err))
		writeRPCError(w, http.StatusBadGateway, msgs[0].rawID, rpcCodeServerError, "MCP server failed to start")
		return
	}
	release := sess.acquire()
	defer release()

	committed := false
	defer func() {
		if !committed {
			sess.close()
		}
	}()

	stream, err := sess.openStream([]string{msgs[0].id}, false)
	if err != nil {
		writeRPCError(w, http.StatusBadGateway, msgs[0].rawID, rpcCodeServerError, "MCP server exited during initialize")
		return
	}
	defer sess.closeStream(stream)
	if err := sess.send(msgs); err != nil {
		writeRPCError(w, http.StatusBadGateway, msgs[0].rawID, rpcCodeServerError, "MCP server exited during initialize")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), stdioInitializeTimeout)
	defer cancel()
	var response json.RawMessage
	for msg := range stream.until(ctx, sess, 1) {
		response = msg
	}
	if response == nil {
		if r.Context().Err() != nil {
			return
		}
		writeRPCError(w, http.StatusGatewayTimeout, msgs[0].rawID, rpcCodeServerError, "MCP server did not answer initialize")
		return
	}

	if isRPCError(response) {
		writeJSONResponses(w, []json.RawMessage{response}, false)
		return
	}
	committed = true
	sess.logger.Info("tunnel stdio session started")
	w.Header().Set(headerMCPSessionID, sess.id)
	writeJSONResponses(w, []json.RawMessage{response}, false)
}

func (b *stdioBridge) handleGet(w http.ResponseWriter, r *http.Request) {
	sid := r.Header.Get(headerMCPSessionID)
	if sid == "" {
		writeRPCError(w, http.StatusBadRequest, nil, rpcCodeInvalidRequest, "Bad Request: Mcp-Session-Id header is required")
		return
	}
	sess := b.session(sid)
	if sess == nil {
		writeRPCError(w, http.StatusNotFound, nil, rpcCodeSessionMissing, "session not found")
		return
	}
	release := sess.acquire()
	defer release()

	listener, backlog, err := sess.attachListener()
	if err != nil {
		writeRPCError(w, http.StatusConflict, nil, rpcCodeInvalidRequest, "a GET stream is already open for this session")
		return
	}
	defer sess.detachListener(listener)

	writeSSEHeaders(w, sid)
	for _, msg := range backlog {
		if err := writeSSEEvent(w, msg); err != nil {
			return
		}
	}
	for msg := range listener.until(r.Context(), sess, -1) {
		if err := writeSSEEvent(w, msg); err != nil {
			return
		}
	}
}

func (b *stdioBridge) handleDelete(w http.ResponseWriter, r *http.Request) {
	sid := r.Header.Get(headerMCPSessionID)
	if sid == "" {
		writeRPCError(w, http.StatusBadRequest, nil, rpcCodeInvalidRequest, "Bad Request: Mcp-Session-Id header is required")
		return
	}
	sess := b.session(sid)
	if sess == nil {
		writeRPCError(w, http.StatusNotFound, nil, rpcCodeSessionMissing, "session not found")
		return
	}
	sess.logger.Info("tunnel stdio session terminated by client")
	sess.close()
	w.WriteHeader(http.StatusNoContent)
}

func (b *stdioBridge) session(id string) *stdioSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[id]
}

func (b *stdioBridge) remove(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessions, id)
}

func (b *stdioBridge) start() (*stdioSession, error) {
	id, err := newStdioSessionID()
	if err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, errStdioBridgeClosed
	}
	if len(b.sessions) >= b.maxSessions {
		return nil, errStdioAtCapacity
	}

	sess, err := startStdioSession(id, b.command, b.env, b.logger.With(slog.String("stdio_session", logSessionID(id))))
	if err != nil {
		return nil, err
	}
	b.sessions[id] = sess
	go func() {
		<-sess.done
		b.remove(id)
	}()
	return sess, nil
}

// reap closes sessions that have been idle for longer than the idle timeout.
func (b *stdioBridge) reap(ctx context.Context) {
	interval := min(b.idleTimeout/4, time.Minute)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			b.mu.Lock()
			idle := make([]*stdioSession, 0)
			for _, sess := range b.sessions {
				if sess.idleSince(now) > b.idleTimeout {
					idle = append(idle, sess)
				}
			}
			b.mu.Unlock()
			for _, sess := range idle {
				sess.logger.Info("tunnel stdio session idle; stopping server")
				sess.close()
			}
		}
	}
}

// Close stops every server process and waits (bounded) for them to exit.
func (b *stdioBridge) Close() {
	b.mu.Lock()
	b.closed = true
	sessions := make([]*stdioSession, 0, len(b.sessions))
	for _, sess := range b.sessions {
		sessions = append(sessions, sess)
	}
	b.mu.Unlock()

	deadline := time.After(2*stdioShutdownGrace + stdioExitDrain + time.Second)
	for _, sess := range sessions {
		sess.close()
	}
	for _, sess := range sessions {
		select {
		case <-sess.done:
		case <-deadline:
			return
		}
	}
}

type stdioSession struct {
	id     string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	logger *slog.Logger

	writeMu sync.Mutex

	// exited closes when the process has been reaped; done closes after stdout
	// has drained too, so final messages reach their streams before teardown.
	exited    chan struct{}
	done      chan struct{}
	closeOnce sync.Once

	mu         sync.Mutex
	pending    map[string]*rpcStream
	streams    map[*rpcStream]struct{}
	listener   *rpcStream
	backlog    []json.RawMessage
	inflight   int
	lastActive time.Time
}

func startStdioSession(id, command string, env []string, logger *slog.Logger) (*stdioSession, error) {
	cmd := shellCommand(command)
	cmd.Env = env
	configureProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	if err := cmd.Start(); err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
		return nil, fmt.Errorf("start %q: %w", command, err)
	}
	_ = stdoutW.Close()
	_ = stderrW.Close()

	s := &stdioSession{
		id:         id,
		cmd:        cmd,
		stdin:      stdin,
		logger:     logger.With(slog.Int("pid", cmd.Process.Pid)),
		writeMu:    sync.Mutex{},
		exited:     make(chan struct{}),
		done:       make(chan struct{}),
		closeOnce:  sync.Once{},
		mu:         sync.Mutex{},
		pending:    make(map[string]*rpcStream),
		streams:    make(map[*rpcStream]struct{}),
		listener:   nil,
		backlog:    nil,
		inflight:   0,
		lastActive: time.Now(),
	}

	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		s.readStdout(stdoutR)
	}()
	go s.logStderr(stderrR)
	go func() {
		err := cmd.Wait()
		close(s.exited)
		select {
		case <-stdoutDone:
		case <-time.After(stdioExitDrain):
			_ = stdoutR.Close()
		}
		s.logger.Info("tunnel stdio server exited", slog.String("status", exitStatus(err)))
		close(s.done)
	}()
	return s, nil
}

func (s *stdioSession) acquire() func() {
	s.mu.Lock()
	s.inflight++
	s.lastActive = time.Now()
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.inflight--
		s.lastActive = time.Now()
		s.mu.Unlock()
	}
}

func (s *stdioSession) idleSince(now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight > 0 {
		return 0
	}
	return now.Sub(s.lastActive)
}

func (s *stdioSession) send(msgs []rpcMessage) error {
	select {
	case <-s.done:
		return errStdioSessionClosed
	default:
	}
	var buf bytes.Buffer
	for _, msg := range msgs {
		buf.Write(msg.raw)
		buf.WriteByte('\n')
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.stdin.Write(buf.Bytes()); err != nil {
		return errors.Join(errStdioSessionClosed, err)
	}
	return nil
}

func (s *stdioSession) openStream(ids []string, unsolicited bool) (*rpcStream, error) {
	stream := newRPCStream()
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.done:
		return nil, errStdioSessionClosed
	default:
	}
	for _, id := range ids {
		if _, exists := s.pending[id]; exists {
			return nil, errStdioDuplicateID
		}
	}
	for _, id := range ids {
		s.pending[id] = stream
	}
	if unsolicited {
		s.streams[stream] = struct{}{}
	}
	return stream, nil
}

func (s *stdioSession) closeStream(stream *rpcStream) {
	s.mu.Lock()
	for id, owner := range s.pending {
		if owner == stream {
			delete(s.pending, id)
		}
	}
	delete(s.streams, stream)
	s.mu.Unlock()
	stream.close()
}

func (s *stdioSession) attachListener() (*rpcStream, []json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return nil, nil, errors.New("listener already attached")
	}
	s.listener = newRPCStream()
	backlog := s.backlog
	s.backlog = nil
	return s.listener, backlog, nil
}

func (s *stdioSession) detachListener(listener *rpcStream) {
	s.mu.Lock()
	if s.listener == listener {
		s.listener = nil
	}
	s.mu.Unlock()
	listener.close()
}

func (s *stdioSession) readStdout(r io.Reader) {
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			msgs, _, perr := parseRPCPayload(trimmed)
			if perr != nil {
				s.logger.Warn("tunnel stdio server wrote a non JSON-RPC line to stdout", slog.Int("bytes", len(trimmed)))
			}
			for _, msg := range msgs {
				s.route(msg)
			}
		}
		if err != nil {
			return
		}
	}
}

// route delivers one server message: responses go to the stream awaiting
// their id; server-initiated requests and notifications prefer the GET
// stream, then any open SSE POST stream, then a bounded backlog.
func (s *stdioSession) route(msg rpcMessage) {
	s.mu.Lock()
	if msg.method == "" && msg.id != "" {
		stream, ok := s.pending[msg.id]
		if ok {
			delete(s.pending, msg.id)
		}
		s.mu.Unlock()
		if !ok {
			s.logger.Warn("tunnel stdio server answered an unknown request id")
			return
		}
		stream.deliver(rpcEvent{msg: msg.raw, response: true})
		return
	}

	target := s.listener
	if target == nil {
		for stream := range s.streams {
			target = stream
			break
		}
	}
	if target == nil {
		if len(s.backlog) >= stdioBacklogLimit {
			s.backlog = s.backlog[1:]
			s.logger.Warn("tunnel stdio backlog full; dropping oldest server message")
		}
		s.backlog = append(s.backlog, msg.raw)
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	target.deliver(rpcEvent{msg: msg.raw, response: false})
}

func (s *stdioSession) logStderr(r io.ReadCloser) {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), 64<<10)
	for scanner.Scan() {
		s.logger.Info("tunnel stdio server stderr", slog.String("line", scanner.Text()))
	}
	// Keep draining after an over-long line so the server never blocks on stderr.
	_, _ = io.Copy(io.Discard, r)
}

// close shuts the server down the way the MCP stdio transport prescribes:
// close stdin, then SIGTERM, then SIGKILL.
func (s *stdioSession) close() {
	s.closeOnce.Do(func() {
		_ = s.stdin.Close()
		go func() {
			select {
			case <-s.exited:
				return
			case <-time.After(stdioShutdownGrace):
			}
			terminateProcessGroup(s.cmd)
			select {
			case <-s.exited:
				return
			case <-time.After(stdioShutdownGrace):
			}
			killProcessGroup(s.cmd)
		}()
	})
}

type rpcEvent struct {
	msg      json.RawMessage
	response bool
}

type rpcStream struct {
	ch       chan rpcEvent
	gone     chan struct{}
	goneOnce sync.Once
}

func newRPCStream() *rpcStream {
	return &rpcStream{
		ch:       make(chan rpcEvent, stdioStreamBuffer),
		gone:     make(chan struct{}),
		goneOnce: sync.Once{},
	}
}

func (s *rpcStream) deliver(event rpcEvent) {
	select {
	case s.ch <- event:
	case <-s.gone:
	}
}

func (s *rpcStream) close() {
	s.goneOnce.Do(func() { close(s.gone) })
}

// until yields messages until wantResponses responses have arrived (-1 for
// never), the context ends, or the session's process exits.
func (s *rpcStream) until(ctx context.Context, sess *stdioSession, wantResponses int) <-chan json.RawMessage {
	out := make(chan json.RawMessage)
	go func() {
		defer close(out)
		received := 0
		for wantResponses < 0 || received < wantResponses {
			var event rpcEvent
			select {
			case event = <-s.ch:
			case <-ctx.Done():
				return
			case <-sess.done:
				// Flush anything routed before the exit.
				select {
				case event = <-s.ch:
				default:
					return
				}
			case <-s.gone:
				return
			}
			if event.response {
				received++
			}
			select {
			case out <- event.msg:
			case <-ctx.Done():
				return
			case <-s.gone:
				return
			}
		}
	}()
	return out
}

type rpcMessage struct {
	raw    json.RawMessage
	rawID  json.RawMessage
	id     string
	method string
}

func parseRPCPayload(body []byte) ([]rpcMessage, bool, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, false, errors.New("empty payload")
	}
	if body[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, true, err
		}
		if len(items) == 0 {
			return nil, true, errors.New("empty batch")
		}
		msgs := make([]rpcMessage, 0, len(items))
		for _, item := range items {
			msg, err := parseRPCMessage(item)
			if err != nil {
				return nil, true, err
			}
			msgs = append(msgs, msg)
		}
		return msgs, true, nil
	}
	msg, err := parseRPCMessage(body)
	if err != nil {
		return nil, false, err
	}
	return []rpcMessage{msg}, false, nil
}

func parseRPCMessage(raw []byte) (rpcMessage, error) {
	var envelope struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return rpcMessage{}, err
	}
	// Compacting guarantees one message per line on the stdio side.
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return rpcMessage{}, err
	}
	msg := rpcMessage{raw: compact.Bytes(), rawID: nil, id: "", method: envelope.Method}
	if id := bytes.TrimSpace(envelope.ID); len(id) > 0 && !bytes.Equal(id, []byte("null")) {
		var compactID bytes.Buffer
		if err := json.Compact(&compactID, id); err != nil {
			return rpcMessage{}, err
		}
		msg.rawID = compactID.Bytes()
		msg.id = compactID.String()
	}
	return msg, nil
}

func requestIDs(msgs []rpcMessage) []string {
	ids := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		if msg.method != "" && msg.id != "" {
			ids = append(ids, msg.id)
		}
	}
	return ids
}

func isRPCError(raw json.RawMessage) bool {
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	return json.Unmarshal(raw, &envelope) == nil && len(envelope.Error) > 0 && !bytes.Equal(envelope.Error, []byte("null"))
}

func acceptsSSE(r *http.Request) bool {
	for _, value := range r.Header.Values("Accept") {
		if strings.Contains(value, "text/event-stream") {
			return true
		}
	}
	return false
}

func writeSSEHeaders(w http.ResponseWriter, sid string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set(headerMCPSessionID, sid)
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func writeSSEEvent(w http.ResponseWriter, msg json.RawMessage) error {
	if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

func writeJSONResponses(w http.ResponseWriter, responses []json.RawMessage, batch bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if !batch && len(responses) == 1 {
		_, _ = w.Write(responses[0])
		return
	}
	payload, _ := json.Marshal(responses)
	_, _ = w.Write(payload)
}

func writeRPCError(w http.ResponseWriter, status int, id json.RawMessage, code int, message string) {
	if id == nil {
		id = json.RawMessage("null")
	}
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func newStdioSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate stdio session id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// logSessionID keeps raw session ids, which act as bearer handles, out of logs.
func logSessionID(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:4])
}

func exitStatus(err error) string {
	if err == nil {
		return "0"
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return strconv.Itoa(exitErr.ExitCode())
	}
	return err.Error()
}
