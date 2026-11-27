package assistants

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// kitLocalHelperReadyTimeout bounds waiting for local kit-ate-init or kit serve readiness.
	kitLocalHelperReadyTimeout = 30 * time.Second

	// kitLocalThreadIdleWindow stops idle per-thread local containers promptly so
	// running multiple threads locally stays frugal with host memory and CPU.
	kitLocalThreadIdleWindow = ateActorWarmWindow
)

// KitLocalBootstrap carries the verified Kit filesystem configuration contract
// delivered through kit-ate-init's write-once POST /bootstrap endpoint.
type KitLocalBootstrap struct {
	Model, ReasoningEffort           string
	Workload                         *KitWorkloadBinding
	AssertionFile, InferenceEndpoint string
	EgressInjectedAuth               bool
	AgentsMD, MCPJSON, GatewayToken  []byte
}

// KitLocalRuntimeConfig is local-only and runs one Docker container per thread
// using the same kit-ate-init + kit serve --remote-acp contract as ATE.
// Env must not contain credentials; credential bytes belong exclusively in
// Bootstrap and the read-only operator-projected assertion mount.
type KitLocalRuntimeConfig struct {
	// Environment must be "local"; non-local values are rejected at startup.
	Environment string

	// Image is an explicit non-latest tag or digest built from agents/ate-runtime/Dockerfile.
	Image string

	// Env holds non-secret environment variables forwarded into kit-ate-init bootstrap.
	Env map[string]string

	// InitPort is the container port served by kit-ate-init (defaults to ateInitPort, 8082).
	InitPort int

	// GuestPort is the container port served by kit serve --remote-acp (defaults to ateACPPort, 8081).
	GuestPort int

	// ServerURL is the local Speakeasy server base URL.
	ServerURL *url.URL

	// WorkloadAssertionFile is the absolute host path to the operator-projected assertion file.
	WorkloadAssertionFile string

	// ExtraCACertFile is an optional PEM CA bundle appended to container system roots.
	ExtraCACertFile string

	// Network is the optional isolated Docker network carrying the local ai.speakeasy.com TLS relay.
	Network string

	// CompatibilityGeneration tags local containers and volumes for ownership verification.
	CompatibilityGeneration string

	// HomeOwnedByRuntimeUser confirms the image provides /home/kit owned by UID/GID 1000.
	HomeOwnedByRuntimeUser bool

	// HelperClient talks to loopback kit-ate-init endpoints on 127.0.0.1.
	HelperClient *guardian.HTTPClient

	// ProvisionWorkload admits the runtime's local workload identity once during Ensure.
	ProvisionWorkload func(context.Context, assistantRuntimeRecord) (*KitWorkloadBinding, error)

	// Bootstrap builds the per-thread kit-ate-init bootstrap payload using the runtime's admitted workload.
	Bootstrap func(context.Context, assistantRuntimeRecord, uuid.UUID, *KitWorkloadBinding) (KitLocalBootstrap, error)

	// Token derives the per-thread ACP bearer token for authenticating calls to kit serve.
	Token func(context.Context, assistantRuntimeRecord, uuid.UUID) (string, error)

	// Fence revokes the runtime's workload admission on failure or retirement.
	Fence func(context.Context, assistantRuntimeRecord) error

	// Protocol constructs the native Kit ACP protocol driver for one thread container.
	Protocol func(context.Context, assistantRuntimeRecord, uuid.UUID, *url.URL, string) (runtimeProtocol, error)

	// Now supplies the clock for idle tracking (defaults to time.Now).
	Now func() time.Time

	// HelperPollInterval overrides the kit-ate-init readiness polling interval when positive.
	HelperPollInterval time.Duration
}

type kitLocalEngine interface {
	ImageID(context.Context, string) (string, error)
	InspectKit(context.Context, string) (kitLocalContainerInfo, error)
	InspectKitVolume(context.Context, string) (map[string]string, error)
	CreateKitVolume(context.Context, string, map[string]string) error
	CreateKit(context.Context, kitLocalContainerSpec) (string, error)
	ListKitContainers(context.Context, string) ([]string, error)
	ListKitVolumes(context.Context, string) ([]string, error)
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Remove(context.Context, string) error
	RemoveVolume(context.Context, string) error
}

type kitLocalContainerInfo struct {
	AssertionDirectory      string
	AssertionReadOnly       bool
	ID, Name, ImageID, User string
	HomeVolume              string
	Running                 bool
	InitHostPort            int
	HostPort                int
	Labels                  map[string]string
}

