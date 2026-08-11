package assistants

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newGKEFakeDynamic() *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		gkeSandboxClaimGVR: "SandboxClaimList",
		gkeSandboxGVR:      "SandboxList",
		gkePodGVR:          "PodList",
	})
}

// seedUnstructured inserts an object under an explicit GVR. The fake's default
// kind->resource guesser pluralizes "Sandbox" to "sandboxs" (not "sandboxes"),
// so seeding via the constructor would file objects under the wrong resource;
// creating through the typed GVR interface registers them where the backend
// reads them.
func seedUnstructured(t *testing.T, dyn dynamic.Interface, gvr schema.GroupVersionResource, obj *unstructured.Unstructured) {
	t.Helper()
	_, err := dyn.Resource(gvr).Namespace(obj.GetNamespace()).Create(t.Context(), obj, metav1.CreateOptions{})
	require.NoError(t, err)
}

func newTestGKEBackend(t *testing.T, dyn dynamic.Interface, doer runtimeHTTPDoer, port int) *GKERuntimeBackend {
	t.Helper()
	return NewGKERuntimeBackend(testenv.NewLogger(t), testenv.NewTracerProvider(t), doer, GKERuntimeConfig{
		Dynamic:          dyn,
		Namespace:        "gram-test",
		SandboxTemplate:  "gram-asst-pool",
		GuestPort:        port,
		OCIImage:         "registry.example.com/gram-assistant-runtime",
		ImageTag:         "test",
		ServerURL:        &url.URL{Scheme: "https", Host: "gram.example.com"},
		RunnerCIDRBlocks: []string{"10.52.0.0/16"},
	})
}

// testRunner spins up an httptest server impersonating the in-pod runner and
// returns a doer wired to it plus the host/port the backend should dial.
func testRunner(t *testing.T, handler http.HandlerFunc) (doer runtimeHTTPDoer, host string, port int) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	h, p, err := net.SplitHostPort(u.Host)
	require.NoError(t, err)
	portNum, err := strconv.Atoi(p)
	require.NoError(t, err)
	return srv.Client(), h, portNum
}

func gkeRecord(t *testing.T, backend *GKERuntimeBackend, assistantID uuid.UUID, podIP string) assistantRuntimeRecord {
	t.Helper()
	meta := gkeRuntimeMetadata{
		Namespace:   "gram-test",
		ClaimName:   "gram-asst-" + assistantID.String(),
		SandboxName: "sb-1",
		PodIP:       podIP,
		Image:       backend.desiredImageRef(),
	}
	raw, err := json.Marshal(meta)
	require.NoError(t, err)
	return assistantRuntimeRecord{
		ID:                  uuid.New(),
		AssistantThreadID:   uuid.Nil,
		AssistantID:         assistantID,
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendGKE,
		BackendMetadataJSON: raw,
		State:               runtimeStateActive,
		WarmUntil:           pgtype.Timestamptz{},
	}
}

func TestSandboxReady(t *testing.T) {
	t.Parallel()

	ready := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "Suspended", "status": "False"},
				map[string]any{"type": "Ready", "status": "True"},
			},
		},
	}}
	require.True(t, sandboxReady(ready))

	notReady := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"conditions": []any{map[string]any{"type": "Ready", "status": "False"}},
		},
	}}
	require.False(t, sandboxReady(notReady))
}

func TestGKERunTurnPostsToRunner(t *testing.T) {
	t.Parallel()

	assistantID := uuid.New()
	threadID := uuid.New()
	var gotPath, gotIdem string
	var gotBody runtimeTurnRequest
	doer, host, port := testRunner(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotIdem = r.Header.Get("X-Idempotency-Key")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	})

	backend := newTestGKEBackend(t, newGKEFakeDynamic(), doer, port)
	err := backend.RunTurn(t.Context(), gkeRecord(t, backend, assistantID, host), runTurnRequest{
		ThreadID:       threadID,
		IdempotencyKey: "event-123",
		AuthToken:      "jwt-token",
		Prompt:         "hello",
		InputParts:     nil,
		MCPServers:     nil,
	})
	require.NoError(t, err)
	require.Equal(t, "/threads/"+threadID.String()+"/turn", gotPath)
	require.Equal(t, "event-123", gotIdem)
	require.Equal(t, "hello", gotBody.Input)
	require.Equal(t, "jwt-token", gotBody.AuthToken)
}

