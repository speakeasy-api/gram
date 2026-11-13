package ateclient

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"google.golang.org/grpc/status"
)

func sanitizedRPCError(err error) error {
	return status.Error(status.Code(err), "ATE control RPC failed")
}

type ConnectError struct{ StatusCode int }

func (e *ConnectError) Error() string {
	return fmt.Sprintf("ATE CONNECT rejected (status %d)", e.StatusCode)
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
// redirects, use proxy environment variables, retry POSTs, or time out SSE bodies.
// Request context cancellation remains the caller's responsibility.
func (c *Client) ActorHTTPClient(actor string, port uint16) (*http.Client, error) {
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
		MaxResponseHeaderBytes: 32 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != authority {
				return nil, errors.New("ATE actor destination rejected")
			}
			return c.connect(ctx, actor, authority)
		},
	}
	return &http.Client{
		Transport:     actorTransport{authority: authority, transport: transport},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

type actorTransport struct {
	authority string
	transport *http.Transport
}

func (t actorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || req.URL.Scheme != "http" || req.URL.Host != t.authority || req.URL.User != nil || req.URL.Opaque != "" || (req.Host != "" && req.Host != t.authority) {
		return nil, errors.New("ATE actor request authority rejected")
	}
	return t.transport.RoundTrip(req)
}
func (t actorTransport) CloseIdleConnections() { t.transport.CloseIdleConnections() }

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

func (c *Client) connect(ctx context.Context, actor, authority string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.ConnectTimeout)
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
	metadata, err := (fileBearer{c.config.AudienceJWTFile}).GetRequestMetadata(ctx)
	if err != nil {
		return nil, err
	}
	request := &http.Request{
		Method: http.MethodConnect, URL: &url.URL{Opaque: authority}, Host: authority,
		Header: http.Header{"Ate-Target-Actor": []string{c.config.Atespace + "/" + actor}, "Authorization": []string{metadata["authorization"]}},
	}
	if err := request.Write(conn); err != nil {
		return nil, errors.New("write ATE CONNECT")
	}
	reader := bufio.NewReaderSize(conn, 4096)
	var headers bytes.Buffer
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil || headers.Len()+len(line) > 32<<10 {
			return nil, errors.New("invalid ATE CONNECT response headers")
		}
		headers.Write(line)
		if bytes.Equal(line, []byte("\r\n")) {
			break
		}
	}
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(headers.Bytes())), request)
	if err != nil {
		return nil, errors.New("invalid ATE CONNECT response")
	}
	if response.ProtoMajor != 1 || response.StatusCode != http.StatusOK || len(response.TransferEncoding) != 0 || response.ContentLength > 0 || response.Header.Get("Connection") == "close" {
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