type kitLocalContainerSpec struct {
	Name, Image, VolumeName string
	Labels, Env             map[string]string
	WorkloadAssertionFile   string
	ExtraCACertFile         string
	Network                 string
}

type kitLocalRuntimeMetadata struct {
	ProjectID      uuid.UUID           `json:"project_id"`
	Workload       *KitWorkloadBinding `json:"workload,omitempty"`
	RuntimeVersion int16               `json:"runtime_version"`
	RuntimeID      uuid.UUID           `json:"runtime_id"`
	AssistantID    uuid.UUID           `json:"assistant_id"`
	Generation     string              `json:"generation"`
	ImageID        string              `json:"image_id"`
}

type kitLocalThreadKey struct {
	runtimeID uuid.UUID
	threadID  uuid.UUID
}

// KitLocalRuntimeBackend runs one local Docker container per assistant thread
// over native Kit ACP (/acp) via kit-ate-init + kit serve --remote-acp.
type KitLocalRuntimeBackend struct {
	config     KitLocalRuntimeConfig
	engine     kitLocalEngine
	mu         sync.Mutex
	specHash   string
	lastActive map[kitLocalThreadKey]time.Time
}

var _ RuntimeBackend = (*KitLocalRuntimeBackend)(nil)

func NewKitLocalRuntimeBackend(config KitLocalRuntimeConfig) (*KitLocalRuntimeBackend, error) {
	initPort := config.InitPort
	if initPort == 0 {
		initPort = ateInitPort
	}
	acpPort := config.GuestPort
	if acpPort == 0 {
		acpPort = ateACPPort
	}
	config.InitPort = initPort
	config.GuestPort = acpPort
	return newKitLocalRuntimeBackend(config, newKitLocalDockerEngine(initPort, acpPort))
}

func newKitLocalRuntimeBackend(config KitLocalRuntimeConfig, engine kitLocalEngine) (*KitLocalRuntimeBackend, error) {
	if config.InitPort == 0 {
		config.InitPort = ateInitPort
	}
	if config.GuestPort == 0 {
		config.GuestPort = ateACPPort
	}
	if config.Environment != "local" || config.Image == "" ||
		config.InitPort < 1 || config.InitPort > 65535 ||
		config.GuestPort < 1 || config.GuestPort > 65535 ||
		config.InitPort == config.GuestPort ||
		config.ServerURL == nil || config.ServerURL.Hostname() == "" ||
		config.CompatibilityGeneration == "" || !config.HomeOwnedByRuntimeUser ||
		config.HelperClient == nil || config.ProvisionWorkload == nil ||
		config.Bootstrap == nil || config.Token == nil || config.Fence == nil ||
		config.Protocol == nil || engine == nil {
		return nil, fmt.Errorf("kit local requires local environment, explicit image/ports, server URL, generation, UID1000 HOME contract, loopback helper client, workload provisioning, bootstrap, token, fence, and native ACP protocol")
	}
	if err := validateKitLocalAssertionFile(config.WorkloadAssertionFile); err != nil {
		return nil, err
	}
	if !kitExplicitImage(config.Image) {
		return nil, fmt.Errorf("kit local requires an explicit non-latest image tag or digest")
	}
	if value, ok := config.Env["HOME"]; ok && value != "/home/kit" {
		return nil, fmt.Errorf("kit local HOME must be /home/kit")
	}
	config.Env = maps.Clone(config.Env)
	if config.Env == nil {
		config.Env = make(map[string]string)
	}
	// Management traffic uses ServerURL; inference always uses the dedicated
	// provider origin. Never route management MCP discovery to that provider.
	origin := "https://ai.speakeasy.com"
	if supplied := config.Env["SPEAKEASY_ORIGIN"]; supplied != "" && supplied != origin {
		return nil, fmt.Errorf("kit requires the HTTPS Speakeasy inference origin")
	}
	config.Env["SPEAKEASY_ORIGIN"] = origin
	serverURL := *config.ServerURL
	config.ServerURL = &serverURL
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.HelperPollInterval <= 0 {
		config.HelperPollInterval = ateHelperPollInterval
	}
	encoded, err := json.Marshal(map[string]any{
		"image":               config.Image,
		"init_port":           config.InitPort,
		"acp_port":            config.GuestPort,
		"env":                 config.Env,
		"ca":                  config.ExtraCACertFile,
		"network":             config.Network,
		"assertion_directory": filepath.Dir(config.WorkloadAssertionFile),
	})
	if err != nil {
		return nil, fmt.Errorf("encode kit local immutable spec: %w", err)
	}
	return &KitLocalRuntimeBackend{
		config:     config,
		engine:     engine,
		mu:         sync.Mutex{},
		specHash:   fmt.Sprintf("%x", sha256.Sum256(encoded)),
		lastActive: map[kitLocalThreadKey]time.Time{},
	}, nil
}

