package ateclient

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/status"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// maxConnectResponseHeaderBytes bounds the HTTP CONNECT response headers from the ATE router.
	maxConnectResponseHeaderBytes = 32 << 10 // 32 KiB

	// tunnelReadBufferBytes sizes the buffered reader shared between CONNECT response parsing and tunneled HTTP.
	tunnelReadBufferBytes = 4 << 10 // 4 KiB
)

func sanitizedRPCError(err error) error {
	return fmt.Errorf("ATE control RPC: %w", status.Error(status.Code(err), "ATE control RPC failed"))
}

// ConnectError reports a rejected HTTP CONNECT tunnel handshake with the ATE router.
type ConnectError struct {
	// StatusCode is the HTTP status code returned by the ATE router.
	StatusCode int
}

func (e *ConnectError) Error() string {
	return fmt.Sprintf("ATE CONNECT rejected (status %d)", e.StatusCode)
}

// Retryable reports whether the router rejection is transient (for example 503
// while an actor resumes or the router sheds under capacity).
func (e *ConnectError) Retryable() bool {
	return e != nil && e.StatusCode == http.StatusServiceUnavailable
}

var namePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func validName(name string) bool { return namePattern.MatchString(name) }

// ActorEndpoint is the synthetic URL accepted by ActorHTTPClient. Only the router
// is ever dialed; the hostname is an actor identity, not a DNS destination.
func ActorEndpoint(actor string, port uint16) (string, error) {
	if !validName(actor) || port == 0 {
		return "", errors.New("invalid ATE actor target")
	}
	return "http://" + net.JoinHostPort(actor, strconv.Itoa(int(port))) + "/acp", nil
}

// ActorHTTPClient restricts all requests to exactly actor:port. It does not follow
// redirects, use proxy environment variables, retry sent POSTs, or time out SSE bodies.
// Transient 503 responses during CONNECT establishment (before any inner HTTP request
// bytes are written) are retried automatically while an actor resumes.
func (c *Client) ActorHTTPClient(actor string, port uint16) (*guardian.HTTPClient, error) {
	endpoint, err := ActorEndpoint(actor, port)
	if err != nil {
		return nil, err
	}
	parsed, _ := url.Parse(endpoint)
	authority := parsed.Host

	transport := &http.Transport{
		Proxy:                  nil,
		DisableKeepAlives:      true,
		ForceAttemptHTTP2:      false,
		ResponseHeaderTimeout:  c.config.RPCTimeout,
		MaxResponseHeaderBytes: maxConnectResponseHeaderBytes,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != authority {
				return nil, errors.New("ATE actor destination rejected")
			}
			return c.connectWithRetry(ctx, actor, authority)
		},
	}

	return &guardian.HTTPClient{
		Transport:     actorTransport{authority: authority, transport: transport},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

type actorTransport struct {
	authority string
	transport *http.Transport
}

// ConfidentialOrigin identifies the exact HTTP origin carried inside verified mTLS CONNECT.
func (t actorTransport) ConfidentialOrigin(u *url.URL) bool {
	return u != nil && u.Scheme == "http" && u.Host == t.authority && u.User == nil && u.Opaque == ""
}

func (t actorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || req.URL.Scheme != "http" || req.URL.Host != t.authority || req.URL.User != nil || req.URL.Opaque != "" || (req.Host != "" && req.Host != t.authority) {
		return nil, errors.New("ATE actor request authority rejected")
	}
	response, err := t.transport.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("ATE actor request: %w", err)
	}
	return response, nil
}

func (t actorTransport) CloseIdleConnections() { t.transport.CloseIdleConnections() }

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	if err == io.EOF {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read ATE tunnel: %w", err)
	}
	return n, nil
}

