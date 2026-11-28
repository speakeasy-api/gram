package assistants

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapiv1 "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistants/ateclient"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	testKitATEImageDigestA = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testKitATEImageDigestB = "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	testKitATEImage        = "ghcr.io/speakeasy-api/kit-ate@" + testKitATEImageDigestA
)

var testKitATEWorkerSelector = map[string]string{"workload-type": "ate-assistants"}

type fakeATEActor struct {
	actor          *ateapiv1.Actor
	egressPolicy   *ateapiv1.EgressPolicy
	bootstrapped   bool
	bootstrapFiles map[string][]byte
	resume503Left  int
	kitReady       bool
}

type fakeATECluster struct {
	ateapiv1.UnimplementedControlServer

	mu                   sync.Mutex
	atespaces            map[string]*ateapiv1.Atespace
	templates            map[string]*ateapiv1.ActorTemplate
	actors               map[string]*fakeATEActor
	nextUID              int
	createAtespaceCalled atomic.Bool
	createTemplateCalls  atomic.Int32

	bootstrapCalls   atomic.Int32
	connect503Count  atomic.Int32
	acpRequestCount  atomic.Int32
	deletedActorUIDs []string
}

func newFakeATECluster() *fakeATECluster {
	return &fakeATECluster{
		atespaces: map[string]*ateapiv1.Atespace{},
		templates: map[string]*ateapiv1.ActorTemplate{},
		actors:    map[string]*fakeATEActor{},
	}
}

