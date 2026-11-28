package assistants

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type fakeKitLocalInitHelper struct {
	mu              sync.Mutex
	server          *httptest.Server
	port            int
	started         bool
	bootstrapCalls  int
	lastPayload     kitATEBootstrapRequest
	readyStatus     int
	bootstrapStatus int
	appReadyStatus  int
	versionHeader   string
}

func newFakeKitLocalInitHelper(t *testing.T) *fakeKitLocalInitHelper {
	t.Helper()
	h := &fakeKitLocalInitHelper{
		readyStatus:     http.StatusOK,
		bootstrapStatus: http.StatusNoContent,
		appReadyStatus:  http.StatusOK,
		versionHeader:   strconv.Itoa(ateInitWireVersion),
	}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.versionHeader != "" {
			w.Header().Set(ateInitVersionHeader, h.versionHeader)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			w.WriteHeader(h.readyStatus)
		case r.Method == http.MethodGet && r.URL.Path == "/status":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]bool{"started": h.started})
		case r.Method == http.MethodPost && r.URL.Path == "/bootstrap":
			if h.bootstrapStatus != http.StatusNoContent {
				w.WriteHeader(h.bootstrapStatus)
				return
			}
			if h.started {
				w.WriteHeader(http.StatusConflict)
				return
			}
			var req kitATEBootstrapRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			h.lastPayload = req
			h.bootstrapCalls++
			h.started = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/app-readyz":
			w.WriteHeader(h.appReadyStatus)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(h.server.Close)
	parsed, err := url.Parse(h.server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	h.port = port
	return h
}

type fakeKitLocalEngine struct {
	mu         sync.Mutex
	t          *testing.T
	imageID    string
	containers map[string]*kitLocalContainerInfo
	byID       map[string]string
	specs      map[string]kitLocalContainerSpec
	volumes    map[string]map[string]string
	helpers    map[string]*fakeKitLocalInitHelper
	calls      []string
	stoppedIDs []string
	nextPort   int
	nextSeq    int
	newHelper  func(*testing.T) *fakeKitLocalInitHelper
}

func newFakeKitLocalEngine(t *testing.T) *fakeKitLocalEngine {
	t.Helper()
	return &fakeKitLocalEngine{
		t:          t,
		imageID:    "sha256:test-image",
		containers: map[string]*kitLocalContainerInfo{},
		byID:       map[string]string{},
		specs:      map[string]kitLocalContainerSpec{},
		volumes:    map[string]map[string]string{},
		helpers:    map[string]*fakeKitLocalInitHelper{},
		nextPort:   32100,
		newHelper:  newFakeKitLocalInitHelper,
	}
}

func (f *fakeKitLocalEngine) ImageID(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.imageID, nil
}

func (f *fakeKitLocalEngine) InspectKit(_ context.Context, name string) (kitLocalContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.containers[name]
	if !ok {
		return kitLocalContainerInfo{}, errLocalContainerNotFound
	}
	copyInfo := *info
	copyInfo.Labels = maps.Clone(info.Labels)
	return copyInfo, nil
}

func (f *fakeKitLocalEngine) InspectKitVolume(_ context.Context, name string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	labels, ok := f.volumes[name]
	if !ok {
		return nil, errLocalContainerNotFound
	}
	return maps.Clone(labels), nil
}

func (f *fakeKitLocalEngine) CreateKitVolume(_ context.Context, name string, labels map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "volume")
	f.volumes[name] = maps.Clone(labels)
	return nil
}

func (f *fakeKitLocalEngine) CreateKit(_ context.Context, spec kitLocalContainerSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "create")
	f.nextSeq++
	id := fmt.Sprintf("container-%d", f.nextSeq)
	f.specs[spec.Name] = spec
	f.containers[spec.Name] = &kitLocalContainerInfo{
		ID:                 id,
		Name:               spec.Name,
		ImageID:            f.imageID,
		User:               "1000:1000",
		HomeVolume:         spec.VolumeName,
		AssertionDirectory: filepath.Dir(spec.WorkloadAssertionFile),
		AssertionReadOnly:  true,
		Labels:             maps.Clone(spec.Labels),
	}
	f.byID[id] = spec.Name
	return id, nil
}

