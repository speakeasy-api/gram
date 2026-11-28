package ateclient

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ateapiv1 "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func testConfig(t *testing.T, dnsNames ...string) (Config, tls.Certificate, *x509.CertPool) {
	t.Helper()
	if len(dnsNames) == 0 {
		dnsNames = []string{"router.test"}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: dnsNames[0]},
		DNSNames:              dnsNames,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	tokenFile := filepath.Join(dir, "token")
	for path, data := range map[string][]byte{certFile: certPEM, keyFile: keyPEM, tokenFile: []byte("test-token")} {
		require.NoError(t, os.WriteFile(path, data, 0o600))
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)

	return Config{
		ServiceCAFile:        certFile,
		ClientCertFile:       certFile,
		ClientKeyFile:        keyFile,
		AudienceJWTFile:      tokenFile,
		Atespace:             "test-space",
		APIServerName:        dnsNames[0],
		RouterServerName:     dnsNames[0],
		ConnectTimeout:       time.Second,
		ConnectRetryInterval: 5 * time.Millisecond,
		ConnectRetryTimeout:  200 * time.Millisecond,
		RPCTimeout:           time.Second,
	}, cert, pool
}

func TestActorTunnel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, header string
		reject       bool
	}{
		{"unframed", "", false},
		{"keepalive", "Connection: keep-alive\r\n", false},
		{"close", "Connection: close\r\n", true},
		{"mixed-case", "Connection: cLoSe\r\n", true},
		{"comma-list", "Connection: keep-alive, CLOSE\r\n", true},
		{"multiple-headers", "Connection: keep-alive\r\nConnection: Close\r\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, cert, pool := testConfig(t)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodConnect || r.Host != "actor-one:8081" || r.Header.Get("ate-target-actor") != "test-space/actor-one" || r.Header.Get("Authorization") != "" || len(r.TLS.PeerCertificates) == 0 {
					t.Error("invalid CONNECT identity")
					w.WriteHeader(http.StatusForbidden)
					return
				}
				hijacker, ok := w.(http.Hijacker)
				if !ok {
					t.Error("response writer cannot hijack")
					return
				}
				conn, rw, err := hijacker.Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = conn.Close() }()
				_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n" + tc.header + "\r\n")
				_ = rw.Flush()
				if tc.reject {
					return
				}
				inner, err := http.ReadRequest(rw.Reader)
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = io.Copy(io.Discard, inner.Body)
				_ = inner.Body.Close()
				_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello")
				_ = rw.Flush()
			}))
			server.TLS = &tls.Config{
				Certificates: []tls.Certificate{cert},
				ClientCAs:    pool,
				ClientAuth:   tls.RequireAndVerifyClientCert,
				MinVersion:   tls.VersionTLS12,
			}
			server.StartTLS()
			defer server.Close()

			cfg.RouterAddress = strings.TrimPrefix(server.URL, "https://")
			client := &Client{
				config: cfg,
				tlsConfig: &tls.Config{
					RootCAs:    pool,
					MinVersion: tls.VersionTLS12,
					GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
						return &cert, nil
					},
				},
			}
			require.NoError(t, os.WriteFile(cfg.AudienceJWTFile, []byte("rotated-token"), 0o600))

			httpClient, err := client.ActorHTTPClient("actor-one", 8081)
			require.NoError(t, err)

			for _, target := range []string{"http://other:8081/acp", "http://actor-one:80/acp", "https://actor-one:8081/acp"} {
				if response, err := httpClient.Get(target); err == nil {
					_ = response.Body.Close()
					t.Fatalf("allowed %s", target)
				}
			}

			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://actor-one:8081/acp", strings.NewReader("{}"))
			require.NoError(t, err)
			response, err := httpClient.Do(req)
			if tc.reject {
				if response != nil {
					_ = response.Body.Close()
				}
				_, ok := errors.AsType[*ConnectError](err)
				require.True(t, ok, "expected CONNECT rejection, got %v", err)
				return
			}
			require.NoError(t, err)
			defer func() { _ = response.Body.Close() }()

			data, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, "hello", string(data))
		})
	}
}

