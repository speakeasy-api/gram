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
	"sync/atomic"
	"time"
)

const (
	defaultStdioMaxSessions   = 16
	defaultStdioIdleTimeout   = 30 * time.Minute
	stdioInitializeTimeout    = 60 * time.Second
	stdioShutdownGrace        = 3 * time.Second
	stdioExitDrain            = 2 * time.Second
	stdioPingTimeout          = 5 * time.Second
	stdioMaxNumericIDLength   = 64
	stdioMaxNumericIDExponent = 64
	stdioPingQueue            = 8
	stdioKillWait             = time.Second
	// Under Speakeasy's 60s idle cutoff for a silent stream, e.g. one awaiting a user's elicitation answer.
	stdioSSEKeepalive = 15 * time.Second

	// stdioEvictIdleAfter is how long a session must go without a request
	// before a new session at capacity may evict it. Shorter pauses are
	// normal within a conversation that is still active.
	stdioEvictIdleAfter = 2 * time.Minute

	// stdioStdinStallTimeout is how long a write to a server's stdin may go
	// without the server accepting a byte before the server counts as stuck
	// and is stopped. A synchronous server stops reading while it runs a slow
	// tool, so this is generous; later writes wait behind a stalled one.
	stdioStdinStallTimeout = 2 * time.Minute

	// stdioResponseWriteTimeout is how long an HTTP response write may go
	// without the client accepting a chunk. A client that stops reading then
	// fails the write instead of holding its handler, and the session, open.
	stdioResponseWriteTimeout = 30 * time.Second

	// stdioResponseChunkBytes is the most written to a response under one
	// deadline, so a large message to a slow client is bounded by progress
	// rather than by its size. It matches yamux's default stream window.
	stdioResponseChunkBytes = 256 << 10 // 256 KiB

	// stdioStderrLineBytes caps one stderr line in the agent's log; the rest
	// of a longer line is dropped, and logging carries on with the next.
	stdioStderrLineBytes = 8 << 10 // 8 KiB

	// stdioStderrLinesPerMinute caps the stderr lines a session logs each
	// minute, so a chatty server cannot flood the agent's logs. Lines over it
	// are counted and the count is logged when the minute ends.
	stdioStderrLinesPerMinute = 120

	stdioMaxMessageBytes = 32 << 20
	stdioStreamMaxBytes  = 64 << 20
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
	errStdioStreamCutOff  = errors.New("stdio stream cut off")
)

type stdioBridge struct {
	command     string
	env         []string
	maxSessions int
	idleTimeout time.Duration
	keepalive   time.Duration
	logger      *slog.Logger

	// evictIdleAfter, responseWriteTimeout and stdinStallTimeout default to
	// their stdio* constants; tests shorten them.
	evictIdleAfter       time.Duration
	responseWriteTimeout time.Duration
	stdinStallTimeout    time.Duration

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
		keepalive:   stdioSSEKeepalive,
		logger:      logger,

		evictIdleAfter:       stdioEvictIdleAfter,
		responseWriteTimeout: stdioResponseWriteTimeout,
		stdinStallTimeout:    stdioStdinStallTimeout,

		sessions: make(map[string]*stdioSession),
		closed:   false,
	}
}