func (f *fakeKitLocalEngine) ListKitContainers(_ context.Context, runtimeID string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var names []string
	for name, info := range f.containers {
		if info.Labels["gram.ai/runtime-lineage"] == runtimeID {
			names = append(names, name)
		}
	}
	return names, nil
}

func (f *fakeKitLocalEngine) ListKitVolumes(_ context.Context, runtimeID string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var names []string
	for name, labels := range f.volumes {
		if labels["gram.ai/runtime-lineage"] == runtimeID {
			names = append(names, name)
		}
	}
	return names, nil
}

func (f *fakeKitLocalEngine) Start(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "start")
	name, ok := f.byID[id]
	if !ok {
		return errLocalContainerNotFound
	}
	helper, ok := f.helpers[name]
	if !ok {
		helper = f.newHelper(f.t)
		f.helpers[name] = helper
	}
	info := f.containers[name]
	info.Running = true
	info.InitHostPort = helper.port
	info.HostPort = f.nextPort
	f.nextPort++
	return nil
}

func (f *fakeKitLocalEngine) Stop(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stoppedIDs = append(f.stoppedIDs, id)
	f.calls = append(f.calls, "stop")
	name, ok := f.byID[id]
	if !ok {
		return nil
	}
	info := f.containers[name]
	info.Running = false
	info.InitHostPort = 0
	info.HostPort = 0
	return nil
}

func (f *fakeKitLocalEngine) Remove(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "remove")
	name, ok := f.byID[id]
	if !ok {
		return nil
	}
	delete(f.byID, id)
	delete(f.containers, name)
	return nil
}

func (f *fakeKitLocalEngine) RemoveVolume(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "remove-volume")
	delete(f.volumes, name)
	return nil
}

type fakeKitLocalProtocol struct {
	mu               sync.Mutex
	turns            int
	interrupts       int
	statuses         int
	closedThreads    []uuid.UUID
	closeErr         error
	observedEndpoint map[uuid.UUID]string
	observedToken    map[uuid.UUID]string
}

func (p *fakeKitLocalProtocol) RunTurn(_ context.Context, _ assistantRuntimeRecord, turn runTurnRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.turns++
	_ = turn
	return nil
}

func (p *fakeKitLocalProtocol) InterruptTurn(context.Context, assistantRuntimeRecord, uuid.UUID) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.interrupts++
	return true, nil
}

func (p *fakeKitLocalProtocol) Status(context.Context, assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.statuses++
	return RuntimeBackendStatus{Configured: true}, nil
}

func (p *fakeKitLocalProtocol) CloseNativeThreadSession(_ context.Context, _ assistantRuntimeRecord, threadID uuid.UUID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closeErr != nil {
		return p.closeErr
	}
	p.closedThreads = append(p.closedThreads, threadID)
	return nil
}

