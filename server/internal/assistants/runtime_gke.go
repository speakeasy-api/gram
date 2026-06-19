package assistants

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const runtimeBackendGKE = "gke"

const (
	// gkeRuntimeReadyTimeout bounds the wait for a freshly claimed sandbox to be
	// assigned a pod and report Ready. A claim with no pre-warmed pod available
	// cold-starts (the pod pulls the image before it becomes ready), so this is
	// generous.
	gkeRuntimeReadyTimeout = 3 * time.Minute
	// gkeRuntimeHealthTimeout bounds the runner /healthz wait once the sandbox
	// reports Ready and its runner pod has a routable IP.
	gkeRuntimeHealthTimeout = 60 * time.Second
	gkeRuntimePollInterval  = time.Second
	// gkeRuntimeReapCallTimeout caps a single delete during reap so one wedged
	// row cannot consume the parent activity's deadline.
	gkeRuntimeReapCallTimeout   = 30 * time.Second
	gkeRuntimeDefaultReqTimeout = 2 * time.Minute
	gkeRuntimeTurnTimeout       = 30 * time.Minute

	gkeSandboxReadyConditionType = "Ready"

	gkeMetadataAssistantID = "gram.speakeasy.com/assistant-id"
	gkeMetadataProjectID   = "gram.speakeasy.com/project-id"
	gkeMetadataRole        = "gram.speakeasy.com/role"
	gkeMetadataRoleValue   = "assistant_runtime"

	// gkeClaimUIDLabel is injected by the SandboxClaim controller onto the pod,
	// carrying the owning claim's UID. We resolve the runner pod (for its IP) by
	// this label.
	gkeClaimUIDLabel = "agents.x-k8s.io/claim-uid"
)

var (
	// GKE's managed Agent Sandbox addon serves v1alpha1 (upstream main is on
	// v1beta1, but the GKE-shipped feature is v1alpha1).
	gkeSandboxClaimGVR = schema.GroupVersionResource{Group: "extensions.agents.x-k8s.io", Version: "v1alpha1", Resource: "sandboxclaims"}
	gkeSandboxGVR      = schema.GroupVersionResource{Group: "agents.x-k8s.io", Version: "v1alpha1", Resource: "sandboxes"}
	gkePodGVR          = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
)

// GKERuntimeConfig configures the GKE Agent Sandbox runtime backend. The dynamic
// client targets the (separate) assistant cluster and is built from its endpoint
// + CA with the process's Google credentials — see k8s.NewRemoteDynamicClient.
// The gram server reaches the cluster by endpoint, not in-cluster config, so it
// runs from the gram cluster (or locally) against a different cluster.
type GKERuntimeConfig struct {
	Dynamic dynamic.Interface

	// Namespace is where SandboxClaims, the SandboxTemplate, and the warm pool live.
	Namespace string
	// SandboxTemplate is the SandboxTemplate name a claim references. A
	// SandboxWarmPool referencing the same template pre-warms pods, and the
	// controller adopts a warm pod for the claim automatically.
	SandboxTemplate string
	// GuestPort is the runner HTTP port inside the sandbox pod.
	GuestPort int

	OCIImage string
	ImageTag string
	// ServerURL is the management API base the server embeds in the runner
	// bootstrap response (completions + MCP URLs). The runner's own
	// GRAM_SERVER_URL (for the bootstrap call) and OTLP config are baked into
	// the SandboxTemplate, so the claim carries no env and adopts a warm pod.
	ServerURL *url.URL
	// RunnerCIDRBlocks are the pod CIDR(s) runner pods draw IPs from. The server
	// dials runners by pod IP (resolved from the Kubernetes API) — RFC1918
	// addresses the guardian egress policy blocks by default — so these blocks
	// are allowlisted for the runner HTTP client only.
	RunnerCIDRBlocks []string
}

