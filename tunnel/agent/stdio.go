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
	stdioWriteTimeout         = 30 * time.Second
	stdioPingQueue            = 8
	stdioKillWait             = time.Second
	// Under Speakeasy's 60s idle cutoff for a silent stream, e.g. one awaiting a user's elicitation answer.
	stdioSSEKeepalive = 15 * time.Second

	stdioMaxMessageBytes = 32 << 20
	stdioStreamMaxBytes  = 64 << 20
	stdioBacklogMaxBytes = 8 << 20

	headerMCPSessionID = "Mcp-Session-Id"

	rpcCodeParseError     = -32700
	rpcCodeInvalidRequest = -32600
	rpcCodeServerError    = -32000
	rpcCodeSessionMissing = -32001
	// rpcCodeUnauthorized answers a request whose caller identity or upstream
	// credential the agent refuses in credentials mode.
	rpcCodeUnauthorized = -32003

	// stdioGroupExitConfirm is how long session cleanup waits, after the
	// shutdown sequence, for the process group to disappear before removing
	// its credentials anyway.
	stdioGroupExitConfirm = 2 * time.Second
)

var (
	errStdioAtCapacity    = errors.New("stdio session capacity reached")
	errStdioBridgeClosed  = errors.New("stdio bridge closed")
	errStdioSessionClosed = errors.New("stdio session closed")
	errStdioDuplicateID   = errors.New("duplicate in-flight JSON-RPC id")
	errStdioFrameTooLarge = errors.New("stdio server message exceeds size limit")
)

type stdioBridge struct {
	command     string
	env         []string
	maxSessions int
	idleTimeout time.Duration
	keepalive   time.Duration
	logger      *slog.Logger
	// creds is nil unless credentials mode is on.
	creds *credentialBroker

	mu       sync.Mutex
	sessions map[string]*stdioSession
	closed   bool
}

// credentialBroker is credentials mode's shared state.
type credentialBroker struct {
	verifier *assertionVerifier
	store    credentialStore
	maxAge   time.Duration
	// expiryGrace is how long a session outlives its token's expiry.
	expiryGrace time.Duration
	now         func() time.Time
	// cleanups tracks session storage removal so shutdown can finish it
	// before releasing the instance directory.
	cleanups sync.WaitGroup
}

