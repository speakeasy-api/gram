// Package ateclient provides the narrow control and actor ingress surfaces of ATE v1.x.
package ateclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	ateapiv1 "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// defaultConnectTimeout bounds a single TLS dial and CONNECT handshake with the ATE router.
	defaultConnectTimeout = 15 * time.Second

	// defaultConnectRetryInterval is the initial wait between retryable router 503 CONNECT responses.
	defaultConnectRetryInterval = 200 * time.Millisecond

	// maxConnectRetryInterval caps backoff between retryable router 503 CONNECT responses while an actor resumes.
	maxConnectRetryInterval = time.Second

	// defaultConnectRetryTimeout bounds how long a single dial retries router 503 CONNECT responses while an actor resumes.
	defaultConnectRetryTimeout = 2 * time.Minute

	// defaultRPCTimeout bounds ordinary ATE control-plane RPCs.
	defaultRPCTimeout = 30 * time.Second

	// suspendRPCTimeout gives synchronous durable rootfs checkpoint uploads time to finish.
	suspendRPCTimeout = 3 * time.Minute

	// maxControlRecvBytes bounds a single gRPC control response payload.
	maxControlRecvBytes = 4 << 20 // 4 MiB

	// maxAudienceJWTBytes bounds the rotating control-plane audience JWT read from disk.
	maxAudienceJWTBytes = 16 << 10 // 16 KiB
)

// Config contains deployment-trusted settings, never request-supplied paths or addresses.
// Addresses are host:port, not URLs. Empty server names use the address hostname.
// ClientCertFile and ClientKeyFile may name the same combined PEM bundle.
type Config struct {
	// APIAddress is the host:port of the ATE control gRPC server.
	APIAddress string

	// RouterAddress is the host:port of the ATE actor ingress router.
	RouterAddress string

	// APIServerName overrides the TLS server name and gRPC authority for the control API.
	APIServerName string

	// RouterServerName overrides the TLS server name for the actor ingress router.
	RouterServerName string

	// ServiceCAFile is the PEM CA bundle trusted for ATE API and router TLS handshakes.
	ServiceCAFile string

	// ClientCertFile is the rotating PEM client certificate for ATE mTLS.
	ClientCertFile string

	// ClientKeyFile is the rotating PEM client private key for ATE mTLS.
	ClientKeyFile string

	// AudienceJWTFile is the rotating audience-bound JWT file presented to ATE.
	AudienceJWTFile string

	// Atespace is the target ATE atespace for actor routing.
	Atespace string

	// ConnectTimeout bounds one router dial and CONNECT handshake attempt.
	ConnectTimeout time.Duration

	// ConnectRetryInterval is the initial delay before retrying a 503 router CONNECT response.
	ConnectRetryInterval time.Duration

	// ConnectRetryTimeout bounds total time spent retrying 503 router CONNECT responses while an actor resumes.
	ConnectRetryTimeout time.Duration

	// RPCTimeout bounds ordinary control-plane RPCs and actor HTTP response headers.
	RPCTimeout time.Duration
}

// Client wraps the allowlisted ATE Control RPCs and mTLS actor CONNECT tunnel.
type Client struct {
	config    Config
	conn      *grpc.ClientConn
	control   ateapiv1.ControlClient
	tlsConfig *tls.Config
}

// New validates credentials and constructs an authenticated ATE Control and router client.
func New(ctx context.Context, cfg Config) (*Client, error) {
	for _, address := range []string{cfg.APIAddress, cfg.RouterAddress} {
		host, port, err := net.SplitHostPort(address)
		if err != nil || host == "" || port == "" {
			return nil, errors.New("ATE address must be host:port")
		}
	}

	if !validName(cfg.Atespace) {
		return nil, errors.New("invalid ATE atespace")
	}

	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = defaultConnectTimeout
	}
	if cfg.ConnectRetryInterval <= 0 {
		cfg.ConnectRetryInterval = defaultConnectRetryInterval
	}
	if cfg.ConnectRetryTimeout <= 0 {
		cfg.ConnectRetryTimeout = defaultConnectRetryTimeout
	}
	if cfg.RPCTimeout <= 0 {
		cfg.RPCTimeout = defaultRPCTimeout
	}

	ca, err := os.ReadFile(cfg.ServiceCAFile)
	if err != nil {
		return nil, errors.New("read ATE service CA")
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("invalid ATE service CA")
	}

	loadCert := func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile)
		if err != nil {
			return nil, errors.New("load ATE client certificate")
		}
		return &cert, nil
	}
	if _, err := loadCert(nil); err != nil {
		return nil, err
	}

	bearer := fileBearer{path: cfg.AudienceJWTFile}
	if _, err := bearer.GetRequestMetadata(ctx); err != nil {
		return nil, err
	}

	routerTLS := &tls.Config{
		MinVersion:           tls.VersionTLS12,
		RootCAs:              pool,
		GetClientCertificate: loadCert,
	}
	// The control API verifies pod-identity client certs if given, whereas the
	// client certificate here is signed by the dedicated router ingress CA.
	// Present NO client certificate to the control API; authenticate solely via
	// the rotating bearer JWT read from AudienceJWTFile on every RPC.
	apiTLS := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
		ServerName: cfg.APIServerName,
	}

	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(apiTLS)),
		grpc.WithPerRPCCredentials(bearer),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxControlRecvBytes)),
	}
	if cfg.APIServerName != "" {
		dialOptions = append(dialOptions, grpc.WithAuthority(cfg.APIServerName))
	}

	conn, err := grpc.NewClient(cfg.APIAddress, dialOptions...)
	if err != nil {
		return nil, errors.New("create ATE control connection")
	}

	c := &Client{
		config:    cfg,
		conn:      conn,
		control:   ateapiv1.NewControlClient(conn),
		tlsConfig: routerTLS,
	}

	// Verify the authenticated bearer/TLS control-plane handshake and that the
	// pre-created atespace exists at startup so misconfiguration fails fast.
	probeCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	_, err = c.control.GetAtespace(probeCtx, &ateapiv1.GetAtespaceRequest{
		Atespace: &ateapiv1.ObjectRef{Atespace: "", Name: cfg.Atespace},
	})
	if err != nil {
		_ = conn.Close()
		if status.Code(err) == codes.NotFound {
			return nil, fmt.Errorf("ATE atespace %q does not exist; pre-create it before enabling the ATE backend", cfg.Atespace)
		}
		return nil, fmt.Errorf("verify ATE control connection: %w", sanitizedRPCError(err))
	}

	return c, nil
}

