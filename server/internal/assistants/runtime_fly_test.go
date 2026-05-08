package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/superfly/fly-go"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestFlyRuntimeBackendEnsureColdCreate(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, false)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)

	apiClient.getAppErr = errors.New("not found")
	apiClient.organization = &fly.Organization{ID: "org-123"}
	flapsClient.listMachines = []*fly.Machine{}
	flapsClient.launchMachine = &fly.Machine{
		ID:         "machine-1",
		State:      "started",
		Region:     "ord",
		InstanceID: "boot-1",
	}

	result, err := backend.Ensure(context.Background(), assistantRuntimeRecord{
		AssistantThreadID: uuid.New(),
		AssistantID:       uuid.New(),
		ProjectID:         uuid.New(),
		Backend:           runtimeBackendFlyIO,
	})
	require.NoError(t, err)
	require.True(t, result.ColdStart)
	require.True(t, result.NeedsConfigure)

	var metadata flyRuntimeMetadata
	require.NoError(t, json.Unmarshal(result.BackendMetadataJSON, &metadata))
	require.Equal(t, "machine-1", metadata.MachineID)
	require.Equal(t, "ord", metadata.Region)
	require.NotEmpty(t, metadata.AppName)
	require.NotEmpty(t, metadata.AppID)
	require.Equal(t, "boot-1", metadata.LastBootID)
}