func (k *KitLocalRuntimeBackend) Backend() string               { return runtimeBackendLocal }
func (k *KitLocalRuntimeBackend) SupportsBackend(b string) bool { return b == runtimeBackendLocal }
func (k *KitLocalRuntimeBackend) ImageRef() string              { return k.config.Image }
func (k *KitLocalRuntimeBackend) ServerURL() *url.URL {
	serverURL := *k.config.ServerURL
	return &serverURL
}

func kitLocalThreadNames(r assistantRuntimeRecord, threadID uuid.UUID) (string, string) {
	sum := sha256.Sum256([]byte(threadID.String()))
	stem := "gram-kit-" + r.AssistantID.String() + "-" + r.ID.String() + "-" + hex.EncodeToString(sum[:8])
	return stem, stem + "-home"
}

func (k *KitLocalRuntimeBackend) runtimeLabels(r assistantRuntimeRecord) map[string]string {
	return map[string]string{
		"gram.ai/assistant-id":             r.AssistantID.String(),
		"gram.ai/project-id":               r.ProjectID.String(),
		"gram.ai/runtime-version":          "3",
		"gram.ai/runtime-lineage":          r.ID.String(),
		"gram.ai/compatibility-generation": k.config.CompatibilityGeneration,
		"gram.ai/local-spec":               k.specHash,
	}
}

func (k *KitLocalRuntimeBackend) threadLabels(r assistantRuntimeRecord, threadID uuid.UUID) map[string]string {
	labels := k.runtimeLabels(r)
	labels["gram.ai/thread-id"] = threadID.String()
	return labels
}

// kitLocalUnprovisionedMetadata recognizes the database reservation sentinel.
// Only absent bytes or an empty JSON object mean "not provisioned". Null, arrays,
// partial records and unknown nonempty objects must still fail closed.
func kitLocalUnprovisionedMetadata(raw []byte) bool {
	if len(raw) == 0 {
		return true
	}
	var fields map[string]json.RawMessage
	return json.Unmarshal(raw, &fields) == nil && fields != nil && len(fields) == 0
}

func (k *KitLocalRuntimeBackend) parseMetadata(r assistantRuntimeRecord) (kitLocalRuntimeMetadata, bool, error) {
	var zero kitLocalRuntimeMetadata
	if r.RuntimeVersion != 3 || r.Backend != runtimeBackendLocal || r.ID == uuid.Nil || r.AssistantID == uuid.Nil || r.ProjectID == uuid.Nil {
		return zero, false, fmt.Errorf("kit local requires an immutable version 3 local runtime and assistant/project identity")
	}
	if kitLocalUnprovisionedMetadata(r.BackendMetadataJSON) {
		return zero, false, nil
	}
	var m kitLocalRuntimeMetadata
	if err := json.Unmarshal(r.BackendMetadataJSON, &m); err != nil {
		return zero, false, fmt.Errorf("decode kit local metadata: %w", err)
	}
	if m.RuntimeVersion != 3 || m.RuntimeID != r.ID || m.ProjectID != r.ProjectID || m.AssistantID != r.AssistantID || m.Generation != k.config.CompatibilityGeneration {
		return zero, false, fmt.Errorf("kit local saved metadata does not match runtime authority")
	}
	if m.ImageID == "" {
		return zero, false, fmt.Errorf("%w: kit local metadata missing image_id requires replacement", errKitRuntimeUnrecoverable)
	}
	return m, true, nil
}

func (k *KitLocalRuntimeBackend) checkLabels(r assistantRuntimeRecord, labels map[string]string, threadID uuid.UUID) error {
	expected := k.runtimeLabels(r)
	if threadID != uuid.Nil {
		expected["gram.ai/thread-id"] = threadID.String()
	}
	for key, value := range expected {
		if labels[key] != value {
			return fmt.Errorf("kit local ownership mismatch: %s", key)
		}
	}
	return nil
}