func TestGKEStatusReadsRunnerState(t *testing.T) {
	t.Parallel()

	doer, host, port := testRunner(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(runnerStateResponse{
			AssistantID:   "a",
			UptimeSeconds: 10,
			Threads:       []runnerThreadState{{ThreadID: "t1", ChatID: "c1", IdleSeconds: 7}},
		})
	})

	backend := newTestGKEBackend(t, newGKEFakeDynamic(), doer, port)
	status, err := backend.Status(t.Context(), gkeRecord(t, backend, uuid.New(), host))
	require.NoError(t, err)
	require.True(t, status.Configured)
	require.NotNil(t, status.IdleSeconds)
	require.Equal(t, uint64(7), *status.IdleSeconds)
}

func TestGKEEnsureWaitsForReadySandbox(t *testing.T) {
	t.Parallel()

	assistantID := uuid.New()
	doer, host, port := testRunner(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	claimName := "gram-asst-" + assistantID.String()
	claim := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": gkeSandboxClaimGVR.Group + "/" + gkeSandboxClaimGVR.Version,
		"kind":       "SandboxClaim",
		"metadata":   map[string]any{"name": claimName, "namespace": "gram-test", "uid": "claim-uid-1"},
		"status":     map[string]any{"sandbox": map[string]any{"Name": "sb-1"}},
	}}
	sandbox := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": gkeSandboxGVR.Group + "/" + gkeSandboxGVR.Version,
		"kind":       "Sandbox",
		"metadata":   map[string]any{"name": "sb-1", "namespace": "gram-test"},
		"status": map[string]any{
			"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
		},
	}}
	// The controller injects the claim-uid label on the runner pod; the backend
	// resolves the pod IP by it.
	pod := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":      "sb-1-pod",
			"namespace": "gram-test",
			"labels":    map[string]any{gkeClaimUIDLabel: "claim-uid-1"},
		},
		"status": map[string]any{"phase": "Running", "podIP": host},
	}}

	dyn := newGKEFakeDynamic()
	seedUnstructured(t, dyn, gkeSandboxClaimGVR, claim)
	seedUnstructured(t, dyn, gkeSandboxGVR, sandbox)
	seedUnstructured(t, dyn, gkePodGVR, pod)
	backend := newTestGKEBackend(t, dyn, doer, port)
	result, err := backend.Ensure(t.Context(), assistantRuntimeRecord{
		ID:                  uuid.New(),
		AssistantThreadID:   uuid.Nil,
		AssistantID:         assistantID,
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendGKE,
		BackendMetadataJSON: nil,
		State:               runtimeStateStarting,
		WarmUntil:           pgtype.Timestamptz{},
	})
	require.NoError(t, err)
	require.False(t, result.ColdStart) // claim already existed
	var meta gkeRuntimeMetadata
	require.NoError(t, json.Unmarshal(result.BackendMetadataJSON, &meta))
	require.Equal(t, claimName, meta.ClaimName)
	require.Equal(t, "sb-1", meta.SandboxName)
	require.Equal(t, host, meta.PodIP)
}

// seedSandboxWithPod seeds a Ready sandbox and a Running runner pod labeled
// with the claim uid — the controller-written state a claim adoption ends in.
func seedSandboxWithPod(t *testing.T, dyn dynamic.Interface, claimUID, sandboxName, podIP, image string) {
	t.Helper()
	seedUnstructured(t, dyn, gkeSandboxGVR, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": gkeSandboxGVR.Group + "/" + gkeSandboxGVR.Version,
		"kind":       "Sandbox",
		"metadata":   map[string]any{"name": sandboxName, "namespace": "gram-test"},
		"status": map[string]any{
			"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
		},
	}})
	seedUnstructured(t, dyn, gkePodGVR, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":      sandboxName + "-pod",
			"namespace": "gram-test",
			"labels":    map[string]any{gkeClaimUIDLabel: claimUID},
		},
		"spec":   map[string]any{"containers": []any{map[string]any{"name": "runner", "image": image}}},
		"status": map[string]any{"phase": "Running", "podIP": podIP},
	}})
}