// connectWithRetry retries router 503 CONNECT responses before any inner request
// is written, so actor resume in ATE v1.0.1 is transparent to callers.
func (c *Client) connectWithRetry(ctx context.Context, actor, authority string) (net.Conn, error) {
	retryTimeout := c.config.ConnectRetryTimeout
	if retryTimeout <= 0 {
		retryTimeout = defaultConnectRetryTimeout
	}
	retryCtx, cancel := context.WithTimeout(ctx, retryTimeout)
	defer cancel()

	delay := c.config.ConnectRetryInterval
	if delay <= 0 {
		delay = defaultConnectRetryInterval
	}

	for {
		conn, err := c.connect(retryCtx, actor, authority)
		if err == nil {
			return conn, nil
		}

		var connectErr *ConnectError
		if !errors.As(err, &connectErr) || !connectErr.Retryable() {
			return nil, err
		}

		timer := time.NewTimer(delay)
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return nil, err
		case <-timer.C:
		}

		if delay < maxConnectRetryInterval {
			delay = min(delay*2, maxConnectRetryInterval)
		}
	}
}

func (c *Client) connect(ctx context.Context, actor, authority string) (net.Conn, error) {
	connectTimeout := c.config.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = defaultConnectTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	tlsConfig := c.tlsConfig.Clone()
	tlsConfig.ServerName = c.config.RouterServerName
	if tlsConfig.ServerName == "" {
		tlsConfig.ServerName, _, _ = net.SplitHostPort(c.config.RouterAddress)
	}
	tlsConfig.NextProtos = []string{"http/1.1"}

	dialer := tls.Dialer{NetDialer: &net.Dialer{}, Config: tlsConfig}
	conn, err := dialer.DialContext(ctx, "tcp", c.config.RouterAddress)
	if err != nil {
		return nil, errors.New("connect ATE router")
	}

	success := false
	defer func() {
		if !success {
			_ = conn.Close()
		}
	}()

	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, errors.New("set ATE CONNECT deadline")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodConnect, "http://"+authority, nil)
	if err != nil {
		return nil, errors.New("create ATE CONNECT request")
	}
	request.URL = &url.URL{Opaque: authority}
	request.Host = authority
	request.Header.Set("ate-target-actor", c.config.Atespace+"/"+actor)
	if err := request.Write(conn); err != nil {
		return nil, errors.New("write ATE CONNECT")
	}

	reader := bufio.NewReaderSize(conn, tunnelReadBufferBytes)
	var headers bytes.Buffer
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil || headers.Len()+len(line) > maxConnectResponseHeaderBytes {
			return nil, errors.New("invalid ATE CONNECT response headers")
		}
		headers.Write(line)
		if bytes.Equal(line, []byte("\r\n")) {
			break
		}
	}

	// The standard response parser removes explicit Connection: close headers.
	// Inspect the original MIME headers, not response.Close (also true for a
	// valid unframed CONNECT response).
	rawReader := textproto.NewReader(bufio.NewReader(bytes.NewReader(headers.Bytes())))
	if _, err := rawReader.ReadLine(); err != nil {
		return nil, errors.New("invalid ATE CONNECT status line")
	}
	rawHeaders, err := rawReader.ReadMIMEHeader()
	if err != nil {
		return nil, errors.New("invalid ATE CONNECT response headers")
	}

	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(headers.Bytes())), request)
	if err != nil {
		return nil, errors.New("invalid ATE CONNECT response")
	}
	defer o11y.NoLogDefer(func() error { return response.Body.Close() })

	if response.ProtoMajor != 1 || response.StatusCode != http.StatusOK || len(response.TransferEncoding) != 0 || response.ContentLength > 0 || hasCloseToken(http.Header(rawHeaders)) {
		return nil, &ConnectError{StatusCode: response.StatusCode}
	}

	// Stop cancellation before handing the socket to net/http. A cancellation that
	// already ran closes the connection, which is safe; it cannot escape the tunnel.
	if !stop() || ctx.Err() != nil {
		return nil, errors.New("ATE CONNECT canceled")
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, errors.New("clear ATE CONNECT deadline")
	}

	success = true
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

// ReadResponse marks an unframed 200 CONNECT as Close even without an explicit
// Connection header. Only explicit close tokens invalidate a successful tunnel.
func hasCloseToken(header http.Header) bool {
	for _, value := range header.Values("Connection") {
		for token := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "close") {
				return true
			}
		}
	}
	return false
}
