package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type fakeContainer struct {
	id      string
	imageID string
	running bool
	spec    localContainerSpec
	starts  int
}

type fakeContainerEngine struct {
	mu         sync.Mutex
	imageIDs   map[string]string
	containers map[string]*fakeContainer
	volumes    map[string]bool
	hostPort   int
	runCalls   int
	runErr     error
	nextID     int
}

func newFakeContainerEngine(imageRef, imageID string, hostPort int) *fakeContainerEngine {
	return &fakeContainerEngine{
		mu:         sync.Mutex{},
		imageIDs:   map[string]string{imageRef: imageID},
		containers: map[string]*fakeContainer{},
		volumes:    map[string]bool{},
		hostPort:   hostPort,
		runCalls:   0,
		runErr:     nil,
		nextID:     0,
	}
}

func (f *fakeContainerEngine) ImageID(_ context.Context, imageRef string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.imageIDs[imageRef]
	if !ok {
		return "", errLocalImageNotFound
	}
	return id, nil
}

func (f *fakeContainerEngine) Inspect(_ context.Context, name string) (localContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	container, ok := f.containers[name]
	if !ok {
		return localContainerInfo{}, errLocalContainerNotFound
	}
	hostPort := 0
	if container.running {
		hostPort = f.hostPort
	}
	return localContainerInfo{
		ID:       container.id,
		Running:  container.running,
		ImageID:  container.imageID,
		SpecHash: container.spec.Labels[runtimeLabelSpecHash],
		HostPort: hostPort,
	}, nil
}

func (f *fakeContainerEngine) Run(_ context.Context, spec localContainerSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runCalls++
	if f.runErr != nil {
		return "", f.runErr
	}
	if _, ok := f.containers[spec.Name]; ok {
		return "", errLocalContainerNameInUse
	}
	f.nextID++
	id := fmt.Sprintf("container-%d", f.nextID)
	f.containers[spec.Name] = &fakeContainer{
		id:      id,
		imageID: f.imageIDs[spec.Image],
		running: true,
		spec:    spec,
		starts:  1,
	}
	f.volumes[spec.VolumeName] = true
	return id, nil
}

func (f *fakeContainerEngine) Start(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	container, ok := f.containers[name]
	if !ok {
		return errLocalContainerNotFound
	}
	container.running = true
	container.starts++
	return nil
}

func (f *fakeContainerEngine) Stop(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if container, ok := f.containers[name]; ok {
		container.running = false
	}
	return nil
}

func (f *fakeContainerEngine) Remove(_ context.Context, nameOrID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for name, container := range f.containers {
		if name == nameOrID || container.id == nameOrID {
			delete(f.containers, name)
		}
	}
	return nil
}

func (f *fakeContainerEngine) RemoveVolume(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.volumes, name)
	return nil
}

func (f *fakeContainerEngine) container(t *testing.T, name string) *fakeContainer {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	container, ok := f.containers[name]
	require.True(t, ok, "expected container %s to exist", name)
	return container
}

const (
	testLocalImageRef = "gram-assistant-runtime:dev"
	testLocalImageID  = "sha256:image-1"
)

func healthyRunnerHandler(idleSeconds uint64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/state":
			_ = json.NewEncoder(w).Encode(runnerStateResponse{
				AssistantID:   "a",
				UptimeSeconds: 1,
				Threads:       []runnerThreadState{{ThreadID: "t1", ChatID: "c1", IdleSeconds: idleSeconds}},
			})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}
}

func newTestLocalBackend(t *testing.T, engine containerEngine, doer runtimeHTTPDoer) *LocalRuntimeBackend {
	t.Helper()
	return NewLocalRuntimeBackend(testenv.NewLogger(t), testenv.NewTracerProvider(t), doer, engine, LocalRuntimeConfig{
		Enabled:         true,
		Environment:     "local",
		OCIImage:        "gram-assistant-runtime",
		ImageTag:        "dev",
		GuestPort:       defaultRuntimeGuestPort,
		ServerURL:       &url.URL{Scheme: "https", Host: "host.docker.internal:8080"},
		ExtraCACertFile: "",
	})
}