// Keeps the tunnel key out of the MCP server's environment.
func childEnv(environ []string) []string {
	env := make([]string, 0, len(environ))
	for _, kv := range environ {
		if len(kv) >= len("TUNNEL_") && strings.EqualFold(kv[:len("TUNNEL_")], "TUNNEL_") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func (b *stdioBridge) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	w := b.newResponseWriter(rw)
	// OAuth back-channel paths have no stdio equivalent.
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

func (b *stdioBridge) handlePost(w *responseWriter, r *http.Request) {
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
	release, ok := sess.acquire()
	if !ok {
		writeRPCError(w, http.StatusNotFound, nil, rpcCodeSessionMissing, "session not found")
		return
	}
	defer release()

	requests := requestMessages(msgs)
	if len(requests) == 0 {
		if err := sess.send(r.Context(), msgs); err != nil {
			writeRPCError(w, http.StatusNotFound, nil, rpcCodeSessionMissing, "session not found")
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	sse := acceptsSSE(r)
	stream, err := sess.openStream(requests, sse)
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
		stream.setAbort(w.abort)
		if err := w.startSSE(sid); err != nil {
			return
		}
		for msg := range stream.events(r.Context(), sess, len(requests), b.keepalive) {
			if err := w.writeSSEEvent(msg); err != nil {
				return
			}
		}
		for _, msg := range sess.unanswered(stream) {
			if err := w.writeSSEEvent(msg); err != nil {
				return
			}
		}
		return
	}

	responses := make([]json.RawMessage, 0, len(requests))
	for msg := range stream.events(r.Context(), sess, len(requests), 0) {
		responses = append(responses, msg)
	}
	if r.Context().Err() != nil {
		return
	}
	responses = append(responses, sess.unanswered(stream)...)
	w.Header().Set(headerMCPSessionID, sid)
	_ = w.writeJSON(responses, batch)
}

func (b *stdioBridge) handleInitialize(w *responseWriter, r *http.Request, msgs []rpcMessage, batch bool) {
	if batch || len(msgs) != 1 || msgs[0].method != "initialize" || msgs[0].id == "" {
		writeRPCError(w, http.StatusBadRequest, nil, rpcCodeInvalidRequest, "Bad Request: Mcp-Session-Id header is required")
		return
	}

	exited := func() {
		writeRPCError(w, http.StatusBadGateway, msgs[0].rawID, rpcCodeServerError, "MCP server exited during initialize")
	}
	sess, err := b.start(r.Context())
	switch {
	case errors.Is(err, errStdioAtCapacity):
		w.Header().Set("Retry-After", "5")
		writeRPCError(w, http.StatusServiceUnavailable, msgs[0].rawID, rpcCodeServerError, "MCP server is at capacity")
		return
	case r.Context().Err() != nil:
		return
	case err != nil:
		b.logger.Warn("tunnel stdio server failed to start", slog.Any("error", err))
		writeRPCError(w, http.StatusBadGateway, msgs[0].rawID, rpcCodeServerError, "MCP server failed to start")
		return
	}
	release, ok := sess.acquire()
	if !ok {
		exited()
		return
	}
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

	stream, err := sess.openStream(msgs, false)
	if err != nil {
		exited()
		return
	}
	defer sess.closeStream(stream)
	if err := sess.send(ctx, msgs); err != nil {
		switch {
		case r.Context().Err() != nil:
		case ctx.Err() != nil:
			writeRPCError(w, http.StatusBadGateway, msgs[0].rawID, rpcCodeServerError, "MCP server did not accept initialize")
		default:
			// A write that fails rather than times out means the server is gone.
			exited()
		}
		return
	}

	var response json.RawMessage
	for msg := range stream.events(ctx, sess, 1, 0) {
		response = msg
	}
	if response == nil {
		if r.Context().Err() != nil {
			return
		}
		select {
		case <-sess.done:
			exited()
		default:
			writeRPCError(w, http.StatusGatewayTimeout, msgs[0].rawID, rpcCodeServerError, "MCP server did not answer initialize")
		}
		return
	}

	if isRPCError(response) {
		_ = w.writeJSON([]json.RawMessage{response}, false)
		return
	}
	w.Header().Set(headerMCPSessionID, sess.id)
	if err := w.writeJSON([]json.RawMessage{response}, false); err != nil || r.Context().Err() != nil {
		return
	}
	committed = true
	sess.logger.Info("tunnel stdio session started")
}

func (b *stdioBridge) handleGet(w *responseWriter, r *http.Request) {
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
	// Not acquire: an idle GET stream must not keep a session alive.
	sess.touch()

	listener, err := sess.attachListener()
	switch {
	case errors.Is(err, errStdioSessionClosed):
		writeRPCError(w, http.StatusNotFound, nil, rpcCodeSessionMissing, "session not found")
		return
	case err != nil:
		writeRPCError(w, http.StatusConflict, nil, rpcCodeInvalidRequest, "a GET stream is already open for this session")
		return
	}
	defer sess.detachListener(listener)

	listener.setAbort(w.abort)
	if err := w.startSSE(sid); err != nil {
		return
	}
	for msg := range listener.events(r.Context(), sess, -1, b.keepalive) {
		if err := w.writeSSEEvent(msg); err != nil {
			return
		}
	}
}

func (b *stdioBridge) handleDelete(w *responseWriter, r *http.Request) {
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

// session finds a live session. A closing one is treated as gone, so its
// client gets 404 while its server shuts down.
func (b *stdioBridge) session(id string) *stdioSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	if sess := b.sessions[id]; sess != nil && !sess.closing.Load() {
		return sess
	}
	return nil
}

func (b *stdioBridge) remove(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessions, id)
}

// start launches a session's server. At capacity it evicts the session idle
// longest, waiting for its server to stop so the process count stays capped.
func (b *stdioBridge) start(ctx context.Context) (*stdioSession, error) {
	id, err := newStdioSessionID()
	if err != nil {
		return nil, err
	}
	for {
		sess, freeing, evicted, err := b.startOrEvict(id)
		if sess != nil || err != nil {
			return sess, err
		}
		if evicted {
			freeing.logger.Info("tunnel stdio session evicted for a new session; stopping server")
			freeing.close()
		}
		select {
		case <-freeing.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		// Its own removal races this retry, which could otherwise still count it and evict another session.
		b.remove(freeing.id)
	}
}

// startOrEvict starts a session if there is room. At capacity it returns a
// session whose slot will free for the caller to wait on: one already
// closing, else the session idle longest, if idle past evictIdleAfter, which
// it claims for the caller to stop.
func (b *stdioBridge) startOrEvict(id string) (sess, freeing *stdioSession, evicted bool, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, nil, false, errStdioBridgeClosed
	}
	if len(b.sessions) >= b.maxSessions {
		for _, other := range b.sessions {
			if other.closing.Load() {
				return nil, other, false, nil
			}
		}
		now := time.Now()
		var idlest *stdioSession
		longest := time.Duration(0)
		for _, sess := range b.sessions {
			if idle := sess.idleFor(now); idle > longest {
				idlest, longest = sess, idle
			}
		}
		if idlest == nil || !idlest.claimIdle(now, b.evictIdleAfter) {
			return nil, nil, false, errStdioAtCapacity
		}
		return nil, idlest, true, nil
	}

	sess, err = startStdioSession(id, b.command, b.env, b.stdinStallTimeout, b.logger.With(slog.String("stdio_session", logSessionID(id))))
	if err != nil {
		return nil, nil, false, err
	}
	b.sessions[id] = sess
	go func() {
		<-sess.done
		b.remove(id)
	}()
	return sess, nil, false, nil
}

func (b *stdioBridge) reap(ctx context.Context) {
	interval := max(min(b.idleTimeout/4, time.Minute), 10*time.Millisecond)
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
				if sess.claimIdle(now, b.idleTimeout) {
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
	stdin  *os.File
	logger *slog.Logger

	// stdinStall bounds how long a stdin write may make no progress.
	stdinStall time.Duration

	// closing is set first thing when the session starts to close. Nothing
	// is admitted once it is set.
	closing atomic.Bool

	// A channel so a waiting writer can give up on cancellation.
	writeSem chan struct{}

	// exited: leader reaped. terminated: process group gone. done: also stdout drained.
	exited     chan struct{}
	terminated chan struct{}
	done       chan struct{}
	closeOnce  sync.Once

	pings chan json.RawMessage

	mu           sync.Mutex
	initializing bool
	pending      map[string]*rpcStream
	streams      map[*rpcStream]struct{}
	listener     *rpcStream
	backlog      []json.RawMessage
	backlogBytes int
	inflight     int
	lastActive   time.Time
}

func startStdioSession(id, command string, env []string, stdinStall time.Duration, logger *slog.Logger) (*stdioSession, error) {
	cmd := shellCommand(command)
	cmd.Env = env
	configureProcessGroup(cmd)

	// The agent keeps an *os.File for stdin so writes can take deadlines.
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		closeFiles(stdinR, stdinW)
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		closeFiles(stdinR, stdinW, stdoutR, stdoutW)
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	if err := cmd.Start(); err != nil {
		closeFiles(stdinR, stdinW, stdoutR, stdoutW, stderrR, stderrW)
		// Omits the command, which commonly carries credentials.
		return nil, fmt.Errorf("start stdio server: %w", err)
	}
	// The server holds its own copies of these ends.
	closeFiles(stdinR, stdoutW, stderrW)

	s := &stdioSession{
		id:           id,
		cmd:          cmd,
		stdin:        stdinW,
		logger:       logger.With(slog.Int("pid", cmd.Process.Pid)),
		stdinStall:   stdinStall,
		closing:      atomic.Bool{},
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

// acquire admits a request, keeping the session from idling out until
// release. It refuses once the session is closing.
func (s *stdioSession) acquire() (func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing.Load() {
		return nil, false
	}
	s.inflight++
	s.lastActive = time.Now()
	return func() {
		s.mu.Lock()
		s.inflight--
		s.lastActive = time.Now()
		s.mu.Unlock()
	}, true
}

func (s *stdioSession) touch() {
	s.mu.Lock()
	s.lastActive = time.Now()
	s.mu.Unlock()
}

// idleFor is how long the session has gone without activity: zero while a
// request is in flight or once it is closing.
func (s *stdioSession) idleFor(now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idleForLocked(now)
}

func (s *stdioSession) idleForLocked(now time.Time) time.Duration {
	if s.inflight > 0 || s.closing.Load() {
		return 0
	}
	return now.Sub(s.lastActive)
}

// claimIdle marks the session closing if it has been idle longer than limit.
// It decides under the lock acquire takes, so a request admitted first keeps
// the session, and one that comes after is refused.
func (s *stdioSession) claimIdle(now time.Time, limit time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idleForLocked(now) <= limit {
		return false
	}
	s.closing.Store(true)
	return true
}

func (s *stdioSession) endInitialize() {
	s.mu.Lock()
	s.initializing = false
	s.mu.Unlock()
}

// send writes msgs to the server's stdin, one write at a time. A caller that
// gives up only stops waiting: its write carries on, because abandoning it
// part way would corrupt the framing of everything after it.
func (s *stdioSession) send(ctx context.Context, msgs []rpcMessage) error {
	select {
	case s.writeSem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return errStdioSessionClosed
	}

	size := 0
	for _, msg := range msgs {
		size += len(msg.raw) + 1
	}
	frames := make([]byte, 0, size)
	for _, msg := range msgs {
		frames = append(append(frames, msg.raw...), '\n')
	}
	written := make(chan error, 1)
	go func() {
		defer func() { <-s.writeSem }()
		written <- s.writeStdin(frames)
	}()
	select {
	case err := <-written:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return errStdioSessionClosed
	}
}

// writeStdin writes p in full, failing only when the server accepts nothing
// for stdinStall or the write fails. Either way it ends the session.
func (s *stdioSession) writeStdin(p []byte) error {
	for {
		_ = s.stdin.SetWriteDeadline(time.Now().Add(s.stdinStall))
		n, err := s.stdin.Write(p)
		p = p[n:]
		if err == nil {
			return nil
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			if n > 0 {
				continue
			}
			s.logger.Warn("tunnel stdio server is not reading stdin; stopping server", slog.Duration("stalled_for", s.stdinStall))
		}
		s.close()
		return errors.Join(errStdioSessionClosed, err)
	}
}

func (s *stdioSession) openStream(requests []rpcMessage, unsolicited bool) (*rpcStream, error) {
	stream := newRPCStream(len(requests))
	for _, req := range requests {
		if _, dup := stream.rawIDs[req.id]; dup {
			return nil, errStdioDuplicateID
		}
		stream.rawIDs[req.id] = req.rawID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.done:
		return nil, errStdioSessionClosed
	default:
	}
	for id := range stream.rawIDs {
		if _, exists := s.pending[id]; exists {
			return nil, errStdioDuplicateID
		}
	}
	for id := range stream.rawIDs {
		s.pending[id] = stream
	}
	if unsolicited {
		s.streams[stream] = struct{}{}
	}
	return stream, nil
}

func (s *stdioSession) unanswered(stream *rpcStream) []json.RawMessage {
	select {
	case <-s.done:
	default:
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []json.RawMessage
	for id, owner := range s.pending {
		if owner != stream {
			continue
		}
		delete(s.pending, id)
		out = append(out, rpcErrorPayload(stream.rawIDs[id], rpcCodeServerError, "MCP server exited before responding"))
	}
	return out
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

func (s *stdioSession) attachListener() (*rpcStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing.Load() {
		return nil, errStdioSessionClosed
	}
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

// Must never block on a consumer: it runs on the session's only stdout reader.
//
// Only a message that reaches a stream counts as activity: one left in the
// backlog has no consumer, and a server that notifies on a timer must not keep
// an abandoned session alive.
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
			// Its handler stops reading after the last response.
			delete(s.streams, stream)
		}
		if stream.deliver(rpcEvent{msg: msg.raw, response: true}) {
			s.lastActive = time.Now()
		} else {
			s.logger.Warn("tunnel stdio response dropped: its HTTP stream is gone or over its buffer limit")
		}
		return
	}

	// The client cannot answer before it has a session id.
	if s.initializing && msg.method == "ping" && msg.id != "" {
		select {
		case s.pings <- msg.rawID:
		default:
			s.logger.Warn("tunnel stdio server sent too many pings during initialize; dropping one")
		}
		return
	}

	if s.deliverUnsolicitedLocked(rpcEvent{msg: msg.raw, response: false}) {
		s.lastActive = time.Now()
		return
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

// deliverUnsolicitedLocked hands a server message to the GET stream, else to
// any POST stream that takes it.
func (s *stdioSession) deliverUnsolicitedLocked(event rpcEvent) bool {
	if s.listener != nil && s.listener.deliver(event) {
		return true
	}
	for stream := range s.streams {
		if stream.deliver(event) {
			return true
		}
	}
	return false
}

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

// logStderr logs the server's stderr line by line until it closes, so the
// server never blocks on a full pipe.
func (s *stdioSession) logStderr(r io.Reader) {
	reader := bufio.NewReaderSize(r, stdioStderrLineBytes)
	window := time.Now()
	logged, suppressed := 0, 0
	reportSuppressed := func() {
		if suppressed > 0 {
			s.logger.Warn("tunnel stdio server stderr lines over the per-minute limit were not logged", slog.Int("lines", suppressed))
			suppressed = 0
		}
	}
	for {
		line, err := reader.ReadSlice('\n')
		if len(line) > 0 {
			if now := time.Now(); now.Sub(window) >= time.Minute {
				reportSuppressed()
				window, logged = now, 0
			}
			if logged >= stdioStderrLinesPerMinute {
				suppressed++
			} else {
				logged++
				attrs := []any{slog.String("line", string(bytes.TrimRight(line, "\r\n")))}
				if errors.Is(err, bufio.ErrBufferFull) {
					attrs = append(attrs, slog.Bool("truncated", true))
				}
				s.logger.Info("tunnel stdio server stderr", attrs...)
			}
		}
		// Drop the rest of a line too long for the buffer.
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = reader.ReadSlice('\n')
		}
		if err != nil {
			reportSuppressed()
			return
		}
	}
}

func (s *stdioSession) close() {
	s.closeOnce.Do(func() {
		s.closing.Store(true)
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

func (s *stdioSession) awaitGroupExit(wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		// Only once cmd.Wait has reaped the leader, so its status is never taken.
		select {
		case <-s.exited:
			reapGroupMembers(s.cmd)
		default:
		}
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

type rpcStream struct {
	mu     sync.Mutex
	queue  []rpcEvent
	bytes  int
	limit  int
	notify chan struct{}

	// abort, when set, fails the writes of the handler serving the stream.
	// Guarded by mu and dropped when the stream closes, so it never runs
	// after that handler returns.
	abort func()

	gone     chan struct{}
	goneOnce sync.Once

	// Guarded by the session's mu.
	remaining int
	rawIDs    map[string]json.RawMessage
}

func newRPCStream(remaining int) *rpcStream {
	return &rpcStream{
		mu:        sync.Mutex{},
		queue:     nil,
		bytes:     0,
		limit:     stdioStreamMaxBytes,
		notify:    make(chan struct{}, 1),
		abort:     nil,
		gone:      make(chan struct{}),
		goneOnce:  sync.Once{},
		remaining: remaining,
		rawIDs:    make(map[string]json.RawMessage),
	}
}

func (s *rpcStream) setAbort(abort func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.abort = abort
}

func (s *rpcStream) deliver(event rpcEvent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.gone:
		return false
	default:
	}
	if s.bytes+len(event.msg) > s.limit {
		// Its client fell too far behind. The handler may be stuck writing to
		// it, so fail that write now rather than when its deadline passes.
		if s.abort != nil {
			s.abort()
		}
		s.closeLocked()
		return false
	}
	s.queue = append(s.queue, event)
	s.bytes += len(event.msg)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked()
}

// closeLocked ends the stream and frees anything still queued on it.
func (s *rpcStream) closeLocked() {
	s.queue, s.bytes, s.abort = nil, 0, nil
	s.goneOnce.Do(func() { close(s.gone) })
}

// A positive keepalive yields a nil message after that long without one.
func (s *rpcStream) events(ctx context.Context, sess *stdioSession, wantResponses int, keepalive time.Duration) iter.Seq[json.RawMessage] {
	return func(yield func(json.RawMessage) bool) {
		var tick <-chan time.Time
		if keepalive > 0 {
			ticker := time.NewTicker(keepalive)
			defer ticker.Stop()
			tick = ticker.C
		}
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
			case <-tick:
				if !yield(nil) {
					return
				}
			case <-ctx.Done():
				return
			case <-s.gone:
				return
			case <-sess.done:
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
	// stdio framing is one message per line.
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
		if err := checkNumericRPCID(v.String()); err != nil {
			return "", err
		}
		n, ok := new(big.Rat).SetString(v.String())
		if !ok {
			return "", fmt.Errorf("invalid JSON-RPC id %s", raw)
		}
		return "n:" + n.RatString(), nil
	default:
		return "", fmt.Errorf("JSON-RPC id must be a string or number, got %s", raw)
	}
}

// Bounds the cost of exact rational parsing of an untrusted id.
func checkNumericRPCID(number string) error {
	if len(number) > stdioMaxNumericIDLength {
		return fmt.Errorf("JSON-RPC id longer than %d characters", stdioMaxNumericIDLength)
	}
	if i := strings.IndexAny(number, "eE"); i >= 0 {
		exp, err := strconv.Atoi(number[i+1:])
		if err != nil || exp > stdioMaxNumericIDExponent || exp < -stdioMaxNumericIDExponent {
			return fmt.Errorf("JSON-RPC id exponent out of range: %s", number)
		}
	}
	return nil
}

func requestMessages(msgs []rpcMessage) []rpcMessage {
	requests := make([]rpcMessage, 0, len(msgs))
	for _, msg := range msgs {
		if msg.method != "" && msg.id != "" {
			requests = append(requests, msg)
		}
	}
	return requests
}

func isRPCError(raw json.RawMessage) bool {
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	return json.Unmarshal(raw, &envelope) == nil && len(envelope.Error) > 0 && !bytes.Equal(envelope.Error, []byte("null"))
}

func acceptsSSE(r *http.Request) bool {
	for _, value := range r.Header.Values("Accept") {
		for mediaRange := range strings.SplitSeq(value, ",") {
			params := strings.Split(mediaRange, ";")
			if !strings.EqualFold(strings.TrimSpace(params[0]), "text/event-stream") {
				continue
			}
			refused := false
			for _, param := range params[1:] {
				key, val, _ := strings.Cut(strings.TrimSpace(param), "=")
				if strings.EqualFold(strings.TrimSpace(key), "q") {
					if q, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil && q == 0 {
						refused = true
					}
				}
			}
			if !refused {
				return true
			}
		}
	}
	return false
}

// responseWriter renews the write deadline before each chunk it writes, so
// every write is bounded by progress; see stdioResponseWriteTimeout.
type responseWriter struct {
	http.ResponseWriter
	rc      *http.ResponseController
	timeout time.Duration

	mu      sync.Mutex
	aborted bool
}

var (
	sseKeepalive   = []byte(": keepalive\n\n")
	sseEventPrefix = []byte("event: message\ndata: ")
	sseEventSuffix = []byte("\n\n")
)

func (b *stdioBridge) newResponseWriter(w http.ResponseWriter) *responseWriter {
	rc := http.NewResponseController(w)
	// A deadline left by an earlier response on a reused connection must not fail this one.
	_ = rc.SetWriteDeadline(time.Time{})
	return &responseWriter{
		ResponseWriter: w,
		rc:             rc,
		timeout:        b.responseWriteTimeout,
		mu:             sync.Mutex{},
		aborted:        false,
	}
}

func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

func (rw *responseWriter) arm() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.aborted {
		return errStdioStreamCutOff
	}
	return ignoreUnsupported(rw.rc.SetWriteDeadline(time.Now().Add(rw.timeout)))
}

// abort fails the write in progress, and every later one, at once.
func (rw *responseWriter) abort() {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	rw.aborted = true
	_ = rw.rc.SetWriteDeadline(time.Now())
}

func (rw *responseWriter) Write(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		if err := rw.arm(); err != nil {
			return written, err
		}
		n, err := rw.ResponseWriter.Write(p[written:min(len(p), written+stdioResponseChunkBytes)])
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func (rw *responseWriter) flush() error {
	if err := rw.arm(); err != nil {
		return err
	}
	return ignoreUnsupported(rw.rc.Flush())
}

func (rw *responseWriter) startSSE(sid string) error {
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set(headerMCPSessionID, sid)
	rw.WriteHeader(http.StatusOK)
	return rw.flush()
}

// writeSSEEvent writes msg as an event, or a keepalive comment when msg is nil.
func (rw *responseWriter) writeSSEEvent(msg json.RawMessage) error {
	parts := [][]byte{sseKeepalive}
	if msg != nil {
		parts = [][]byte{sseEventPrefix, msg, sseEventSuffix}
	}
	for _, part := range parts {
		if _, err := rw.Write(part); err != nil {
			return err
		}
	}
	return rw.flush()
}

func (rw *responseWriter) writeJSON(responses []json.RawMessage, batch bool) error {
	payload := []byte(nil)
	if !batch && len(responses) == 1 {
		payload = responses[0]
	} else {
		var err error
		if payload, err = json.Marshal(responses); err != nil {
			return err
		}
	}
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	rw.WriteHeader(http.StatusOK)
	if _, err := rw.Write(payload); err != nil {
		return err
	}
	return rw.flush()
}

func ignoreUnsupported(err error) error {
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}

func rpcErrorPayload(id json.RawMessage, code int, message string) json.RawMessage {
	if id == nil {
		id = json.RawMessage("null")
	}
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
	return payload
}

func writeRPCError(w http.ResponseWriter, status int, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(rpcErrorPayload(id, code, message))
}

func closeFiles(files ...*os.File) {
	for _, f := range files {
		_ = f.Close()
	}
}

func newStdioSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate stdio session id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// Session ids act as bearer handles; never log them raw.
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
