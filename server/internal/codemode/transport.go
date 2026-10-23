package codemode

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"golang.org/x/sync/semaphore"
)

const (
	// ProtocolVersion binds the Go coordinator to the dedicated runner protocol.
	ProtocolVersion = 1
	// maxFrameBytes allows a 1 MiB callback plus its JSON envelope.
	maxFrameBytes = MaxResultBytes + 4096
	// connectTimeout bounds runner admission and connection establishment.
	connectTimeout = 5 * time.Second
	// maxUpgradeHeaderBytes bounds an untrusted runner's HTTP response headers.
	maxUpgradeHeaderBytes = 16 << 10
	// RunnerStreams is one runner pod's concurrent executions. Each runs in
	// its own Monty worker that starts in about a millisecond and is capped at
	// 64 MiB, so a pod is CPU-bound rather than startup-bound. It matches the
	// runner's GRAM_CODE_RUNNER_CAPACITY default and stays below the peer's
	// 16-stream limit, which counts closing streams too.
	RunnerStreams = 12
	// runnerCapacityHeader is the upgrade response header in which a runner
	// reports how many executions it admits concurrently.
	runnerCapacityHeader = "Gram-Code-Capacity"
	// runnerSlotWait bounds how long an execution waits for a free stream on
	// a runner that every caller shares, such as the local runner.
	runnerSlotWait = DefaultAdmissionWait
)

// RunnerClient multiplexes executions over one authenticated runner connection.
// Reconnection is only for a new execution; a submitted program is never replayed.
type RunnerClient struct {
	mu       sync.Mutex
	endpoint *url.URL
	token    string
	dialer   *net.Dialer
	session  *yamux.Session
	closed   bool
	slots    *semaphore.Weighted
	// capacity is the runner's reported concurrency, capped at RunnerStreams;
	// zero until the first connection.
	capacity int
	// streams admits one open stream per unit of the runner's capacity. It is
	// created with the first connection, so no stream opens before the
	// capacity is known and executions above it wait instead of being refused.
	streams *semaphore.Weighted
	// connecting is closed when the in-flight connection attempt finishes, so
	// other callers wait on it, or their own context, instead of the mutex.
	connecting chan struct{}
}

// NewRunnerClient accepts only server-owned endpoints and a guarded dialer.
func NewRunnerClient(endpoint, token string, dialer *net.Dialer) (*RunnerClient, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("invalid code runner endpoint")
	}
	if len(token) < 32 || strings.ContainsAny(token, "\r\n") || dialer == nil {
		return nil, fmt.Errorf("code runner requires a guarded dialer and a token of at least 32 bytes")
	}
	return &RunnerClient{mu: sync.Mutex{}, endpoint: u, token: token, dialer: dialer, session: nil, closed: false, slots: semaphore.NewWeighted(RunnerStreams), capacity: 0, streams: nil, connecting: nil}, nil
}

// Close terminates the transport and all active executions during provider release.
func (c *RunnerClient) Close() error {
	c.mu.Lock()
	c.closed = true
	session := c.session
	c.mu.Unlock()
	if session != nil {
		if err := session.Close(); err != nil {
			return fmt.Errorf("close code runner: %w", err)
		}
	}
	return nil
}