// seedClaimedSandbox seeds a claim (with uid) plus its Ready sandbox and
// Running runner pod, exercising the same controller-written fields the
// backend reads in production.
func seedClaimedSandbox(t *testing.T, dyn dynamic.Interface, claimName, claimUID, sandboxName, podIP, image string) {
	t.Helper()
	seedUnstructured(t, dyn, gkeSandboxClaimGVR, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": gkeSandboxClaimGVR.Group + "/" + gkeSandboxClaimGVR.Version,
		"kind":       "SandboxClaim",
		"metadata":   map[string]any{"name": claimName, "namespace": "gram-test", "uid": claimUID},
		"status":     map[string]any{"sandbox": map[string]any{"Name": sandboxName}},
	}})
	seedSandboxWithPod(t, dyn, claimUID, sandboxName, podIP, image)
}

// adoptOnClaimCreate simulates the SandboxClaim controller in the fake
// cluster: every claim create is stamped with the given uid and assigned the
// given sandbox, mirroring warm-pool adoption. The matching sandbox and pod
// must be seeded separately.
func adoptOnClaimCreate(dyn *dynamicfake.FakeDynamicClient, claimUID, sandboxName string) {
	dyn.PrependReactor("create", "sandboxclaims", func(action k8stesting.Action) (bool, runtime.Object, error) {
		obj, ok := action.(k8stesting.CreateAction).GetObject().(*unstructured.Unstructured)
		if !ok {
			return false, nil, nil
		}
		obj.SetUID(types.UID(claimUID))
		_ = unstructured.SetNestedField(obj.Object, sandboxName, "status", "sandbox", "Name")
		return false, nil, nil
	})
}

func TestGKEEnsureRecyclesClaimOnStaleImage(t *testing.T) {
	t.Parallel()

	assistantID := uuid.New()
	doer, host, port := testRunner(t, healthyRunnerHandler(30))

	dyn := newGKEFakeDynamic()
	backend := newTestGKEBackend(t, dyn, doer, port)
	claimName := "gram-asst-" + assistantID.String()
	seedClaimedSandbox(t, dyn, claimName, "claim-uid-1", "sb-stale", host, "registry.example.com/gram-assistant-runtime:old")

	// The re-created claim adopts a warm pod already on the desired image.
	adoptOnClaimCreate(dyn, "claim-uid-2", "sb-fresh")
	seedSandboxWithPod(t, dyn, "claim-uid-2", "sb-fresh", host, backend.desiredImageRef())

	result, err := backend.Ensure(t.Context(), assistantRuntimeRecord{
		ID:                  uuid.New(),
		AssistantThreadID:   uuid.Nil,
		AssistantID:         assistantID,
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendGKE,
		BackendMetadataJSON: nil,
		State:               runtimeStateStarting,
		WarmUntil:           pgtype.Timestamptz{},
	})
	require.NoError(t, err)
	require.True(t, result.ColdStart, "recycled claim is a fresh pod")
	var meta gkeRuntimeMetadata
	require.NoError(t, json.Unmarshal(result.BackendMetadataJSON, &meta))
	require.Equal(t, "sb-fresh", meta.SandboxName)
	require.Equal(t, backend.desiredImageRef(), meta.Image)
}