func kitLocalFixture(t *testing.T) (*KitLocalRuntimeBackend, *fakeKitLocalEngine, assistantRuntimeRecord, *fakeKitLocalProtocol) {
	t.Helper()
	engine := newFakeKitLocalEngine(t)
	protocol := &fakeKitLocalProtocol{
		observedEndpoint: map[uuid.UUID]string{},
		observedToken:    map[uuid.UUID]string{},
	}
	helperClient := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)).Client(
		guardian.WithAllowedCIDRBlocks("127.0.0.1/32"),
	)
	workload := kitTestWorkloadBootstrap().Workload
	config := KitLocalRuntimeConfig{
		Environment:             "local",
		Image:                   "example.invalid/kit:local",
		ServerURL:               &url.URL{Scheme: "https", Host: "ai.speakeasy.com"},
		WorkloadAssertionFile:   "/operator/workload/assertion",
		CompatibilityGeneration: "local-generation-one",
		HomeOwnedByRuntimeUser:  true,
		HelperClient:            helperClient,
		HelperPollInterval:      5 * time.Millisecond,
		ProvisionWorkload: func(context.Context, assistantRuntimeRecord) (*KitWorkloadBinding, error) {
			copyWorkload := *workload
			return &copyWorkload, nil
		},
		Bootstrap: func(_ context.Context, _ assistantRuntimeRecord, threadID uuid.UUID, w *KitWorkloadBinding) (KitLocalBootstrap, error) {
			b := kitTestWorkloadBootstrap()
			b.Workload = w
			b.GatewayToken = []byte("thread-token-" + threadID.String())
			gatewayURL, _ := url.Parse("https://api.example/mcp/gateway/org-test/projects/proj-test")
			var err error
			b.MCPJSON, err = kitCompositionMCP(gatewayURL, w)
			return b, err
		},
		Token: func(_ context.Context, _ assistantRuntimeRecord, threadID uuid.UUID) (string, error) {
			return "thread-token-" + threadID.String(), nil
		},
		Fence: func(context.Context, assistantRuntimeRecord) error { return nil },
		Protocol: func(_ context.Context, _ assistantRuntimeRecord, threadID uuid.UUID, endpoint *url.URL, token string) (runtimeProtocol, error) {
			protocol.mu.Lock()
			protocol.observedEndpoint[threadID] = endpoint.String()
			protocol.observedToken[threadID] = token
			protocol.mu.Unlock()
			return protocol, nil
		},
	}
	backend, err := newKitLocalRuntimeBackend(config, engine)
	require.NoError(t, err)
	r := assistantRuntimeRecord{ID: uuid.New(), AssistantID: uuid.New(), ProjectID: uuid.New(), RuntimeVersion: 3, Backend: runtimeBackendLocal}
	return backend, engine, r, protocol
}

func TestKitLocalPerThreadLifecycleAndIdleSweeping(t *testing.T) {
	t.Parallel()
	k, engine, r, protocol := kitLocalFixture(t)
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	k.config.Now = func() time.Time { return now }

	// Ensure provisions runtime workload metadata once without starting containers.
	ensured, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	require.True(t, ensured.ColdStart)
	require.Empty(t, engine.calls)
	r.BackendMetadataJSON = ensured.BackendMetadataJSON

	// Two distinct threads get two isolated containers, volumes, /acp endpoints, and per-thread tokens.
	threadA, threadB := uuid.New(), uuid.New()
	require.NoError(t, k.RunTurn(t.Context(), r, runTurnRequest{ThreadID: threadA}))
	nameA, volA := kitLocalThreadNames(r, threadA)
	require.True(t, strings.HasPrefix(nameA, "gram-kit-"))
	require.NotContains(t, volA, "gram-asst")
	require.Equal(t, "http://127.0.0.1:32100/acp", protocol.observedEndpoint[threadA])
	require.Equal(t, "thread-token-"+threadA.String(), protocol.observedToken[threadA])
	require.Equal(t, 1, engine.helpers[nameA].bootstrapCalls)

	now = now.Add(4 * time.Minute)
	require.NoError(t, k.RunTurn(t.Context(), r, runTurnRequest{ThreadID: threadB}))
	nameB, _ := kitLocalThreadNames(r, threadB)
	require.NotEqual(t, nameA, nameB)
	require.Equal(t, "http://127.0.0.1:32101/acp", protocol.observedEndpoint[threadB])
	require.Equal(t, "thread-token-"+threadB.String(), protocol.observedToken[threadB])
	require.Equal(t, 1, engine.helpers[nameB].bootstrapCalls)

	// Idle sweep after 6 minutes stops threadA (idle 6m >= 5m) while keeping threadB (idle 2m) running.
	now = now.Add(2 * time.Minute)
	stopped, err := k.StopIdleThreadContainers(t.Context(), r, now, 5*time.Minute, nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, stopped)
	require.False(t, engine.containers[nameA].Running)
	require.True(t, engine.containers[nameB].Running)

	// Next turn on threadA restarts its stopped container and skips POST /bootstrap because started=true.
	require.NoError(t, k.RunTurn(t.Context(), r, runTurnRequest{ThreadID: threadA}))
	require.True(t, engine.containers[nameA].Running)
	require.Equal(t, "http://127.0.0.1:32102/acp", protocol.observedEndpoint[threadA])
	require.Equal(t, 1, engine.helpers[nameA].bootstrapCalls, "restart of already-started helper must not re-POST /bootstrap")

	interrupted, err := k.InterruptTurn(t.Context(), r, threadA)
	require.NoError(t, err)
	require.True(t, interrupted)
	status, err := k.Status(t.Context(), r)
	require.NoError(t, err)
	require.True(t, status.Configured)

	// ReapStoppedMachine refuses while containers are running.
	require.Error(t, k.ReapStoppedMachine(t.Context(), r))
	require.NoError(t, k.Stop(t.Context(), r))
	require.Len(t, engine.volumes, 2, "Stop preserves volumes for warm resume")
	require.NoError(t, k.ReapStoppedMachine(t.Context(), r))
	require.Empty(t, engine.containers)
	require.Empty(t, engine.volumes)
	require.NoError(t, k.Reap(t.Context(), r))
}