// open never holds the mutex across network I/O: a cancelled caller stops
// waiting at once instead of queueing behind another caller's connection
// attempt or a slow stream open. waitUntil bounds the whole admission path,
// connecting included; when it passes first, open fails with errRunnerFull.
func (c *RunnerClient) open(ctx context.Context, waitUntil time.Time) (*runnerStream, error) {
	admission, stopAdmission := context.WithDeadline(ctx, waitUntil)
	defer stopAdmission()
	admissionErr := func(err error) error {
		// A handshake bounded by the same deadline can time out a moment
		// before the context reports it.
		if ctx.Err() == nil && (admission.Err() != nil || !time.Now().Before(waitUntil)) {
			return errRunnerFull
		}
		return err
	}
	if _, err := c.currentSession(admission); err != nil {
		return nil, admissionErr(err)
	}
	c.mu.Lock()
	streams := c.streams
	c.mu.Unlock()
	if err := streams.Acquire(admission, 1); err != nil {
		return nil, admissionErr(fmt.Errorf("wait for code stream: %w", err))
	}
	release := sync.OnceFunc(func() { streams.Release(1) })
	// The session may have closed while this execution waited for a stream;
	// take the current one, reconnecting if needed, before opening.
	session, err := c.currentSession(admission)
	if err != nil {
		release()
		return nil, admissionErr(err)
	}
	type opened struct {
		stream *yamux.Stream
		err    error
	}
	result := make(chan opened, 1)
	go func() {
		stream, err := session.OpenStream()
		result <- opened{stream: stream, err: err}
	}()
	select {
	case r := <-result:
		if r.err != nil {
			release()
			return nil, fmt.Errorf("open code runner stream: %w", r.err)
		}
		return &runnerStream{Stream: r.stream, session: session, release: release}, nil
	case <-admission.Done():
		go func() {
			if r := <-result; r.stream != nil {
				(&runnerStream{Stream: r.stream, session: session, release: release}).finish()
			} else {
				release()
			}
		}()
		return nil, admissionErr(fmt.Errorf("open code stream: %w", admission.Err()))
	}
}

func (c *RunnerClient) currentSession(ctx context.Context) (*yamux.Session, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("open code stream: %w", err)
		}
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, fmt.Errorf("code runner connection released")
		}
		if c.session != nil && !c.session.IsClosed() {
			session := c.session
			c.mu.Unlock()
			return session, nil
		}
		if wait := c.connecting; wait != nil {
			c.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, fmt.Errorf("wait for code runner connection: %w", ctx.Err())
			}
		}
		done := make(chan struct{})
		c.connecting = done
		c.mu.Unlock()

		session, capacity, err := c.connect(ctx)
		c.mu.Lock()
		c.connecting = nil
		close(done)
		if err == nil && c.closed {
			c.mu.Unlock()
			_ = session.Close()
			return nil, fmt.Errorf("code runner connection released")
		}
		if err == nil {
			c.session = session
			if c.streams == nil {
				c.capacity = capacity
				c.streams = semaphore.NewWeighted(int64(capacity))
			}
		}
		c.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return session, nil
	}
}

// knownCapacity returns the concurrency the runner reported on its first
// connection, or zero before the runner has been reached.
func (c *RunnerClient) knownCapacity() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.capacity
}

