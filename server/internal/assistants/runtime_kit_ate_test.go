package assistants

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
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
	actor               *ateapiv1.Actor
	egressPolicy        *ateapiv1.EgressPolicy
	bootstrapped        bool
	hasBootstrapHash    bool
	bootstrapHash       [32]byte
	bootstrapFiles      map[string][]byte
	bootstrapEnv        map[string]string
	resume503Left       int
	kitReady            bool
	draining            bool
	failRevert          bool
	initVersionOverride string
}

type fakeATECluster struct {
	ateapiv1.UnimplementedControlServer

	mu                   sync.Mutex
	atespaces            map[string]*ateapiv1.Atespace
	templates            map[string]*ateapiv1.ActorTemplate
	actors               map[string]*fakeATEActor
	nextUID              int
	nextWorker           int
	createAtespaceCalled atomic.Bool
	createTemplateCalls  atomic.Int32
	initVersionOverride  string

	bootstrapCalls   atomic.Int32
	connect503Count  atomic.Int32
	acpRequestCount  atomic.Int32
	resumeCalls      atomic.Int32
	revertCalls      atomic.Int32
	suspendCalls     atomic.Int32
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
	f.nextWorker++
	workerNode := fmt.Sprintf("worker-%d", f.nextWorker)
	actor := &ateapiv1.Actor{
		Metadata: &ateapiv1.ResourceMetadata{
			Atespace: meta.GetAtespace(),
			Name:     meta.GetName(),
			Uid:      fmt.Sprintf("actor-uid-%d", f.nextUID),
		},
		ActorTemplate: req.GetActor().GetActorTemplate(),
		Status: &ateapiv1.ActorStatus{
			State:        ateapiv1.ActorState_ACTOR_STATE_RUNNING,
			AssignedNode: workerNode,
			WorkerAssignment: &ateapiv1.WorkerAssignment{
				Worker: &ateapiv1.ObjectRef{Name: workerNode},
			},
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
	f.suspendCalls.Add(1)
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
	// Simulate ATE v1.0.1 router returning retryable 503s while the resumed actor registers on its new worker.
	// SNAPSHOT_FIDELITY_ROOTFS preserves rootfs files but restarts kit-ate-init fresh on resume.
	entry.resume503Left = 2
	entry.bootstrapped = false
	entry.hasBootstrapHash = false
	entry.bootstrapHash = [32]byte{}
	entry.kitReady = false
	entry.draining = false
	return &ateapiv1.SuspendActorResponse{Actor: entry.actor}, nil
}

func (f *fakeATECluster) ResumeActor(_ context.Context, req *ateapiv1.ResumeActorRequest) (*ateapiv1.ResumeActorResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := req.GetActor().GetAtespace() + "/" + req.GetActor().GetName()
	entry, ok := f.actors[key]
	if !ok {
		return nil, status.Error(codes.NotFound, "actor not found") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	st := entry.actor.GetStatus().GetState()
	if st == ateapiv1.ActorState_ACTOR_STATE_CRASHED {
		return nil, status.Error(codes.FailedPrecondition, "crashed actor cannot resume before revert") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	if st == ateapiv1.ActorState_ACTOR_STATE_RUNNING {
		return &ateapiv1.ResumeActorResponse{Actor: entry.actor, Resumed: false}, nil
	}
	f.resumeCalls.Add(1)
	f.nextWorker++
	workerNode := fmt.Sprintf("worker-%d", f.nextWorker)
	entry.actor.Status.State = ateapiv1.ActorState_ACTOR_STATE_RUNNING
	entry.actor.Status.AssignedNode = workerNode
	entry.actor.Status.WorkerAssignment = &ateapiv1.WorkerAssignment{Worker: &ateapiv1.ObjectRef{Name: workerNode}}
	entry.draining = false
	return &ateapiv1.ResumeActorResponse{Actor: entry.actor, Resumed: true}, nil
}

func (f *fakeATECluster) RevertActor(_ context.Context, req *ateapiv1.RevertActorRequest) (*ateapiv1.RevertActorResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := req.GetActor().GetAtespace() + "/" + req.GetActor().GetName()
	entry, ok := f.actors[key]
	if !ok {
		return nil, status.Error(codes.NotFound, "actor not found") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	f.revertCalls.Add(1)
	if entry.failRevert {
		return nil, status.Error(codes.Internal, "simulated revert failure") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	st := entry.actor.GetStatus().GetState()
	if st != ateapiv1.ActorState_ACTOR_STATE_CRASHED && st != ateapiv1.ActorState_ACTOR_STATE_REVERTING && st != ateapiv1.ActorState_ACTOR_STATE_RUNNING {
		return nil, status.Error(codes.FailedPrecondition, "actor is not in a revertable state") //nolint:wrapcheck // gRPC server handler returns status.Error directly.
	}
	entry.actor.Status.State = ateapiv1.ActorState_ACTOR_STATE_SUSPENDED
	entry.actor.Status.AssignedNode = ""
	entry.actor.Status.WorkerAssignment = nil
	entry.actor.Status.Crash = nil
	entry.bootstrapped = false
	entry.hasBootstrapHash = false
	entry.bootstrapHash = [32]byte{}
	entry.draining = false
	entry.kitReady = false
	return &ateapiv1.RevertActorResponse{Actor: entry.actor}, nil
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
	if entry.actor.GetStatus().GetState() == ateapiv1.ActorState_ACTOR_STATE_CRASHED {
		f.mu.Unlock()
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	if entry.resume503Left > 0 {
		entry.resume503Left--
		f.mu.Unlock()
		f.connect503Count.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if entry.actor.GetStatus().GetState() == ateapiv1.ActorState_ACTOR_STATE_SUSPENDED {
		entry.actor.Status.State = ateapiv1.ActorState_ACTOR_STATE_RUNNING
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

	initVersion := fmt.Sprintf("%d", ateInitWireVersion)
	if f.initVersionOverride != "" {
		initVersion = f.initVersionOverride
	}
	if entry.initVersionOverride != "" {
		initVersion = entry.initVersionOverride
	}
	versionHdr := fmt.Sprintf("%s: %s\r\n", ateInitVersionHeader, initVersion)

	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/readyz":
		_, _ = fmt.Fprintf(rw, "HTTP/1.1 200 OK\r\n%sContent-Length: 0\r\n\r\n", versionHdr)
	case req.Method == http.MethodGet && req.URL.Path == "/status":
		payload, _ := json.Marshal(struct {
			Draining bool `json:"draining"`
			Started  bool `json:"started"`
		}{Draining: entry.draining, Started: entry.bootstrapped})
		_, _ = fmt.Fprintf(rw, "HTTP/1.1 200 OK\r\n%sContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", versionHdr, len(payload), payload)
	case req.Method == http.MethodGet && req.URL.Path == "/app-readyz":
		if entry.kitReady {
			_, _ = fmt.Fprintf(rw, "HTTP/1.1 200 OK\r\n%sContent-Length: 0\r\n\r\n", versionHdr)
		} else {
			_, _ = fmt.Fprintf(rw, "HTTP/1.1 503 Service Unavailable\r\n%sContent-Length: 0\r\n\r\n", versionHdr)
		}
	case req.Method == http.MethodPost && req.URL.Path == "/bootstrap":
		f.bootstrapCalls.Add(1)
		sum := sha256.Sum256(body)
		if entry.bootstrapped {
			if entry.hasBootstrapHash && sum == entry.bootstrapHash {
				_, _ = fmt.Fprintf(rw, "HTTP/1.1 204 No Content\r\n%sContent-Length: 0\r\n\r\n", versionHdr)
				_ = rw.Flush()
				return
			}
			_, _ = fmt.Fprintf(rw, "HTTP/1.1 409 Conflict\r\n%sContent-Length: 0\r\n\r\n", versionHdr)
			_ = rw.Flush()
			return
		}
		var payload kitATEBootstrapRequest
		if err := json.Unmarshal(body, &payload); err != nil || payload.Version != ateInitWireVersion {
			_, _ = fmt.Fprintf(rw, "HTTP/1.1 400 Bad Request\r\n%sContent-Length: 0\r\n\r\n", versionHdr)
			_ = rw.Flush()
			return
		}
		files := make(map[string][]byte, len(payload.Files))
		for _, fEntry := range payload.Files {
			files[fEntry.Path] = fEntry.Content
		}
		entry.bootstrapFiles = files
		entry.bootstrapEnv = payload.Env
		entry.bootstrapped = true
		entry.hasBootstrapHash = true
		entry.bootstrapHash = sum
		entry.kitReady = true
		_, _ = fmt.Fprintf(rw, "HTTP/1.1 204 No Content\r\n%sContent-Length: 0\r\n\r\n", versionHdr)
	default:
		_, _ = fmt.Fprintf(rw, "HTTP/1.1 404 Not Found\r\n%sContent-Length: 0\r\n\r\n", versionHdr)
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

func (p *fakeATEProtocol) VerifyNativeThreadSettled(_ context.Context, _ assistantRuntimeRecord, _ uuid.UUID) error {
	return nil
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

type fakeKitATEActorStore struct {
	mu               sync.Mutex
	actors           map[uuid.UUID]kitATEThreadActor
	threadInitiators map[uuid.UUID]ateThreadInitiator
}

func newFakeKitATEActorStore() *fakeKitATEActorStore {
	return &fakeKitATEActorStore{
		actors:           map[uuid.UUID]kitATEThreadActor{},
		threadInitiators: map[uuid.UUID]ateThreadInitiator{},
	}
}

func (s *fakeKitATEActorStore) GetForThread(_ context.Context, r assistantRuntimeRecord, threadID uuid.UUID) (kitATEThreadActor, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.actors {
		if a.RuntimeID == r.ID && a.ThreadID == threadID {
			return a, true, nil
		}
	}
	return kitATEThreadActor{}, false, nil
}

func (s *fakeKitATEActorStore) GetThreadInitiator(_ context.Context, _ assistantRuntimeRecord, threadID uuid.UUID) (ateThreadInitiator, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	init, ok := s.threadInitiators[threadID]
	return init, ok, nil
}

func (s *fakeKitATEActorStore) Insert(_ context.Context, r assistantRuntimeRecord, actor kitATEThreadActor) (kitATEThreadActor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateATEThreadInitiator(actor.Initiator); err != nil {
		return kitATEThreadActor{}, err
	}
	actor.ID = uuid.New()
	actor.RuntimeID = r.ID
	s.actors[actor.ID] = actor
	if _, exists := s.threadInitiators[actor.ThreadID]; !exists {
		s.threadInitiators[actor.ThreadID] = actor.Initiator
	}
	return actor, nil
}

func (s *fakeKitATEActorStore) UpdateBootstrap(_ context.Context, _ assistantRuntimeRecord, actor kitATEThreadActor) (kitATEThreadActor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actors[actor.ID] = actor
	return actor, nil
}

func (s *fakeKitATEActorStore) UpdateState(_ context.Context, _ assistantRuntimeRecord, actorID uuid.UUID, state string, warmUntil *time.Time) (kitATEThreadActor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	actor, ok := s.actors[actorID]
	if !ok {
		return kitATEThreadActor{}, ErrKitRuntimeBinding
	}
	actor.State = state
	actor.WarmUntil = warmUntil
	s.actors[actorID] = actor
	return actor, nil
}

func (s *fakeKitATEActorStore) SoftDelete(_ context.Context, _ assistantRuntimeRecord, actorID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.actors, actorID)
	return nil
}

func (s *fakeKitATEActorStore) ListForRuntime(_ context.Context, r assistantRuntimeRecord) ([]kitATEThreadActor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []kitATEThreadActor
	for _, a := range s.actors {
		if a.RuntimeID == r.ID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *fakeKitATEActorStore) CountForRuntime(_ context.Context, r assistantRuntimeRecord) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for _, a := range s.actors {
		if a.RuntimeID == r.ID {
			n++
		}
	}
	return n, nil
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
		store          = newFakeKitATEActorStore()
	)

	runtimeID := uuid.New()
	assistantID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	agentID := uuid.New()

	backend, err := NewKitATERuntimeBackend(KitATERuntimeConfig{
		Image:          testKitATEImage,
		Atespace:       atespace,
		Template:       template,
		WorkerSelector: testKitATEWorkerSelector,
		ServerURL:      serverURL,
		Client:         client,
		Lifecycle: func(ctx context.Context, r assistantRuntimeRecord, op func(context.Context, assistantRuntimeRecord) error) error {
			mu.Lock()
			r.BackendMetadataJSON = append([]byte(nil), storedMetadata...)
			mu.Unlock()
			return op(ctx, r)
		},
		ThreadLifecycle: func(ctx context.Context, r assistantRuntimeRecord, _ uuid.UUID, op func(context.Context, assistantRuntimeRecord) error) error {
			mu.Lock()
			r.BackendMetadataJSON = append([]byte(nil), storedMetadata...)
			mu.Unlock()
			return op(ctx, r)
		},
		Store: store,
		Token: func(_ context.Context, _ assistantRuntimeRecord, thread uuid.UUID) (string, error) {
			return kitATEActorToken("root-secret", thread.String()), nil
		},
		Bootstrap: func(_ context.Context, r assistantRuntimeRecord, thread uuid.UUID, actorName string, _ *KitWorkloadBinding) (KitLocalBootstrap, error) {
			bootstrapCount++
			mcpJSON := []byte(`{"mcpServers":{"project-gateway":{"type":"streamable-http","url":"https://tools.speakeasy.test/mcp/gateway/org/projects/proj","headers":{"Authorization":"Bearer ` + kitATEEgressAuthPlaceholder + `"}}}}`)
			return KitLocalBootstrap{
				Model:              "openai/gpt-4.1-mini",
				ReasoningEffort:    "medium",
				EgressInjectedAuth: true,
				InferenceEndpoint:  "https://api.speakeasy.test/chat/completions",
				AgentsMD:           []byte("# Speakeasy assistant\n"),
				MCPJSON:            mcpJSON,
				GatewayToken:       []byte(kitATEActorToken("root-secret", thread.String())),
				Workload: &KitWorkloadBinding{
					MCPProjectGateway: true,
					IssuerID:          uuid.Nil,
					Subject:           "actor/" + atespace + "/" + actorName,
					Audience:          "",
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
	turn1 := runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}
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
	require.NotContains(t, string(actorEntry.bootstrapFiles[".kit/config.toml"]), "jwtBearer")
	require.Contains(t, string(actorEntry.bootstrapFiles[".kit/config.toml"]), `provider = "openrouter"`)
	require.NotContains(t, string(actorEntry.bootstrapFiles[".kit/credentials/mcp.json"]), "workload_identity")
	require.Contains(t, string(actorEntry.bootstrapFiles[".kit/credentials/mcp.json"]), "Bearer "+kitATEEgressAuthPlaceholder)
	require.Equal(t, "https://api.speakeasy.test/chat/completions", actorEntry.bootstrapEnv["OPENROUTER_BASE_URL"])
	require.Equal(t, kitATEEgressAuthPlaceholder, actorEntry.bootstrapEnv["OPENROUTER_API_KEY"])

	// Verify ActorTemplate matches the exact desired contract including ATE's projected egress-mitm + system-roots trust bundle.
	tmplKey := atespace + "/" + kitATETemplateVersionName(template, testKitATEImageDigestA)
	createdTmpl := cluster.templates[tmplKey]
	require.NotNil(t, createdTmpl)
	require.True(t, matchesKitATETemplate(createdTmpl, backend.desiredTemplate(kitATETemplateVersionName(template, testKitATEImageDigestA)), testKitATEImageDigestA))

	// Verify EgressPolicy has split HTTPS MITM+credential_uri rules for api.speakeasy.test (inference)
	// and tools.speakeasy.test (project gateway), plus * tls_passthrough (:443) and * http (:80) with nil effects.
	wantURI := "ate-secret://speakeasy.com/v1/actors/" + atespace + "/" + actorName + "/" + originalUID
	require.NotNil(t, actorEntry.egressPolicy)
	require.Len(t, actorEntry.egressPolicy.GetRules(), 4)
	rules := actorEntry.egressPolicy.GetRules()
	require.NoError(t, validateKitATEEgressRulesNoWildcardEffects(rules))
	require.Equal(t, []string{"api.speakeasy.test"}, rules[0].GetHttps().GetHostnames())
	require.Equal(t, wantURI, rules[0].GetHttps().GetEffects().GetReplaceHeaders()[0].GetCredentialUri())
	require.Nil(t, rules[0].GetHttps().GetEffects().GetReplaceHeaders()[0].GetActorJwt())
	require.Equal(t, []string{"tools.speakeasy.test"}, rules[1].GetHttps().GetHostnames())
	require.Equal(t, wantURI, rules[1].GetHttps().GetEffects().GetReplaceHeaders()[0].GetCredentialUri())
	require.Nil(t, rules[1].GetHttps().GetEffects().GetReplaceHeaders()[0].GetActorJwt())
	require.Equal(t, []string{"*"}, rules[2].GetTlsPassthrough().GetHostnames())
	require.Equal(t, []int32{443}, rules[2].GetTlsPassthrough().GetPorts().GetNumbers())
	require.Equal(t, []string{"*"}, rules[3].GetHttp().GetHostnames())
	require.Equal(t, []int32{80}, rules[3].GetHttp().GetPorts().GetNumbers())
	require.Nil(t, rules[3].GetHttp().GetEffects())
	cluster.mu.Unlock()

	storedRow, ok, err := store.GetForThread(t.Context(), record, threadID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, originalUID, storedRow.ActorUID)
	require.Equal(t, ateActorStateRunning, storedRow.State)
	require.True(t, storedRow.Configured)
	require.NotNil(t, storedRow.WarmUntil)

	// 3. Stop settles the native thread session and suspends the actor synchronously.
	require.NoError(t, backend.Stop(t.Context(), record))
	require.Equal(t, []uuid.UUID{threadID}, protoDriver.closedThread)

	cluster.mu.Lock()
	require.Equal(t, ateapiv1.ActorState_ACTOR_STATE_SUSPENDED, actorEntry.actor.GetStatus().GetState())
	cluster.mu.Unlock()

	storedRow, ok, err = store.GetForThread(t.Context(), record, threadID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, ateActorStateSuspended, storedRow.State)
	require.Nil(t, storedRow.WarmUntil)

	// 4. Non-retired ReapStoppedMachine refuses to discard suspended workspace.
	err = backend.ReapStoppedMachine(t.Context(), record)
	require.ErrorContains(t, err, "must not be reaped by TTL")

	// 5. Resume after suspend: prepare explicitly calls ResumeActor (placing
	// the actor on a different worker), router returns retryable 503s while the
	// new worker registers, server re-POSTs /bootstrap to rewrite provisioned
	// files before launching Kit, and the second turn runs on the same actor UID.
	cluster.mu.Lock()
	firstWorker := actorEntry.actor.GetStatus().GetAssignedNode()
	cluster.mu.Unlock()
	turn2 := runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}
	require.NoError(t, backend.RunTurn(t.Context(), record, turn2))

	require.Equal(t, int32(1), cluster.resumeCalls.Load(), "suspended actor must be explicitly resumed before dial")
	require.Equal(t, int32(2), cluster.connect503Count.Load(), "router 503 during resume must be retried")
	require.Equal(t, 2, bootstrapCount, "resume must re-run bootstrap to overwrite sandbox-writable files")
	require.Equal(t, int32(2), cluster.bootstrapCalls.Load(), "resume must re-POST /bootstrap before launching Kit")
	require.Equal(t, int32(2), cluster.acpRequestCount.Load())
	require.Len(t, protoDriver.turns, 2)

	cluster.mu.Lock()
	require.Equal(t, ateapiv1.ActorState_ACTOR_STATE_RUNNING, actorEntry.actor.GetStatus().GetState())
	require.Equal(t, originalUID, actorEntry.actor.GetMetadata().GetUid())
	require.NotEmpty(t, actorEntry.actor.GetStatus().GetAssignedNode())
	require.NotEqual(t, firstWorker, actorEntry.actor.GetStatus().GetAssignedNode(), "actor must resume cleanly on a different worker")
	cluster.mu.Unlock()

	storedRow, ok, err = store.GetForThread(t.Context(), record, threadID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, ateActorStateRunning, storedRow.State)
	require.NotNil(t, storedRow.WarmUntil)

	// 6. Reap withdraws workload binding, deletes the actor by exact UID, and soft-deletes the row.
	record.Retired = true
	require.NoError(t, backend.ReapStoppedMachine(t.Context(), record))
	require.Len(t, revoked, 1)
	require.Equal(t, "actor/"+atespace+"/"+actorName, revoked[0].Subject)

	cluster.mu.Lock()
	require.Equal(t, []string{originalUID}, cluster.deletedActorUIDs)
	require.Empty(t, cluster.actors)
	cluster.mu.Unlock()

	_, ok, err = store.GetForThread(t.Context(), record, threadID)
	require.NoError(t, err)
	require.False(t, ok)

	exists, err := backend.RuntimeExists(t.Context(), record)
	require.NoError(t, err)
	require.False(t, exists, "retired runtime with no remaining actors must report non-existent")

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
		ThreadLifecycle: func(ctx context.Context, r assistantRuntimeRecord, _ uuid.UUID, op func(context.Context, assistantRuntimeRecord) error) error {
			return op(ctx, r)
		},
		Store: newFakeKitATEActorStore(),
		Token: func(context.Context, assistantRuntimeRecord, uuid.UUID) (string, error) { return "tok", nil },
		Bootstrap: func(context.Context, assistantRuntimeRecord, uuid.UUID, string, *KitWorkloadBinding) (KitLocalBootstrap, error) {
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
		store          = newFakeKitATEActorStore()
	)
	runtimeID := uuid.New()
	assistantID := uuid.New()
	projectID := uuid.New()
	thread1 := uuid.New()
	thread2 := uuid.New()
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
			Lifecycle: func(ctx context.Context, r assistantRuntimeRecord, op func(context.Context, assistantRuntimeRecord) error) error {
				mu.Lock()
				r.BackendMetadataJSON = append([]byte(nil), storedMetadata...)
				mu.Unlock()
				return op(ctx, r)
			},
			ThreadLifecycle: func(ctx context.Context, r assistantRuntimeRecord, _ uuid.UUID, op func(context.Context, assistantRuntimeRecord) error) error {
				mu.Lock()
				r.BackendMetadataJSON = append([]byte(nil), storedMetadata...)
				mu.Unlock()
				return op(ctx, r)
			},
			Store: store,
			Token: func(_ context.Context, _ assistantRuntimeRecord, thread uuid.UUID) (string, error) {
				return kitATEActorToken("root-secret", thread.String()), nil
			},
			Bootstrap: func(_ context.Context, _ assistantRuntimeRecord, thread uuid.UUID, actorName string, _ *KitWorkloadBinding) (KitLocalBootstrap, error) {
				return KitLocalBootstrap{
					Model:              "openai/gpt-4.1-mini",
					ReasoningEffort:    "medium",
					EgressInjectedAuth: true,
					InferenceEndpoint:  "https://api.speakeasy.test/chat/completions",
					AgentsMD:           []byte("# Speakeasy assistant\n"),
					MCPJSON:            []byte(`{"mcpServers":{"project-gateway":{"type":"streamable-http","url":"https://api.speakeasy.test/mcp/gateway/org/projects/proj","headers":{"Authorization":"Bearer ` + kitATEEgressAuthPlaceholder + `"}}}}`),
					GatewayToken:       []byte(kitATEActorToken("root-secret", thread.String())),
					Workload: &KitWorkloadBinding{
						MCPProjectGateway: true,
						IssuerID:          uuid.Nil,
						Subject:           "actor/" + atespace + "/" + actorName,
						Audience:          "",
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

	require.NoError(t, b1.RunTurn(t.Context(), record, runTurnRequest{ThreadID: thread1, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
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
	cluster.mu.Lock()
	require.Equal(t, uid1, cluster.actors[actor1Key].actor.GetMetadata().GetUid(), "existing actor must not be replaced on tag-only release")
	cluster.mu.Unlock()
	require.Equal(t, int32(2), cluster.bootstrapCalls.Load(), "resumed actor must re-bootstrap provisioned files")
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
	require.NoError(t, b3.RunTurn(t.Context(), record, runTurnRequest{ThreadID: thread2, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
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
		{"tampered trust bundle name", func(tmpl *ateapiv1.ActorTemplate) {
			tmpl.Volumes[0].SystemInfo.DataSources[0].TrustBundle.Names = []string{"attacker-ca.example"}
		}},
		{"tampered trust bundle mount path", func(tmpl *ateapiv1.ActorTemplate) {
			tmpl.Containers[0].VolumeMounts[0].MountPath = "/etc/ssl/other"
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

func TestKitATEReapCleansUpAcrossConfigDriftAndBadRow(t *testing.T) {
	t.Parallel()
	const (
		recordedAtespace = "old-assistants"
		liveAtespace     = "new-assistants"
		recordedTemplate = "kit-v1"
		liveTemplate     = "kit-v2"
	)
	cluster, client := startFakeATEStack(t, recordedAtespace)
	cluster.mu.Lock()
	cluster.atespaces[liveAtespace] = &ateapiv1.Atespace{Metadata: &ateapiv1.ResourceMetadata{Name: liveAtespace}}
	cluster.mu.Unlock()

	runtimeID := uuid.New()
	assistantID := uuid.New()
	projectID := uuid.New()
	validThread := uuid.New()
	validActorName := kitATEActorName(assistantRuntimeRecord{ID: runtimeID}, validThread)

	// Seed a real actor in the RECORDED atespace with the RECORDED template.
	created, err := client.CreateActor(t.Context(), &ateapiv1.CreateActorRequest{
		Actor: &ateapiv1.Actor{
			Metadata:      &ateapiv1.ResourceMetadata{Atespace: recordedAtespace, Name: validActorName},
			ActorTemplate: &ateapiv1.ObjectRef{Atespace: recordedAtespace, Name: kitATETemplateVersionName(recordedTemplate, testKitATEImageDigestA)},
		},
	})
	require.NoError(t, err)
	validUID := created.GetMetadata().GetUid()
	require.NotEmpty(t, validUID)

	validBinding := &KitWorkloadBinding{
		MCPProjectGateway: true,
		IssuerID:          uuid.New(),
		Subject:           "actor/" + recordedAtespace + "/" + validActorName,
		Audience:          "https://api.speakeasy.test/oauth/usi/test",
	}
	meta := kitATEMetadata{
		RuntimeID:         runtimeID,
		AssistantID:       assistantID,
		ProjectID:         projectID,
		ImageDigest:       testKitATEImageDigestA,
		Atespace:          recordedAtespace,
		Template:          recordedTemplate,
		LegacyEgressHosts: []string{"legacy.speakeasy.test"},
	}
	rawMeta, err := json.Marshal(meta)
	require.NoError(t, err)

	record := assistantRuntimeRecord{
		ID:                  runtimeID,
		AssistantID:         assistantID,
		ProjectID:           projectID,
		RuntimeVersion:      3,
		Backend:             runtimeBackendATE,
		Retired:             true,
		BackendMetadataJSON: rawMeta,
	}

	store := newFakeKitATEActorStore()
	_, err = store.Insert(t.Context(), record, kitATEThreadActor{
		RuntimeID: runtimeID,
		ThreadID:  uuid.Nil,
		ActorName: "corrupted-actor-name",
		ActorUID:  "",
		State:     ateActorStateRunning,
		Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser},
	})
	require.NoError(t, err)
	_, err = store.Insert(t.Context(), record, kitATEThreadActor{
		RuntimeID:  runtimeID,
		ThreadID:   validThread,
		ActorName:  validActorName,
		ActorUID:   validUID,
		State:      ateActorStateRunning,
		Configured: true,
		Workload:   validBinding,
		Initiator:  ateThreadInitiator{Resolution: ATEInitiatorNoUser},
	})
	require.NoError(t, err)

	serverURL, err := url.Parse("https://api.speakeasy.test")
	require.NoError(t, err)
	var revoked []*KitWorkloadBinding
	backend, err := NewKitATERuntimeBackend(KitATERuntimeConfig{
		Image:          testKitATEImage,
		Atespace:       liveAtespace,
		Template:       liveTemplate,
		WorkerSelector: testKitATEWorkerSelector,
		ServerURL:      serverURL,
		Client:         client,
		Lifecycle: func(ctx context.Context, r assistantRuntimeRecord, op func(context.Context, assistantRuntimeRecord) error) error {
			return op(ctx, r)
		},
		ThreadLifecycle: func(ctx context.Context, r assistantRuntimeRecord, _ uuid.UUID, op func(context.Context, assistantRuntimeRecord) error) error {
			return op(ctx, r)
		},
		Store: store,
		Token: func(context.Context, assistantRuntimeRecord, uuid.UUID) (string, error) { return "tok", nil },
		Bootstrap: func(context.Context, assistantRuntimeRecord, uuid.UUID, string, *KitWorkloadBinding) (KitLocalBootstrap, error) {
			return KitLocalBootstrap{}, nil
		},
		Protocol: func(context.Context, assistantRuntimeRecord, uuid.UUID, string, *url.URL, *guardian.HTTPClient, string) (runtimeProtocol, error) {
			return nil, nil
		},
		Revoke: func(_ context.Context, _ assistantRuntimeRecord, b *KitWorkloadBinding) error {
			revoked = append(revoked, b)
			return nil
		},
		CheckSettled: func(context.Context, assistantRuntimeRecord) error { return nil },
	})
	require.NoError(t, err)

	// Non-Reap callers (like Status) still enforce strict live config matching.
	_, err = backend.Status(t.Context(), record)
	require.ErrorIs(t, err, ErrKitRuntimeBinding)

	// Reap must succeed across config drift and bad rows, revoking and deleting the valid actor in the recorded atespace.
	require.NoError(t, backend.Reap(t.Context(), record))
	require.Len(t, revoked, 1)
	require.Equal(t, validBinding.Subject, revoked[0].Subject)
	cluster.mu.Lock()
	require.Equal(t, []string{validUID}, cluster.deletedActorUIDs)
	require.Empty(t, cluster.actors)
	cluster.mu.Unlock()

	// Ownership mismatch must still be rejected by Reap.
	foreign := record
	foreign.ProjectID = uuid.New()
	require.ErrorIs(t, backend.Reap(t.Context(), foreign), ErrKitRuntimeBinding)
}

func TestKitATEBootstrapConflictRetiresAndRecreatesWhileIdenticalRetryIsIdempotent(t *testing.T) {
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
		store          = newFakeKitATEActorStore()
	)
	runtimeID := uuid.New()
	assistantID := uuid.New()
	projectID := uuid.New()
	threadRetry := uuid.New()
	threadConflict := uuid.New()

	makeBootstrap := func(thread uuid.UUID, actorName string) KitLocalBootstrap {
		return KitLocalBootstrap{
			Model:              "openai/gpt-4.1-mini",
			ReasoningEffort:    "medium",
			EgressInjectedAuth: true,
			InferenceEndpoint:  "https://ai.speakeasy.com/chat/completions",
			AgentsMD:           []byte("# Speakeasy assistant\n"),
			MCPJSON:            []byte(`{"mcpServers":{"project-gateway":{"type":"streamable-http","url":"https://api.speakeasy.test/mcp/gateway/org/projects/proj","headers":{"Authorization":"Bearer ` + kitATEEgressAuthPlaceholder + `"}}}}`),
			GatewayToken:       []byte(kitATEActorToken("root-secret", thread.String())),
			Workload: &KitWorkloadBinding{
				MCPProjectGateway: true,
				IssuerID:          uuid.Nil,
				Subject:           "actor/" + atespace + "/" + actorName,
				Audience:          "",
			},
		}
	}

	backend, err := NewKitATERuntimeBackend(KitATERuntimeConfig{
		Image:          testKitATEImage,
		Atespace:       atespace,
		Template:       template,
		WorkerSelector: testKitATEWorkerSelector,
		ServerURL:      serverURL,
		Client:         client,
		Lifecycle: func(ctx context.Context, r assistantRuntimeRecord, op func(context.Context, assistantRuntimeRecord) error) error {
			mu.Lock()
			r.BackendMetadataJSON = append([]byte(nil), storedMetadata...)
			mu.Unlock()
			return op(ctx, r)
		},
		ThreadLifecycle: func(ctx context.Context, r assistantRuntimeRecord, _ uuid.UUID, op func(context.Context, assistantRuntimeRecord) error) error {
			mu.Lock()
			r.BackendMetadataJSON = append([]byte(nil), storedMetadata...)
			mu.Unlock()
			return op(ctx, r)
		},
		Store: store,
		Token: func(_ context.Context, _ assistantRuntimeRecord, thread uuid.UUID) (string, error) {
			return kitATEActorToken("root-secret", thread.String()), nil
		},
		Bootstrap: func(_ context.Context, _ assistantRuntimeRecord, thread uuid.UUID, actorName string, _ *KitWorkloadBinding) (KitLocalBootstrap, error) {
			return makeBootstrap(thread, actorName), nil
		},
		Protocol: func(_ context.Context, _ assistantRuntimeRecord, _ uuid.UUID, _ string, endpoint *url.URL, hc *guardian.HTTPClient, token string) (runtimeProtocol, error) {
			return &fakeATEProtocol{client: hc, endpoint: endpoint, token: token}, nil
		},
		Revoke: func(_ context.Context, _ assistantRuntimeRecord, b *KitWorkloadBinding) error {
			revoked = append(revoked, b)
			return nil
		},
		CheckSettled:       func(context.Context, assistantRuntimeRecord) error { return nil },
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
	res, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	storedMetadata = res.BackendMetadataJSON
	record.BackendMetadataJSON = res.BackendMetadataJSON

	// 1. Crash-retry idempotency: actor already accepted the IDENTICAL bootstrap body
	//    before server persisted Configured=true -> re-POST returns 204 and keeps the same UID.
	retryActorName := kitATEActorName(record, threadRetry)
	retryCreated, err := client.CreateActor(t.Context(), &ateapiv1.CreateActorRequest{
		Actor: &ateapiv1.Actor{
			Metadata:      &ateapiv1.ResourceMetadata{Atespace: atespace, Name: retryActorName},
			ActorTemplate: &ateapiv1.ObjectRef{Atespace: atespace, Name: kitATETemplateVersionName(template, testKitATEImageDigestA)},
		},
	})
	require.NoError(t, err)
	retryUID := retryCreated.GetMetadata().GetUid()
	expectedPayload, err := kitATEBootstrapPayload(makeBootstrap(threadRetry, retryActorName))
	require.NoError(t, err)
	cluster.mu.Lock()
	cluster.actors[atespace+"/"+retryActorName].bootstrapped = true
	cluster.actors[atespace+"/"+retryActorName].hasBootstrapHash = true
	cluster.actors[atespace+"/"+retryActorName].bootstrapHash = sha256.Sum256(expectedPayload)
	cluster.actors[atespace+"/"+retryActorName].kitReady = true
	cluster.mu.Unlock()

	require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadRetry, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
	cluster.mu.Lock()
	require.Equal(t, retryUID, cluster.actors[atespace+"/"+retryActorName].actor.GetMetadata().GetUid(), "identical crash-retry must keep existing actor UID")
	require.Empty(t, cluster.deletedActorUIDs)
	cluster.mu.Unlock()
	require.Empty(t, revoked)

	// 2. 409 Conflict: actor already accepted a DIFFERENT bootstrap body -> server treats 409
	//    as failure, revokes & deletes the conflicting actor, recreates a fresh replacement actor, and succeeds.
	conflictActorName := kitATEActorName(record, threadConflict)
	conflictCreated, err := client.CreateActor(t.Context(), &ateapiv1.CreateActorRequest{
		Actor: &ateapiv1.Actor{
			Metadata:      &ateapiv1.ResourceMetadata{Atespace: atespace, Name: conflictActorName},
			ActorTemplate: &ateapiv1.ObjectRef{Atespace: atespace, Name: kitATETemplateVersionName(template, testKitATEImageDigestA)},
		},
	})
	require.NoError(t, err)
	conflictUID := conflictCreated.GetMetadata().GetUid()
	cluster.mu.Lock()
	cluster.actors[atespace+"/"+conflictActorName].bootstrapped = true
	cluster.actors[atespace+"/"+conflictActorName].hasBootstrapHash = true
	cluster.actors[atespace+"/"+conflictActorName].bootstrapHash = sha256.Sum256([]byte("different-bootstrap-body"))
	cluster.actors[atespace+"/"+conflictActorName].kitReady = true
	cluster.mu.Unlock()

	require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadConflict, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
	rowAfter, ok, err := store.GetForThread(t.Context(), record, threadConflict)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, rowAfter.Configured)
	require.NotEqual(t, conflictUID, rowAfter.ActorUID, "409 conflict must retire and recreate the actor with a new UID")
	require.NotEqual(t, conflictActorName, rowAfter.ActorName, "409 conflict must recreate with a fresh replacement name")
	cluster.mu.Lock()
	require.Equal(t, []string{conflictUID}, cluster.deletedActorUIDs)
	require.NotNil(t, cluster.actors[atespace+"/"+rowAfter.ActorName])
	cluster.mu.Unlock()
	require.Len(t, revoked, 1, "conflicting actor workload must be revoked before recreation")
}

func newTestKitATEHarness(t *testing.T, onRunTurn func(runTurnRequest)) (*fakeATECluster, *KitATERuntimeBackend, *fakeKitATEActorStore, assistantRuntimeRecord, *[]*KitWorkloadBinding, *[]uuid.UUID) {
	t.Helper()
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
		clearedThreads []uuid.UUID
		store          = newFakeKitATEActorStore()
	)

	runtimeID := uuid.New()
	assistantID := uuid.New()
	projectID := uuid.New()
	agentID := uuid.New()

	backend, err := NewKitATERuntimeBackend(KitATERuntimeConfig{
		Image:          testKitATEImage,
		Atespace:       atespace,
		Template:       template,
		WorkerSelector: testKitATEWorkerSelector,
		ServerURL:      serverURL,
		Client:         client,
		Lifecycle: func(ctx context.Context, r assistantRuntimeRecord, op func(context.Context, assistantRuntimeRecord) error) error {
			mu.Lock()
			r.BackendMetadataJSON = append([]byte(nil), storedMetadata...)
			mu.Unlock()
			return op(ctx, r)
		},
		ThreadLifecycle: func(ctx context.Context, r assistantRuntimeRecord, _ uuid.UUID, op func(context.Context, assistantRuntimeRecord) error) error {
			mu.Lock()
			r.BackendMetadataJSON = append([]byte(nil), storedMetadata...)
			mu.Unlock()
			return op(ctx, r)
		},
		Store: store,
		Token: func(_ context.Context, _ assistantRuntimeRecord, thread uuid.UUID) (string, error) {
			return kitATEActorToken("root-secret", thread.String()), nil
		},
		Bootstrap: func(_ context.Context, _ assistantRuntimeRecord, thread uuid.UUID, actorName string, _ *KitWorkloadBinding) (KitLocalBootstrap, error) {
			return KitLocalBootstrap{
				Model:              "openai/gpt-4.1-mini",
				ReasoningEffort:    "medium",
				EgressInjectedAuth: true,
				InferenceEndpoint:  "https://api.speakeasy.test/chat/completions",
				AgentsMD:           []byte("# Speakeasy assistant\n"),
				MCPJSON:            []byte(`{"mcpServers":{"project-gateway":{"type":"streamable-http","url":"https://api.speakeasy.test/mcp/gateway/org/projects/proj","headers":{"Authorization":"Bearer ` + kitATEEgressAuthPlaceholder + `"}}}}`),
				GatewayToken:       []byte(kitATEActorToken("root-secret", thread.String())),
				Workload: &KitWorkloadBinding{
					MCPProjectGateway: true,
					IssuerID:          uuid.Nil,
					Subject:           "actor/" + atespace + "/" + actorName,
					Audience:          "",
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
			return &fakeATEHookedProtocol{
				fakeATEProtocol: &fakeATEProtocol{client: hc, endpoint: endpoint, token: token},
				onRunTurn:       onRunTurn,
			}, nil
		},
		Revoke: func(_ context.Context, _ assistantRuntimeRecord, binding *KitWorkloadBinding) error {
			mu.Lock()
			revoked = append(revoked, binding)
			mu.Unlock()
			return nil
		},
		CheckSettled: func(context.Context, assistantRuntimeRecord) error { return nil },
		CheckThreadSettled: func(context.Context, assistantRuntimeRecord, uuid.UUID, uuid.UUID) error {
			return nil
		},
		ClearThreadSession: func(_ context.Context, _ assistantRuntimeRecord, thread uuid.UUID) error {
			mu.Lock()
			clearedThreads = append(clearedThreads, thread)
			mu.Unlock()
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
	res, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	storedMetadata = res.BackendMetadataJSON
	record.BackendMetadataJSON = res.BackendMetadataJSON
	return cluster, backend, store, record, &revoked, &clearedThreads
}

type fakeATEHookedProtocol struct {
	*fakeATEProtocol
	onRunTurn func(runTurnRequest)
}

func (p *fakeATEHookedProtocol) RunTurn(ctx context.Context, r assistantRuntimeRecord, req runTurnRequest) error {
	if p.onRunTurn != nil {
		p.onRunTurn(req)
	}
	return p.fakeATEProtocol.RunTurn(ctx, r, req)
}

func TestKitATECrashedActorRecoveredWithoutManualAction(t *testing.T) {
	t.Parallel()

	t.Run("reverts and resumes crashed actor with durable snapshot", func(t *testing.T) {
		t.Parallel()
		cluster, backend, store, record, revoked, cleared := newTestKitATEHarness(t, nil)
		threadID := uuid.New()

		require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
		require.NoError(t, backend.Stop(t.Context(), record))

		rowBefore, ok, err := store.GetForThread(t.Context(), record, threadID)
		require.NoError(t, err)
		require.True(t, ok)

		// Resume, then crash after a completed durable snapshot exists.
		require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
		actorKey := "prod-assistants/" + rowBefore.ActorName
		cluster.mu.Lock()
		cluster.actors[actorKey].actor.Status.State = ateapiv1.ActorState_ACTOR_STATE_CRASHED
		cluster.actors[actorKey].actor.Status.LastAssignedGeneration++
		cluster.mu.Unlock()

		// Next turn automatically reverts to the last durable snapshot and resumes.
		require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
		require.Equal(t, int32(1), cluster.revertCalls.Load())
		require.Equal(t, int32(2), cluster.resumeCalls.Load())
		require.Empty(t, *revoked)
		require.Empty(t, *cleared)

		rowAfter, ok, err := store.GetForThread(t.Context(), record, threadID)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, rowBefore.ActorUID, rowAfter.ActorUID)
		require.Equal(t, ateActorStateRunning, rowAfter.State)
	})

	t.Run("retires and replaces crashed actor with no durable snapshot", func(t *testing.T) {
		t.Parallel()
		cluster, backend, store, record, revoked, cleared := newTestKitATEHarness(t, nil)
		threadID := uuid.New()

		require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))

		rowBefore, ok, err := store.GetForThread(t.Context(), record, threadID)
		require.NoError(t, err)
		require.True(t, ok)

		// Crash before any durable snapshot has ever been taken.
		actorKey := "prod-assistants/" + rowBefore.ActorName
		cluster.mu.Lock()
		cluster.actors[actorKey].actor.Status.State = ateapiv1.ActorState_ACTOR_STATE_CRASHED
		cluster.actors[actorKey].actor.Status.Snapshots = nil
		cluster.mu.Unlock()

		require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))

		require.Equal(t, int32(0), cluster.revertCalls.Load())
		require.Len(t, *revoked, 1)
		require.Equal(t, []uuid.UUID{threadID}, *cleared)
		require.Equal(t, int32(2), cluster.bootstrapCalls.Load(), "replacement actor must be freshly bootstrapped")

		rowAfter, ok, err := store.GetForThread(t.Context(), record, threadID)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotEqual(t, rowBefore.ActorUID, rowAfter.ActorUID)
		require.NotEqual(t, rowBefore.ActorName, rowAfter.ActorName, "replacement actor must use a fresh non-reused subject name")
		require.Equal(t, ateActorStateRunning, rowAfter.State)
		require.True(t, rowAfter.Configured)
	})

	t.Run("falls back to replacement when RevertActor fails", func(t *testing.T) {
		t.Parallel()
		cluster, backend, store, record, revoked, cleared := newTestKitATEHarness(t, nil)
		threadID := uuid.New()

		require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
		require.NoError(t, backend.Stop(t.Context(), record))
		require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))

		rowBefore, ok, err := store.GetForThread(t.Context(), record, threadID)
		require.NoError(t, err)
		require.True(t, ok)

		actorKey := "prod-assistants/" + rowBefore.ActorName
		cluster.mu.Lock()
		cluster.actors[actorKey].actor.Status.State = ateapiv1.ActorState_ACTOR_STATE_CRASHED
		cluster.actors[actorKey].failRevert = true
		cluster.mu.Unlock()

		require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
		require.Equal(t, int32(1), cluster.revertCalls.Load())
		require.Len(t, *revoked, 1)
		require.Equal(t, []uuid.UUID{threadID}, *cleared)

		rowAfter, ok, err := store.GetForThread(t.Context(), record, threadID)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotEqual(t, rowBefore.ActorUID, rowAfter.ActorUID)
	})
}

func TestKitATEEvictionDrainLeadsToDurableSuspendAndResume(t *testing.T) {
	t.Parallel()

	t.Run("warm idle thread drains on SIGTERM and resumes on a new worker", func(t *testing.T) {
		t.Parallel()
		cluster, backend, store, record, _, _ := newTestKitATEHarness(t, nil)
		threadID := uuid.New()

		require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
		row, ok, err := store.GetForThread(t.Context(), record, threadID)
		require.NoError(t, err)
		require.True(t, ok)

		actorKey := "prod-assistants/" + row.ActorName
		cluster.mu.Lock()
		originalWorker := cluster.actors[actorKey].actor.GetStatus().GetAssignedNode()
		cluster.actors[actorKey].draining = true
		cluster.mu.Unlock()

		// Even though the thread's warm window has NOT elapsed, the drain signal
		// triggers an immediate synchronous durable suspend.
		didSuspend, err := backend.SuspendThreadActor(t.Context(), record, threadID, time.Now())
		require.NoError(t, err)
		require.True(t, didSuspend)
		require.Equal(t, int32(1), cluster.suspendCalls.Load())

		row, ok, err = store.GetForThread(t.Context(), record, threadID)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, ateActorStateSuspended, row.State)

		// Next turn explicitly resumes the actor onto a healthy worker and re-bootstraps provisioned files.
		require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
		require.Equal(t, int32(1), cluster.resumeCalls.Load())
		require.Equal(t, int32(2), cluster.bootstrapCalls.Load(), "resumed actor must re-POST /bootstrap")

		cluster.mu.Lock()
		newWorker := cluster.actors[actorKey].actor.GetStatus().GetAssignedNode()
		cluster.mu.Unlock()
		require.NotEqual(t, originalWorker, newWorker)
	})

	t.Run("turn in flight finishes then synchronously suspends on drain signal", func(t *testing.T) {
		t.Parallel()
		turnStarted := make(chan struct{})
		releaseTurn := make(chan struct{})
		var gateOnce sync.Once
		cluster, backend, store, record, _, _ := newTestKitATEHarness(t, func(runTurnRequest) {
			gateOnce.Do(func() {
				close(turnStarted)
				<-releaseTurn
			})
		})
		threadID := uuid.New()
		targetKey := "prod-assistants/" + kitATEActorName(record, threadID)

		turnErrCh := make(chan error, 1)
		go func() {
			turnErrCh <- backend.RunTurn(t.Context(), record, runTurnRequest{
				ThreadID:        threadID,
				EventID:         uuid.New(),
				Model:           "openai/gpt-4.1-mini",
				ReasoningEffort: "medium",
				Initiator:       ateThreadInitiator{Resolution: ATEInitiatorNoUser},
			})
		}()

		// Wait until the turn is genuinely blocked mid-flight before firing drain SIGTERM.
		<-turnStarted
		cluster.mu.Lock()
		require.NotNil(t, cluster.actors[targetKey])
		cluster.actors[targetKey].draining = true
		cluster.mu.Unlock()

		// Drain must NOT interrupt the in-flight turn before it finishes.
		require.Equal(t, int32(0), cluster.suspendCalls.Load())
		close(releaseTurn)

		require.NoError(t, <-turnErrCh)

		// Turn succeeded and the post-turn drain check durably suspended the actor.
		require.Equal(t, int32(1), cluster.suspendCalls.Load())
		row, ok, err := store.GetForThread(t.Context(), record, threadID)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, ateActorStateSuspended, row.State)

		// Subsequent turn resumes cleanly on another worker.
		require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
		require.Equal(t, int32(1), cluster.resumeCalls.Load())
		require.Equal(t, int32(2), cluster.acpRequestCount.Load())
	})
}

func TestKitATEInitVersionMismatchFailsClearly(t *testing.T) {
	t.Parallel()
	cluster, backend, _, record, _, _ := newTestKitATEHarness(t, nil)
	cluster.mu.Lock()
	cluster.initVersionOverride = "99"
	cluster.mu.Unlock()

	err := backend.RunTurn(t.Context(), record, runTurnRequest{
		ThreadID:        uuid.New(),
		Model:           "openai/gpt-4.1-mini",
		ReasoningEffort: "medium",
		Initiator:       ateThreadInitiator{Resolution: ATEInitiatorNoUser},
	})
	require.ErrorContains(t, err, fmt.Sprintf(`ATE init helper version mismatch: got "99", want "%d"`, ateInitWireVersion))
}

func TestBuildKitATEEgressPolicyRules(t *testing.T) {
	t.Parallel()

	const (
		atespace  = "prod-assistants"
		actorName = "spk-kit-actor-1"
		actorUID  = "uid-12345"
		wantURI   = "ate-secret://speakeasy.com/v1/actors/prod-assistants/spk-kit-actor-1/uid-12345"
	)

	t.Run("same host deduplicates to one HTTPS rule with credentialUri and keeps wildcards effect-free", func(t *testing.T) {
		t.Parallel()
		rules, err := BuildKitATEEgressPolicyRules(
			atespace,
			actorName,
			actorUID,
			"https://api.speakeasy.com/mcp/gateway/org/projects/proj",
			"https://api.speakeasy.com/chat/completions",
		)
		require.NoError(t, err)
		require.NoError(t, validateKitATEEgressRulesNoWildcardEffects(rules))
		require.Len(t, rules, 3)

		httpsRule := rules[0].GetHttps()
		require.NotNil(t, httpsRule)
		require.Equal(t, []string{"api.speakeasy.com"}, httpsRule.GetHostnames())
		require.Equal(t, []int32{443}, httpsRule.GetPorts().GetNumbers())
		require.Len(t, httpsRule.GetEffects().GetReplaceHeaders(), 1)
		hdr := httpsRule.GetEffects().GetReplaceHeaders()[0]
		require.Equal(t, "Authorization", hdr.GetHeader())
		require.Equal(t, "Bearer ", hdr.GetPrefix())
		require.Equal(t, wantURI, hdr.GetCredentialUri())
		require.Nil(t, hdr.GetActorJwt())

		require.Equal(t, []string{"*"}, rules[1].GetTlsPassthrough().GetHostnames())
		require.Equal(t, []int32{443}, rules[1].GetTlsPassthrough().GetPorts().GetNumbers())
		require.Equal(t, []string{"*"}, rules[2].GetHttp().GetHostnames())
		require.Equal(t, []int32{80}, rules[2].GetHttp().GetPorts().GetNumbers())
		require.Nil(t, rules[2].GetHttp().GetEffects())
	})

	t.Run("distinct hosts emit sorted HTTPS rules with actor credentialUri", func(t *testing.T) {
		t.Parallel()
		rules, err := BuildKitATEEgressPolicyRules(
			atespace,
			actorName,
			actorUID,
			"https://mcp.speakeasy.com/mcp/gateway/org/projects/proj",
			"https://ai.speakeasy.com/chat/completions",
		)
		require.NoError(t, err)
		require.NoError(t, validateKitATEEgressRulesNoWildcardEffects(rules))
		require.Len(t, rules, 4)

		require.Equal(t, []string{"ai.speakeasy.com"}, rules[0].GetHttps().GetHostnames())
		require.Equal(t, wantURI, rules[0].GetHttps().GetEffects().GetReplaceHeaders()[0].GetCredentialUri())
		require.Nil(t, rules[0].GetHttps().GetEffects().GetReplaceHeaders()[0].GetActorJwt())

		require.Equal(t, []string{"mcp.speakeasy.com"}, rules[1].GetHttps().GetHostnames())
		require.Equal(t, wantURI, rules[1].GetHttps().GetEffects().GetReplaceHeaders()[0].GetCredentialUri())
		require.Nil(t, rules[1].GetHttps().GetEffects().GetReplaceHeaders()[0].GetActorJwt())
	})

	t.Run("rejects wildcard, IP, or non-HTTPS Speakeasy targets and forbids wildcard header effects", func(t *testing.T) {
		t.Parallel()
		for _, bad := range []string{
			"http://api.speakeasy.com/mcp/gateway/org/projects/proj",
			"https://*.speakeasy.com/mcp/gateway/org/projects/proj",
			"https://169.254.169.254/latest/meta-data",
			"https://127.0.0.1/mcp/gateway/org/projects/proj",
		} {
			_, err := BuildKitATEEgressPolicyRules(atespace, actorName, actorUID, bad, "https://api.speakeasy.com/chat/completions")
			require.Error(t, err, bad)
		}

		badWildcardRule := []*ateapiv1.EgressRule{{
			Https: &ateapiv1.HttpsRule{
				Hostnames: []string{"*"},
				Ports:     &ateapiv1.Ports{Numbers: []int32{443}},
				Effects: &ateapiv1.HttpRuleEffects{
					ReplaceHeaders: []*ateapiv1.CredentialHeader{{
						Header:        "Authorization",
						Prefix:        "Bearer ",
						CredentialUri: wantURI,
					}},
				},
			},
		}}
		require.Error(t, validateKitATEEgressRulesNoWildcardEffects(badWildcardRule))
	})
}

func TestKitATEResumeAndRevertReBootstrapOverwritingTamperedFiles(t *testing.T) {
	t.Parallel()
	cluster, backend, store, record, revoked, _ := newTestKitATEHarness(t, nil)
	threadID := uuid.New()

	// 1. Initial cold turn populates provisioned files.
	require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
	row, ok, err := store.GetForThread(t.Context(), record, threadID)
	require.NoError(t, err)
	require.True(t, ok)
	actorKey := "prod-assistants/" + row.ActorName

	provisionedPaths := []string{
		".kit/AGENTS.md",
		".kit/config.toml",
		".kit/credentials/mcp.json",
		".kit/credentials/gateway-token",
	}
	tamperFiles := func() {
		cluster.mu.Lock()
		defer cluster.mu.Unlock()
		for _, p := range provisionedPaths {
			cluster.actors[actorKey].bootstrapFiles[p] = []byte("tampered-by-kit")
		}
	}
	assertRestored := func() {
		cluster.mu.Lock()
		defer cluster.mu.Unlock()
		for _, p := range provisionedPaths {
			require.NotEqual(t, "tampered-by-kit", string(cluster.actors[actorKey].bootstrapFiles[p]), p)
			require.NotEmpty(t, cluster.actors[actorKey].bootstrapFiles[p], p)
		}
	}

	// 2. Suspend after tampering, then resume -> server must re-POST /bootstrap and overwrite all provisioned files.
	tamperFiles()
	require.NoError(t, backend.Stop(t.Context(), record))
	require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
	require.Equal(t, int32(1), cluster.resumeCalls.Load())
	require.Equal(t, int32(2), cluster.bootstrapCalls.Load())
	assertRestored()

	// 3. Crash after tampering (with a completed durable snapshot) -> revert + resume must also re-POST /bootstrap.
	tamperFiles()
	cluster.mu.Lock()
	cluster.actors[actorKey].actor.Status.State = ateapiv1.ActorState_ACTOR_STATE_CRASHED
	cluster.actors[actorKey].actor.Status.LastAssignedGeneration++
	cluster.mu.Unlock()

	require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium", Initiator: ateThreadInitiator{Resolution: ATEInitiatorNoUser}}))
	require.Equal(t, int32(1), cluster.revertCalls.Load())
	require.Equal(t, int32(2), cluster.resumeCalls.Load())
	require.Equal(t, int32(3), cluster.bootstrapCalls.Load())
	require.Empty(t, *revoked)
	assertRestored()
}