func TestKitLocalRejectsForeignAuthority(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"version", "assistant-label", "generation-label", "runtime-label", "volume-label", "saved-version", "saved-generation", "saved-project", "user", "mount", "assertion-source", "assertion-writable"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			k, engine, r, _ := kitLocalFixture(t)
			ensured, err := k.Ensure(t.Context(), r)
			require.NoError(t, err)
			r.BackendMetadataJSON = ensured.BackendMetadataJSON
			threadID := uuid.New()
			require.NoError(t, k.RunTurn(t.Context(), r, runTurnRequest{ThreadID: threadID}))
			name, vol := kitLocalThreadNames(r, threadID)
			engine.calls = nil
			switch field {
			case "version":
				r.RuntimeVersion = 2
			case "assistant-label":
				engine.containers[name].Labels["gram.ai/assistant-id"] = uuid.NewString()
			case "generation-label":
				engine.containers[name].Labels["gram.ai/compatibility-generation"] = "other"
			case "runtime-label":
				engine.containers[name].Labels["gram.ai/runtime-lineage"] = uuid.NewString()
			case "volume-label":
				engine.volumes[vol]["gram.ai/assistant-id"] = uuid.NewString()
			case "saved-version":
				r.BackendMetadataJSON = bytes.ReplaceAll(r.BackendMetadataJSON, []byte(`"runtime_version":3`), []byte(`"runtime_version":2`))
			case "saved-project":
				var metadata kitLocalRuntimeMetadata
				require.NoError(t, json.Unmarshal(r.BackendMetadataJSON, &metadata))
				metadata.ProjectID = uuid.New()
				r.BackendMetadataJSON, err = json.Marshal(metadata)
				require.NoError(t, err)
			case "assertion-source":
				engine.containers[name].AssertionDirectory = "/other/projection"
			case "assertion-writable":
				engine.containers[name].AssertionReadOnly = false
			case "saved-generation":
				r.BackendMetadataJSON = bytes.ReplaceAll(r.BackendMetadataJSON, []byte("local-generation-one"), []byte("other"))
			case "user":
				engine.containers[name].User = "0"
			case "mount":
				engine.containers[name].HomeVolume = "foreign-home"
			}
			require.Error(t, k.RunTurn(t.Context(), r, runTurnRequest{ThreadID: threadID}))
			if field != "runtime-label" {
				require.Error(t, k.Reap(t.Context(), r))
			}
			require.Empty(t, engine.calls)
		})
	}
}