func (c *RunnerClient) connect(ctx context.Context) (*yamux.Session, int, error) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	port := c.endpoint.Port()
	if port == "" {
		if c.endpoint.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	conn, err := c.dialer.DialContext(ctx, "tcp", net.JoinHostPort(c.endpoint.Hostname(), port))
	if err != nil {
		return nil, 0, fmt.Errorf("dial code runner: %w", err)
	}
	accepted := false
	defer func() {
		if !accepted {
			o11y.NoLogDefer(conn.Close)
		}
	}()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, 0, fmt.Errorf("bound code handshake: %w", err)
	}
	// Cancellation, not only the deadline, interrupts the handshake reads.
	stopInterrupt := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stopInterrupt()
	if c.endpoint.Scheme == "https" {
		secure := tls.Client(conn, &tls.Config{ServerName: c.endpoint.Hostname(), MinVersion: tls.VersionTLS12})
		if err := secure.HandshakeContext(ctx); err != nil {
			return nil, 0, fmt.Errorf("code runner TLS: %w", err)
		}
		conn = secure
	}
	u := *c.endpoint
	u.Path = "/v1/connect"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build code upgrade: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "gram-code-yamux")
	req.Header.Set("Gram-Code-Protocol", "1")
	if err := req.Write(conn); err != nil {
		return nil, 0, fmt.Errorf("write code upgrade: %w", err)
	}
	reader := bufio.NewReaderSize(conn, maxUpgradeHeaderBytes)
	var headers bytes.Buffer
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			return nil, 0, fmt.Errorf("read code upgrade: %w", err)
		}
		if headers.Len()+len(line) > maxUpgradeHeaderBytes {
			return nil, 0, fmt.Errorf("code upgrade headers exceed limit")
		}
		headers.Write(line)
		if bytes.Equal(line, []byte("\r\n")) {
			break
		}
	}
	response, err := http.ReadResponse(bufio.NewReader(&headers), req) //nolint:bodyclose // Closed through o11y.NoLogDefer below; the body only wraps the bounded header buffer.
	if err != nil {
		return nil, 0, fmt.Errorf("parse code upgrade: %w", err)
	}
	defer o11y.NoLogDefer(response.Body.Close)
	if response.StatusCode != http.StatusSwitchingProtocols || !strings.EqualFold(response.Header.Get("Upgrade"), "gram-code-yamux") {
		return nil, 0, fmt.Errorf("code runner refused protocol upgrade")
	}
	capacity, err := strconv.Atoi(response.Header.Get(runnerCapacityHeader))
	if err != nil || capacity < 1 {
		return nil, 0, fmt.Errorf("code runner did not report its capacity")
	}
	capacity = min(capacity, RunnerStreams)
	// An interrupt that already started may still set its deadline after
	// this point, so the connection is unusable unless it is stopped first.
	if !stopInterrupt() {
		return nil, 0, fmt.Errorf("code runner handshake interrupted: %w", ctx.Err())
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, 0, fmt.Errorf("clear code handshake deadline: %w", err)
	}
	config := yamux.DefaultConfig()
	config.LogOutput = io.Discard
	config.StreamOpenTimeout = connectTimeout
	config.ConnectionWriteTimeout = connectTimeout
	session, err := yamux.Client(&bufferedRunnerConn{Conn: conn, reader: reader}, config)
	if err != nil {
		return nil, 0, fmt.Errorf("start code multiplexing: %w", err)
	}
	accepted = true
	return session, capacity, nil
}

type bufferedRunnerConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedRunnerConn) Read(p []byte) (int, error) { return c.reader.Read(p) } //nolint:wrapcheck // net.Conn preserves I/O errors.

func readRunnerFrame(reader io.Reader) (json.RawMessage, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(reader, prefix[:]); err != nil {
		return nil, fmt.Errorf("read code frame length: %w", err)
	}
	size := binary.BigEndian.Uint32(prefix[:])
	if size == 0 || size > maxFrameBytes {
		return nil, fmt.Errorf("invalid code frame size")
	}
	frame := make([]byte, size)
	if _, err := io.ReadFull(reader, frame); err != nil {
		return nil, fmt.Errorf("read code frame: %w", err)
	}
	return frame, nil
}

func writeRunnerFrame(writer io.Writer, frame any) error {
	body, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("encode code frame: %w", err)
	}
	if len(body) > maxFrameBytes {
		return fmt.Errorf("code frame exceeds limit")
	}
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(body))) //nolint:gosec // maxFrameBytes bounds the conversion.
	if _, err := writer.Write(prefix[:]); err != nil {
		return fmt.Errorf("write code frame length: %w", err)
	}
	if _, err := writer.Write(body); err != nil {
		return fmt.Errorf("write code frame: %w", err)
	}
	return nil
}

func decodeStrict(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("invalid code message: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing code message")
	}
	return nil
}

// runnerStream retains its owning connection so failed cleanup cannot close a replacement.
type runnerStream struct {
	*yamux.Stream
	session *yamux.Session
	// release returns the stream's runner admission slot.
	release func()
}

func (s *runnerStream) finish() {
	_ = s.Close()
	_ = s.SetReadDeadline(time.Now().Add(time.Second))
	// Both FINs must be observed before releasing admission. Otherwise a burst
	// of completed/refused streams can still exceed the remote yamux limit.
	_, err := io.CopyN(io.Discard, s, maxFrameBytes+1)
	if !errors.Is(err, io.EOF) {
		_ = s.session.Close()
	}
	s.release()
}