func localRecord(assistantID uuid.UUID, metadata []byte) assistantRuntimeRecord {
	return assistantRuntimeRecord{
		ID:                  uuid.New(),
		AssistantThreadID:   uuid.Nil,
		AssistantID:         assistantID,
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendLocal,
		BackendMetadataJSON: metadata,
		State:               runtimeStateStarting,
		WarmUntil:           pgtype.Timestamptz{},
	}
}

func decodeLocalMetadata(t *testing.T, raw []byte) localRuntimeMetadata {
	t.Helper()
	metadata, err := decodeLocalRuntimeMetadata(raw)
	require.NoError(t, err)
	return metadata
}

func TestLocalEnsureLaunchesAndReuses(t *testing.T) {
	t.Parallel()

	doer, _, port := testRunner(t, healthyRunnerHandler(5))
	engine := newFakeContainerEngine(testLocalImageRef, testLocalImageID, port)
	backend := newTestLocalBackend(t, engine, doer)
	assistantID := uuid.New()
	record := localRecord(assistantID, nil)

	result, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	require.True(t, result.ColdStart)
	metadata := decodeLocalMetadata(t, result.BackendMetadataJSON)
	require.Equal(t, "gram-asst-"+strings.ToLower(assistantID.String()), metadata.ContainerName)
	require.Equal(t, port, metadata.HostPort)
	require.Equal(t, 1, engine.runCalls)

	container := engine.container(t, metadata.ContainerName)
	require.Equal(t, "https://host.docker.internal:8080", container.spec.Env["GRAM_SERVER_URL"])
	require.Equal(t, assistantID.String(), container.spec.Env["GRAM_ASSISTANT_ID"])

	// A second Ensure adopts the running container without launching another.
	result, err = backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	require.False(t, result.ColdStart)
	require.Equal(t, 1, engine.runCalls)
}

func TestLocalEnsureRestartsStoppedContainer(t *testing.T) {
	t.Parallel()

	doer, _, port := testRunner(t, healthyRunnerHandler(5))
	engine := newFakeContainerEngine(testLocalImageRef, testLocalImageID, port)
	backend := newTestLocalBackend(t, engine, doer)
	record := localRecord(uuid.New(), nil)

	_, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	name := localContainerName(record)
	require.NoError(t, backend.Stop(t.Context(), record))
	require.False(t, engine.container(t, name).running)

	// The stopped container is rediscovered by name and restarted — no new
	// launch, even with no persisted metadata (e.g. after a server restart).
	result, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	require.True(t, result.ColdStart)
	require.Equal(t, 1, engine.runCalls)
	require.Equal(t, 2, engine.container(t, name).starts)
}

func TestLocalEnsureReplacesDriftedIdleContainer(t *testing.T) {
	t.Parallel()

	doer, _, port := testRunner(t, healthyRunnerHandler(5))
	engine := newFakeContainerEngine(testLocalImageRef, testLocalImageID, port)
	backend := newTestLocalBackend(t, engine, doer)
	record := localRecord(uuid.New(), nil)

	_, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	firstID := engine.container(t, localContainerName(record)).id

	// Rebuild: same tag, new image ID.
	engine.mu.Lock()
	engine.imageIDs[testLocalImageRef] = "sha256:image-2"
	engine.mu.Unlock()

	result, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	require.True(t, result.ColdStart)
	metadata := decodeLocalMetadata(t, result.BackendMetadataJSON)
	replacement := engine.container(t, metadata.ContainerName)
	require.NotEqual(t, firstID, replacement.id)
	require.Equal(t, "sha256:image-2", replacement.imageID)
	// The workspace volume survives the replacement.
	engine.mu.Lock()
	defer engine.mu.Unlock()
	require.True(t, engine.volumes[localVolumeName(record)])
}