func (k *KitLocalRuntimeBackend) checkThreadContainer(r assistantRuntimeRecord, threadID uuid.UUID, info kitLocalContainerInfo) error {
	if err := k.checkLabels(r, info.Labels, threadID); err != nil {
		return err
	}
	_, volume := kitLocalThreadNames(r, threadID)
	if info.ID == "" || info.User != "1000:1000" || info.HomeVolume != volume {
		return fmt.Errorf("kit local container has invalid identity, user or HOME mount")
	}
	if info.AssertionDirectory != filepath.Dir(k.config.WorkloadAssertionFile) || !info.AssertionReadOnly {
		return fmt.Errorf("kit local requires its exact read-only projected assertion directory")
	}
	return nil
}

// Ensure resolves the local image and provisions the runtime's local workload
// admission once. Individual thread containers are created lazily in RunTurn.
func (k *KitLocalRuntimeBackend) Ensure(ctx context.Context, r assistantRuntimeRecord) (result RuntimeBackendEnsureResult, resultErr error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	m, provisioned, err := k.parseMetadata(r)
	if err != nil {
		return result, err
	}
	if r.Retired {
		return result, ErrKitRuntimeBinding
	}
	imageID, err := k.engine.ImageID(ctx, k.config.Image)
	if err != nil {
		return result, fmt.Errorf("resolve kit local image: %w", err)
	}
	if provisioned {
		if m.ImageID != imageID {
			return result, fmt.Errorf("kit local image changed; retire authority and use a new runtime generation")
		}
		if m.Workload == nil || !m.Workload.MCPProjectGateway {
			return result, errKitMCPConfigurationUnverified
		}
		return RuntimeBackendEnsureResult{ColdStart: false, BackendMetadataJSON: r.BackendMetadataJSON}, nil
	}

	workload, err := k.config.ProvisionWorkload(ctx, r)
	if err != nil {
		return result, fmt.Errorf("provision kit local workload: %w", err)
	}
	if workload == nil || !workload.MCPProjectGateway {
		return result, errKitMCPConfigurationUnverified
	}
	raw, err := json.Marshal(kitLocalRuntimeMetadata{
		ProjectID:      r.ProjectID,
		Workload:       workload,
		RuntimeVersion: 3,
		RuntimeID:      r.ID,
		AssistantID:    r.AssistantID,
		Generation:     k.config.CompatibilityGeneration,
		ImageID:        imageID,
	})
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		r.BackendMetadataJSON, _ = json.Marshal(struct {
			Workload *KitWorkloadBinding `json:"workload"`
		}{workload})
		return result, errors.Join(fmt.Errorf("encode kit local metadata: %w", err), k.config.Fence(cleanup, r))
	}
	return RuntimeBackendEnsureResult{ColdStart: true, BackendMetadataJSON: raw}, nil
}