func TestFlyRuntimeBackendEnsureWarmReuse(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)

	threadID := uuid.New()
	apiClient.app = &fly.App{ID: "app-1", Name: "gram-asst-test"}
	flapsClient.machine = &fly.Machine{
		ID:         "machine-1",
		State:      "started",
		Region:     "iad",
		InstanceID: "boot-1",
		Config: &fly.MachineConfig{
			Metadata: map[string]string{flyMachineMetadataThreadID: threadID.String()},
		},
	}

	rawMetadata, err := json.Marshal(flyRuntimeMetadata{
		AppName:    "gram-asst-test",
		AppID:      "app-1",
		AppURL:     server.URL,
		AppIP:      "",
		MachineID:  "machine-1",
		Region:     "iad",
		LastBootID: "boot-1",
	})
	require.NoError(t, err)

	result, err := backend.Ensure(context.Background(), assistantRuntimeRecord{
		AssistantThreadID:   threadID,
		AssistantID:         uuid.New(),
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.NoError(t, err)
	require.False(t, result.ColdStart)
	require.False(t, result.NeedsConfigure)
}

func TestFlyRuntimeBackendEnsureMachineUnconfigured(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, false)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)

	threadID := uuid.New()
	apiClient.app = &fly.App{ID: "app-1", Name: "gram-asst-test"}
	flapsClient.machine = &fly.Machine{
		ID:         "machine-1",
		State:      "started",
		Region:     "iad",
		InstanceID: "boot-1",
		Config: &fly.MachineConfig{
			Metadata: map[string]string{flyMachineMetadataThreadID: threadID.String()},
		},
	}

	rawMetadata, err := json.Marshal(flyRuntimeMetadata{
		AppName:    "gram-asst-test",
		AppID:      "app-1",
		AppURL:     server.URL,
		AppIP:      "",
		MachineID:  "machine-1",
		Region:     "iad",
		LastBootID: "boot-1",
	})
	require.NoError(t, err)

	result, err := backend.Ensure(context.Background(), assistantRuntimeRecord{
		AssistantThreadID:   threadID,
		AssistantID:         uuid.New(),
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.NoError(t, err)
	require.True(t, result.ColdStart)
	require.True(t, result.NeedsConfigure)
}

func TestFlyRuntimeBackendEnsureFlapsNotFoundEstablishedTearsDown(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)

	// ensureApp succeeds (GraphQL says app exists) but flaps Get + List both
	// 404. Established runtime — LastBootID + MachineID set from a prior boot.
	apiClient.app = &fly.App{ID: "app-1", Name: "gram-asst-test"}
	flapsClient.getErr = errors.New("not found")
	flapsClient.listErr = errors.New("app not found")

	rawMetadata, err := json.Marshal(flyRuntimeMetadata{
		AppName:    "gram-asst-test",
		AppID:      "app-1",
		AppURL:     server.URL,
		AppIP:      "",
		MachineID:  "machine-1",
		Region:     "iad",
		LastBootID: "boot-1",
	})
	require.NoError(t, err)

	_, err = backend.Ensure(context.Background(), assistantRuntimeRecord{
		AssistantThreadID:   uuid.New(),
		AssistantID:         uuid.New(),
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.ErrorIs(t, err, errFlyAppCorrupted)
	require.Equal(t, 1, apiClient.deleteCalls, "corrupted app must be torn down so the next ensure recreates it")
}

func TestFlyRuntimeBackendEnsureFlapsNotFoundFreshAppPropagates(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)

	// Fresh app — no prior boot recorded. flaps List 404 here is the
	// propagation case isFlyAppPropagating + launchMachineWithRetry already
	// cover; corruption detection must NOT trigger and tear the app down.
	apiClient.app = &fly.App{ID: "app-1", Name: "gram-asst-test"}
	flapsClient.listErr = errors.New("app not found")

	rawMetadata, err := json.Marshal(flyRuntimeMetadata{
		AppName:    "gram-asst-test",
		AppID:      "",
		AppURL:     server.URL,
		AppIP:      "",
		MachineID:  "",
		Region:     "iad",
		LastBootID: "",
	})
	require.NoError(t, err)

	_, err = backend.Ensure(context.Background(), assistantRuntimeRecord{
		AssistantThreadID:   uuid.New(),
		AssistantID:         uuid.New(),
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.Error(t, err)
	require.NotErrorIs(t, err, errFlyAppCorrupted)
	require.Equal(t, 0, apiClient.deleteCalls, "fresh app must not be torn down on a propagation 404")
}

func TestFlyRuntimeBackendEnsureFreshlyRecreatedAppClearsPriorBootMetadata(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)

	// Metadata can be copied from a failed prior runtime row. If the app was
	// deleted for confirmed corruption and ensureApp recreates it, those old
	// machine IDs belong to the previous app incarnation and must not turn
	// fresh Machines propagation into another corruption teardown.
	apiClient.getAppErr = errors.New("not found")
	apiClient.organization = &fly.Organization{ID: "org-123"}
	flapsClient.listErr = errors.New("app not found")

	rawMetadata, err := json.Marshal(flyRuntimeMetadata{
		AppName:    "gram-asst-test",
		AppID:      "old-app",
		AppURL:     server.URL,
		AppIP:      "",
		MachineID:  "machine-from-old-app",
		Region:     "iad",
		LastBootID: "boot-from-old-app",
	})
	require.NoError(t, err)

	_, err = backend.Ensure(context.Background(), assistantRuntimeRecord{
		AssistantThreadID:   uuid.New(),
		AssistantID:         uuid.New(),
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.Error(t, err)
	require.NotErrorIs(t, err, errFlyAppCorrupted)
	require.Equal(t, 0, apiClient.deleteCalls, "freshly recreated app must not be torn down because of stale prior boot metadata")
}

func TestFlyRuntimeBackendEnsureExistingAppWithLegacyMetadataDoesNotTreatPropagationAsCorruption(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)

	apiClient.app = &fly.App{ID: "app-1", Name: "gram-asst-test"}
	flapsClient.getErr = errors.New("not found")
	flapsClient.listErr = errors.New("app not found")

	rawMetadata, err := json.Marshal(flyRuntimeMetadata{
		AppName:    "gram-asst-test",
		AppID:      "",
		AppURL:     server.URL,
		AppIP:      "",
		MachineID:  "machine-from-legacy-metadata",
		Region:     "iad",
		LastBootID: "boot-from-legacy-metadata",
	})
	require.NoError(t, err)

	_, err = backend.Ensure(context.Background(), assistantRuntimeRecord{
		AssistantThreadID:   uuid.New(),
		AssistantID:         uuid.New(),
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.Error(t, err)
	require.NotErrorIs(t, err, errFlyAppCorrupted)
	require.Equal(t, 0, apiClient.deleteCalls, "legacy metadata without app_id is not proof that the current app had a prior boot")
}

func TestFlyRuntimeBackendEnsureExistingAppAfterPartialCreateFailureClearsStaleMetadata(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)

	apiClient.getAppErr = errors.New("not found")
	apiClient.organization = &fly.Organization{ID: "org-123"}
	apiClient.allocateSharedErr = errors.New("allocate shared ip failed")

	rawMetadata, err := json.Marshal(flyRuntimeMetadata{
		AppName:    "gram-asst-test",
		AppID:      "old-app",
		AppURL:     server.URL,
		AppIP:      "",
		MachineID:  "machine-from-old-app",
		Region:     "iad",
		LastBootID: "boot-from-old-app",
	})
	require.NoError(t, err)

	_, err = backend.Ensure(context.Background(), assistantRuntimeRecord{
		AssistantThreadID:   uuid.New(),
		AssistantID:         uuid.New(),
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.ErrorContains(t, err, "allocate assistant fly runtime shared ip")

	apiClient.getAppErr = nil
	apiClient.allocateSharedErr = nil
	flapsClient.listErr = errors.New("app not found")

	_, err = backend.Ensure(context.Background(), assistantRuntimeRecord{
		AssistantThreadID:   uuid.New(),
		AssistantID:         uuid.New(),
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.Error(t, err)
	require.NotErrorIs(t, err, errFlyAppCorrupted)
	require.Equal(t, 0, apiClient.deleteCalls, "partially created current app should not be torn down because metadata came from an older app incarnation")
}

func TestFlyRuntimeBackendStopWithoutMachineMetadataIsNoop(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)

	rawMetadata, err := json.Marshal(flyRuntimeMetadata{
		AppName:    "gram-asst-test",
		AppID:      "",
		AppURL:     "",
		AppIP:      "",
		MachineID:  "",
		Region:     "",
		LastBootID: "",
	})
	require.NoError(t, err)

	err = backend.Stop(context.Background(), assistantRuntimeRecord{
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.NoError(t, err)
	require.Equal(t, 0, flapsClient.stopCalls, "stop is a no-op without a machine id to target")
	require.Equal(t, 0, apiClient.deleteCalls, "stop must not delete the fly app")
}

func TestFlyRuntimeBackendStopStopsMachineKeepsApp(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)

	rawMetadata, err := json.Marshal(flyRuntimeMetadata{
		AppName:    "gram-asst-test",
		AppID:      "app-1",
		AppURL:     server.URL,
		AppIP:      "",
		MachineID:  "machine-1",
		Region:     "iad",
		LastBootID: "boot-1",
	})
	require.NoError(t, err)

	err = backend.Stop(context.Background(), assistantRuntimeRecord{
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.NoError(t, err)
	require.Equal(t, 1, flapsClient.stopCalls, "stop must target the machine via flaps")
	require.Equal(t, 0, apiClient.deleteCalls, "stop must not delete the fly app — reuse the next admit")
}

func TestFlyRuntimeBackendStopToleratesMissingMachine(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)

	flapsClient.stopErr = errors.New("not found")
	rawMetadata, err := json.Marshal(flyRuntimeMetadata{
		AppName:    "gram-asst-test",
		AppID:      "app-1",
		AppURL:     server.URL,
		AppIP:      "",
		MachineID:  "machine-gone",
		Region:     "iad",
		LastBootID: "boot-1",
	})
	require.NoError(t, err)

	err = backend.Stop(context.Background(), assistantRuntimeRecord{
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.NoError(t, err)
	require.Equal(t, 0, apiClient.deleteCalls, "stop must not fall back to delete-app when flaps reports missing")
}

func TestFlyRuntimeBackendReapDeletesAppByMetadataName(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, apiClient, _ := newTestFlyRuntimeBackend(t, server)

	rawMetadata, err := json.Marshal(flyRuntimeMetadata{
		AppName:    "gram-asst-orphan",
		AppID:      "",
		AppURL:     "",
		AppIP:      "",
		MachineID:  "",
		Region:     "",
		LastBootID: "",
	})
	require.NoError(t, err)

	err = backend.Reap(context.Background(), assistantRuntimeRecord{
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.NoError(t, err)
	require.Equal(t, 1, apiClient.deleteCalls)
}

func TestFlyRuntimeBackendReapWithoutMetadataIsNoop(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, apiClient, _ := newTestFlyRuntimeBackend(t, server)

	err := backend.Reap(context.Background(), assistantRuntimeRecord{
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: nil,
	})
	require.NoError(t, err)
	require.Equal(t, 0, apiClient.deleteCalls)
}

func TestFlyRuntimeBackendReapTreatsAppNotFoundAsSuccess(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, apiClient, _ := newTestFlyRuntimeBackend(t, server)

	apiClient.deleteErr = errors.New("not found")
	rawMetadata, err := json.Marshal(flyRuntimeMetadata{
		AppName:    "gram-asst-already-gone",
		AppID:      "",
		AppURL:     "",
		AppIP:      "",
		MachineID:  "",
		Region:     "",
		LastBootID: "",
	})
	require.NoError(t, err)

	err = backend.Reap(context.Background(), assistantRuntimeRecord{
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.NoError(t, err)
}

func TestFlyRuntimeBackendServerURLRejectsLoopback(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, _, _ := newTestFlyRuntimeBackend(t, server)
	backend.config.ServerURLOverride = nil

	_, err := backend.ServerURL(context.Background(), assistantRuntimeRecord{
		Backend: runtimeBackendFlyIO,
	}, &url.URL{Scheme: "https", Host: "127.0.0.1:8080"})
	require.Error(t, err)
	require.ErrorContains(t, err, "public")
}

type testFlyRuntimeAPIClient struct {
	app               *fly.App
	getAppErr         error
	deleteErr         error
	deleteCalls       int
	createAppErr      error
	allocateSharedErr error
	organization      *fly.Organization
}

func (c *testFlyRuntimeAPIClient) AllocateSharedIPAddress(_ context.Context, _ string) (net.IP, error) {
	if c.allocateSharedErr != nil {
		return nil, c.allocateSharedErr
	}
	// Returning nil leaves AppIP empty so clientForTarget falls back to the
	// default httpClient — tests point AppURL at a local httptest server that
	// isn't reachable by the real shared-IP dialer.
	return nil, nil
}

func (c *testFlyRuntimeAPIClient) AllocateIPAddress(_ context.Context, _ string, _ string, _ string, _ string, _ string) (*fly.IPAddress, error) {
	return &fly.IPAddress{Address: "2001:db8::1"}, nil
}

func (c *testFlyRuntimeAPIClient) CreateApp(_ context.Context, input fly.CreateAppInput) (*fly.App, error) {
	if c.createAppErr != nil {
		return nil, c.createAppErr
	}
	c.app = &fly.App{ID: "app-created", Name: input.Name}
	c.getAppErr = nil
	return c.app, nil
}

func (c *testFlyRuntimeAPIClient) DeleteApp(_ context.Context, _ string) error {
	c.deleteCalls++
	return c.deleteErr
}

func (c *testFlyRuntimeAPIClient) GetApp(_ context.Context, _ string) (*fly.App, error) {
	if c.getAppErr != nil {
		return nil, c.getAppErr
	}
	if c.app == nil {
		return nil, errors.New("not found")
	}
	return c.app, nil
}

func (c *testFlyRuntimeAPIClient) GetOrganizationBySlug(_ context.Context, _ string) (*fly.Organization, error) {
	if c.organization == nil {
		return nil, errors.New("organization not configured")
	}
	return c.organization, nil
}

type testFlyRuntimeFlapsClient struct {
	machine       *fly.Machine
	getErr        error
	listMachines  []*fly.Machine
	listErr       error
	launchMachine *fly.Machine
	launchErr     error
	startErr      error
	stopErr       error
	stopCalls     int
	waitErr       error
}

func (c *testFlyRuntimeFlapsClient) Get(_ context.Context, _ string, _ string) (*fly.Machine, error) {
	if c.getErr != nil {
		return nil, c.getErr
	}
	if c.machine == nil {
		return nil, errors.New("not found")
	}
	return c.machine, nil
}

func (c *testFlyRuntimeFlapsClient) Launch(_ context.Context, _ string, _ fly.LaunchMachineInput) (*fly.Machine, error) {
	if c.launchErr != nil {
		return nil, c.launchErr
	}
	if c.launchMachine != nil {
		c.machine = c.launchMachine
		return c.launchMachine, nil
	}
	return nil, errors.New("launch machine not configured")
}

func (c *testFlyRuntimeFlapsClient) List(_ context.Context, _ string, _ string) ([]*fly.Machine, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return c.listMachines, nil
}

func (c *testFlyRuntimeFlapsClient) Start(_ context.Context, _ string, _ string, _ string) (*fly.MachineStartResponse, error) {
	if c.startErr != nil {
		return nil, c.startErr
	}
	if c.machine != nil {
		c.machine.State = "started"
	}
	return &fly.MachineStartResponse{}, nil
}

func (c *testFlyRuntimeFlapsClient) Stop(_ context.Context, _ string, _ fly.StopMachineInput, _ string) error {
	c.stopCalls++
	if c.stopErr != nil {
		return c.stopErr
	}
	if c.machine != nil {
		c.machine.State = "stopped"
	}
	return nil
}

func (c *testFlyRuntimeFlapsClient) Wait(_ context.Context, _ string, _ *fly.Machine, _ string, _ time.Duration) error {
	return c.waitErr
}

type testFlyRuntimeFlapsFactory struct {
	client flyRuntimeFlapsClient
}

func (f *testFlyRuntimeFlapsFactory) New(context.Context) (flyRuntimeFlapsClient, error) {
	return f.client, nil
}

func newTestAssistantRuntimeServer(t *testing.T, configured bool) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/state", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(runnerStateResponse{Configured: configured})
	})
	mux.HandleFunc("/configure", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/turn", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"finish_reason":"accepted","final_text":""}`))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func newTestFlyRuntimeBackend(t *testing.T, server *httptest.Server) (*FlyRuntimeBackend, *testFlyRuntimeAPIClient, *testFlyRuntimeFlapsClient) {
	t.Helper()

	apiClient := &testFlyRuntimeAPIClient{}
	flapsClient := &testFlyRuntimeFlapsClient{}

	backend := &FlyRuntimeBackend{
		logger: testenv.NewLogger(t),
		tracer: testenv.NewTracerProvider(t).Tracer("test"),
		config: FlyRuntimeConfig{
			DefaultFlyOrg:     "speakeasy-lab",
			DefaultFlyRegion:  "iad",
			OCIImage:          "registry.fly.io/assistant-runtime",
			ImageVersion:      "dev",
			AppNamePrefix:     "gram-asst",
			ServerURLOverride: mustParseURL(t, "https://gram.example.com"),
		},
		client:       apiClient,
		flapsFactory: &testFlyRuntimeFlapsFactory{client: flapsClient},
		httpClient: &testRuntimeHTTPDoer{
			baseURL: server.URL,
			client:  server.Client(),
		},
	}

	return backend, apiClient, flapsClient
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed
}

var _ RuntimeBackend = (*FlyRuntimeBackend)(nil)

type testRuntimeHTTPDoer struct {
	baseURL string
	client  *http.Client
}

func (d *testRuntimeHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	target, err := url.Parse(d.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse runtime base url: %w", err)
	}
	cloned.URL.Scheme = target.Scheme
	cloned.URL.Host = target.Host
	cloned.Host = target.Host
	resp, err := d.client.Do(cloned)
	if err != nil {
		return nil, fmt.Errorf("send test runtime request: %w", err)
	}
	return resp, nil
}