func (c GKERuntimeConfig) Validate() error {
	if c.Dynamic == nil {
		return fmt.Errorf("gke assistant runtime requires a kubernetes client for the assistant cluster (set --assistant-runtime-gke-cluster-endpoint)")
	}
	if c.Namespace == "" {
		return fmt.Errorf("--assistant-runtime-gke-namespace is required")
	}
	if c.SandboxTemplate == "" {
		return fmt.Errorf("--assistant-runtime-gke-sandbox-template is required")
	}
	if c.OCIImage == "" {
		return fmt.Errorf("--assistant-runtime-oci-image is required")
	}
	if c.ServerURL == nil || c.ServerURL.Hostname() == "" {
		return fmt.Errorf("gke assistant runtime requires a public --assistant-runtime-server-url or --server-url")
	}
	if len(c.RunnerCIDRBlocks) == 0 {
		return fmt.Errorf("--assistant-runtime-gke-runner-cidr is required: the server dials runner pod IPs, which the egress policy blocks unless their CIDR is allowlisted")
	}
	for _, cidr := range c.RunnerCIDRBlocks {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("invalid --assistant-runtime-gke-runner-cidr %q: %w", cidr, err)
		}
	}
	return nil
}

// gkeRuntimeMetadata is persisted opaquely in assistant_runtimes.backend_metadata_json
// for gke-backed rows. PodIP is the runner pod's IP, routable from the gram
// cluster across the shared VPC; the runner is reached at http://<PodIP>:<guest
// port> (in-cluster Service DNS is not resolvable across clusters).
type gkeRuntimeMetadata struct {
	Namespace   string `json:"namespace"`
	ClaimName   string `json:"claim_name"`
	SandboxName string `json:"sandbox_name"`
	PodIP       string `json:"pod_ip"`
	Image       string `json:"image,omitempty"`
}

type GKERuntimeBackend struct {
	logger     *slog.Logger
	tracer     trace.Tracer
	config     GKERuntimeConfig
	httpClient runtimeHTTPDoer
}

func NewGKERuntimeBackend(logger *slog.Logger, tracerProvider trace.TracerProvider, httpClient runtimeHTTPDoer, config GKERuntimeConfig) *GKERuntimeBackend {
	if config.GuestPort == 0 {
		config.GuestPort = defaultRuntimeGuestPort
	}
	return &GKERuntimeBackend{
		logger:     logger.With(attr.SlogComponent("assistants_gke")),
		tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/assistants"),
		config:     config,
		httpClient: httpClient,
	}
}

func (g *GKERuntimeBackend) Backend() string { return runtimeBackendGKE }

func (g *GKERuntimeBackend) SupportsBackend(backend string) bool { return backend == runtimeBackendGKE }

func (g *GKERuntimeBackend) ServerURL() *url.URL { return g.config.ServerURL }

func (g *GKERuntimeBackend) ImageRef() string { return g.desiredImageRef() }

// ReusesIdleRuntimes is false: Stop deletes the SandboxClaim, so the next
// admission recreates the claim and adopts a fresh pre-warmed pod from the
// pool — already running whatever image the SandboxTemplate now points at. A
// new image therefore rolls in by terminating idle runtimes (the deploy sweep
// and the warm-TTL expiry both do this) rather than the in-place RecycleImage
// Fly relies on.
func (g *GKERuntimeBackend) ReusesIdleRuntimes() bool { return false }

func (g *GKERuntimeBackend) desiredImageRef() string {
	if g.config.ImageTag == "" {
		return g.config.OCIImage
	}
	return g.config.OCIImage + ":" + g.config.ImageTag
}

func (g *GKERuntimeBackend) claimName(runtime assistantRuntimeRecord) string {
	return "gram-asst-" + strings.ToLower(runtime.AssistantID.String())
}

func (g *GKERuntimeBackend) claims() dynamic.ResourceInterface {
	return g.config.Dynamic.Resource(gkeSandboxClaimGVR).Namespace(g.config.Namespace)
}