func (k *KitLocalRuntimeBackend) prepareThread(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID) (endpoint *url.URL, token string, returnErr error) {
	if threadID == uuid.Nil {
		return nil, "", ErrKitRuntimeBinding
	}
	k.mu.Lock()
	defer k.mu.Unlock()

	m, provisioned, err := k.parseMetadata(r)
	if err != nil {
		return nil, "", err
	}
	if !provisioned || m.Workload == nil || !m.Workload.MCPProjectGateway {
		return nil, "", errKitMCPConfigurationUnverified
	}
	imageID, err := k.engine.ImageID(ctx, k.config.Image)
	if err != nil {
		return nil, "", fmt.Errorf("resolve kit local image: %w", err)
	}
	if m.ImageID != imageID {
		return nil, "", fmt.Errorf("kit local image changed; retire authority and use a new runtime generation")
	}

	startedID := ""
	defer func() {
		if returnErr == nil || startedID == "" {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if stopErr := k.engine.Stop(cleanup, startedID); stopErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("stop unready kit local container: %w", stopErr))
		}
	}()

	name, volume := kitLocalThreadNames(r, threadID)
	labels, err := k.engine.InspectKitVolume(ctx, volume)
	if errors.Is(err, errLocalContainerNotFound) {
		if err := k.engine.CreateKitVolume(ctx, volume, k.threadLabels(r, threadID)); err != nil {
			return nil, "", fmt.Errorf("create kit thread HOME volume: %w", err)
		}
		labels, err = k.engine.InspectKitVolume(ctx, volume)
	}
	if err != nil {
		return nil, "", fmt.Errorf("inspect kit thread HOME volume: %w", err)
	}
	if err := k.checkLabels(r, labels, threadID); err != nil {
		return nil, "", err
	}

	info, err := k.engine.InspectKit(ctx, name)
	created := errors.Is(err, errLocalContainerNotFound)
	if err != nil && !created {
		return nil, "", fmt.Errorf("inspect kit thread container: %w", err)
	}
	if created {
		spec := kitLocalContainerSpec{
			Name:                  name,
			Image:                 k.config.Image,
			VolumeName:            volume,
			Labels:                k.threadLabels(r, threadID),
			Env:                   k.config.Env,
			ExtraCACertFile:       k.config.ExtraCACertFile,
			WorkloadAssertionFile: k.config.WorkloadAssertionFile,
			Network:               k.config.Network,
		}
		if _, err := k.engine.CreateKit(ctx, spec); err != nil {
			return nil, "", fmt.Errorf("create kit thread container: %w", err)
		}
		info, err = k.engine.InspectKit(ctx, name)
		if err != nil {
			return nil, "", fmt.Errorf("inspect created kit thread container: %w", err)
		}
	}
	if err := k.checkThreadContainer(r, threadID, info); err != nil {
		return nil, "", err
	}
	if info.ImageID != imageID {
		return nil, "", fmt.Errorf("kit local image changed; retire authority and use a new runtime generation")
	}

	if !info.Running {
		startedID = info.ID
		if err := k.engine.Start(ctx, info.ID); err != nil {
			return nil, "", fmt.Errorf("start kit thread container: %w", err)
		}
		info, err = k.engine.InspectKit(ctx, name)
		if err != nil {
			return nil, "", fmt.Errorf("rediscover kit thread endpoint: %w", err)
		}
		if err := k.checkThreadContainer(r, threadID, info); err != nil {
			return nil, "", err
		}
	}
	if !info.Running || info.InitHostPort < 1 || info.InitHostPort > 65535 || info.HostPort < 1 || info.HostPort > 65535 {
		return nil, "", fmt.Errorf("%w: kit local container has no running loopback endpoints", ErrRuntimeUnhealthy)
	}

	initBase := "http://127.0.0.1:" + strconv.Itoa(info.InitHostPort)
	if err := waitKitLocalHelper(ctx, k.config.HelperClient, initBase+"/readyz", k.config.HelperPollInterval); err != nil {
		return nil, "", fmt.Errorf("wait for kit local init helper: %w", err)
	}
	started, err := kitLocalHelperStarted(ctx, k.config.HelperClient, initBase+"/status")
	if err != nil {
		return nil, "", err
	}
	if !started {
		bootstrap, err := k.config.Bootstrap(ctx, r, threadID, m.Workload)
		if err != nil {
			return nil, "", fmt.Errorf("build kit local bootstrap: %w", err)
		}
		payload, err := kitLocalBootstrapPayload(bootstrap, k.config.Env)
		if err != nil {
			return nil, "", err
		}
		code, err := kitATEPost(ctx, k.config.HelperClient, initBase+"/bootstrap", payload)
		if err != nil {
			return nil, "", fmt.Errorf("post kit local bootstrap: %w", err)
		}
		if code != http.StatusNoContent && code != http.StatusConflict {
			return nil, "", fmt.Errorf("kit local bootstrap rejected with status %d", code)
		}
	}
	if err := waitKitLocalHelper(ctx, k.config.HelperClient, initBase+"/app-readyz", k.config.HelperPollInterval); err != nil {
		return nil, "", fmt.Errorf("wait for kit local ACP server: %w", err)
	}

	token, err = k.config.Token(ctx, r, threadID)
	if err != nil {
		return nil, "", fmt.Errorf("derive kit local ACP token: %w", err)
	}
	k.lastActive[kitLocalThreadKey{runtimeID: r.ID, threadID: threadID}] = k.config.Now()
	return kitLocalEndpoint(info.HostPort), token, nil
}

func waitKitLocalHelper(ctx context.Context, client *guardian.HTTPClient, endpoint string, pollInterval time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, kitLocalHelperReadyTimeout)
	defer cancel()

	for {
		req, err := http.NewRequestWithContext(waitCtx, http.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("build local Kit helper request: %w", err)
		}
		res, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxATEHelperErrorBodyBytes))
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return validateATEInitVersionHeader(res)
			}
			if res.StatusCode != http.StatusServiceUnavailable {
				return fmt.Errorf("local Kit helper returned status %d", res.StatusCode)
			}
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			if err != nil {
				return errors.Join(waitCtx.Err(), err)
			}
			return fmt.Errorf("local Kit helper readiness timed out: %w", waitCtx.Err())
		case <-timer.C:
		}
	}
}