func TestActorTunnelRetries503WhileActorResumes(t *testing.T) {
	t.Parallel()
	cfg, cert, pool := testConfig(t)

	var attempts atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("response writer cannot hijack")
			return
		}
		conn, rw, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		inner, err := http.ReadRequest(rw.Reader)
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = io.Copy(io.Discard, inner.Body)
		_ = inner.Body.Close()
		_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 7\r\n\r\nresumed")
		_ = rw.Flush()
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}
	server.StartTLS()
	defer server.Close()

	cfg.RouterAddress = strings.TrimPrefix(server.URL, "https://")
	client := &Client{
		config: cfg,
		tlsConfig: &tls.Config{
			RootCAs:    pool,
			MinVersion: tls.VersionTLS12,
			GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &cert, nil
			},
		},
	}

	httpClient, err := client.ActorHTTPClient("actor-one", 8081)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://actor-one:8081/acp", strings.NewReader("{}"))
	require.NoError(t, err)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "resumed", string(body))
	require.Equal(t, int32(3), attempts.Load())
}

func TestActorTunnelDoesNotRetryNon503(t *testing.T) {
	t.Parallel()
	cfg, cert, pool := testConfig(t)

	var attempts atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}
	server.StartTLS()
	defer server.Close()

	cfg.RouterAddress = strings.TrimPrefix(server.URL, "https://")
	client := &Client{
		config: cfg,
		tlsConfig: &tls.Config{
			RootCAs:    pool,
			MinVersion: tls.VersionTLS12,
			GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &cert, nil
			},
		},
	}

	httpClient, err := client.ActorHTTPClient("actor-one", 8081)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://actor-one:8081/acp", nil)
	require.NoError(t, err)
	resp, err := httpClient.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	connectErr, ok := errors.AsType[*ConnectError](err)
	require.True(t, ok, "expected ConnectError, got %v", err)
	require.Equal(t, http.StatusNotFound, connectErr.StatusCode)
	require.False(t, connectErr.Retryable())
	require.Equal(t, int32(1), attempts.Load())
}

type fakeControlServer struct {
	ateapiv1.UnimplementedControlServer
	deadlines chan time.Time
	auth      chan string
}