func (f *fakeATECluster) GetAtespace(_ context.Context, req *ateapiv1.GetAtespaceRequest) (*ateapiv1.Atespace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	space, ok := f.atespaces[req.GetAtespace().GetName()]
	if !ok {
		return nil, status.Error(codes.NotFound, "atespace not found") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	return space, nil
}

func (f *fakeATECluster) CreateAtespace(_ context.Context, _ *ateapiv1.CreateAtespaceRequest) (*ateapiv1.Atespace, error) {
	f.createAtespaceCalled.Store(true)
	return nil, status.Error(codes.PermissionDenied, "server principal cannot create atespaces") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
}

func (f *fakeATECluster) GetActorTemplate(_ context.Context, req *ateapiv1.GetActorTemplateRequest) (*ateapiv1.ActorTemplate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := req.GetActorTemplate().GetAtespace() + "/" + req.GetActorTemplate().GetName()
	tmpl, ok := f.templates[key]
	if !ok {
		return nil, status.Error(codes.NotFound, "template not found") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	return tmpl, nil
}

func (f *fakeATECluster) CreateActorTemplate(_ context.Context, req *ateapiv1.CreateActorTemplateRequest) (*ateapiv1.ActorTemplate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tmpl := req.GetActorTemplate()
	key := tmpl.GetMetadata().GetAtespace() + "/" + tmpl.GetMetadata().GetName()
	if _, exists := f.templates[key]; exists {
		return nil, status.Error(codes.AlreadyExists, "template exists") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	f.templates[key] = tmpl
	f.createTemplateCalls.Add(1)
	return tmpl, nil
}

func (f *fakeATECluster) GetActor(_ context.Context, req *ateapiv1.GetActorRequest) (*ateapiv1.Actor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := req.GetActor().GetAtespace() + "/" + req.GetActor().GetName()
	entry, ok := f.actors[key]
	if !ok {
		return nil, status.Error(codes.NotFound, "actor not found") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	return entry.actor, nil
}

func (f *fakeATECluster) CreateActor(_ context.Context, req *ateapiv1.CreateActorRequest) (*ateapiv1.Actor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	meta := req.GetActor().GetMetadata()
	key := meta.GetAtespace() + "/" + meta.GetName()
	if _, exists := f.actors[key]; exists {
		return nil, status.Error(codes.AlreadyExists, "actor exists") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	f.nextUID++
	actor := &ateapiv1.Actor{
		Metadata: &ateapiv1.ResourceMetadata{
			Atespace: meta.GetAtespace(),
			Name:     meta.GetName(),
			Uid:      fmt.Sprintf("actor-uid-%d", f.nextUID),
		},
		ActorTemplate: req.GetActor().GetActorTemplate(),
		Status: &ateapiv1.ActorStatus{
			State: ateapiv1.ActorState_ACTOR_STATE_RUNNING,
		},
	}
	f.actors[key] = &fakeATEActor{actor: actor}
	return actor, nil
}

func (f *fakeATECluster) SuspendActor(_ context.Context, req *ateapiv1.SuspendActorRequest) (*ateapiv1.SuspendActorResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := req.GetActor().GetAtespace() + "/" + req.GetActor().GetName()
	entry, ok := f.actors[key]
	if !ok {
		return nil, status.Error(codes.NotFound, "actor not found") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	if req.GetOptions().GetMode() != ateapiv1.DurabilityMode_DURABILITY_MODE_SYNCHRONOUS {
		return nil, status.Error(codes.InvalidArgument, "expected synchronous durability mode") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	gen := entry.actor.GetStatus().GetLastAssignedGeneration() + 1
	entry.actor.Status = &ateapiv1.ActorStatus{
		State:                  ateapiv1.ActorState_ACTOR_STATE_SUSPENDED,
		LastAssignedGeneration: gen,
		Snapshots: []*ateapiv1.Snapshot{{
			Uuid:       fmt.Sprintf("snap-%d", gen),
			Generation: gen,
			Fidelity:   ateapiv1.SnapshotFidelity_SNAPSHOT_FIDELITY_ROOTFS,
			DurableSnapshot: &ateapiv1.SnapshotStorage{
				State: ateapiv1.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED,
			},
		}},
	}
	// Simulate ATE v1.0.1 router returning retryable 503s while the suspended actor resumes.
	entry.resume503Left = 2
	entry.kitReady = false
	return &ateapiv1.SuspendActorResponse{Actor: entry.actor}, nil
}

func (f *fakeATECluster) DeleteActor(_ context.Context, req *ateapiv1.DeleteActorRequest) (*ateapiv1.Actor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := req.GetActor().GetAtespace() + "/" + req.GetActor().GetName()
	entry, ok := f.actors[key]
	if !ok {
		return nil, status.Error(codes.NotFound, "actor not found") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	if uid := req.GetOptions().GetUid(); uid != "" && uid != entry.actor.GetMetadata().GetUid() {
		return nil, status.Error(codes.FailedPrecondition, "actor UID mismatch") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	f.deletedActorUIDs = append(f.deletedActorUIDs, entry.actor.GetMetadata().GetUid())
	delete(f.actors, key)
	return entry.actor, nil
}

func (f *fakeATECluster) GetActorEgressPolicy(_ context.Context, req *ateapiv1.GetActorEgressPolicyRequest) (*ateapiv1.EgressPolicy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := req.GetActor().GetAtespace() + "/" + req.GetActor().GetName()
	entry, ok := f.actors[key]
	if !ok || entry.egressPolicy == nil {
		return nil, status.Error(codes.NotFound, "egress policy not found") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	return entry.egressPolicy, nil
}

func (f *fakeATECluster) CreateActorEgressPolicy(_ context.Context, req *ateapiv1.CreateActorEgressPolicyRequest) (*ateapiv1.EgressPolicy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := req.GetActor().GetAtespace() + "/" + req.GetActor().GetName()
	entry, ok := f.actors[key]
	if !ok {
		return nil, status.Error(codes.NotFound, "actor not found") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	entry.egressPolicy = req.GetEgressPolicy()
	return entry.egressPolicy, nil
}

func (f *fakeATECluster) handleRouterConnect(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if r.Method != http.MethodConnect {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	target := r.Header.Get("ate-target-actor")
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil || host == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	entry, ok := f.actors[target]
	if !ok {
		f.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if entry.actor.GetStatus().GetState() == ateapiv1.ActorState_ACTOR_STATE_SUSPENDED {
		if entry.resume503Left > 0 {
			entry.resume503Left--
			f.mu.Unlock()
			f.connect503Count.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// Actor finished resuming from durable rootfs snapshot; kit-ate-init
		// immediately launches kit serve because .kit/ate-initialized is on disk.
		entry.actor.Status.State = ateapiv1.ActorState_ACTOR_STATE_RUNNING
		if entry.bootstrapped {
			entry.kitReady = true
		}
	}
	f.mu.Unlock()

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
		return
	}
	body, _ := io.ReadAll(inner.Body)
	_ = inner.Body.Close()

	switch port {
	case "8082":
		f.serveInit(entry, rw, inner, body)
	case "8081":
		f.acpRequestCount.Add(1)
		_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		_ = rw.Flush()
	default:
		_, _ = rw.WriteString("HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n")
		_ = rw.Flush()
	}
}

func (f *fakeATECluster) serveInit(entry *fakeATEActor, rw *bufio.ReadWriter, req *http.Request, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/readyz":
		_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
	case req.Method == http.MethodGet && req.URL.Path == "/kit-readyz":
		if entry.kitReady {
			_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
		} else {
			_, _ = rw.WriteString("HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\n\r\n")
		}
	case req.Method == http.MethodPost && req.URL.Path == "/bootstrap":
		f.bootstrapCalls.Add(1)
		if entry.bootstrapped {
			_, _ = rw.WriteString("HTTP/1.1 409 Conflict\r\nContent-Length: 0\r\n\r\n")
			_ = rw.Flush()
			return
		}
		files := map[string][]byte{}
		tr := tar.NewReader(bytes.NewReader(body))
		for {
			hdr, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				_, _ = rw.WriteString("HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n")
				_ = rw.Flush()
				return
			}
			if hdr.Typeflag == tar.TypeReg {
				data, _ := io.ReadAll(tr)
				files[hdr.Name] = data
			}
		}
		entry.bootstrapFiles = files
		entry.bootstrapped = true
		entry.kitReady = true
		_, _ = rw.WriteString("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n")
	default:
		_, _ = rw.WriteString("HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n")
	}
	_ = rw.Flush()
}

type fakeATEProtocol struct {
	mu           sync.Mutex
	turns        []runTurnRequest
	closedThread []uuid.UUID
	client       *guardian.HTTPClient
	endpoint     *url.URL
	token        string
}

func (p *fakeATEProtocol) RunTurn(ctx context.Context, _ assistantRuntimeRecord, req runTurnRequest) error {
	// Exercise the actual ATE actor HTTP tunnel on port 8081 during RunTurn.
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint.String(), strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("build fake ACP request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send fake ACP request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected ACP status %d", resp.StatusCode)
	}
	p.mu.Lock()
	p.turns = append(p.turns, req)
	p.mu.Unlock()
	return nil
}

func (p *fakeATEProtocol) InterruptTurn(context.Context, assistantRuntimeRecord, uuid.UUID) (bool, error) {
	return true, nil
}

func (p *fakeATEProtocol) Status(context.Context, assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	return RuntimeBackendStatus{Configured: true}, nil
}

func (p *fakeATEProtocol) CloseNativeThreadSession(_ context.Context, _ assistantRuntimeRecord, thread uuid.UUID) error {
	p.mu.Lock()
	p.closedThread = append(p.closedThread, thread)
	p.mu.Unlock()
	return nil
}

func startFakeATEStack(t *testing.T, atespace string) (*fakeATECluster, *ateclient.Client) {
	t.Helper()
	cluster := newFakeATECluster()
	cluster.atespaces[atespace] = &ateapiv1.Atespace{
		Metadata: &ateapiv1.ResourceMetadata{Name: atespace, Uid: "space-" + atespace},
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ate.test"},
		DNSNames:              []string{"ate.test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	tokenFile := filepath.Join(dir, "token")
	for path, data := range map[string][]byte{certFile: certPEM, keyFile: keyPEM, tokenFile: []byte("control-jwt")} {
		require.NoError(t, os.WriteFile(path, data, 0o600))
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequestClientCert,
		MinVersion:   tls.VersionTLS12,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) > 0 {
				return errors.New("control API must not receive router client certificate")
			}
			return nil
		},
	})))
	ateapiv1.RegisterControlServer(grpcServer, cluster)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = grpcServer.Serve(lis) }()
	t.Cleanup(grpcServer.Stop)

	router := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cluster.handleRouterConnect(t, w, r)
	}))
	router.TLS = &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}
	router.StartTLS()
	t.Cleanup(router.Close)

	client, err := ateclient.New(t.Context(), ateclient.Config{
		APIAddress:           lis.Addr().String(),
		RouterAddress:        strings.TrimPrefix(router.URL, "https://"),
		APIServerName:        "ate.test",
		RouterServerName:     "ate.test",
		ServiceCAFile:        certFile,
		ClientCertFile:       certFile,
		ClientKeyFile:        keyFile,
		AudienceJWTFile:      tokenFile,
		Atespace:             atespace,
		ConnectTimeout:       2 * time.Second,
		ConnectRetryInterval: 5 * time.Millisecond,
		ConnectRetryTimeout:  2 * time.Second,
		RPCTimeout:           2 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	return cluster, client
}

func TestKitATEBackendPrepareTurnStopResumeAndReap(t *testing.T) {
	t.Parallel()
	const (
		atespace = "prod-assistants"
		template = "kit-v1"
	)

	cluster, client := startFakeATEStack(t, atespace)
	serverURL, err := url.Parse("https://api.speakeasy.test")
	require.NoError(t, err)

	var (
		mu             sync.Mutex
		storedMetadata = []byte("{}")
		revoked        []*KitWorkloadBinding
		protoDriver    *fakeATEProtocol
		bootstrapCount int
	)

	runtimeID := uuid.New()
	assistantID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	issuerID := uuid.New()
	agentID := uuid.New()

	backend, err := NewKitATERuntimeBackend(KitATERuntimeConfig{
		Image:          testKitATEImage,
		Atespace:       atespace,
		Template:       template,
		WorkerSelector: testKitATEWorkerSelector,
		ServerURL:      serverURL,
		Client:         client,
		EgressHosts:    []string{"ai.speakeasy.com", "tools.speakeasy.test"},
		Lifecycle: func(ctx context.Context, r assistantRuntimeRecord, op func(context.Context, assistantRuntimeRecord) error) error {
			mu.Lock()
			r.BackendMetadataJSON = append([]byte(nil), storedMetadata...)
			mu.Unlock()
			progressCtx := context.WithValue(ctx, kitAdmissionProgressKey{}, kitAdmissionProgressWriter(func(_ context.Context, raw []byte) error {
				mu.Lock()
				storedMetadata = append([]byte(nil), raw...)
				mu.Unlock()
				return nil
			}))
			return op(progressCtx, r)
		},
		Token: func(_ context.Context, _ assistantRuntimeRecord, thread uuid.UUID) (string, error) {
			return kitATEActorToken("root-secret", thread.String()), nil
		},
		Bootstrap: func(_ context.Context, r assistantRuntimeRecord, thread uuid.UUID, actorName string) (KitLocalBootstrap, error) {
			bootstrapCount++
			mcpJSON := []byte(`{"mcpServers":{"project-gateway":{"type":"streamable-http","url":"https://tools.speakeasy.test/mcp/gateway/org/projects/proj","headers":{}}}}`)
			return KitLocalBootstrap{
				Model:              "openai/gpt-4.1-mini",
				ReasoningEffort:    "medium",
				EgressInjectedAuth: true,
				InferenceEndpoint:  "https://ai.speakeasy.com/chat/completions",
				AgentsMD:           []byte("# Speakeasy assistant\n"),
				MCPJSON:            mcpJSON,
				GatewayToken:       []byte(kitATEActorToken("root-secret", thread.String())),
				Workload: &KitWorkloadBinding{
					MCPProjectGateway: true,
					IssuerID:          issuerID,
					Subject:           "actor/" + atespace + "/" + actorName,
					Audience:          "https://api.speakeasy.test/oauth/usi/test",
					Identity: assistantidentity.RuntimeIdentity{
						OrganizationID:    "org_test",
						ProjectID:         projectID,
						AssistantID:       assistantID,
						AgentID:           agentID,
						BindingID:         uuid.New(),
						BindingGeneration: 1,
					},
				},
			}, nil
		},
		Protocol: func(_ context.Context, _ assistantRuntimeRecord, _ uuid.UUID, _ string, endpoint *url.URL, hc *guardian.HTTPClient, token string) (runtimeProtocol, error) {
			if protoDriver == nil {
				protoDriver = &fakeATEProtocol{}
			}
			protoDriver.client = hc
			protoDriver.endpoint = endpoint
			protoDriver.token = token
			return protoDriver, nil
		},
		Revoke: func(_ context.Context, _ assistantRuntimeRecord, binding *KitWorkloadBinding) error {
			revoked = append(revoked, binding)
			return nil
		},
		CheckSettled: func(context.Context, assistantRuntimeRecord) error {
			return nil
		},
		HelperPollInterval: 5 * time.Millisecond,
	})
	require.NoError(t, err)

	record := assistantRuntimeRecord{
		ID:                  runtimeID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		RuntimeVersion:      3,
		Backend:             runtimeBackendATE,
		BackendMetadataJSON: []byte("{}"),
	}

	// 1. Ensure creates atespace + actor template with rootfs fidelity.
	ensureRes, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	require.True(t, ensureRes.ColdStart)
	storedMetadata = ensureRes.BackendMetadataJSON
	record.BackendMetadataJSON = ensureRes.BackendMetadataJSON

	// 2. First turn: cold prepare creates actor, configures egress, uploads
	// credential-free bootstrap archive, waits for kit-readyz, and runs turn.
	turn1 := runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium"}
	require.NoError(t, backend.RunTurn(t.Context(), record, turn1))

	require.Equal(t, 1, bootstrapCount)
	require.Equal(t, int32(1), cluster.bootstrapCalls.Load())
	require.Equal(t, int32(1), cluster.acpRequestCount.Load())
	require.Len(t, protoDriver.turns, 1)

	actorName := kitATEActorName(record, threadID)
	actorKey := atespace + "/" + actorName
	cluster.mu.Lock()
	actorEntry := cluster.actors[actorKey]
	require.NotNil(t, actorEntry)
	originalUID := actorEntry.actor.GetMetadata().GetUid()
	require.NotEmpty(t, originalUID)
	require.NotContains(t, string(actorEntry.bootstrapFiles[".kit/config.toml"]), "tokenFile")
	require.NotContains(t, string(actorEntry.bootstrapFiles[".kit/credentials/mcp.json"]), "workload_identity")
	require.NotNil(t, actorEntry.egressPolicy)
	require.Len(t, actorEntry.egressPolicy.GetRules(), 1)
	rule := actorEntry.egressPolicy.GetRules()[0]
	require.Nil(t, rule.GetHttp())
	require.Nil(t, rule.GetHttps())
	require.Equal(t, []string{"ai.speakeasy.com", "api.speakeasy.test", "tools.speakeasy.test"}, rule.GetTlsPassthrough().GetHostnames())
	require.Nil(t, rule.GetTlsPassthrough().GetPorts().GetAll())
	require.Equal(t, []int32{443}, rule.GetTlsPassthrough().GetPorts().GetNumbers())
	cluster.mu.Unlock()

	// 3. Stop settles the native thread session and suspends the actor synchronously.
	require.NoError(t, backend.Stop(t.Context(), record))
	require.Equal(t, []uuid.UUID{threadID}, protoDriver.closedThread)

	cluster.mu.Lock()
	require.Equal(t, ateapiv1.ActorState_ACTOR_STATE_SUSPENDED, actorEntry.actor.GetStatus().GetState())
	cluster.mu.Unlock()

	// 4. Non-retired ReapStoppedMachine refuses to discard suspended workspace.
	err = backend.ReapStoppedMachine(t.Context(), record)
	require.ErrorContains(t, err, "must not be reaped by TTL")

	// 5. Resume after suspend: router returns retryable 503s while actor resumes,
	// ateclient retries transparently, /bootstrap is skipped (Configured == true),
	// and second turn runs on the exact same actor UID.
	turn2 := runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium"}
	require.NoError(t, backend.RunTurn(t.Context(), record, turn2))

	require.Equal(t, int32(2), cluster.connect503Count.Load(), "router 503 during resume must be retried")
	require.Equal(t, 1, bootstrapCount, "resume must not re-run bootstrap")
	require.Equal(t, int32(1), cluster.bootstrapCalls.Load(), "resume must not re-POST /bootstrap")
	require.Equal(t, int32(2), cluster.acpRequestCount.Load())
	require.Len(t, protoDriver.turns, 2)

	cluster.mu.Lock()
	require.Equal(t, ateapiv1.ActorState_ACTOR_STATE_RUNNING, actorEntry.actor.GetStatus().GetState())
	require.Equal(t, originalUID, actorEntry.actor.GetMetadata().GetUid())
	cluster.mu.Unlock()

	// 6. Reap withdraws workload binding and deletes the actor by exact UID.
	record.Retired = true
	require.NoError(t, backend.ReapStoppedMachine(t.Context(), record))
	require.Len(t, revoked, 1)
	require.Equal(t, "actor/"+atespace+"/"+actorName, revoked[0].Subject)

	cluster.mu.Lock()
	require.Equal(t, []string{originalUID}, cluster.deletedActorUIDs)
	require.Empty(t, cluster.actors)
	cluster.mu.Unlock()

	// Reap is idempotent once the actor is gone.
	require.NoError(t, backend.Reap(t.Context(), record))
}

func TestKitATEActorNamesBindThreadAndGeneration(t *testing.T) {
	t.Parallel()
	r := assistantRuntimeRecord{ID: uuid.New()}
	thread := uuid.New()
	name := kitATEActorName(r, thread)
	require.LessOrEqual(t, len(name), maxATEDNSLabelLen)
	require.True(t, kitATENamePattern.MatchString(name))
	require.Equal(t, name, kitATEActorName(r, thread))
	require.NotEqual(t, name, kitATEActorName(r, uuid.New()))
	r.ID = uuid.New()
	require.NotEqual(t, name, kitATEActorName(r, thread))
}

func TestKitATEMetadataRejectsForeignBinding(t *testing.T) {
	t.Parallel()
	k := &KitATERuntimeBackend{
		config:      KitATERuntimeConfig{Image: testKitATEImage, Atespace: "prod-space", Template: "kit"},
		imageDigest: testKitATEImageDigestA,
	}
	r := assistantRuntimeRecord{ID: uuid.New(), AssistantID: uuid.New(), ProjectID: uuid.New(), Backend: runtimeBackendATE, RuntimeVersion: 3}
	m, err := k.metadata(r)
	require.NoError(t, err)
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	r.BackendMetadataJSON = raw
	_, err = k.metadata(r)
	require.NoError(t, err)
	r.ProjectID = uuid.New()
	_, err = k.metadata(r)
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
}

func TestParseKitATEWorkerSelector(t *testing.T) {
	t.Parallel()
	labels, err := ParseKitATEWorkerSelector("workload-type=ate-assistants,ate.dev/tier=prod")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"workload-type": "ate-assistants", "ate.dev/tier": "prod"}, labels)

	longLabel := strings.Repeat("a", 64)
	longPrefix := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 63)
	for _, invalid := range []string{
		"",
		"   ",
		"noequals",
		"=val",
		"key=",
		"a=b,a=c",
		"bad key=val",
		"key=bad val",
		"a..b/key=val",
		"-bad/key=val",
		longLabel + "=val",
		"ate.dev/" + longLabel + "=val",
		longLabel + ".ate.dev/key=val",
		longPrefix + "/key=val",
		"key=" + longLabel,
	} {
		_, err := ParseKitATEWorkerSelector(invalid)
		require.Error(t, err, invalid)
	}
}

func TestKitATEEnsureFailsWhenAtespaceMissing(t *testing.T) {
	t.Parallel()
	const (
		atespace = "prod-assistants"
		template = "kit-v1"
	)
	cluster, client := startFakeATEStack(t, atespace)
	// Remove the pre-created atespace after client startup to verify Ensure fails clearly
	// without attempting CreateAtespace.
	cluster.mu.Lock()
	delete(cluster.atespaces, atespace)
	cluster.mu.Unlock()

	serverURL, err := url.Parse("https://api.speakeasy.test")
	require.NoError(t, err)
	backend, err := NewKitATERuntimeBackend(KitATERuntimeConfig{
		Image:          testKitATEImage,
		Atespace:       atespace,
		Template:       template,
		WorkerSelector: testKitATEWorkerSelector,
		ServerURL:      serverURL,
		Client:         client,
		Lifecycle: func(ctx context.Context, r assistantRuntimeRecord, op func(context.Context, assistantRuntimeRecord) error) error {
			return op(ctx, r)
		},
		Token: func(context.Context, assistantRuntimeRecord, uuid.UUID) (string, error) { return "tok", nil },
		Bootstrap: func(context.Context, assistantRuntimeRecord, uuid.UUID, string) (KitLocalBootstrap, error) {
			return KitLocalBootstrap{}, nil
		},
		Protocol: func(context.Context, assistantRuntimeRecord, uuid.UUID, string, *url.URL, *guardian.HTTPClient, string) (runtimeProtocol, error) {
			return nil, nil
		},
		Revoke:       func(context.Context, assistantRuntimeRecord, *KitWorkloadBinding) error { return nil },
		CheckSettled: func(context.Context, assistantRuntimeRecord) error { return nil },
	})
	require.NoError(t, err)

	_, err = backend.Ensure(t.Context(), assistantRuntimeRecord{
		ID:                  uuid.New(),
		AssistantID:         uuid.New(),
		ProjectID:           uuid.New(),
		RuntimeVersion:      3,
		Backend:             runtimeBackendATE,
		BackendMetadataJSON: []byte("{}"),
	})
	require.ErrorContains(t, err, `ATE atespace "prod-assistants" does not exist`)
	require.False(t, cluster.createAtespaceCalled.Load(), "server must never call CreateAtespace")
}

func TestKitATEImageIdentityKeysOnDigestOnly(t *testing.T) {
	t.Parallel()
	const (
		atespace = "prod-assistants"
		template = "kit-v1"
	)
	cluster, client := startFakeATEStack(t, atespace)
	serverURL, err := url.Parse("https://api.speakeasy.test")
	require.NoError(t, err)

	var (
		mu             sync.Mutex
		storedMetadata = []byte("{}")
	)
	runtimeID := uuid.New()
	assistantID := uuid.New()
	projectID := uuid.New()
	thread1 := uuid.New()
	thread2 := uuid.New()
	issuerID := uuid.New()
	agentID := uuid.New()

	newBackend := func(imageRef string) *KitATERuntimeBackend {
		t.Helper()
		b, err := NewKitATERuntimeBackend(KitATERuntimeConfig{
			Image:          imageRef,
			Atespace:       atespace,
			Template:       template,
			WorkerSelector: testKitATEWorkerSelector,
			ServerURL:      serverURL,
			Client:         client,
			EgressHosts:    []string{"ai.speakeasy.com"},
			Lifecycle: func(ctx context.Context, r assistantRuntimeRecord, op func(context.Context, assistantRuntimeRecord) error) error {
				mu.Lock()
				r.BackendMetadataJSON = append([]byte(nil), storedMetadata...)
				mu.Unlock()
				progressCtx := context.WithValue(ctx, kitAdmissionProgressKey{}, kitAdmissionProgressWriter(func(_ context.Context, raw []byte) error {
					mu.Lock()
					storedMetadata = append([]byte(nil), raw...)
					mu.Unlock()
					return nil
				}))
				return op(progressCtx, r)
			},
			Token: func(_ context.Context, _ assistantRuntimeRecord, thread uuid.UUID) (string, error) {
				return kitATEActorToken("root-secret", thread.String()), nil
			},
			Bootstrap: func(_ context.Context, _ assistantRuntimeRecord, thread uuid.UUID, actorName string) (KitLocalBootstrap, error) {
				return KitLocalBootstrap{
					Model:              "openai/gpt-4.1-mini",
					ReasoningEffort:    "medium",
					EgressInjectedAuth: true,
					InferenceEndpoint:  "https://ai.speakeasy.com/chat/completions",
					AgentsMD:           []byte("# Speakeasy assistant\n"),
					MCPJSON:            []byte(`{"mcpServers":{"project-gateway":{"type":"streamable-http","url":"https://api.speakeasy.test/mcp/gateway/org/projects/proj","headers":{}}}}`),
					GatewayToken:       []byte(kitATEActorToken("root-secret", thread.String())),
					Workload: &KitWorkloadBinding{
						MCPProjectGateway: true,
						IssuerID:          issuerID,
						Subject:           "actor/" + atespace + "/" + actorName,
						Audience:          "https://api.speakeasy.test/oauth/usi/test",
						Identity: assistantidentity.RuntimeIdentity{
							OrganizationID:    "org_test",
							ProjectID:         projectID,
							AssistantID:       assistantID,
							AgentID:           agentID,
							BindingID:         uuid.New(),
							BindingGeneration: 1,
						},
					},
				}, nil
			},
			Protocol: func(_ context.Context, _ assistantRuntimeRecord, _ uuid.UUID, _ string, endpoint *url.URL, hc *guardian.HTTPClient, token string) (runtimeProtocol, error) {
				return &fakeATEProtocol{client: hc, endpoint: endpoint, token: token}, nil
			},
			Revoke:             func(context.Context, assistantRuntimeRecord, *KitWorkloadBinding) error { return nil },
			CheckSettled:       func(context.Context, assistantRuntimeRecord) error { return nil },
			HelperPollInterval: 5 * time.Millisecond,
		})
		require.NoError(t, err)
		return b
	}

	record := assistantRuntimeRecord{
		ID:                  runtimeID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		RuntimeVersion:      3,
		Backend:             runtimeBackendATE,
		BackendMetadataJSON: []byte("{}"),
	}

	// Release 1: tag sha-111... with digest A.
	imgRel1 := "gcr.io/example/gram-ate-runtime:sha-1111111111111111111111111111111111111111@" + testKitATEImageDigestA
	b1 := newBackend(imgRel1)
	res1, err := b1.Ensure(t.Context(), record)
	require.NoError(t, err)
	storedMetadata = res1.BackendMetadataJSON
	record.BackendMetadataJSON = res1.BackendMetadataJSON
	require.Equal(t, int32(1), cluster.createTemplateCalls.Load())
	tmplA := kitATETemplateVersionName(template, testKitATEImageDigestA)

	require.NoError(t, b1.RunTurn(t.Context(), record, runTurnRequest{ThreadID: thread1, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium"}))
	require.NoError(t, b1.Stop(t.Context(), record))

	actor1Key := atespace + "/" + kitATEActorName(record, thread1)
	cluster.mu.Lock()
	uid1 := cluster.actors[actor1Key].actor.GetMetadata().GetUid()
	require.Equal(t, tmplA, cluster.actors[actor1Key].actor.GetActorTemplate().GetName())
	require.Equal(t, testKitATEWorkerSelector, cluster.templates[atespace+"/"+tmplA].GetWorkerSelector().GetMatchLabels())
	cluster.mu.Unlock()

	// Release 2: different commit tag, SAME digest A -> NO new template, NO actor replacement.
	imgRel2 := "gcr.io/example/gram-ate-runtime:sha-2222222222222222222222222222222222222222@" + testKitATEImageDigestA
	b2 := newBackend(imgRel2)
	record.BackendMetadataJSON = storedMetadata
	res2, err := b2.Ensure(t.Context(), record)
	require.NoError(t, err)
	storedMetadata = res2.BackendMetadataJSON
	record.BackendMetadataJSON = res2.BackendMetadataJSON
	require.Equal(t, int32(1), cluster.createTemplateCalls.Load(), "same digest with new tag must not create a new template")

	require.NoError(t, b2.RunTurn(t.Context(), record, runTurnRequest{ThreadID: thread1, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium"}))
	require.Equal(t, int32(1), cluster.bootstrapCalls.Load(), "existing actor must not be replaced on tag-only release")
	require.NoError(t, b2.Stop(t.Context(), record))

	// Release 3: DIFFERENT digest B -> creates new template version <template>-<digestB[:12]>;
	// existing suspended actor keeps running on old template/image until recreated,
	// while a new thread immediately uses the new template version.
	imgRel3 := "gcr.io/example/gram-ate-runtime:sha-3333333333333333333333333333333333333333@" + testKitATEImageDigestB
	b3 := newBackend(imgRel3)
	record.BackendMetadataJSON = storedMetadata
	res3, err := b3.Ensure(t.Context(), record)
	require.NoError(t, err)
	storedMetadata = res3.BackendMetadataJSON
	record.BackendMetadataJSON = res3.BackendMetadataJSON
	require.Equal(t, int32(2), cluster.createTemplateCalls.Load(), "new digest must create a new template version")
	tmplB := kitATETemplateVersionName(template, testKitATEImageDigestB)
	require.NotEqual(t, tmplA, tmplB)

	// Existing suspended actor resumes cleanly on its existing template (tmplA) and UID.
	require.NoError(t, b3.RunTurn(t.Context(), record, runTurnRequest{ThreadID: thread1, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium"}))
	cluster.mu.Lock()
	require.Equal(t, uid1, cluster.actors[actor1Key].actor.GetMetadata().GetUid())
	require.Equal(t, tmplA, cluster.actors[actor1Key].actor.GetActorTemplate().GetName())
	cluster.mu.Unlock()

	// New thread uses the new template version (tmplB).
	require.NoError(t, b3.RunTurn(t.Context(), record, runTurnRequest{ThreadID: thread2, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium"}))
	actor2Key := atespace + "/" + kitATEActorName(record, thread2)
	cluster.mu.Lock()
	require.Equal(t, tmplB, cluster.actors[actor2Key].actor.GetActorTemplate().GetName())
	cluster.mu.Unlock()
}

func TestKitATETrustedEgressHosts(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"api.example.test", "ai.speakeasy.com"} {
		require.True(t, kitATETrustedDNSHost(host), host)
	}
	for _, host := range []string{"169.254.169.254", "127.0.0.1", "[::1]", "*.example.test", "localhost", "example.test/path", "example.test:443", "user@example.test"} {
		require.False(t, kitATETrustedDNSHost(host), host)
	}
}

func TestKitATEStoppedTTLRetainsReusableActor(t *testing.T) {
	t.Parallel()
	calls := 0
	k := &KitATERuntimeBackend{config: KitATERuntimeConfig{Lifecycle: func(ctx context.Context, r assistantRuntimeRecord, operation func(context.Context, assistantRuntimeRecord) error) error {
		calls++
		return operation(ctx, r)
	}}}
	err := k.ReapStoppedMachine(t.Context(), assistantRuntimeRecord{Backend: runtimeBackendATE, RuntimeVersion: 3})
	require.ErrorContains(t, err, "must not be reaped by TTL")
	require.Equal(t, 1, calls)
}

func TestKitATETemplateMatchRejectsSecurityRelevantDrift(t *testing.T) {
	t.Parallel()
	k := &KitATERuntimeBackend{
		config: KitATERuntimeConfig{
			Image:          testKitATEImage,
			Atespace:       "prod-assistants",
			Template:       "kit-v1",
			WorkerSelector: testKitATEWorkerSelector,
		},
		imageDigest: testKitATEImageDigestA,
	}
	desired := k.desiredTemplate(k.templateVersionName())

	// Identical spec with different mutable tag (same digest) and server-populated metadata/status matches.
	sameDigest, ok := proto.Clone(desired).(*ateapiv1.ActorTemplate)
	require.True(t, ok)
	sameDigest.Metadata.Uid = "server-uid"
	sameDigest.Metadata.Version = 3
	sameDigest.Status = &ateapiv1.ActorTemplateStatus{}
	sameDigest.Containers[0].Image = "gcr.io/example/gram-ate-runtime:sha-other@" + testKitATEImageDigestA
	require.True(t, matchesKitATETemplate(sameDigest, desired, testKitATEImageDigestA))

	mutations := []struct {
		name   string
		mutate func(*ateapiv1.ActorTemplate)
	}{
		{"container env", func(tmpl *ateapiv1.ActorTemplate) {
			tmpl.Containers[0].Env = []*ateapiv1.EnvVar{{Name: "LD_PRELOAD", Value: "/tmp/evil.so"}}
		}},
		{"container security context", func(tmpl *ateapiv1.ActorTemplate) {
			tmpl.Containers[0].SecurityContext = &ateapiv1.SecurityContext{
				Capabilities: &ateapiv1.Capabilities{Add: []string{"SYS_ADMIN"}},
			}
		}},
		{"container resources", func(tmpl *ateapiv1.ActorTemplate) {
			tmpl.Containers[0].Resources = &ateapiv1.ResourceRequirements{
				Limits: &ateapiv1.Resources{CpuMillicores: 2000, MemoryBytes: 1 << 30},
			}
		}},
		{"container args", func(tmpl *ateapiv1.ActorTemplate) {
			tmpl.Containers[0].Args = []string{"--extra"}
		}},
		{"container volume mounts", func(tmpl *ateapiv1.ActorTemplate) {
			tmpl.Containers[0].VolumeMounts = []*ateapiv1.VolumeMount{{Name: "extra", MountPath: "/extra"}}
		}},
		{"template volumes", func(tmpl *ateapiv1.ActorTemplate) {
			tmpl.Volumes = []*ateapiv1.Volume{{Name: "extra"}}
		}},
		{"snapshot storage location", func(tmpl *ateapiv1.ActorTemplate) {
			tmpl.SnapshotConfig.StorageLocation = "gs://attacker-bucket/snaps"
		}},
		{"golden egress policy", func(tmpl *ateapiv1.ActorTemplate) {
			tmpl.GoldenEgressPolicy = &ateapiv1.EgressPolicyTemplate{}
		}},
		{"extra template resource limits", func(tmpl *ateapiv1.ActorTemplate) {
			tmpl.Resources.Limits.CpuMillicores = ateKitCPUMillicores + 500
		}},
	}

	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			candidate, ok := proto.Clone(desired).(*ateapiv1.ActorTemplate)
			require.True(t, ok)
			tc.mutate(candidate)
			require.False(t, matchesKitATETemplate(candidate, desired, testKitATEImageDigestA), tc.name)
		})
	}
}