// Close closes the underlying gRPC control-plane connection.
func (c *Client) Close() error {
	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("close ATE control connection: %w", err)
	}
	return nil
}

func (c *Client) GetAtespace(ctx context.Context, req *ateapiv1.GetAtespaceRequest) (*ateapiv1.Atespace, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RPCTimeout)
	defer cancel()
	resp, err := c.control.GetAtespace(ctx, req)
	if err != nil {
		return nil, sanitizedRPCError(err)
	}
	return resp, nil
}

func (c *Client) GetActorTemplate(ctx context.Context, req *ateapiv1.GetActorTemplateRequest) (*ateapiv1.ActorTemplate, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RPCTimeout)
	defer cancel()
	resp, err := c.control.GetActorTemplate(ctx, req)
	if err != nil {
		return nil, sanitizedRPCError(err)
	}
	return resp, nil
}

func (c *Client) CreateActorTemplate(ctx context.Context, req *ateapiv1.CreateActorTemplateRequest) (*ateapiv1.ActorTemplate, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RPCTimeout)
	defer cancel()
	resp, err := c.control.CreateActorTemplate(ctx, req)
	if err != nil {
		return nil, sanitizedRPCError(err)
	}
	return resp, nil
}

func (c *Client) GetActor(ctx context.Context, req *ateapiv1.GetActorRequest) (*ateapiv1.Actor, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RPCTimeout)
	defer cancel()
	resp, err := c.control.GetActor(ctx, req)
	if err != nil {
		return nil, sanitizedRPCError(err)
	}
	return resp, nil
}

func (c *Client) CreateActor(ctx context.Context, req *ateapiv1.CreateActorRequest) (*ateapiv1.Actor, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RPCTimeout)
	defer cancel()
	resp, err := c.control.CreateActor(ctx, req)
	if err != nil {
		return nil, sanitizedRPCError(err)
	}
	return resp, nil
}

func (c *Client) SuspendActor(ctx context.Context, req *ateapiv1.SuspendActorRequest) (*ateapiv1.SuspendActorResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, suspendRPCTimeout)
	defer cancel()
	resp, err := c.control.SuspendActor(ctx, req)
	if err != nil {
		return nil, sanitizedRPCError(err)
	}
	return resp, nil
}

func (c *Client) DeleteActor(ctx context.Context, req *ateapiv1.DeleteActorRequest) (*ateapiv1.Actor, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RPCTimeout)
	defer cancel()
	resp, err := c.control.DeleteActor(ctx, req)
	if err != nil {
		return nil, sanitizedRPCError(err)
	}
	return resp, nil
}

func (c *Client) GetActorEgressPolicy(ctx context.Context, req *ateapiv1.GetActorEgressPolicyRequest) (*ateapiv1.EgressPolicy, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RPCTimeout)
	defer cancel()
	resp, err := c.control.GetActorEgressPolicy(ctx, req)
	if err != nil {
		return nil, sanitizedRPCError(err)
	}
	return resp, nil
}

func (c *Client) CreateActorEgressPolicy(ctx context.Context, req *ateapiv1.CreateActorEgressPolicyRequest) (*ateapiv1.EgressPolicy, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RPCTimeout)
	defer cancel()
	resp, err := c.control.CreateActorEgressPolicy(ctx, req)
	if err != nil {
		return nil, sanitizedRPCError(err)
	}
	return resp, nil
}

type fileBearer struct{ path string }

func (fileBearer) RequireTransportSecurity() bool { return true }

func (b fileBearer) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	f, err := os.Open(b.path)
	if err != nil {
		return nil, errors.New("read ATE audience token")
	}
	defer o11y.NoLogDefer(func() error { return f.Close() })

	token, err := io.ReadAll(io.LimitReader(f, maxAudienceJWTBytes+1))
	if err != nil || len(token) > maxAudienceJWTBytes {
		return nil, errors.New("invalid ATE audience token size")
	}

	s := strings.TrimSpace(string(token))
	if s == "" || strings.ContainsAny(s, " \t\r\n") {
		return nil, errors.New("invalid ATE audience token")
	}

	return map[string]string{"authorization": "Bearer " + s}, nil
}