func TestLocalEnsureReplacesConfigDriftedIdleContainer(t *testing.T) {
	t.Parallel()

	doer, _, port := testRunner(t, healthyRunnerHandler(5))
	engine := newFakeContainerEngine(testLocalImageRef, testLocalImageID, port)
	backend := newTestLocalBackend(t, engine, doer)
	record := localRecord(uuid.New(), nil)

	_, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	firstID := engine.container(t, localContainerName(record)).id

	// Same image, changed backend config: the server URL frozen into the
	// container's env no longer matches.
	drifted := NewLocalRuntimeBackend(testenv.NewLogger(t), testenv.NewTracerProvider(t), doer, engine, LocalRuntimeConfig{
		Enabled:         true,
		Environment:     "local",
		OCIImage:        "gram-assistant-runtime",
		ImageTag:        "dev",
		GuestPort:       defaultRuntimeGuestPort,
		ServerURL:       &url.URL{Scheme: "https", Host: "host.docker.internal:9443"},
		ExtraCACertFile: "",
	})

	result, err := drifted.Ensure(t.Context(), record)
	require.NoError(t, err)
	require.True(t, result.ColdStart)
	metadata := decodeLocalMetadata(t, result.BackendMetadataJSON)
	replacement := engine.container(t, metadata.ContainerName)
	require.NotEqual(t, firstID, replacement.id)
	require.Equal(t, "https://host.docker.internal:9443", replacement.spec.Env["GRAM_SERVER_URL"])
	// The workspace volume survives the replacement.
	engine.mu.Lock()
	defer engine.mu.Unlock()
	require.True(t, engine.volumes[localVolumeName(record)])
}

func TestLocalRecycleImageReplacesConfigDriftedIdleContainer(t *testing.T) {
	t.Parallel()

	doer, _, port := testRunner(t, healthyRunnerHandler(5))
	engine := newFakeContainerEngine(testLocalImageRef, testLocalImageID, port)
	backend := newTestLocalBackend(t, engine, doer)
	record := localRecord(uuid.New(), nil)

	_, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	firstID := engine.container(t, localContainerName(record)).id

	drifted := NewLocalRuntimeBackend(testenv.NewLogger(t), testenv.NewTracerProvider(t), doer, engine, LocalRuntimeConfig{
		Enabled:         true,
		Environment:     "local",
		OCIImage:        "gram-assistant-runtime",
		ImageTag:        "dev",
		GuestPort:       defaultRuntimeGuestPort,
		ServerURL:       &url.URL{Scheme: "https", Host: "host.docker.internal:9443"},
		ExtraCACertFile: "",
	})

	result, err := drifted.RecycleImage(t.Context(), record)
	require.NoError(t, err)
	require.True(t, result.Recycled)
	metadata := decodeLocalMetadata(t, result.BackendMetadataJSON)
	replacement := engine.container(t, metadata.ContainerName)
	require.NotEqual(t, firstID, replacement.id)
	require.Equal(t, "https://host.docker.internal:9443", replacement.spec.Env["GRAM_SERVER_URL"])
}

func TestLocalEnsureKeepsDriftedBusyContainer(t *testing.T) {
	t.Parallel()

	doer, _, port := testRunner(t, healthyRunnerHandler(0)) // idle 0 = turn in flight
	engine := newFakeContainerEngine(testLocalImageRef, testLocalImageID, port)
	backend := newTestLocalBackend(t, engine, doer)
	record := localRecord(uuid.New(), nil)

	_, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	firstID := engine.container(t, localContainerName(record)).id

	engine.mu.Lock()
	engine.imageIDs[testLocalImageRef] = "sha256:image-2"
	engine.mu.Unlock()

	// The runner reports a turn in flight, so the drifted container is kept.
	result, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	require.False(t, result.ColdStart)
	require.Equal(t, firstID, engine.container(t, localContainerName(record)).id)
	require.Equal(t, 1, engine.runCalls)
}