func TestGKEEnsureKeepsStaleImageClaimWhileTurnInFlight(t *testing.T) {
	t.Parallel()

	assistantID := uuid.New()
	doer, host, port := testRunner(t, healthyRunnerHandler(0))

	dyn := newGKEFakeDynamic()
	backend := newTestGKEBackend(t, dyn, doer, port)
	claimName := "gram-asst-" + assistantID.String()
	seedClaimedSandbox(t, dyn, claimName, "claim-uid-1", "sb-stale", host, "registry.example.com/gram-assistant-runtime:old")

	result, err := backend.Ensure(t.Context(), assistantRuntimeRecord{
		ID:                  uuid.New(),
		AssistantThreadID:   uuid.Nil,
		AssistantID:         assistantID,
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendGKE,
		BackendMetadataJSON: nil,
		State:               runtimeStateStarting,
		WarmUntil:           pgtype.Timestamptz{},
	})
	require.NoError(t, err)
	require.False(t, result.ColdStart)
	var meta gkeRuntimeMetadata
	require.NoError(t, json.Unmarshal(result.BackendMetadataJSON, &meta))
	require.Equal(t, "sb-stale", meta.SandboxName, "busy runner is never recycled mid-turn")
}

func TestGKERecycleImageReplacesStaleClaim(t *testing.T) {
	t.Parallel()

	assistantID := uuid.New()
	doer, host, port := testRunner(t, healthyRunnerHandler(30))

	dyn := newGKEFakeDynamic()
	backend := newTestGKEBackend(t, dyn, doer, port)
	record := gkeRecord(t, backend, assistantID, host)
	claimName := "gram-asst-" + assistantID.String()
	seedClaimedSandbox(t, dyn, claimName, "claim-uid-1", "sb-stale", host, "registry.example.com/gram-assistant-runtime:old")

	adoptOnClaimCreate(dyn, "claim-uid-2", "sb-fresh")
	seedSandboxWithPod(t, dyn, "claim-uid-2", "sb-fresh", host, backend.desiredImageRef())

	result, err := backend.RecycleImage(t.Context(), record)
	require.NoError(t, err)
	require.True(t, result.Recycled)
	var meta gkeRuntimeMetadata
	require.NoError(t, json.Unmarshal(result.BackendMetadataJSON, &meta))
	require.Equal(t, "sb-fresh", meta.SandboxName)
	require.Equal(t, backend.desiredImageRef(), meta.Image)
}

func TestGKERecycleImageSkipsCurrentImageAndMissingClaim(t *testing.T) {
	t.Parallel()

	assistantID := uuid.New()
	doer, host, port := testRunner(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	dyn := newGKEFakeDynamic()
	backend := newTestGKEBackend(t, dyn, doer, port)
	record := gkeRecord(t, backend, assistantID, host)

	// Missing claim: resource gone is a skip, not an error.
	result, err := backend.RecycleImage(t.Context(), record)
	require.NoError(t, err)
	require.False(t, result.Recycled)

	// Pod already on the configured image: nothing to roll.
	claimName := "gram-asst-" + assistantID.String()
	seedClaimedSandbox(t, dyn, claimName, "claim-uid-1", "sb-1", host, backend.desiredImageRef())
	result, err = backend.RecycleImage(t.Context(), record)
	require.NoError(t, err)
	require.False(t, result.Recycled)
}

func TestGKEReapDeletesClaimIdempotently(t *testing.T) {
	t.Parallel()

	assistantID := uuid.New()
	claimName := "gram-asst-" + assistantID.String()
	claim := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": gkeSandboxClaimGVR.Group + "/" + gkeSandboxClaimGVR.Version,
		"kind":       "SandboxClaim",
		"metadata":   map[string]any{"name": claimName, "namespace": "gram-test"},
	}}
	dyn := newGKEFakeDynamic()
	seedUnstructured(t, dyn, gkeSandboxClaimGVR, claim)
	// No runner doer: Reap only deletes the SandboxClaim, no HTTP to the pod.
	backend := newTestGKEBackend(t, dyn, nil, 8081)
	record := gkeRecord(t, backend, assistantID, "127.0.0.1")

	require.NoError(t, backend.Reap(t.Context(), record))
	_, err := dyn.Resource(gkeSandboxClaimGVR).Namespace("gram-test").Get(t.Context(), claimName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))

	// Idempotent: reaping an already-gone claim succeeds.
	require.NoError(t, backend.Reap(t.Context(), record))
}
