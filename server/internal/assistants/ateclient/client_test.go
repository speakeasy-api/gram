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
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	reflectionv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func testConfig(t *testing.T) (Config, tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "router.test"}, DNSNames: []string{"router.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	dir := t.TempDir()
	certFile, keyFile, tokenFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"), filepath.Join(dir, "token")
	for path, data := range map[string][]byte{certFile: certPEM, keyFile: keyPEM, tokenFile: []byte("test-token")} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	return Config{ServiceCAFile: certFile, ClientCertFile: certFile, ClientKeyFile: keyFile, AudienceJWTFile: tokenFile, Atespace: "test-space", APIServerName: "router.test", RouterServerName: "router.test", ConnectTimeout: time.Second, RPCTimeout: time.Second}, cert, pool
}

func TestActorTunnel(t *testing.T) {
	cfg, cert, pool := testConfig(t)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || r.Host != "actor-one:8081" || r.Header.Get("ate-target-actor") != "test-space/actor-one" || r.Header.Get("Authorization") != "Bearer rotated-token" || len(r.TLS.PeerCertificates) == 0 {
			t.Error("invalid CONNECT identity")
			w.WriteHeader(403)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		// Send the first tunneled bytes with the CONNECT response to exercise read-ahead.
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\nHTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello")
		_ = rw.Flush()
		inner, err := http.ReadRequest(rw.Reader)
		if err != nil {
			t.Error(err)
			return
		}
		_ = inner.Body.Close()
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()
	cfg.RouterAddress = strings.TrimPrefix(server.URL, "https://")
	client := &Client{config: cfg, tlsConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12, GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return &cert, nil }}}
	if err := os.WriteFile(cfg.AudienceJWTFile, []byte("rotated-token"), 0600); err != nil {
		t.Fatal(err)
	}
	httpClient, err := client.ActorHTTPClient("actor-one", 8081)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"http://other:8081/acp", "http://actor-one:80/acp", "https://actor-one:8081/acp"} {
		if _, err := httpClient.Get(target); err == nil {
			t.Fatalf("allowed %s", target)
		}
	}
	req, _ := http.NewRequest("POST", "http://actor-one:8081/acp", strings.NewReader("{}"))
	response, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || string(data) != "hello" {
		t.Fatalf("buffered response: %q, %v", data, err)
	}
}

func TestReflectionAndStrictInvocation(t *testing.T) {
	cfg, cert, pool := testConfig(t)
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name: new("control_test.proto"), Package: new("substrate.ateapi.v1"), Syntax: new("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{Name: new("Actor"), Field: []*descriptorpb.FieldDescriptorProto{{Name: new("name"), Number: proto.Int32(1), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()}}}},
		Service:     []*descriptorpb.ServiceDescriptorProto{{Name: new("Control"), Method: []*descriptorpb.MethodDescriptorProto{{Name: new("GetActor"), InputType: new(".substrate.ateapi.v1.Actor"), OutputType: new(".substrate.ateapi.v1.Actor")}}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	registry := new(protoregistry.Files)
	if err := registry.RegisterFile(file); err != nil {
		t.Fatal(err)
	}
	message := file.Messages().Get(0)
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS12})), grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(ctx, req)
	}))
	server.RegisterService(&grpc.ServiceDesc{ServiceName: controlService, HandlerType: (*any)(nil), Metadata: "control_test.proto", Methods: []grpc.MethodDesc{{MethodName: "GetActor", Handler: func(_ any, _ context.Context, decode func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
		request := dynamicpb.NewMessage(message)
		if err := decode(request); err != nil {
			return nil, err
		}
		return request, nil
	}}}}, struct{}{})
	reflectionv1.RegisterServerReflectionServer(server, reflection.NewServerV1(reflection.ServerOptions{Services: server, DescriptorResolver: registry}))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	cfg.APIAddress = listener.Addr().String()
	cfg.RouterAddress = cfg.APIAddress
	client, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	out, err := client.Invoke(context.Background(), "GetActor", []byte(`{"name":"actor-one"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "actor-one") {
		t.Fatalf("unexpected response %s", out)
	}
	for _, tc := range []struct{ method, input string }{{"DeleteWorker", `{}`}, {"GetActor", `{"unexpected":true}`}} {
		if _, err := client.Invoke(context.Background(), tc.method, []byte(tc.input)); err == nil {
			t.Fatalf("accepted %s %s", tc.method, tc.input)
		}
	}
}

func TestTokenBoundsAndRotation(t *testing.T) {
	cfg, _, _ := testConfig(t)
	bearer := fileBearer{cfg.AudienceJWTFile}
	for _, token := range []string{"", strings.Repeat("x", 16385), "injected\r\nheader"} {
		if err := os.WriteFile(cfg.AudienceJWTFile, []byte(token), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := bearer.GetRequestMetadata(context.Background()); err == nil {
			t.Fatal("accepted invalid token")
		}
	}
	if !bearer.RequireTransportSecurity() {
		t.Fatal("bearer must require TLS")
	}
}

// Ensure the connection wrapper does not discard prefetched bytes.
func TestBufferedConn(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	reader := bufio.NewReader(a)
	go func() { _, _ = b.Write([]byte("header\nbody")) }()
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4)
	if _, err := io.ReadFull(&bufferedConn{Conn: a, reader: reader}, data); err != nil || string(data) != "body" {
		t.Fatalf("%q %v", data, err)
	}
}