func (g *GKERuntimeBackend) Ensure(ctx context.Context, runtime assistantRuntimeRecord) (result RuntimeBackendEnsureResult, err error) {
	if err := validateRuntimeBackend(g, runtime.Backend); err != nil {
		return RuntimeBackendEnsureResult{}, err
	}

	ctx, span := g.tracer.Start(ctx, "assistants.runtime.gke.ensure")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	name := g.claimName(runtime)
	coldStart := false
	if _, getErr := g.claims().Get(ctx, name, metav1.GetOptions{}); getErr != nil {
		if !k8serrors.IsNotFound(getErr) {
			return RuntimeBackendEnsureResult{}, fmt.Errorf("get sandbox claim %s: %w", name, getErr)
		}
		_, createErr := g.claims().Create(ctx, g.buildClaim(name, runtime), metav1.CreateOptions{})
		switch {
		case createErr == nil:
			coldStart = true
			// We created this claim. If readiness below fails, delete it so the
			// next admission recreates a fresh claim rather than re-attaching to
			// an unhealthy one with no persisted metadata. Only this branch owns
			// the claim: a pre-existing claim, or one a racing Ensure created
			// (AlreadyExists), is left alone — another turn may be using it. Use
			// WithoutCancel so cleanup still runs when the failure was a timeout.
			defer func() {
				if err == nil {
					return
				}
				delCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gkeRuntimeReapCallTimeout)
				defer cancel()
				if delErr := g.claims().Delete(delCtx, name, metav1.DeleteOptions{}); delErr != nil && !k8serrors.IsNotFound(delErr) {
					g.logger.WarnContext(ctx, "delete sandbox claim after failed ensure",
						attr.SlogAssistantID(runtime.AssistantID.String()),
						attr.SlogError(delErr),
					)
				}
			}()
		case k8serrors.IsAlreadyExists(createErr):
			// A racing Ensure created the claim between our Get and Create. Attach
			// to it like a pre-existing claim; the other turn owns its lifecycle.
		default:
			return RuntimeBackendEnsureResult{}, fmt.Errorf("create sandbox claim %s: %w", name, createErr)
		}
	}

	metadata, err := g.waitForSandbox(ctx, name)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	metadata.Image = g.desiredImageRef()

	if err := g.waitForHealth(ctx, metadata); err != nil {
		return RuntimeBackendEnsureResult{}, err
	}

	payload, err := json.Marshal(metadata)
	if err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("encode gke runtime metadata: %w", err)
	}
	return RuntimeBackendEnsureResult{ColdStart: coldStart, BackendMetadataJSON: payload}, nil
}

func (g *GKERuntimeBackend) buildClaim(name string, runtime assistantRuntimeRecord) *unstructured.Unstructured {
	labels := map[string]any{
		gkeMetadataAssistantID: strings.ToLower(runtime.AssistantID.String()),
		gkeMetadataProjectID:   strings.ToLower(runtime.ProjectID.String()),
		gkeMetadataRole:        gkeMetadataRoleValue,
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": gkeSandboxClaimGVR.Group + "/" + gkeSandboxClaimGVR.Version,
		"kind":       "SandboxClaim",
		"metadata": map[string]any{
			"name":      name,
			"namespace": g.config.Namespace,
			"labels":    labels,
		},
		// The claim references the SandboxTemplate; a SandboxWarmPool on the same
		// template pre-warms pods and the controller adopts one for the claim.
		// No spec.env — the runner boots generic from a pooled pod and learns its
		// assistant from the first /turn (env-or-request); shared config
		// (GRAM_SERVER_URL, OTLP) lives in the SandboxTemplate. shutdownPolicy
		// Delete cascades a claim delete to the Sandbox and its pod.
		"spec": map[string]any{
			"sandboxTemplateRef": map[string]any{"name": g.config.SandboxTemplate},
			"lifecycle":          map[string]any{"shutdownPolicy": "Delete"},
		},
	}}
}