func TestKitLocalDoesNotAdoptLegacyOrCrossThreadVolume(t *testing.T) {
	t.Parallel()
	k, engine, r, _ := kitLocalFixture(t)
	ensured, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = ensured.BackendMetadataJSON
	threadID := uuid.New()
	_, volume := kitLocalThreadNames(r, threadID)
	engine.volumes[volume] = map[string]string{"gram.ai/runtime-version": "2"}
	require.Error(t, k.RunTurn(t.Context(), r, runTurnRequest{ThreadID: threadID}))
	require.Empty(t, engine.calls)

	name1, vol1 := kitLocalThreadNames(r, threadID)
	name2, vol2 := kitLocalThreadNames(r, uuid.New())
	require.NotEqual(t, name1, name2)
	require.NotEqual(t, vol1, vol2)
}

func TestKitLocalConstructorRequiresExplicitContracts(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"environment", "provision", "bootstrap", "token", "fence", "protocol", "helper-client", "ownership", "generation", "home", "same-ports"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			k, engine, _, _ := kitLocalFixture(t)
			config := k.config
			switch field {
			case "environment":
				config.Environment = "production"
			case "provision":
				config.ProvisionWorkload = nil
			case "bootstrap":
				config.Bootstrap = nil
			case "token":
				config.Token = nil
			case "fence":
				config.Fence = nil
			case "protocol":
				config.Protocol = nil
			case "helper-client":
				config.HelperClient = nil
			case "ownership":
				config.HomeOwnedByRuntimeUser = false
			case "generation":
				config.CompatibilityGeneration = ""
			case "home":
				config.Env = map[string]string{"HOME": "/root"}
			case "same-ports":
				config.InitPort = 8081
				config.GuestPort = 8081
			}
			_, err := newKitLocalRuntimeBackend(config, engine)
			require.Error(t, err)
		})
	}
}

func TestKitLocalDockerCreateUsesInitHelperAndNativeACP(t *testing.T) {
	t.Parallel()
	k, engine, r, _ := kitLocalFixture(t)
	ensured, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = ensured.BackendMetadataJSON
	threadID := uuid.New()
	require.NoError(t, k.RunTurn(t.Context(), r, runTurnRequest{ThreadID: threadID}))
	name, _ := kitLocalThreadNames(r, threadID)
	spec := engine.specs[name]
	spec.ExtraCACertFile = "/public-ca.pem"
	args := kitLocalCreateArgs(ateInitPort, ateACPPort, spec)
	joined := strings.Join(args, " ")
	require.Equal(t, "create", args[0])
	require.Contains(t, joined, "--user 1000:1000")
	require.Contains(t, joined, "127.0.0.1::8082")
	require.Contains(t, joined, "127.0.0.1::8081")
	require.Contains(t, joined, "host.docker.internal:host-gateway")
	require.Contains(t, joined, ":/home/kit")
	require.Contains(t, joined, "--entrypoint /bin/sh example.invalid/kit:local -c")
	require.Contains(t, args, kitLocalCABundleScript)
	require.Contains(t, joined, "/usr/local/bin/kit-ate-init")
	require.Contains(t, joined, "--remote-acp")
	spec.ExtraCACertFile = ""
	noCA := strings.Join(kitLocalCreateArgs(ateInitPort, ateACPPort, spec), " ")
	require.Contains(t, noCA, "--entrypoint /usr/local/bin/kit-ate-init example.invalid/kit:local --root=/home/kit")
}