func TestLocalEnsureMissingImageFails(t *testing.T) {
	t.Parallel()

	doer, _, port := testRunner(t, healthyRunnerHandler(5))
	engine := newFakeContainerEngine("other-image:dev", testLocalImageID, port)
	backend := newTestLocalBackend(t, engine, doer)

	_, err := backend.Ensure(t.Context(), localRecord(uuid.New(), nil))
	require.ErrorIs(t, err, errLocalImageNotFound)
	require.ErrorContains(t, err, "build:assistants-runtime-image")
}

func TestLocalEnsureCleansUpUnhealthyLaunch(t *testing.T) {
	t.Parallel()

	doer, _, port := testRunner(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	engine := newFakeContainerEngine(testLocalImageRef, testLocalImageID, port)
	backend := newTestLocalBackend(t, engine, doer)
	backend.healthTimeout = 50 * time.Millisecond
	record := localRecord(uuid.New(), nil)

	_, err := backend.Ensure(t.Context(), record)
	require.ErrorIs(t, err, ErrRuntimeUnhealthy)

	// The container this call created was removed so the next admission
	// relaunches cleanly; the workspace volume is preserved.
	engine.mu.Lock()
	defer engine.mu.Unlock()
	require.Empty(t, engine.containers)
	require.True(t, engine.volumes[localVolumeName(record)])
}

func TestLocalEnsureAdoptsRacingLaunch(t *testing.T) {
	t.Parallel()

	doer, _, port := testRunner(t, healthyRunnerHandler(5))
	engine := newFakeContainerEngine(testLocalImageRef, testLocalImageID, port)
	backend := newTestLocalBackend(t, engine, doer)
	record := localRecord(uuid.New(), nil)
	name := localContainerName(record)

	// Simulate a racing Ensure creating the container between this call's
	// inspect and run: the engine reports the name in use, and the container
	// appears (running) for the follow-up inspect.
	engine.mu.Lock()
	engine.runErr = errLocalContainerNameInUse
	engine.containers[name] = &fakeContainer{
		id:      "container-racer",
		imageID: testLocalImageID,
		running: true,
		spec:    backend.containerSpec(record, name),
		starts:  1,
	}
	engine.mu.Unlock()

	result, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	metadata := decodeLocalMetadata(t, result.BackendMetadataJSON)
	require.Equal(t, "container-racer", metadata.ContainerID)
}

func TestLocalRunTurnPostsToRunner(t *testing.T) {
	t.Parallel()

	threadID := uuid.New()
	var gotPath, gotIdem string
	var gotBody runtimeTurnRequest
	doer, _, port := testRunner(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotIdem = r.Header.Get("X-Idempotency-Key")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	})
	engine := newFakeContainerEngine(testLocalImageRef, testLocalImageID, port)
	backend := newTestLocalBackend(t, engine, doer)

	metadata, err := json.Marshal(localRuntimeMetadata{
		ContainerID:   "container-1",
		ContainerName: "gram-asst-x",
		HostPort:      port,
	})
	require.NoError(t, err)
	record := localRecord(uuid.New(), metadata)

	err = backend.RunTurn(t.Context(), record, threadID, "event-1", "jwt-token", "hello", nil)
	require.NoError(t, err)
	require.Equal(t, "/threads/"+threadID.String()+"/turn", gotPath)
	require.Equal(t, "event-1", gotIdem)
	require.Equal(t, "hello", gotBody.Input)
	require.Equal(t, "jwt-token", gotBody.AuthToken)
}

func TestLocalStatusReadsRunnerState(t *testing.T) {
	t.Parallel()

	doer, _, port := testRunner(t, healthyRunnerHandler(7))
	engine := newFakeContainerEngine(testLocalImageRef, testLocalImageID, port)
	backend := newTestLocalBackend(t, engine, doer)

	metadata, err := json.Marshal(localRuntimeMetadata{
		ContainerID:   "container-1",
		ContainerName: "gram-asst-x",
		HostPort:      port,
	})
	require.NoError(t, err)

	status, err := backend.Status(t.Context(), localRecord(uuid.New(), metadata))
	require.NoError(t, err)
	require.True(t, status.Configured)
	require.NotNil(t, status.IdleSeconds)
	require.Equal(t, uint64(7), *status.IdleSeconds)
}

