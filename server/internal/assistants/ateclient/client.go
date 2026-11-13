// Package ateclient provides the narrow control and actor ingress surfaces of ATE v1.0.0.
package ateclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	reflection "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

const controlService = "substrate.ateapi.v1.Control"

// Config contains deployment-trusted settings, never request-supplied paths or addresses.
// Addresses are host:port, not URLs. Empty server names use the address hostname.
// ClientCertFile and ClientKeyFile may name the same combined PEM bundle.
type Config struct {
	APIAddress       string
	RouterAddress    string
	APIServerName    string
	RouterServerName string
	ServiceCAFile    string
	ClientCertFile   string
	ClientKeyFile    string
	AudienceJWTFile  string
	Atespace         string
	ConnectTimeout   time.Duration
	RPCTimeout       time.Duration
}

type Client struct {
	config    Config
	conn      *grpc.ClientConn
	service   protoreflect.ServiceDescriptor
	tlsConfig *tls.Config
}

// New validates credentials and resolves the pinned Control API through authenticated reflection.
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
		cfg.ConnectTimeout = 15 * time.Second
	}
	if cfg.RPCTimeout <= 0 {
		cfg.RPCTimeout = 30 * time.Second
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
	bearer := fileBearer{cfg.AudienceJWTFile}
	if _, err := bearer.GetRequestMetadata(ctx); err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool, GetClientCertificate: loadCert}
	apiTLS := tlsConfig.Clone()
	apiTLS.ServerName = cfg.APIServerName
	conn, err := grpc.NewClient(cfg.APIAddress, grpc.WithTransportCredentials(credentials.NewTLS(apiTLS)), grpc.WithPerRPCCredentials(bearer), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4<<20)))
	if err != nil {
		return nil, errors.New("create ATE control connection")
	}
	c := &Client{config: cfg, conn: conn, tlsConfig: tlsConfig}
	resolveCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	c.service, err = resolveControl(resolveCtx, conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("resolve ATE control schema: %w", err)
	}
	return c, nil
}

func (c *Client) Close() error { return c.conn.Close() }

var allowedMethods = map[string]bool{
	"CreateActor": true, "GetActor": true, "DeleteActor": true, "ListActors": true, "SuspendActor": true, "MintActorJwt": true,
	"CreateActorTemplate": true, "GetActorTemplate": true, "DeleteActorTemplate": true,
	"CreateAtespace": true, "GetAtespace": true, "CreateActorEgressPolicy": true, "GetActorEgressPolicy": true,
}

// Invoke accepts protobuf JSON, rejecting unknown fields and all non-allowlisted RPCs.
// Errors intentionally omit request, response and remote status text.
func (c *Client) Invoke(ctx context.Context, method string, input json.RawMessage) (json.RawMessage, error) {
	if !allowedMethods[method] {
		return nil, errors.New("ATE control method is not allowed")
	}
	md := c.service.Methods().ByName(protoreflect.Name(method))
	if md == nil || md.IsStreamingClient() || md.IsStreamingServer() {
		return nil, errors.New("unsupported ATE control schema")
	}
	req := dynamicpb.NewMessage(md.Input())
	if err := protojson.Unmarshal(input, req); err != nil {
		return nil, errors.New("invalid ATE control request")
	}
	resp := dynamicpb.NewMessage(md.Output())
	ctx, cancel := context.WithTimeout(ctx, c.config.RPCTimeout)
	defer cancel()
	if err := c.conn.Invoke(ctx, "/"+controlService+"/"+method, req, resp); err != nil {
		// Preserve status codes for callers without exposing server-generated bodies.
		return nil, sanitizedRPCError(err)
	}
	out, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(resp)
	if err != nil {
		return nil, errors.New("encode ATE control response")
	}
	return out, nil
}

type fileBearer struct{ path string }

func (fileBearer) RequireTransportSecurity() bool { return true }
func (b fileBearer) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	f, err := os.Open(b.path)
	if err != nil {
		return nil, errors.New("read ATE audience token")
	}
	defer f.Close()
	token, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(token) > 16384 {
		return nil, errors.New("invalid ATE audience token size")
	}
	s := strings.TrimSpace(string(token))
	if s == "" || strings.ContainsAny(s, " \t\r\n") {
		return nil, errors.New("invalid ATE audience token")
	}
	return map[string]string{"authorization": "Bearer " + s}, nil
}

func resolveControl(ctx context.Context, conn *grpc.ClientConn) (protoreflect.ServiceDescriptor, error) {
	stream, err := reflection.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	if err != nil {
		return nil, errors.New("open reflection stream")
	}
	defer stream.CloseSend()
	files := map[string]*descriptorpb.FileDescriptorProto{}
	pending := []*reflection.ServerReflectionRequest{{MessageRequest: &reflection.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: controlService}}}
	for requests := 0; len(pending) > 0; requests++ {
		if len(files) > 128 || requests >= 256 {
			return nil, errors.New("ATE reflection schema too large")
		}
		request := pending[0]
		pending = pending[1:]
		if name := request.GetFileByFilename(); name != "" && files[name] != nil {
			continue
		}
		if err := stream.Send(request); err != nil {
			return nil, errors.New("send reflection request")
		}
		response, err := stream.Recv()
		if err != nil {
			return nil, errors.New("receive reflection response")
		}
		result := response.GetFileDescriptorResponse()
		if result == nil || len(result.FileDescriptorProto) == 0 {
			return nil, errors.New("missing ATE reflection descriptors")
		}
		for _, data := range result.FileDescriptorProto {
			file := new(descriptorpb.FileDescriptorProto)
			if err := proto.Unmarshal(data, file); err != nil || file.GetName() == "" {
				return nil, errors.New("invalid ATE reflection descriptor")
			}
			files[file.GetName()] = file
		}
		for _, file := range files {
			for _, dep := range file.Dependency {
				if files[dep] == nil {
					pending = append(pending, &reflection.ServerReflectionRequest{MessageRequest: &reflection.ServerReflectionRequest_FileByFilename{FileByFilename: dep}})
				}
			}
		}
	}
	set := &descriptorpb.FileDescriptorSet{}
	for _, file := range files {
		set.File = append(set.File, file)
	}
	registry, err := protodesc.NewFiles(set)
	if err != nil {
		return nil, errors.New("invalid ATE schema dependency graph")
	}
	descriptor, err := registry.FindDescriptorByName(controlService)
	if err != nil {
		return nil, errors.New("ATE Control service missing")
	}
	service, ok := descriptor.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, errors.New("ATE Control is not a service")
	}
	return service, nil
}