func (s *fakeControlServer) GetAtespace(_ context.Context, req *ateapiv1.GetAtespaceRequest) (*ateapiv1.Atespace, error) {
	name := req.GetAtespace().GetName()
	if name == "missing-space" {
		return nil, status.Error(codes.NotFound, "atespace not found") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	return &ateapiv1.Atespace{
		Metadata: &ateapiv1.ResourceMetadata{Name: name, Uid: "space-" + name},
	}, nil
}

func (s *fakeControlServer) GetActor(ctx context.Context, req *ateapiv1.GetActorRequest) (*ateapiv1.Actor, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("authorization"); len(vals) > 0 {
			s.auth <- vals[0]
		}
	}
	deadline, _ := ctx.Deadline()
	s.deadlines <- deadline
	if req.GetActor().GetName() == "missing" {
		return nil, status.Error(codes.NotFound, "sensitive backend detail") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	return &ateapiv1.Actor{
		Metadata: &ateapiv1.ResourceMetadata{
			Atespace: req.GetActor().GetAtespace(),
			Name:     req.GetActor().GetName(),
			Uid:      "uid-1",
		},
	}, nil
}

func (s *fakeControlServer) SuspendActor(ctx context.Context, req *ateapiv1.SuspendActorRequest) (*ateapiv1.SuspendActorResponse, error) {
	deadline, _ := ctx.Deadline()
	s.deadlines <- deadline
	return &ateapiv1.SuspendActorResponse{
		Actor: &ateapiv1.Actor{
			Metadata: &ateapiv1.ResourceMetadata{
				Atespace: req.GetActor().GetAtespace(),
				Name:     req.GetActor().GetName(),
				Uid:      "uid-1",
			},
			Status: &ateapiv1.ActorStatus{
				State: ateapiv1.ActorState_ACTOR_STATE_SUSPENDED,
			},
		},
	}, nil
}

func TestControlClientHonorsAPIServerNameAndDeadlines(t *testing.T) {
	t.Parallel()
	// Certificate is valid ONLY for api.ate.test (not 127.0.0.1), so dialing
	// 127.0.0.1:<port> succeeds only if APIServerName is passed as the gRPC
	// authority and used in TLS SNI / peer certificate verification.
	cfg, cert, _ := testConfig(t, "api.ate.test")

	sni := make(chan string, 1)
	deadlines := make(chan time.Time, 4)
	auth := make(chan string, 4)

	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			select {
			case sni <- hello.ServerName:
			default:
			}
			return nil, nil
		},
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) > 0 {
				return errors.New("control API must not receive router client certificate")
			}
			return nil
		},
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequestClientCert,
		MinVersion:   tls.VersionTLS12,
	})))
	ateapiv1.RegisterControlServer(server, &fakeControlServer{deadlines: deadlines, auth: auth})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	cfg.APIAddress = listener.Addr().String()
	cfg.RouterAddress = cfg.APIAddress
	client, err := New(t.Context(), cfg)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	actor, err := client.GetActor(t.Context(), &ateapiv1.GetActorRequest{
		Actor: &ateapiv1.ObjectRef{Atespace: "test-space", Name: "actor-one"},
	})
	require.NoError(t, err)
	require.Equal(t, "actor-one", actor.GetMetadata().GetName())
	require.Equal(t, "api.ate.test", <-sni)
	require.Equal(t, "Bearer test-token", <-auth)

	remaining := time.Until(<-deadlines)
	require.Greater(t, remaining, time.Duration(0))
	require.LessOrEqual(t, remaining, cfg.RPCTimeout)

	// NotFound status code is preserved while remote detail text is sanitized.
	_, err = client.GetActor(t.Context(), &ateapiv1.GetActorRequest{
		Actor: &ateapiv1.ObjectRef{Atespace: "test-space", Name: "missing"},
	})
	require.Equal(t, codes.NotFound, status.Code(err))
	require.NotContains(t, err.Error(), "sensitive backend detail")
	<-auth
	<-deadlines

	// SuspendActor gets the extended 3-minute checkpoint budget.
	_, err = client.SuspendActor(t.Context(), &ateapiv1.SuspendActorRequest{
		Actor: &ateapiv1.ObjectRef{Atespace: "test-space", Name: "actor-one"},
	})
	require.NoError(t, err)
	remaining = time.Until(<-deadlines)
	require.GreaterOrEqual(t, remaining, 179*time.Second)
	require.LessOrEqual(t, remaining, suspendRPCTimeout)

	// Earlier caller deadline still wins over the suspend budget.
	callerCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err = client.SuspendActor(callerCtx, &ateapiv1.SuspendActorRequest{
		Actor: &ateapiv1.ObjectRef{Atespace: "test-space", Name: "actor-one"},
	})
	require.NoError(t, err)
	remaining = time.Until(<-deadlines)
	require.Greater(t, remaining, time.Duration(0))
	require.LessOrEqual(t, remaining, time.Second)
}

