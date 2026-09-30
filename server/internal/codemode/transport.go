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
	// maxRunnerStreams stays below the peer's 16-stream limit, including closing streams.
	maxRunnerStreams = 4
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
	return &RunnerClient{mu: sync.Mutex{}, endpoint: u, token: token, dialer: dialer, session: nil, closed: false, slots: semaphore.NewWeighted(maxRunnerStreams)}, nil
}

// Close terminates the transport and all active executions during provider release.
func (c *RunnerClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.session != nil {
		if err := c.session.Close(); err != nil {
			return fmt.Errorf("close code runner: %w", err)
		}
	}
	return nil
}

func (c *RunnerClient) open(ctx context.Context) (*runnerStream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("open code stream: %w", err)
	}
	if c.closed {
		return nil, fmt.Errorf("code runner connection released")
	}
	if c.session == nil || c.session.IsClosed() {
		session, err := c.connect(ctx)
		if err != nil {
			return nil, err
		}
		c.session = session
	}
	stream, err := c.session.OpenStream()
	if err != nil {
		return nil, fmt.Errorf("open code runner stream: %w", err)
	}
	return &runnerStream{Stream: stream, session: c.session}, nil
}

func (c *RunnerClient) connect(ctx context.Context) (*yamux.Session, error) {
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
		return nil, fmt.Errorf("dial code runner: %w", err)
	}
	accepted := false
	defer func() {
		if !accepted {
			o11y.NoLogDefer(conn.Close)
		}
	}()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("bound code handshake: %w", err)
	}
	if c.endpoint.Scheme == "https" {
		secure := tls.Client(conn, &tls.Config{ServerName: c.endpoint.Hostname(), MinVersion: tls.VersionTLS12})
		if err := secure.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("code runner TLS: %w", err)
		}
		conn = secure
	}
	u := *c.endpoint
	u.Path = "/v1/connect"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build code upgrade: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "gram-code-yamux")
	req.Header.Set("Gram-Code-Protocol", "1")
	if err := req.Write(conn); err != nil {
		return nil, fmt.Errorf("write code upgrade: %w", err)
	}
	reader := bufio.NewReaderSize(conn, maxUpgradeHeaderBytes)
	var headers bytes.Buffer
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			return nil, fmt.Errorf("read code upgrade: %w", err)
		}
		if headers.Len()+len(line) > maxUpgradeHeaderBytes {
			return nil, fmt.Errorf("code upgrade headers exceed limit")
		}
		headers.Write(line)
		if bytes.Equal(line, []byte("\r\n")) {
			break
		}
	}
	response, err := http.ReadResponse(bufio.NewReader(&headers), req) //nolint:bodyclose // Closed through o11y.NoLogDefer below; the body only wraps the bounded header buffer.
	if err != nil {
		return nil, fmt.Errorf("parse code upgrade: %w", err)
	}
	defer o11y.NoLogDefer(response.Body.Close)
	if response.StatusCode != http.StatusSwitchingProtocols || !strings.EqualFold(response.Header.Get("Upgrade"), "gram-code-yamux") {
		return nil, fmt.Errorf("code runner refused protocol upgrade")
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, fmt.Errorf("clear code handshake deadline: %w", err)
	}
	config := yamux.DefaultConfig()
	config.LogOutput = io.Discard
	config.StreamOpenTimeout = connectTimeout
	config.ConnectionWriteTimeout = connectTimeout
	session, err := yamux.Client(&bufferedRunnerConn{Conn: conn, reader: reader}, config)
	if err != nil {
		return nil, fmt.Errorf("start code multiplexing: %w", err)
	}
	accepted = true
	return session, nil
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
}