func newStdioBridge(command string, maxSessions int, idleTimeout time.Duration, creds *credentialBroker, logger *slog.Logger) *stdioBridge {
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
		creds:       creds,
		sessions:    make(map[string]*stdioSession),
		closed:      false,
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

func (b *stdioBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	var assertion callerAssertion
	if b.creds != nil {
		var ok bool
		if assertion, ok = b.authenticate(w, r); !ok {
			return
		}
	}

	sid := r.Header.Get(headerMCPSessionID)
	if sid == "" {
		b.handleInitialize(w, r, msgs, batch, assertion)
		return
	}
	sess := b.ownedSession(w, sid, assertion)
	if sess == nil {
		return
	}
	var cred admittedCredential
	if b.creds != nil {
		var ok bool
		if cred, ok = b.admitSessionCredential(w, r, sess, assertion, msgs); !ok {
			return
		}
	}
	forward := func(ctx context.Context) error {
		if b.creds != nil {
			return sess.sendCredentialed(ctx, msgs, cred)
		}
		return sess.send(ctx, msgs)
	}
	release := sess.acquire()
	defer release()

	requests := requestMessages(msgs)
	if len(requests) == 0 {
		if err := forward(r.Context()); err != nil {
			writeForwardError(w, err)
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

	if err := forward(r.Context()); err != nil {
		writeForwardError(w, err)
		return
	}

	if sse {
		writeSSEHeaders(w, sid)
		for msg := range stream.events(r.Context(), sess, len(requests), b.keepalive) {
			if err := writeSSEEvent(w, msg); err != nil {
				return
			}
		}
		for _, msg := range sess.unanswered(stream) {
			if err := writeSSEEvent(w, msg); err != nil {
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
	writeJSONResponses(w, responses, batch)
}

func (b *stdioBridge) handleInitialize(w http.ResponseWriter, r *http.Request, msgs []rpcMessage, batch bool, assertion callerAssertion) {
	if batch || len(msgs) != 1 || msgs[0].method != "initialize" || msgs[0].id == "" {
		writeRPCError(w, http.StatusBadRequest, nil, rpcCodeInvalidRequest, "Bad Request: Mcp-Session-Id header is required")
		return
	}

	var cred *admittedCredential
	if b.creds != nil {
		admitted, err := admitCredential(r.Header, assertion, b.creds.now())
		if err != nil {
			b.logger.Warn("tunnel stdio initialize refused: upstream credential not admitted", slog.String("reason", err.Error()))
			writeUnauthorized(w)
			return
		}
		if !assertion.permits(msgs[0].method) {
			writeRPCError(w, http.StatusForbidden, msgs[0].rawID, rpcCodeUnauthorized, "method not permitted")
			return
		}
		cred = &admitted
	}

	sess, err := b.start(cred, assertion.principal)
	switch {
	case errors.Is(err, errCredentialExpired):
		writeUnauthorized(w)
		return
	case errors.Is(err, errStdioAtCapacity):
		w.Header().Set("Retry-After", "5")
		writeRPCError(w, http.StatusServiceUnavailable, msgs[0].rawID, rpcCodeServerError, "MCP server is at capacity")
		return
	case err != nil:
		b.logger.Warn("tunnel stdio server failed to start", slog.Any("error", err))
		writeRPCError(w, http.StatusBadGateway, msgs[0].rawID, rpcCodeServerError, "MCP server failed to start")
		return
	}
	if cred != nil {
		b.logger.Info("tunnel stdio session credential admitted", slog.String("stdio_session", logSessionID(sess.id)))
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

	stream, err := sess.openStream(msgs, false)
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
	for msg := range stream.events(ctx, sess, 1, 0) {
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
	w.Header().Set(headerMCPSessionID, sess.id)
	if err := writeJSONResponses(w, []json.RawMessage{response}, false); err != nil || r.Context().Err() != nil {
		return
	}
	committed = true
	sess.logger.Info("tunnel stdio session started")
}

func (b *stdioBridge) handleGet(w http.ResponseWriter, r *http.Request) {
	var assertion callerAssertion
	if b.creds != nil {
		var ok bool
		if assertion, ok = b.authenticate(w, r); !ok {
			return
		}
	}
	sid := r.Header.Get(headerMCPSessionID)
	if sid == "" {
		writeRPCError(w, http.StatusBadRequest, nil, rpcCodeInvalidRequest, "Bad Request: Mcp-Session-Id header is required")
		return
	}
	sess := b.ownedSession(w, sid, assertion)
	if sess == nil {
		return
	}
	// Not acquire: an idle GET stream must not keep a session alive.
	sess.touch()

	listener, err := sess.attachListener()
	if err != nil {
		writeRPCError(w, http.StatusConflict, nil, rpcCodeInvalidRequest, "a GET stream is already open for this session")
		return
	}
	defer sess.detachListener(listener)

	writeSSEHeaders(w, sid)
	for msg := range listener.events(r.Context(), sess, -1, b.keepalive) {
		if err := writeSSEEvent(w, msg); err != nil {
			return
		}
	}
}

func (b *stdioBridge) handleDelete(w http.ResponseWriter, r *http.Request) {
	var assertion callerAssertion
	if b.creds != nil {
		var ok bool
		if assertion, ok = b.authenticate(w, r); !ok {
			return
		}
	}
	sid := r.Header.Get(headerMCPSessionID)
	if sid == "" {
		writeRPCError(w, http.StatusBadRequest, nil, rpcCodeInvalidRequest, "Bad Request: Mcp-Session-Id header is required")
		return
	}
	sess := b.ownedSession(w, sid, assertion)
	if sess == nil {
		return
	}
	sess.logger.Info("tunnel stdio session terminated by client")
	sess.terminate()
	w.WriteHeader(http.StatusNoContent)
}

// authenticate verifies the caller assertion, answering 401 when it fails.
// Nothing about any session is touched before it succeeds.
func (b *stdioBridge) authenticate(w http.ResponseWriter, r *http.Request) (callerAssertion, bool) {
	assertion, err := b.creds.verifier.verify(r.Context(), r.Header)
	if err != nil {
		b.logger.Warn("tunnel stdio request refused: caller assertion not verified", slog.String("reason", assertionFailureReason(err)))
		writeUnauthorized(w)
		return callerAssertion{}, false
	}
	return assertion, true
}

// ownedSession finds the session the caller owns. Another principal's
// session answers exactly like an unknown one and is left untouched.
func (b *stdioBridge) ownedSession(w http.ResponseWriter, sid string, assertion callerAssertion) *stdioSession {
	sess := b.session(sid)
	if sess == nil || (b.creds != nil && (sess.cred == nil || sess.cred.principal != assertion.principal)) {
		writeRPCError(w, http.StatusNotFound, nil, rpcCodeSessionMissing, "session not found")
		return nil
	}
	return sess
}

// admitSessionCredential admits a POST to the caller's own session. The
// principal is proven, so a missing or invalid credential ends the session:
// the user's upstream access is gone. A credential from another grant
// context also ends it, so the client starts over with a fresh server.
func (b *stdioBridge) admitSessionCredential(w http.ResponseWriter, r *http.Request, sess *stdioSession, assertion callerAssertion, msgs []rpcMessage) (admittedCredential, bool) {
	cred, err := admitCredential(r.Header, assertion, b.creds.now())
	if err != nil {
		sess.logger.Info("tunnel stdio session credential no longer admitted; stopping server", slog.String("reason", err.Error()))
		writeUnauthorized(w)
		go sess.terminate()
		return cred, false
	}
	if cred.context != sess.cred.context {
		sess.logger.Info("tunnel stdio session credential changed grant; stopping server")
		writeRPCError(w, http.StatusNotFound, nil, rpcCodeSessionMissing, "session not found")
		go sess.terminate()
		return cred, false
	}
	for _, msg := range msgs {
		if msg.method == "" && assertion.principal.consent {
			writeRPCError(w, http.StatusForbidden, msg.rawID, rpcCodeUnauthorized, "method not permitted")
			return cred, false
		}
		if msg.method != "" && !assertion.permits(msg.method) {
			writeRPCError(w, http.StatusForbidden, msg.rawID, rpcCodeUnauthorized, "method not permitted")
			return cred, false
		}
	}
	return cred, true
}

func assertionFailureReason(err error) string {
	switch {
	case errors.Is(err, errAssertionMissing):
		return "missing"
	case errors.Is(err, errVerificationKeys):
		return "verification keys unavailable"
	case errors.Is(err, errUnknownAssertKey):
		return "unknown signing key"
	default:
		return "invalid"
	}
}

func writeUnauthorized(w http.ResponseWriter) {
	writeRPCError(w, http.StatusUnauthorized, nil, rpcCodeUnauthorized, "unauthorized")
}

// writeForwardError answers a request that could not be forwarded.
func writeForwardError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errCredentialExpired):
		writeUnauthorized(w)
	default:
		writeRPCError(w, http.StatusNotFound, nil, rpcCodeSessionMissing, "session not found")
	}
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

// start launches a session's server. In credentials mode cred is the
// admitted credential: its token is written before the server starts, so a
// server that reads it during startup finds it.
func (b *stdioBridge) start(cred *admittedCredential, owner principal) (*stdioSession, error) {
	id, err := newStdioSessionID()
	if err != nil {
		return nil, err
	}
	logger := b.logger.With(slog.String("stdio_session", logSessionID(id)))

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, errStdioBridgeClosed
	}
	if len(b.sessions) >= b.maxSessions {
		return nil, errStdioAtCapacity
	}

	if b.creds == nil {
		sess, err := startStdioSession(id, b.command, b.env, false, logger)
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

	dir, err := b.creds.store.createSession()
	if err != nil {
		return nil, fmt.Errorf("create session credentials: %w", err)
	}
	creds := newSessionCredentials(owner, cred.context, dir, b.creds.maxAge, b.creds.expiryGrace, b.creds.now)
	sess, err := startCredentialedSession(id, b.command, credentialChildEnv(b.env, dir.tokenPath(), dir.homePath()), creds, *cred, logger)
	if err != nil {
		if rerr := dir.remove(); rerr != nil {
			logger.Warn("tunnel stdio session credentials could not be removed", slog.Any("error", rerr))
		}
		return nil, err
	}
	b.sessions[id] = sess
	b.creds.cleanups.Go(func() {
		<-sess.terminated
		sess.confirmGroupExit()
		sess.removeCredentials()
	})
	go func() {
		<-sess.done
		b.remove(id)
	}()
	return sess, nil
}

// startCredentialedSession publishes the first token, then starts the server.
func startCredentialedSession(id, command string, env []string, creds *sessionCredentials, cred admittedCredential, logger *slog.Logger) (*stdioSession, error) {
	now := creds.now()
	if cred.expired(now) {
		return nil, errCredentialExpired
	}
	if err := creds.dir.writeToken(cred.token); err != nil {
		return nil, fmt.Errorf("write session token: %w", err)
	}
	sess, err := startStdioSession(id, command, env, true, logger)
	if err != nil {
		return nil, err
	}
	sess.cred = creds
	sess.enterGate(context.Background())
	creds.generation++
	generation := creds.generation
	creds.timer = time.AfterFunc(creds.deadline(cred, now), func() { sess.expireCredential(generation) })
	sess.leaveGate()
	return sess, nil
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
				if sess.idleSince(now) > b.idleTimeout {
					idle = append(idle, sess)
				}
			}
			b.mu.Unlock()
			for _, sess := range idle {
				sess.logger.Info("tunnel stdio session idle; stopping server")
				if sess.cred == nil {
					sess.close()
				} else {
					// Waits for any admitted write, so it must not block the reaper.
					go sess.terminate()
				}
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
			b.closeCredentials()
			return
		}
	}
	b.closeCredentials()
}

// closeCredentials waits for every session's credentials to be removed, then
// releases this agent's storage. Each removal is bounded, so this ends even
// when a server could not be stopped.
func (b *stdioBridge) closeCredentials() {
	if b.creds == nil {
		return
	}
	b.creds.cleanups.Wait()
	if err := b.creds.store.Close(); err != nil {
		b.logger.Warn("tunnel credentials storage could not be removed", slog.Any("error", err))
	}
}

type stdioSession struct {
	id     string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	logger *slog.Logger

	// cred is nil unless credentials mode is on.
	cred *sessionCredentials

	// closing is set first thing when the session starts to close. Nothing
	// is admitted once it is set.
	closing atomic.Bool

	// groupAlive reports whether any of the server's process group is left.
	groupAlive func(wait time.Duration) bool

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

// quietStderr counts the server's stderr instead of logging it: in
// credentials mode it can carry the user's token.
func startStdioSession(id, command string, env []string, quietStderr bool, logger *slog.Logger) (*stdioSession, error) {
	cmd := shellCommand(command)
	cmd.Env = env
	configureProcessGroup(cmd, quietStderr)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
		// Omits the command, which commonly carries credentials.
		return nil, fmt.Errorf("start stdio server: %w", err)
	}
	_ = stdoutW.Close()
	_ = stderrW.Close()

	s := &stdioSession{
		id:           id,
		cmd:          cmd,
		stdin:        stdin,
		logger:       logger.With(slog.Int("pid", cmd.Process.Pid)),
		cred:         nil,
		closing:      atomic.Bool{},
		groupAlive:   nil,
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

	s.groupAlive = s.awaitGroupExit
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		s.readStdout(stdoutR)
	}()
	if quietStderr {
		go s.countStderr(stderrR)
	} else {
		go s.logStderr(stderrR)
	}
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

func (s *stdioSession) touch() {
	s.mu.Lock()
	s.lastActive = time.Now()
	s.mu.Unlock()
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

// A write that cannot finish closes the session: a partial message would
// corrupt the framing of everything after it.
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
	writeTimeout := time.NewTimer(stdioWriteTimeout)
	defer writeTimeout.Stop()
	select {
	case err := <-written:
		if err != nil {
			return errors.Join(errStdioSessionClosed, err)
		}
		return nil
	case <-ctx.Done():
	case <-writeTimeout.C:
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
	select {
	case <-written:
	case <-time.After(stdioShutdownGrace):
	}
	return errStdioSessionClosed
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
func (s *stdioSession) route(msg rpcMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastActive = time.Now()

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
		if !stream.deliver(rpcEvent{msg: msg.raw, response: true}) {
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
	// Keep draining so the server never blocks on a full stderr pipe.
	_, _ = io.Copy(io.Discard, r)
}

// countStderr drains the server's stderr and logs only its size.
func (s *stdioSession) countStderr(r io.Reader) {
	var bytesRead, lines int
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		bytesRead += n
		lines += bytes.Count(buf[:n], []byte{'\n'})
		if err != nil {
			break
		}
	}
	if bytesRead > 0 {
		s.logger.Info("tunnel stdio server wrote to stderr; not logged in credentials mode", slog.Int("bytes", bytesRead), slog.Int("lines", lines))
	}
}

// confirmGroupExit waits briefly for the process group to be gone after the
// shutdown sequence, killing it again if needed. Credentials are removed
// afterwards regardless: a process that cannot be stopped must not keep a
// token file forever.
func (s *stdioSession) confirmGroupExit() {
	if !s.groupAlive(stdioGroupExitConfirm) {
		return
	}
	killProcessGroup(s.cmd)
	if s.groupAlive(stdioKillWait) {
		s.logger.Warn("tunnel stdio server process group survived shutdown; removing its credentials anyway")
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
		gone:      make(chan struct{}),
		goneOnce:  sync.Once{},
		remaining: remaining,
		rawIDs:    make(map[string]json.RawMessage),
	}
}

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
	var err error
	if msg == nil {
		_, err = io.WriteString(w, ": keepalive\n\n")
	} else {
		_, err = fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
	}
	if err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

func writeJSONResponses(w http.ResponseWriter, responses []json.RawMessage, batch bool) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	payload := []byte(nil)
	if !batch && len(responses) == 1 {
		payload = responses[0]
	} else {
		var err error
		if payload, err = json.Marshal(responses); err != nil {
			return err
		}
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
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