func TestControlClientRejectsMismatchedAPIServerName(t *testing.T) {
	t.Parallel()
	cfg, cert, _ := testConfig(t, "api.ate.test")

	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequestClientCert,
		MinVersion:   tls.VersionTLS12,
	})))
	ateapiv1.RegisterControlServer(server, &fakeControlServer{
		deadlines: make(chan time.Time, 1),
		auth:      make(chan string, 1),
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	cfg.APIAddress = listener.Addr().String()
	cfg.RouterAddress = cfg.APIAddress
	cfg.APIServerName = "wrong.ate.test"
	client, err := New(t.Context(), cfg)
	if client != nil {
		_ = client.Close()
	}
	require.Error(t, err)

	// Missing pre-created atespace fails startup clearly.
	missingCfg := cfg
	missingCfg.APIServerName = "api.ate.test"
	missingCfg.Atespace = "missing-space"
	missingClient, err := New(t.Context(), missingCfg)
	if missingClient != nil {
		_ = missingClient.Close()
	}
	require.ErrorContains(t, err, `ATE atespace "missing-space" does not exist`)
}

func TestControlAPIPresentsNoClientCertWhileRouterRequiresClientCert(t *testing.T) {
	t.Parallel()
	cfg, cert, pool := testConfig(t, "api.ate.test")

	apiCertSeen := make(chan int, 2)
	apiServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequestClientCert,
		MinVersion:   tls.VersionTLS12,
		VerifyConnection: func(cs tls.ConnectionState) error {
			select {
			case apiCertSeen <- len(cs.PeerCertificates):
			default:
			}
			if len(cs.PeerCertificates) > 0 {
				return errors.New("control API rejects any presented client certificate")
			}
			return nil
		},
	})))
	ateapiv1.RegisterControlServer(apiServer, &fakeControlServer{
		deadlines: make(chan time.Time, 4),
		auth:      make(chan string, 4),
	})
	apiLis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = apiServer.Serve(apiLis) }()
	defer apiServer.Stop()

	routerCertCount := make(chan int, 2)
	routerServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil {
			select {
			case routerCertCount <- len(r.TLS.PeerCertificates):
			default:
			}
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("response writer cannot hijack")
			return
		}
		conn, rw, hijackErr := hijacker.Hijack()
		if hijackErr != nil {
			t.Error(hijackErr)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		_, _ = http.ReadRequest(rw.Reader)
		_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		_ = rw.Flush()
	}))
	routerServer.TLS = &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}
	routerServer.StartTLS()
	defer routerServer.Close()

	cfg.APIAddress = apiLis.Addr().String()
	cfg.RouterAddress = strings.TrimPrefix(routerServer.URL, "https://")
	cfg.RouterServerName = "api.ate.test"

	client, err := New(t.Context(), cfg)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	_, err = client.GetActor(t.Context(), &ateapiv1.GetActorRequest{
		Actor: &ateapiv1.ObjectRef{Atespace: "test-space", Name: "actor-one"},
	})
	require.NoError(t, err)
	require.Equal(t, 0, <-apiCertSeen, "control API must not receive a client certificate")

	hc, err := client.ActorHTTPClient("actor-one", 8081)
	require.NoError(t, err)
	defer hc.CloseIdleConnections()
	endpoint, err := ActorEndpoint("actor-one", 8081)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint, nil)
	require.NoError(t, err)
	resp, err := hc.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 1, <-routerCertCount, "router must receive the mTLS client certificate")
}

func TestTokenBoundsAndRotation(t *testing.T) {
	t.Parallel()
	cfg, _, _ := testConfig(t)
	bearer := fileBearer{path: cfg.AudienceJWTFile}
	for _, token := range []string{"", strings.Repeat("x", maxAudienceJWTBytes+1), "injected\r\nheader"} {
		require.NoError(t, os.WriteFile(cfg.AudienceJWTFile, []byte(token), 0o600))
		_, err := bearer.GetRequestMetadata(t.Context())
		require.Error(t, err)
	}
	require.True(t, bearer.RequireTransportSecurity())
}

// Ensure the connection wrapper does not discard prefetched bytes.
func TestBufferedConn(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()
	reader := bufio.NewReader(a)
	go func() {
		_, _ = b.Write([]byte("header\nbody"))
		_ = b.Close()
	}()
	_, err := reader.ReadString('\n')
	require.NoError(t, err)

	data := make([]byte, 4)
	conn := &bufferedConn{Conn: a, reader: reader}
	_, err = io.ReadFull(conn, data)
	require.NoError(t, err)
	require.Equal(t, "body", string(data))

	n, err := conn.Read(data)
	require.Equal(t, 0, n)
	require.Equal(t, io.EOF, err)
}