func kitLocalHelperStarted(ctx context.Context, client *guardian.HTTPClient, endpoint string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("build local Kit status request: %w", err)
	}
	res, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("query local Kit init status: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return res.Body.Close() })
	if err := validateATEInitVersionHeader(res); err != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxATEHelperErrorBodyBytes))
		return false, err
	}
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxATEHelperErrorBodyBytes))
		return false, fmt.Errorf("local Kit status returned %d", res.StatusCode)
	}
	var payload struct {
		Started bool `json:"started"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxATEHelperErrorBodyBytes)).Decode(&payload); err != nil {
		return false, fmt.Errorf("decode local Kit status: %w", err)
	}
	return payload.Started, nil
}

func validateKitLocalBootstrap(b KitLocalBootstrap) error {
	if b.EgressInjectedAuth {
		return fmt.Errorf("kit local does not support egress-injected auth")
	}
	if b.AssertionFile != KitWorkloadAssertionPath {
		return fmt.Errorf("kit local assertion path must match the external read-only projection")
	}
	return validateKitBootstrap(b)
}

func (k *KitLocalRuntimeBackend) touchThread(runtimeID, threadID uuid.UUID) {
	k.mu.Lock()
	k.lastActive[kitLocalThreadKey{runtimeID: runtimeID, threadID: threadID}] = k.config.Now()
	k.mu.Unlock()
}

func (k *KitLocalRuntimeBackend) hasTrackedThreads(runtimeID uuid.UUID) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	for key := range k.lastActive {
		if key.runtimeID == runtimeID {
			return true
		}
	}
	return false
}

func (k *KitLocalRuntimeBackend) RunTurn(ctx context.Context, r assistantRuntimeRecord, turn runTurnRequest) error {
	endpoint, token, err := k.prepareThread(ctx, r, turn.ThreadID)
	if err != nil {
		return err
	}
	p, err := k.config.Protocol(ctx, r, turn.ThreadID, endpoint, token)
	if err != nil {
		return fmt.Errorf("resolve kit local ACP protocol: %w", err)
	}
	if p == nil {
		return fmt.Errorf("kit local ACP protocol is unavailable")
	}
	runErr := p.RunTurn(ctx, r, turn)
	k.touchThread(r.ID, turn.ThreadID)
	if runErr != nil {
		return fmt.Errorf("run kit turn: %w", runErr)
	}
	return nil
}

func (k *KitLocalRuntimeBackend) InterruptTurn(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID) (bool, error) {
	if threadID == uuid.Nil {
		return false, ErrKitRuntimeBinding
	}
	if _, _, err := k.parseMetadata(r); err != nil {
		return false, err
	}
	name, _ := kitLocalThreadNames(r, threadID)
	info, err := k.engine.InspectKit(ctx, name)
	if errors.Is(err, errLocalContainerNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect kit local container for interrupt: %w", err)
	}
	if err := k.checkThreadContainer(r, threadID, info); err != nil {
		return false, err
	}
	if !info.Running || info.HostPort < 1 || info.HostPort > 65535 {
		return false, nil
	}
	token, err := k.config.Token(ctx, r, threadID)
	if err != nil {
		return false, err
	}
	p, err := k.config.Protocol(ctx, r, threadID, kitLocalEndpoint(info.HostPort), token)
	if err != nil {
		return false, fmt.Errorf("resolve kit local interrupt protocol: %w", err)
	}
	interrupted, err := p.InterruptTurn(ctx, r, threadID)
	if err != nil {
		return false, fmt.Errorf("interrupt kit turn: %w", err)
	}
	return interrupted, nil
}

func (k *KitLocalRuntimeBackend) Status(_ context.Context, r assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	_, _, err := k.parseMetadata(r)
	return RuntimeBackendStatus{Configured: err == nil, IdleSeconds: nil}, err
}

func (k *KitLocalRuntimeBackend) RecycleImage(_ context.Context, r assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
	if _, _, err := k.parseMetadata(r); err != nil {
		return RuntimeBackendRecycleResult{}, err
	}
	return RuntimeBackendRecycleResult{Recycled: false, BackendMetadataJSON: nil}, nil
}

type kitLocalOwnedContainer struct {
	threadID uuid.UUID
	info     kitLocalContainerInfo
}

func (k *KitLocalRuntimeBackend) ownedThreadContainers(ctx context.Context, r assistantRuntimeRecord) ([]kitLocalOwnedContainer, error) {
	if _, _, err := k.parseMetadata(r); err != nil {
		return nil, err
	}
	names, err := k.engine.ListKitContainers(ctx, r.ID.String())
	if err != nil {
		return nil, fmt.Errorf("list kit local containers: %w", err)
	}
	out := make([]kitLocalOwnedContainer, 0, len(names))
	for _, name := range names {
		info, err := k.engine.InspectKit(ctx, name)
		if errors.Is(err, errLocalContainerNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect owned kit thread container: %w", err)
		}
		threadID, err := uuid.Parse(info.Labels["gram.ai/thread-id"])
		if err != nil || threadID == uuid.Nil {
			return nil, fmt.Errorf("kit local container has invalid thread label")
		}
		if err := k.checkThreadContainer(r, threadID, info); err != nil {
			return nil, err
		}
		out = append(out, kitLocalOwnedContainer{threadID: threadID, info: info})
	}
	return out, nil
}

func (k *KitLocalRuntimeBackend) closeRunningThreadSession(ctx context.Context, r assistantRuntimeRecord, entry kitLocalOwnedContainer) error {
	if !entry.info.Running || entry.info.HostPort < 1 || entry.info.HostPort > 65535 {
		return nil
	}
	token, err := k.config.Token(ctx, r, entry.threadID)
	if err != nil {
		return err
	}
	protocol, err := k.config.Protocol(ctx, r, entry.threadID, kitLocalEndpoint(entry.info.HostPort), token)
	if err != nil {
		return fmt.Errorf("resolve kit local close protocol: %w", err)
	}
	closer, ok := protocol.(interface {
		CloseNativeThreadSession(context.Context, assistantRuntimeRecord, uuid.UUID) error
	})
	if !ok {
		return nil
	}
	if err := closer.CloseNativeThreadSession(ctx, r, entry.threadID); err != nil {
		return fmt.Errorf("close native kit thread session: %w", err)
	}
	return nil
}

func (k *KitLocalRuntimeBackend) CheckpointIdleRuntime(ctx context.Context, r assistantRuntimeRecord) error {
	return k.Stop(ctx, r)
}

// Stop stops all running thread containers for r. When r is retired (switch or
// teardown), workload admission is also revoked; for non-retired idle stops,
// containers, volumes, and workload admission stay intact so the next turn on
// any thread simply starts its stopped container.
func (k *KitLocalRuntimeBackend) Stop(ctx context.Context, r assistantRuntimeRecord) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	containers, err := k.ownedThreadContainers(ctx, r)
	if err != nil {
		return err
	}
	for _, entry := range containers {
		if !entry.info.Running {
			continue
		}
		if !r.Retired && r.LifecycleTx != nil {
			if err := k.closeRunningThreadSession(ctx, r, entry); err != nil {
				return fmt.Errorf("close local kit thread session before stop: %w", err)
			}
		}
		if err := k.engine.Stop(ctx, entry.info.ID); err != nil {
			return fmt.Errorf("stop kit local thread container: %w", err)
		}
		delete(k.lastActive, kitLocalThreadKey{runtimeID: r.ID, threadID: entry.threadID})
	}
	if r.Retired && k.config.Fence != nil {
		if err := k.config.Fence(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

// StopIdleThreadContainers stops individual running local thread containers
// that have been idle past idleAfter and have no unsettled events, keeping
// local multi-thread resource use low even while sibling threads stay active.
func (k *KitLocalRuntimeBackend) StopIdleThreadContainers(
	ctx context.Context,
	r assistantRuntimeRecord,
	now time.Time,
	idleAfter time.Duration,
	checkThreadSettled func(context.Context, assistantRuntimeRecord, uuid.UUID) error,
) (int64, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	containers, err := k.ownedThreadContainers(ctx, r)
	if err != nil {
		return 0, err
	}
	var stopped int64
	for _, entry := range containers {
		if !entry.info.Running {
			continue
		}
		last, seen := k.lastActive[kitLocalThreadKey{runtimeID: r.ID, threadID: entry.threadID}]
		if seen && now.Sub(last) < idleAfter {
			continue
		}
		if checkThreadSettled != nil {
			if err := checkThreadSettled(ctx, r, entry.threadID); err != nil {
				continue
			}
		}
		if r.LifecycleTx != nil {
			if err := k.closeRunningThreadSession(ctx, r, entry); err != nil {
				return stopped, fmt.Errorf("close idle local kit thread session: %w", err)
			}
		}
		if err := k.engine.Stop(ctx, entry.info.ID); err != nil {
			return stopped, fmt.Errorf("stop idle kit local container: %w", err)
		}
		delete(k.lastActive, kitLocalThreadKey{runtimeID: r.ID, threadID: entry.threadID})
		stopped++
	}
	return stopped, nil
}

func (k *KitLocalRuntimeBackend) RuntimeExists(ctx context.Context, r assistantRuntimeRecord) (bool, error) {
	containers, err := k.ownedThreadContainers(ctx, r)
	if err != nil {
		return false, err
	}
	if len(containers) == 0 {
		return !r.Retired, nil
	}
	return true, nil
}

func (k *KitLocalRuntimeBackend) Reap(ctx context.Context, r assistantRuntimeRecord) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.reap(ctx, r)
}

func (k *KitLocalRuntimeBackend) reap(ctx context.Context, r assistantRuntimeRecord) error {
	_, _, metaErr := k.parseMetadata(r)
	missingImageID := errors.Is(metaErr, errKitRuntimeUnrecoverable)
	if metaErr != nil && !missingImageID {
		return metaErr
	}
	var containers []kitLocalOwnedContainer
	var lineageContainerIDs []string
	if missingImageID {
		names, err := k.engine.ListKitContainers(ctx, r.ID.String())
		if err != nil {
			return fmt.Errorf("list kit local containers: %w", err)
		}
		for _, name := range names {
			info, err := k.engine.InspectKit(ctx, name)
			if errors.Is(err, errLocalContainerNotFound) {
				continue
			}
			if err != nil {
				return fmt.Errorf("inspect kit local container: %w", err)
			}
			if info.Labels["gram.ai/assistant-id"] != r.AssistantID.String() || info.Labels["gram.ai/project-id"] != r.ProjectID.String() || info.Labels["gram.ai/runtime-lineage"] != r.ID.String() {
				return fmt.Errorf("kit local container ownership mismatch")
			}
			lineageContainerIDs = append(lineageContainerIDs, info.ID)
		}
	} else {
		var err error
		containers, err = k.ownedThreadContainers(ctx, r)
		if err != nil {
			return err
		}
	}
	volumes, err := k.engine.ListKitVolumes(ctx, r.ID.String())
	if err != nil {
		return fmt.Errorf("list kit local volumes: %w", err)
	}
	for _, volume := range volumes {
		labels, err := k.engine.InspectKitVolume(ctx, volume)
		if errors.Is(err, errLocalContainerNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect kit HOME volume before reap: %w", err)
		}
		if missingImageID {
			if labels["gram.ai/assistant-id"] != r.AssistantID.String() || labels["gram.ai/project-id"] != r.ProjectID.String() || labels["gram.ai/runtime-lineage"] != r.ID.String() {
				return fmt.Errorf("kit local volume ownership mismatch")
			}
		} else if err := k.checkLabels(r, labels, uuid.Nil); err != nil {
			return err
		}
	}
	if k.config.Fence != nil {
		if err := k.config.Fence(ctx, r); err != nil {
			return err
		}
	}
	for _, id := range lineageContainerIDs {
		if err := k.engine.Remove(ctx, id); err != nil {
			return fmt.Errorf("remove kit local container: %w", err)
		}
	}
	for _, entry := range containers {
		if err := k.engine.Remove(ctx, entry.info.ID); err != nil {
			return fmt.Errorf("remove kit thread container: %w", err)
		}
		delete(k.lastActive, kitLocalThreadKey{runtimeID: r.ID, threadID: entry.threadID})
	}
	for _, volume := range volumes {
		if err := k.engine.RemoveVolume(ctx, volume); err != nil {
			return fmt.Errorf("remove kit HOME volume: %w", err)
		}
	}
	return nil
}

func (k *KitLocalRuntimeBackend) ReapStoppedMachine(ctx context.Context, r assistantRuntimeRecord) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	containers, err := k.ownedThreadContainers(ctx, r)
	if err != nil {
		return err
	}
	for _, entry := range containers {
		if entry.info.Running {
			return fmt.Errorf("refusing to reap a running kit local runtime")
		}
	}
	return k.reap(ctx, r)
}

func kitLocalEndpoint(port int) *url.URL {
	return &url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(port), Path: "/acp"}
}