// waitForSandbox polls the claim until it names an assigned Sandbox, then polls
// that Sandbox until it reports Ready and its runner pod has an IP.
func (g *GKERuntimeBackend) waitForSandbox(ctx context.Context, claimName string) (gkeRuntimeMetadata, error) {
	deadline := time.Now().Add(gkeRuntimeReadyTimeout)
	sandboxes := g.config.Dynamic.Resource(gkeSandboxGVR).Namespace(g.config.Namespace)
	for {
		claim, err := g.claims().Get(ctx, claimName, metav1.GetOptions{})
		if err != nil {
			return gkeRuntimeMetadata{}, fmt.Errorf("get sandbox claim %s: %w", claimName, err)
		}
		sandboxName, _, _ := unstructured.NestedString(claim.Object, "status", "sandbox", "Name")
		if sandboxName != "" {
			sandbox, err := sandboxes.Get(ctx, sandboxName, metav1.GetOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				return gkeRuntimeMetadata{}, fmt.Errorf("get sandbox %s: %w", sandboxName, err)
			}
			if err == nil && sandboxReady(sandbox) {
				podIP, err := g.resolvePodIP(ctx, string(claim.GetUID()))
				if err != nil {
					return gkeRuntimeMetadata{}, err
				}
				if podIP != "" {
					return gkeRuntimeMetadata{
						Namespace:   g.config.Namespace,
						ClaimName:   claimName,
						SandboxName: sandboxName,
						PodIP:       podIP,
						Image:       "",
					}, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return gkeRuntimeMetadata{}, fmt.Errorf("%w: sandbox claim %s not ready within %s", ErrRuntimeUnhealthy, claimName, gkeRuntimeReadyTimeout)
		}
		select {
		case <-ctx.Done():
			return gkeRuntimeMetadata{}, fmt.Errorf("wait for sandbox %s: %w", claimName, ctx.Err())
		case <-time.After(gkeRuntimePollInterval):
		}
	}
}

// sandboxReady reports whether the Sandbox's Ready condition is true.
func sandboxReady(sandbox *unstructured.Unstructured) bool {
	conditions, _, _ := unstructured.NestedSlice(sandbox.Object, "status", "conditions")
	for _, raw := range conditions {
		cond, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		condType, _, _ := unstructured.NestedString(cond, "type")
		condStatus, _, _ := unstructured.NestedString(cond, "status")
		if condType == gkeSandboxReadyConditionType {
			return condStatus == string(metav1.ConditionTrue)
		}
	}
	return false
}

// resolvePodIP finds the runner pod for a claim (by the controller-injected
// claim-uid label) and returns its IP once the pod is Running. An empty string
// means the pod is not scheduled/running yet, so the caller keeps polling.
func (g *GKERuntimeBackend) resolvePodIP(ctx context.Context, claimUID string) (string, error) {
	pods, err := g.config.Dynamic.Resource(gkePodGVR).Namespace(g.config.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: gkeClaimUIDLabel + "=" + claimUID,
	})
	if err != nil {
		return "", fmt.Errorf("list runner pods for claim %s: %w", claimUID, err)
	}
	for i := range pods.Items {
		phase, _, _ := unstructured.NestedString(pods.Items[i].Object, "status", "phase")
		ip, _, _ := unstructured.NestedString(pods.Items[i].Object, "status", "podIP")
		if phase == "Running" && ip != "" {
			return ip, nil
		}
	}
	return "", nil
}

func (g *GKERuntimeBackend) endpoint(metadata gkeRuntimeMetadata) string {
	return fmt.Sprintf("http://%s:%d", metadata.PodIP, g.config.GuestPort)
}

func (g *GKERuntimeBackend) waitForHealth(ctx context.Context, metadata gkeRuntimeMetadata) error {
	deadline := time.Now().Add(gkeRuntimeHealthTimeout)
	for {
		if _, err := g.doRequest(ctx, metadata, http.MethodGet, "/healthz", nil, "", "", 0); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: gke runtime health check timed out", ErrRuntimeUnhealthy)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for gke runtime health: %w", ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (g *GKERuntimeBackend) RunTurn(ctx context.Context, runtime assistantRuntimeRecord, threadID uuid.UUID, idempotencyKey string, authToken string, prompt string) error {
	if err := validateRuntimeBackend(g, runtime.Backend); err != nil {
		return err
	}
	metadata, err := decodeGKERuntimeMetadata(runtime.BackendMetadataJSON)
	if err != nil {
		return err
	}
	if metadata.PodIP == "" {
		return fmt.Errorf("%w: gke runtime pod ip is not available", ErrRuntimeUnhealthy)
	}

	reqBody, err := json.Marshal(runtimeTurnRequest{
		Input:       prompt,
		AuthToken:   authToken,
		AssistantID: runtime.AssistantID.String(),
	})
	if err != nil {
		return fmt.Errorf("marshal gke runtime turn request: %w", err)
	}
	if _, err := g.doRequest(ctx, metadata, http.MethodPost, "/threads/"+threadID.String()+"/turn", reqBody, "application/json", idempotencyKey, gkeRuntimeTurnTimeout); err != nil {
		return fmt.Errorf("%w: execute gke turn request: %w", classifyTurnError(err), err)
	}
	return nil
}

func (g *GKERuntimeBackend) Status(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	if err := validateRuntimeBackend(g, runtime.Backend); err != nil {
		return RuntimeBackendStatus{}, err
	}
	metadata, err := decodeGKERuntimeMetadata(runtime.BackendMetadataJSON)
	if err != nil {
		return RuntimeBackendStatus{}, err
	}
	if metadata.PodIP == "" {
		return RuntimeBackendStatus{}, fmt.Errorf("%w: gke runtime pod ip is not available", ErrRuntimeUnhealthy)
	}
	body, err := g.doRequest(ctx, metadata, http.MethodGet, "/state", nil, "", "", 0)
	if err != nil {
		return RuntimeBackendStatus{}, fmt.Errorf("load gke runtime state: %w", err)
	}
	var state runnerStateResponse
	if err := json.Unmarshal(body, &state); err != nil {
		return RuntimeBackendStatus{}, fmt.Errorf("decode gke runtime state: %w", err)
	}
	return RuntimeBackendStatus{Configured: true, IdleSeconds: state.minThreadIdle()}, nil
}

// Stop deletes the SandboxClaim. GKE has no cheap pause: a fresh admit recreates
// the claim and adopts a pre-warmed pod from the pool. The runner holds no
// per-assistant state across restarts — history is replayed from the server on
// the next /turn — so deleting on Stop loses nothing.
func (g *GKERuntimeBackend) Stop(ctx context.Context, runtime assistantRuntimeRecord) error {
	return g.deleteClaim(ctx, runtime)
}

// Reap permanently deletes the SandboxClaim, which cascades to the Sandbox and
// pod. Idempotent: a missing claim is success.
func (g *GKERuntimeBackend) Reap(ctx context.Context, runtime assistantRuntimeRecord) error {
	return g.deleteClaim(ctx, runtime)
}

func (g *GKERuntimeBackend) deleteClaim(ctx context.Context, runtime assistantRuntimeRecord) error {
	if err := validateRuntimeBackend(g, runtime.Backend); err != nil {
		return err
	}
	metadata, err := decodeGKERuntimeMetadata(runtime.BackendMetadataJSON)
	if err != nil {
		return err
	}
	name := metadata.ClaimName
	if name == "" {
		name = g.claimName(runtime)
	}

	deleteCtx, cancel := context.WithTimeout(ctx, gkeRuntimeReapCallTimeout)
	defer cancel()
	if err := g.claims().Delete(deleteCtx, name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete sandbox claim %s: %w", name, err)
	}
	return nil
}

// RecycleImage is a no-op on GKE. There is no in-place image swap: a claimed
// sandbox's pod ownership has moved off the warm pool, so a SandboxTemplate /
// warm-pool image bump never disturbs an in-flight turn — the running sandbox
// finishes on its current image and new admissions draw new-image pods. Old
// idle sandboxes roll over via warm-TTL expiry and the janitor.
func (g *GKERuntimeBackend) RecycleImage(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
	if err := validateRuntimeBackend(g, runtime.Backend); err != nil {
		return RuntimeBackendRecycleResult{}, err
	}
	return RuntimeBackendRecycleResult{Recycled: false, BackendMetadataJSON: nil}, nil
}

func (g *GKERuntimeBackend) doRequest(ctx context.Context, metadata gkeRuntimeMetadata, method, path string, body []byte, contentType, idempotencyKey string, maxTimeout time.Duration) ([]byte, error) {
	fallback := gkeRuntimeDefaultReqTimeout
	if maxTimeout > 0 {
		fallback = maxTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, fallback)
	defer cancel()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, g.endpoint(metadata)+path, reader)
	if err != nil {
		return nil, fmt.Errorf("build gke runtime request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if idempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", idempotencyKey)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute gke runtime request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gke runtime response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &runtimeResponseError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(respBody))}
	}
	return respBody, nil
}

func decodeGKERuntimeMetadata(raw []byte) (gkeRuntimeMetadata, error) {
	if len(raw) == 0 {
		return gkeRuntimeMetadata{}, fmt.Errorf("gke runtime metadata is empty")
	}
	var metadata gkeRuntimeMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return gkeRuntimeMetadata{}, fmt.Errorf("decode gke runtime metadata: %w", err)
	}
	return metadata, nil
}

var _ RuntimeBackend = (*GKERuntimeBackend)(nil)