func TestLocalStopAndReapLifecycle(t *testing.T) {
	t.Parallel()

	doer, _, port := testRunner(t, healthyRunnerHandler(5))
	engine := newFakeContainerEngine(testLocalImageRef, testLocalImageID, port)
	backend := newTestLocalBackend(t, engine, doer)
	record := localRecord(uuid.New(), nil)
	name := localContainerName(record)
	volume := localVolumeName(record)

	_, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)

	// Stop keeps the container and volume; it is idempotent.
	require.NoError(t, backend.Stop(t.Context(), record))
	require.NoError(t, backend.Stop(t.Context(), record))
	require.False(t, engine.container(t, name).running)

	// ReapStoppedMachine removes the container, preserving the workspace.
	require.NoError(t, backend.ReapStoppedMachine(t.Context(), record))
	require.NoError(t, backend.ReapStoppedMachine(t.Context(), record))
	engine.mu.Lock()
	_, containerExists := engine.containers[name]
	volumeExists := engine.volumes[volume]
	engine.mu.Unlock()
	require.False(t, containerExists)
	require.True(t, volumeExists)

	// Reap removes everything and is idempotent when already gone.
	require.NoError(t, backend.Reap(t.Context(), record))
	require.NoError(t, backend.Reap(t.Context(), record))
	engine.mu.Lock()
	defer engine.mu.Unlock()
	require.False(t, engine.volumes[volume])
}

func TestLocalRecycleImageReplacesDriftedIdleContainer(t *testing.T) {
	t.Parallel()

	doer, _, port := testRunner(t, healthyRunnerHandler(5))
	engine := newFakeContainerEngine(testLocalImageRef, testLocalImageID, port)
	backend := newTestLocalBackend(t, engine, doer)
	record := localRecord(uuid.New(), nil)

	// No container yet: nothing to recycle, nothing launched.
	result, err := backend.RecycleImage(t.Context(), record)
	require.NoError(t, err)
	require.False(t, result.Recycled)
	require.Equal(t, 0, engine.runCalls)

	_, err = backend.Ensure(t.Context(), record)
	require.NoError(t, err)

	// Image current: skip.
	result, err = backend.RecycleImage(t.Context(), record)
	require.NoError(t, err)
	require.False(t, result.Recycled)

	engine.mu.Lock()
	engine.imageIDs[testLocalImageRef] = "sha256:image-2"
	engine.mu.Unlock()

	result, err = backend.RecycleImage(t.Context(), record)
	require.NoError(t, err)
	require.True(t, result.Recycled)
	metadata := decodeLocalMetadata(t, result.BackendMetadataJSON)
	require.Equal(t, "sha256:image-2", engine.container(t, metadata.ContainerName).imageID)

	// A stopped drifted container is never resurrected by the sweep.
	require.NoError(t, backend.Stop(t.Context(), record))
	engine.mu.Lock()
	engine.imageIDs[testLocalImageRef] = "sha256:image-3"
	engine.mu.Unlock()
	result, err = backend.RecycleImage(t.Context(), record)
	require.NoError(t, err)
	require.False(t, result.Recycled)
	require.False(t, engine.container(t, localContainerName(record)).running)
}

func TestLocalConcurrentEnsureCreatesOneContainer(t *testing.T) {
	t.Parallel()

	doer, _, port := testRunner(t, healthyRunnerHandler(5))
	engine := newFakeContainerEngine(testLocalImageRef, testLocalImageID, port)
	backend := newTestLocalBackend(t, engine, doer)
	record := localRecord(uuid.New(), nil)

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = backend.Ensure(t.Context(), record)
		})
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	require.Len(t, engine.containers, 1)
}
