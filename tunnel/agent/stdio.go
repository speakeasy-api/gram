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
	"iter"
	"log/slog"
	"math/big"
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
	// stdioInitializeTimeout bounds delivering initialize to a fresh server and receiving its answer.
	stdioInitializeTimeout = 60 * time.Second
	// stdioShutdownGrace is how long a server gets after stdin closes before SIGTERM, then again before SIGKILL.
	stdioShutdownGrace = 3 * time.Second
	// stdioExitDrain bounds how long stdout may stay open after the process exits (e.g. held by a grandchild).
	stdioExitDrain = 2 * time.Second
	// stdioPingTimeout bounds the bridge's own answer to a ping sent during initialize.
	stdioPingTimeout = 5 * time.Second
	// stdioPingQueue caps pings awaiting the bridge's answer during initialize.
	stdioPingQueue = 8
	// stdioKillWait bounds waiting for a process group to vanish after SIGKILL.
	stdioKillWait = time.Second

	// stdioMaxMessageBytes caps one JSON-RPC payload in either direction.
	stdioMaxMessageBytes = 32 << 20
	// stdioStreamMaxBytes caps what one HTTP stream may have queued but unsent;
	// a stream past it is a slow consumer and is cut off.
	stdioStreamMaxBytes = 64 << 20
	// stdioBacklogMaxBytes caps server messages held for a GET stream that is not open.
	stdioBacklogMaxBytes = 8 << 20

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
	errStdioFrameTooLarge = errors.New("stdio server message exceeds size limit")
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
	body, err := io.ReadAll(io.LimitReader(r.Body, stdioMaxMessageBytes+1))
	if err != nil {
		writeRPCError(w, http.StatusBadRequest, nil, rpcCodeParseError, "failed to read request body")
		return
	}
	if len(body) > stdioMaxMessageBytes {
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
		if err := sess.send(r.Context(), msgs); err != nil {
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

	if err := sess.send(r.Context(), msgs); err != nil {
		writeRPCError(w, http.StatusNotFound, nil, rpcCodeSessionMissing, "session not found")
		return
	}

	if sse {
		writeSSEHeaders(w, sid)
		for msg := range stream.events(r.Context(), sess, len(ids)) {
			if err := writeSSEEvent(w, msg); err != nil {
				return
			}
		}
		return
	}

	responses := make([]json.RawMessage, 0, len(ids))
	for msg := range stream.events(r.Context(), sess, len(ids)) {
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
		sess.endInitialize()
		if !committed {
			sess.close()
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), stdioInitializeTimeout)
	defer cancel()

	stream, err := sess.openStream([]string{msgs[0].id}, false)
	if err != nil {
		writeRPCError(w, http.StatusBadGateway, msgs[0].rawID, rpcCodeServerError, "MCP server exited during initialize")
		return
	}
	defer sess.closeStream(stream)
	if err := sess.send(ctx, msgs); err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeRPCError(w, http.StatusBadGateway, msgs[0].rawID, rpcCodeServerError, "MCP server did not accept initialize")
		return
	}

	var response json.RawMessage
	for msg := range stream.events(ctx, sess, 1) {
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

	listener, err := sess.attachListener()
	if err != nil {
		writeRPCError(w, http.StatusConflict, nil, rpcCodeInvalidRequest, "a GET stream is already open for this session")
		return
	}
	defer sess.detachListener(listener)

	writeSSEHeaders(w, sid)
	for msg := range listener.events(r.Context(), sess, -1) {
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

	// writeSem serializes stdin writes; a channel rather than a mutex so a
	// waiting writer can give up when its request is cancelled.
	writeSem chan struct{}

	// exited closes when the process has been reaped; terminated when its
	// whole process group is gone; done after stdout has drained too, so
	// final messages reach their streams before teardown.
	exited     chan struct{}
	terminated chan struct{}
	done       chan struct{}
	closeOnce  sync.Once

	// pings queues ids of pings to answer during initialize; bounded so a
	// flood of them is dropped rather than buffered.
	pings chan json.RawMessage

	mu           sync.Mutex
	initializing bool
	pending      map[string]*rpcStream
	// streams holds SSE POST streams still awaiting a response; they may carry
	// server messages when no GET stream is open.
	streams      map[*rpcStream]struct{}
	listener     *rpcStream
	backlog      []json.RawMessage
	backlogBytes int
	inflight     int
	lastActive   time.Time
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
		// The command itself is left out: it commonly carries credentials.
		return nil, fmt.Errorf("start stdio server: %w", err)
	}
	_ = stdoutW.Close()
	_ = stderrW.Close()

	s := &stdioSession{
		id:           id,
		cmd:          cmd,
		stdin:        stdin,
		logger:       logger.With(slog.Int("pid", cmd.Process.Pid)),
		writeSem:     make(chan struct{}, 1),
		exited:       make(chan struct{}),
		terminated:   make(chan struct{}),
		done:         make(chan struct{}),
		closeOnce:    sync.Once{},
		pings:        make(chan json.RawMessage, stdioPingQueue),
		mu:           sync.Mutex{},
		initializing: true,
		pending:      make(map[string]*rpcStream),
		streams:      make(map[*rpcStream]struct{}),
		listener:     nil,
		backlog:      nil,
		backlogBytes: 0,
		inflight:     0,
		lastActive:   time.Now(),
	}

	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		s.readStdout(stdoutR)
	}()
	go s.logStderr(stderrR)
	go s.answerPings()
	go func() {
		err := cmd.Wait()
		close(s.exited)
		// Children of the server can outlive it; the session is not done
		// until the whole group is gone.
		s.close()
		<-s.terminated
		select {
		case <-stdoutDone:
		case <-time.After(stdioExitDrain):
		}
		_ = stdoutR.Close()
		_ = stderrR.Close()
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

func (s *stdioSession) endInitialize() {
	s.mu.Lock()
	s.initializing = false
	s.mu.Unlock()
}

// send writes messages to the server's stdin. A write that cannot finish
// before ctx ends closes the session: a partly written message would corrupt
// the framing of everything after it.
func (s *stdioSession) send(ctx context.Context, msgs []rpcMessage) error {
	select {
	case s.writeSem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return errStdioSessionClosed
	}
	defer func() { <-s.writeSem }()

	var buf bytes.Buffer
	for _, msg := range msgs {
		buf.Write(msg.raw)
		buf.WriteByte('\n')
	}
	written := make(chan error, 1)
	go func() {
		_, err := s.stdin.Write(buf.Bytes())
		written <- err
	}()
	select {
	case err := <-written:
		if err != nil {
			return errors.Join(errStdioSessionClosed, err)
		}
		return nil
	case <-ctx.Done():
	case <-s.done:
	}
	select {
	case err := <-written:
		if err != nil {
			return errors.Join(errStdioSessionClosed, err)
		}
		return nil
	default:
	}
	s.logger.Warn("tunnel stdio server is not reading stdin; stopping server")
	s.close()
	// Closing stdin unblocks the write; wait so the next writer starts clean.
	select {
	case <-written:
	case <-time.After(stdioShutdownGrace):
	}
	return errStdioSessionClosed
}

func (s *stdioSession) openStream(ids []string, unsolicited bool) (*rpcStream, error) {
	stream := newRPCStream(len(ids))
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

// attachListener opens the session's GET stream, preloaded with the backlog.
func (s *stdioSession) attachListener() (*rpcStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return nil, errors.New("listener already attached")
	}
	listener := newRPCStream(0)
	for _, msg := range s.backlog {
		listener.deliver(rpcEvent{msg: msg, response: false})
	}
	s.backlog = nil
	s.backlogBytes = 0
	s.listener = listener
	return listener, nil
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
		line, err := readFrame(reader, stdioMaxMessageBytes)
		if errors.Is(err, errStdioFrameTooLarge) {
			s.logger.Warn("tunnel stdio server wrote a message over the size limit; stopping server", slog.Int("limit_bytes", stdioMaxMessageBytes))
			s.close()
			return
		}
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

// readFrame reads one newline-terminated line of at most limit bytes.
func readFrame(r *bufio.Reader, limit int) ([]byte, error) {
	var frame []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(frame)+len(chunk) > limit {
			return nil, errStdioFrameTooLarge
		}
		frame = append(frame, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return frame, err
	}
}

// route delivers one server message without ever waiting on a consumer, so a
// slow stream cannot stall the session's single stdout reader. Responses go
// to the stream awaiting their id; server-initiated requests and
// notifications prefer the GET stream, then an SSE POST stream still awaiting
// responses, then a bounded backlog for the next GET stream. Delivery never
// blocks, so all of it runs under s.mu and cannot race a GET stream being
// replaced.
func (s *stdioSession) route(msg rpcMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if msg.method == "" && msg.id != "" {
		stream, ok := s.pending[msg.id]
		if !ok {
			s.logger.Warn("tunnel stdio server answered an unknown request id")
			return
		}
		delete(s.pending, msg.id)
		stream.remaining--
		if stream.remaining == 0 {
			// Its handler stops reading after this response, so it must not
			// be picked for any later server message.
			delete(s.streams, stream)
		}
		if !stream.deliver(rpcEvent{msg: msg.raw, response: true}) {
			s.logger.Warn("tunnel stdio response dropped: its HTTP stream is gone or over its buffer limit")
		}
		return
	}

	// Servers may ping before initialize completes, when the client has no
	// way to answer yet, so the bridge answers for it.
	if s.initializing && msg.method == "ping" && msg.id != "" {
		select {
		case s.pings <- msg.rawID:
		default:
			s.logger.Warn("tunnel stdio server sent too many pings during initialize; dropping one")
		}
		return
	}

	event := rpcEvent{msg: msg.raw, response: false}
	if s.listener != nil && s.listener.deliver(event) {
		return
	}
	for stream := range s.streams {
		if stream.deliver(event) {
			return
		}
	}

	s.backlog = append(s.backlog, msg.raw)
	s.backlogBytes += len(msg.raw)
	for s.backlogBytes > stdioBacklogMaxBytes && len(s.backlog) > 0 {
		s.backlogBytes -= len(s.backlog[0])
		s.backlog[0] = nil
		s.backlog = s.backlog[1:]
		s.logger.Warn("tunnel stdio backlog full; dropping oldest server message")
	}
}

// answerPings writes the bridge's answers to pings sent during initialize,
// one at a time, until the session ends.
func (s *stdioSession) answerPings() {
	for {
		select {
		case <-s.done:
			return
		case id := <-s.pings:
			ctx, cancel := context.WithTimeout(context.Background(), stdioPingTimeout)
			pong := fmt.Appendf(nil, `{"jsonrpc":"2.0","id":%s,"result":{}}`, id)
			_ = s.send(ctx, []rpcMessage{{raw: pong, rawID: id, id: "", method: ""}})
			cancel()
		}
	}
}

func (s *stdioSession) logStderr(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), 64<<10)
	for scanner.Scan() {
		s.logger.Info("tunnel stdio server stderr", slog.String("line", scanner.Text()))
	}
	// Keep draining after an over-long line so the server never blocks on stderr.
	_, _ = io.Copy(io.Discard, r)
}

// close shuts the server down the way the MCP stdio transport prescribes:
// close stdin, then SIGTERM, then SIGKILL. Signals go to the whole process
// group, whether or not its leader has already exited, and terminated closes
// once the group is gone.
func (s *stdioSession) close() {
	s.closeOnce.Do(func() {
		_ = s.stdin.Close()
		go func() {
			defer close(s.terminated)
			select {
			case <-s.exited:
			case <-time.After(stdioShutdownGrace):
			}
			if !s.awaitGroupExit(0) {
				return
			}
			terminateProcessGroup(s.cmd)
			if !s.awaitGroupExit(stdioShutdownGrace) {
				return
			}
			killProcessGroup(s.cmd)
			s.awaitGroupExit(stdioKillWait)
		}()
	})
}

// awaitGroupExit polls until the server's process group is gone or wait
// elapses, and reports whether the group is still alive.
func (s *stdioSession) awaitGroupExit(wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		if !processGroupAlive(s.cmd, s.exited) {
			return false
		}
		if !time.Now().Before(deadline) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type rpcEvent struct {
	msg      json.RawMessage
	response bool
}

// rpcStream queues messages for one HTTP response stream. Delivery never
// blocks; a consumer that falls behind by more than limit bytes is cut off.
type rpcStream struct {
	mu     sync.Mutex
	queue  []rpcEvent
	bytes  int
	limit  int
	notify chan struct{}

	gone     chan struct{}
	goneOnce sync.Once

	// remaining counts responses not yet routed; guarded by the session's mu.
	remaining int
}

func newRPCStream(remaining int) *rpcStream {
	return &rpcStream{
		mu:        sync.Mutex{},
		queue:     nil,
		bytes:     0,
		limit:     stdioStreamMaxBytes,
		notify:    make(chan struct{}, 1),
		gone:      make(chan struct{}),
		goneOnce:  sync.Once{},
		remaining: remaining,
	}
}

// deliver queues event and reports whether it was accepted.
func (s *rpcStream) deliver(event rpcEvent) bool {
	s.mu.Lock()
	select {
	case <-s.gone:
		s.mu.Unlock()
		return false
	default:
	}
	if s.bytes+len(event.msg) > s.limit {
		s.mu.Unlock()
		s.close()
		return false
	}
	s.queue = append(s.queue, event)
	s.bytes += len(event.msg)
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
	return true
}

func (s *rpcStream) pop() (rpcEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return rpcEvent{msg: nil, response: false}, false
	}
	event := s.queue[0]
	s.queue[0] = rpcEvent{msg: nil, response: false}
	s.queue = s.queue[1:]
	s.bytes -= len(event.msg)
	return event, true
}

func (s *rpcStream) close() {
	s.goneOnce.Do(func() { close(s.gone) })
}

// events yields queued messages until wantResponses responses have been
// yielded (-1 for never), the context ends, the stream is cut off, or the
// session's process exits.
func (s *rpcStream) events(ctx context.Context, sess *stdioSession, wantResponses int) iter.Seq[json.RawMessage] {
	return func(yield func(json.RawMessage) bool) {
		received := 0
		for wantResponses < 0 || received < wantResponses {
			if event, ok := s.pop(); ok {
				if event.response {
					received++
				}
				if !yield(event.msg) {
					return
				}
				continue
			}
			select {
			case <-s.notify:
			case <-ctx.Done():
				return
			case <-s.gone:
				return
			case <-sess.done:
				// Flush anything routed before the exit, then stop.
				for {
					event, ok := s.pop()
					if !ok || !yield(event.msg) {
						return
					}
				}
			}
		}
	}
}

type rpcMessage struct {
	raw   json.RawMessage
	rawID json.RawMessage
	// id is the canonical form of the JSON-RPC id, for matching responses
	// to requests by value rather than by spelling.
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
	if rawID := bytes.TrimSpace(envelope.ID); len(rawID) > 0 && !bytes.Equal(rawID, []byte("null")) {
		id, err := canonicalRPCID(rawID)
		if err != nil {
			return rpcMessage{}, err
		}
		var compactID bytes.Buffer
		if err := json.Compact(&compactID, rawID); err != nil {
			return rpcMessage{}, err
		}
		msg.rawID = compactID.Bytes()
		msg.id = id
	}
	return msg, nil
}

// canonicalRPCID maps a JSON-RPC id to a key that is equal for equal values:
// "ab" and "ab" match, as do 1 and 1.0.
func canonicalRPCID(raw json.RawMessage) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return "", err
	}
	switch v := value.(type) {
	case string:
		return "s:" + v, nil
	case json.Number:
		n, ok := new(big.Rat).SetString(v.String())
		if !ok {
			return "", fmt.Errorf("invalid JSON-RPC id %s", raw)
		}
		return "n:" + n.RatString(), nil
	default:
		return "", fmt.Errorf("JSON-RPC id must be a string or number, got %s", raw)
	}
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