func TestKitLocalDockerInspectRejectsPublicPort(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"Id":"id","Image":"image","Config":{"User":"1000:1000","Labels":{}},"State":{"Running":true},"Mounts":[{"Type":"volume","Name":"home","Destination":"/home/kit"}],"NetworkSettings":{"Ports":{"8082/tcp":[{"HostIp":"127.0.0.1","HostPort":"33001"}],"8081/tcp":[{"HostIp":"127.0.0.1","HostPort":"33000"}]}}}`)
	info, err := decodeKitLocalInspect(raw, ateInitPort, ateACPPort)
	require.NoError(t, err)
	require.Equal(t, 33001, info.InitHostPort)
	require.Equal(t, 33000, info.HostPort)
	require.Equal(t, "home", info.HomeVolume)
	_, err = decodeKitLocalInspect(bytes.ReplaceAll(raw, []byte("127.0.0.1"), []byte("0.0.0.0")), ateInitPort, ateACPPort)
	require.Error(t, err)
}

func TestKitLocalReservationMetadataSentinel(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "{}", "{ \n\t }"} {
		t.Run(fmt.Sprintf("empty-%q", raw), func(t *testing.T) {
			t.Parallel()
			backend, _, r, _ := kitLocalFixture(t)
			r.BackendMetadataJSON = []byte(raw)
			result, err := backend.Ensure(t.Context(), r)
			require.NoError(t, err)
			require.True(t, result.ColdStart)
			require.NotEmpty(t, result.BackendMetadataJSON)
			r.BackendMetadataJSON = result.BackendMetadataJSON
			second, err := backend.Ensure(t.Context(), r)
			require.NoError(t, err)
			require.False(t, second.ColdStart)
		})
	}
	for _, raw := range []string{"null", "[]", "[{}]", `{"runtime_version":3}`, `{"unknown":true}`, "{"} {
		t.Run("reject-"+raw, func(t *testing.T) {
			t.Parallel()
			backend, engine, r, _ := kitLocalFixture(t)
			r.BackendMetadataJSON = []byte(raw)
			_, err := backend.Ensure(t.Context(), r)
			require.Error(t, err)
			require.Empty(t, engine.calls)
		})
	}
}

func TestKitLocalEnsureAcceptsDatabaseReservation(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_reservation_metadata")
	require.NoError(t, err)
	assistant, project := seedAssistant(t, db, "kit-reservation-metadata", 1)
	thread := seedThreadWithEvent(t, db, assistant, "kit-reservation-metadata", "kit-reservation-metadata", eventStatusPending)
	err = assistantrepo.New(db).SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: project, AssistantID: assistant, RuntimeVersion: 3})
	require.NoError(t, err)
	queries := assistantrepo.New(db)
	require.NoError(t, queries.ReserveAssistantRuntimeV2(t.Context(), assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant, AssistantThreadID: thread, Backend: runtimeBackendLocal, State: runtimeStateStarting}))
	row, err := queries.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant})
	require.NoError(t, err)
	require.JSONEq(t, "{}", string(row.BackendMetadataJson))
	backend, _, _, _ := kitLocalFixture(t)
	record := assistantRuntimeRecord{ID: row.ID, ProjectID: row.ProjectID, AssistantID: row.AssistantID, AssistantThreadID: row.AssistantThreadID, Backend: row.Backend, RuntimeVersion: row.RuntimeVersion, BackendMetadataJSON: row.BackendMetadataJson}
	result, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	require.True(t, result.ColdStart)
	record.BackendMetadataJSON = result.BackendMetadataJSON
	_, provisioned, err := backend.parseMetadata(record)
	require.NoError(t, err)
	require.True(t, provisioned)
	record.ID = uuid.New()
	_, _, err = backend.parseMetadata(record)
	require.Error(t, err, "a different runtime generation cannot inherit saved authority")
}

func TestKitLocalCABundlePreservesTrustAndArgv(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	public := filepath.Join(dir, "public roots.pem")
	extra := filepath.Join(dir, "relay $(false).pem")
	require.NoError(t, os.WriteFile(public, []byte("public roots"), 0o600))
	require.NoError(t, os.WriteFile(extra, []byte("relay root\n"), 0o600))
	child := `printf '%s\n%s' "$SSL_CERT_FILE" "$1"`
	arg := "spaces ; $(false) '"
	cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", kitLocalCABundleScript, "kit-ca-bundle", public, extra, "/bin/sh", "-c", child, "child", arg)
	cmd.Env = append(os.Environ(), "TMPDIR="+dir)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	path, gotArg, ok := strings.Cut(string(out), "\n")
	require.True(t, ok)
	t.Cleanup(func() { _ = os.Remove(path) })
	require.Equal(t, arg, gotArg)
	bundle, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "public roots\nrelay root\n", string(bundle))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestKitLocalCABundleFailsClosed(t *testing.T) {
	t.Parallel()
	for _, missing := range []int{0, 1} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			roots := filepath.Join(dir, "roots.pem")
			require.NoError(t, os.WriteFile(roots, []byte("root"), 0o600))
			paths := []string{roots, roots}
			paths[missing] = filepath.Join(dir, "missing.pem")
			marker := filepath.Join(dir, "started")
			cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", kitLocalCABundleScript, "kit-ca-bundle", paths[0], paths[1], "touch", marker)
			cmd.Env = append(os.Environ(), "TMPDIR="+dir)
			require.Error(t, cmd.Run())
			_, err := os.Stat(marker)
			require.True(t, os.IsNotExist(err))
		})
	}
}

func TestKitLocalMetadataPersistsProjectAndWorkload(t *testing.T) {
	t.Parallel()
	k, _, r, _ := kitLocalFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	var metadata kitLocalRuntimeMetadata
	require.NoError(t, json.Unmarshal(result.BackendMetadataJSON, &metadata))
	require.Equal(t, r.ProjectID, metadata.ProjectID)
	require.Equal(t, r.ID, metadata.RuntimeID)
	require.Equal(t, r.AssistantID, metadata.AssistantID)
	require.NotNil(t, metadata.Workload)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	_, provisioned, err := k.parseMetadata(r)
	require.NoError(t, err)
	require.True(t, provisioned)
	metadata.ProjectID = uuid.Nil
	r.BackendMetadataJSON, err = json.Marshal(metadata)
	require.NoError(t, err)
	_, _, err = k.parseMetadata(r)
	require.Error(t, err)
}

func TestKitLocalImageMismatchRejected(t *testing.T) {
	t.Parallel()
	backend, engine, r, _ := kitLocalFixture(t)
	admitted, err := backend.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = admitted.BackendMetadataJSON
	engine.imageID = "sha256:changed-image"
	_, err = backend.Ensure(t.Context(), r)
	require.ErrorContains(t, err, "image changed")
	require.ErrorContains(t, backend.RunTurn(t.Context(), r, runTurnRequest{ThreadID: uuid.New()}), "image changed")
}

func TestKitLocalStopRetiredRevokesWorkload(t *testing.T) {
	t.Parallel()
	backend, engine, r, protocol := kitLocalFixture(t)
	admitted, err := backend.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = admitted.BackendMetadataJSON
	threadID := uuid.New()
	require.NoError(t, backend.RunTurn(t.Context(), r, runTurnRequest{ThreadID: threadID}))
	fenced := false
	backend.config.Fence = func(context.Context, assistantRuntimeRecord) error {
		fenced = true
		return nil
	}
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_local_stop_close")
	require.NoError(t, err)
	tx := testenv.BeginTx(t, t.Context(), db)
	defer func() { _ = tx.Rollback(t.Context()) }()
	r.LifecycleTx = tx
	require.NoError(t, backend.Stop(t.Context(), r))
	require.False(t, fenced, "non-retired idle stop preserves workload admission")
	require.Equal(t, []uuid.UUID{threadID}, protocol.closedThreads, "non-retired stop with lifecycle lock closes native thread session")
	r.LifecycleTx = nil
	r.Retired = true
	require.NoError(t, backend.Stop(t.Context(), r))
	require.True(t, fenced, "retired stop revokes workload admission")
	for _, info := range engine.containers {
		require.False(t, info.Running)
	}
}

func TestKitLocalIncompleteMetadataWithoutImageIDTriggersReplacement(t *testing.T) {
	t.Parallel()
	backend, _, r, _ := kitLocalFixture(t)
	incomplete, err := json.Marshal(map[string]any{
		"project_id":      r.ProjectID,
		"runtime_version": 3,
		"runtime_id":      r.ID,
		"assistant_id":    r.AssistantID,
		"generation":      backend.config.CompatibilityGeneration,
	})
	require.NoError(t, err)
	r.BackendMetadataJSON = incomplete
	_, err = backend.Ensure(t.Context(), r)
	require.ErrorIs(t, err, errKitRuntimeUnrecoverable)
	require.NoError(t, backend.Reap(t.Context(), r))
}
