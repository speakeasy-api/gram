package assistants

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// KitLocalBootstrap uses the verified Kit filesystem configuration contract. Secrets travel only through an in-memory archive into a stopped guest.
type KitLocalBootstrap struct {
	Model                                          string
	AgentsMD, InferenceJSON, MCPJSON, GatewayToken []byte
	MCPFiles                                       map[string][]byte
}

// KitLocalRuntimeConfig is local-only. Command, Args and Env must not contain
// credentials; credential bytes belong exclusively in Bootstrap. The image
// contract must provide /home/kit owned by UID/GID 1000 so Docker's initial
// named-volume copy preserves writable HOME without a privileged init process.
// A named HOME persists files only; it does not restore process/session memory.
type KitLocalRuntimeConfig struct {
	Environment             string
	Image                   string
	Command, Args           []string
	Env                     map[string]string
	GuestPort               int
	ServerURL               *url.URL
	ExtraCACertFile         string
	Network                 string
	CompatibilityGeneration string
	HomeOwnedByRuntimeUser  bool
	// Bootstrap runs before every stopped-container start. Use PrepareKitStartupBootstrap
	// to revoke/remint inference authority before provisioning; never return snapshot secrets.
	Bootstrap func(context.Context, assistantRuntimeRecord) (KitLocalBootstrap, error)
	Protocol  func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error)
	// Ready must perform authenticated ACP initialization, not container liveness.
	Ready func(context.Context, assistantRuntimeRecord, *url.URL) error
}

type kitLocalEngine interface {
	ImageID(context.Context, string) (string, error)
	InspectKit(context.Context, string) (kitLocalContainerInfo, error)
	InspectKitVolume(context.Context, string) (map[string]string, error)
	CreateKitVolume(context.Context, string, map[string]string) error
	CreateKit(context.Context, kitLocalContainerSpec) (string, error)
	CopyKitBootstrap(context.Context, string, KitLocalBootstrap) error
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Remove(context.Context, string) error
	RemoveVolume(context.Context, string) error
}

type kitLocalContainerInfo struct {
	ID, ImageID, User, HomeVolume string
	Running                       bool
	HostPort                      int
	Labels                        map[string]string
}
type kitLocalContainerSpec struct {
	Name, Image, VolumeName string
	Command, Args           []string
	Labels, Env             map[string]string
	ExtraCACertFile         string
	Network                 string
}

type kitLocalRuntimeMetadata struct {
	RuntimeVersion int16     `json:"runtime_version"`
	RuntimeID      uuid.UUID `json:"runtime_id"`
	AssistantID    uuid.UUID `json:"assistant_id"`
	Generation     string    `json:"generation"`
	ContainerID    string    `json:"container_id"`
	ContainerName  string    `json:"container_name"`
	HomeVolume     string    `json:"home_volume"`
	HostPort       int       `json:"host_port"`
}

// KitLocalRuntimeBackend owns infrastructure only; the injected ACP adapter owns
// the guest protocol. There are no legacy runner health/state/bootstrap requests.
// The caller must retain its shared admission/fencing lock across lifecycle and
// protocol operations. This mutex only serializes local lifecycle calls.
type KitLocalRuntimeBackend struct {
	config   KitLocalRuntimeConfig
	engine   kitLocalEngine
	mu       sync.Mutex
	specHash string
}

var _ RuntimeBackend = (*KitLocalRuntimeBackend)(nil)

func NewKitLocalRuntimeBackend(config KitLocalRuntimeConfig) (*KitLocalRuntimeBackend, error) {
	return newKitLocalRuntimeBackend(config, newDockerCLIEngine(config.GuestPort))
}
func newKitLocalRuntimeBackend(config KitLocalRuntimeConfig, engine kitLocalEngine) (*KitLocalRuntimeBackend, error) {
	if config.Environment != "local" || config.Image == "" || len(config.Command) == 0 || config.Command[0] == "" || config.Args == nil || config.GuestPort < 1 || config.GuestPort > 65535 || config.ServerURL == nil || config.ServerURL.Hostname() == "" || config.CompatibilityGeneration == "" || !config.HomeOwnedByRuntimeUser || config.Bootstrap == nil || config.Protocol == nil || config.Ready == nil || engine == nil {
		return nil, fmt.Errorf("kit local requires local environment, image/command/args/port, server URL, generation, UID1000 HOME image contract, bootstrap, ACP protocol and authenticated readiness")
	}
	if value, ok := config.Env["HOME"]; ok && value != "/home/kit" {
		return nil, fmt.Errorf("kit local HOME must be /home/kit")
	}
	if err := validateKitGatewayCommand(config.Command, config.Args, config.GuestPort); err != nil {
		return nil, err
	}
	config.Command = slices.Clone(config.Command)
	config.Args = slices.Clone(config.Args)
	config.Env = maps.Clone(config.Env)
	if config.Env == nil {
		config.Env = make(map[string]string)
	}
	origin := strings.TrimRight(config.ServerURL.String(), "/")
	if supplied := config.Env["SPEAKEASY_ORIGIN"]; supplied != "" && supplied != origin {
		return nil, fmt.Errorf("kit inference origin must match server URL")
	}
	config.Env["SPEAKEASY_ORIGIN"] = origin
	if config.ServerURL.Scheme == "http" {
		config.Env["SPEAKEASY_ALLOW_INSECURE_HTTP"] = "1"
	}
	serverURL := *config.ServerURL
	config.ServerURL = &serverURL
	encoded, err := json.Marshal(map[string]any{"image": config.Image, "command": config.Command, "args": config.Args, "env": config.Env, "port": config.GuestPort, "ca": config.ExtraCACertFile, "network": config.Network})
	if err != nil {
		return nil, fmt.Errorf("encode kit local immutable spec: %w", err)
	}
	return &KitLocalRuntimeBackend{config: config, engine: engine, mu: sync.Mutex{}, specHash: fmt.Sprintf("%x", sha256.Sum256(encoded))}, nil
}
func (k *KitLocalRuntimeBackend) Backend() string { return runtimeBackendLocal }
func (k *KitLocalRuntimeBackend) SupportsBackend(backend string) bool {
	return backend == runtimeBackendLocal
}
func (k *KitLocalRuntimeBackend) ServerURL() *url.URL {
	serverURL := *k.config.ServerURL
	return &serverURL
}
func (k *KitLocalRuntimeBackend) ImageRef() string { return k.config.Image }
func kitLocalNames(r assistantRuntimeRecord) (string, string) {
	stem := "gram-kit-" + r.AssistantID.String() + "-" + r.ID.String()
	return stem, stem + "-home"
}
func (k *KitLocalRuntimeBackend) labels(r assistantRuntimeRecord) map[string]string {
	return map[string]string{"gram.ai/assistant-id": r.AssistantID.String(), "gram.ai/project-id": r.ProjectID.String(), "gram.ai/runtime-version": "3", "gram.ai/runtime-lineage": r.ID.String(), "gram.ai/compatibility-generation": k.config.CompatibilityGeneration, "gram.ai/local-spec": k.specHash}
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

func (k *KitLocalRuntimeBackend) validate(r assistantRuntimeRecord) error {
	if r.RuntimeVersion != 3 || r.Backend != runtimeBackendLocal || r.ID == uuid.Nil || r.AssistantID == uuid.Nil || r.ProjectID == uuid.Nil {
		return fmt.Errorf("kit local requires an immutable version 3 local runtime and assistant/project identity")
	}
	if kitLocalUnprovisionedMetadata(r.BackendMetadataJSON) {
		return nil
	}
	var m kitLocalRuntimeMetadata
	if err := json.Unmarshal(r.BackendMetadataJSON, &m); err != nil {
		return fmt.Errorf("decode kit local metadata: %w", err)
	}
	name, volume := kitLocalNames(r)
	if m.RuntimeVersion != 3 || m.RuntimeID != r.ID || m.AssistantID != r.AssistantID || m.Generation != k.config.CompatibilityGeneration || m.ContainerName != name || m.HomeVolume != volume || m.ContainerID == "" {
		return fmt.Errorf("kit local saved metadata does not match runtime authority")
	}
	return nil
}
func (k *KitLocalRuntimeBackend) checkLabels(r assistantRuntimeRecord, labels map[string]string) error {
	for key, value := range k.labels(r) {
		if labels[key] != value {
			return fmt.Errorf("kit local ownership mismatch: %s", key)
		}
	}
	return nil
}
func (k *KitLocalRuntimeBackend) checkContainer(r assistantRuntimeRecord, info kitLocalContainerInfo) error {
	if err := k.checkLabels(r, info.Labels); err != nil {
		return err
	}
	_, volume := kitLocalNames(r)
	if info.ID == "" || info.User != "1000:1000" || info.HomeVolume != volume {
		return fmt.Errorf("kit local container has invalid identity, user or HOME mount")
	}
	if !kitLocalUnprovisionedMetadata(r.BackendMetadataJSON) {
		var m kitLocalRuntimeMetadata
		if err := json.Unmarshal(r.BackendMetadataJSON, &m); err != nil {
			return fmt.Errorf("decode kit local metadata: %w", err)
		}
		if m.ContainerID != info.ID {
			return fmt.Errorf("kit local container was replaced outside runtime admission")
		}
	}
	return nil
}
func (k *KitLocalRuntimeBackend) metadata(r assistantRuntimeRecord, info kitLocalContainerInfo) ([]byte, error) {
	name, volume := kitLocalNames(r)
	b, err := json.Marshal(kitLocalRuntimeMetadata{RuntimeVersion: 3, RuntimeID: r.ID, AssistantID: r.AssistantID, Generation: k.config.CompatibilityGeneration, ContainerID: info.ID, ContainerName: name, HomeVolume: volume, HostPort: info.HostPort})
	if err != nil {
		return nil, fmt.Errorf("encode kit local metadata: %w", err)
	}
	return b, nil
}

func (k *KitLocalRuntimeBackend) Ensure(ctx context.Context, r assistantRuntimeRecord) (result RuntimeBackendEnsureResult, resultErr error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	startedID := ""
	defer func() {
		if resultErr == nil || startedID == "" {
			return
		}
		// Cancellation must not prevent stopping the exact incarnation started
		// by this attempt. The janitor only reaps stopped containers.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := k.engine.Stop(cleanup, startedID); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("stop unready kit gateway: %w", err))
		}
	}()
	if err := k.validate(r); err != nil {
		return result, err
	}
	name, volume := kitLocalNames(r)
	imageID, err := k.engine.ImageID(ctx, k.config.Image)
	if err != nil {
		return result, fmt.Errorf("resolve kit local image: %w", err)
	}
	labels, err := k.engine.InspectKitVolume(ctx, volume)
	if errors.Is(err, errLocalContainerNotFound) {
		if err := k.engine.CreateKitVolume(ctx, volume, k.labels(r)); err != nil {
			return result, fmt.Errorf("create kit HOME volume: %w", err)
		}
		labels, err = k.engine.InspectKitVolume(ctx, volume)
	}
	if err != nil {
		return result, fmt.Errorf("inspect kit HOME volume: %w", err)
	}
	if err := k.checkLabels(r, labels); err != nil {
		return result, err
	}
	info, err := k.engine.InspectKit(ctx, name)
	created := errors.Is(err, errLocalContainerNotFound)
	if err != nil && !created {
		return result, fmt.Errorf("inspect kit local container: %w", err)
	}
	if created {
		spec := kitLocalContainerSpec{Name: name, Image: k.config.Image, VolumeName: volume, Command: k.config.Command, Args: k.config.Args, Labels: k.labels(r), Env: k.config.Env, ExtraCACertFile: k.config.ExtraCACertFile, Network: k.config.Network}
		if _, err := k.engine.CreateKit(ctx, spec); err != nil {
			return result, fmt.Errorf("create kit local container: %w", err)
		}
		info, err = k.engine.InspectKit(ctx, name)
		if err != nil {
			return result, fmt.Errorf("inspect created kit container: %w", err)
		}
		// A new container replaces a missing incarnation; only Ensure can persist it.
		r.BackendMetadataJSON = nil
	}
	if err := k.checkContainer(r, info); err != nil {
		return result, err
	}
	if info.ImageID != imageID {
		return result, fmt.Errorf("kit local image changed; retire authority and use a new runtime generation")
	}
	if !info.Running {
		bootstrap, err := k.config.Bootstrap(ctx, r)
		if err != nil {
			return result, fmt.Errorf("build kit bootstrap: %w", err)
		}
		if origin, err := kitInferenceOrigin(bootstrap.InferenceJSON); err != nil || origin != k.config.Env["SPEAKEASY_ORIGIN"] {
			return result, fmt.Errorf("kit bootstrap inference origin does not match configured origin")
		}
		if err := validateKitLocalBootstrap(bootstrap); err != nil {
			return result, err
		}
		if err := k.engine.CopyKitBootstrap(ctx, info.ID, bootstrap); err != nil {
			return result, fmt.Errorf("inject kit bootstrap: %w", err)
		}
		startedID = info.ID
		if err := k.engine.Start(ctx, info.ID); err != nil {
			return result, fmt.Errorf("start kit gateway: %w", err)
		}
	}
	info, err = k.engine.InspectKit(ctx, name)
	if err != nil {
		return result, fmt.Errorf("rediscover kit endpoint: %w", err)
	}
	if err := k.checkContainer(r, info); err != nil {
		return result, err
	}
	if !info.Running || info.HostPort < 1 || info.HostPort > 65535 {
		return result, fmt.Errorf("%w: kit gateway has no running loopback endpoint", ErrRuntimeUnhealthy)
	}
	payload, err := k.metadata(r, info)
	if err != nil {
		return result, err
	}
	r.BackendMetadataJSON = payload
	if err := k.config.Ready(ctx, r, kitLocalEndpoint(info.HostPort)); err != nil {
		return result, fmt.Errorf("authenticated kit readiness: %w", err)
	}
	return RuntimeBackendEnsureResult{ColdStart: created, BackendMetadataJSON: payload}, nil
}
func validateKitLocalBootstrap(b KitLocalBootstrap) error { return validateKitBootstrap(b) }

func (k *KitLocalRuntimeBackend) endpoint(ctx context.Context, r assistantRuntimeRecord) (*url.URL, error) {
	if err := k.validate(r); err != nil {
		return nil, err
	}
	name, volume := kitLocalNames(r)
	labels, err := k.engine.InspectKitVolume(ctx, volume)
	if err != nil {
		return nil, fmt.Errorf("inspect kit HOME: %w", err)
	}
	if err := k.checkLabels(r, labels); err != nil {
		return nil, err
	}
	info, err := k.engine.InspectKit(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("inspect kit endpoint: %w", err)
	}
	if err := k.checkContainer(r, info); err != nil {
		return nil, err
	}
	if !info.Running || info.HostPort < 1 || info.HostPort > 65535 {
		return nil, fmt.Errorf("%w: kit gateway is stopped", ErrRuntimeUnhealthy)
	}
	return kitLocalEndpoint(info.HostPort), nil
}
func (k *KitLocalRuntimeBackend) protocol(ctx context.Context, r assistantRuntimeRecord) (runtimeProtocol, error) {
	endpoint, err := k.endpoint(ctx, r)
	if err != nil {
		return nil, err
	}
	protocol, err := k.config.Protocol(ctx, r, endpoint)
	if err != nil {
		return nil, fmt.Errorf("resolve kit ACP protocol: %w", err)
	}
	if protocol == nil {
		return nil, fmt.Errorf("kit ACP protocol is unavailable")
	}
	return protocol, nil
}
func (k *KitLocalRuntimeBackend) RunTurn(ctx context.Context, r assistantRuntimeRecord, turn runTurnRequest) error {
	p, err := k.protocol(ctx, r)
	if err != nil {
		return err
	}
	if err := p.RunTurn(ctx, r, turn); err != nil {
		return fmt.Errorf("run kit turn: %w", err)
	}
	return nil
}
func (k *KitLocalRuntimeBackend) InterruptTurn(ctx context.Context, r assistantRuntimeRecord, thread uuid.UUID) (bool, error) {
	p, err := k.protocol(ctx, r)
	if err != nil {
		return false, err
	}
	interrupted, err := p.InterruptTurn(ctx, r, thread)
	if err != nil {
		return false, fmt.Errorf("interrupt kit turn: %w", err)
	}
	return interrupted, nil
}
func (k *KitLocalRuntimeBackend) Status(ctx context.Context, r assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	p, err := k.protocol(ctx, r)
	if err != nil {
		return RuntimeBackendStatus{}, err
	}
	status, err := p.Status(ctx, r)
	if err != nil {
		return RuntimeBackendStatus{}, fmt.Errorf("get kit status: %w", err)
	}
	return status, nil
}
func (k *KitLocalRuntimeBackend) RecycleImage(ctx context.Context, r assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
	if err := k.validate(r); err != nil {
		return RuntimeBackendRecycleResult{}, err
	}
	// No settled-work/authority-retirement contract is available here. Never
	// silently replace a Kit process or claim that persisted files are a snapshot.
	return RuntimeBackendRecycleResult{Recycled: false, BackendMetadataJSON: nil}, nil
}
func (k *KitLocalRuntimeBackend) Stop(ctx context.Context, r assistantRuntimeRecord) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	info, exists, err := k.ownedContainer(ctx, r)
	if err != nil || !exists {
		return err
	}
	if err := k.engine.Stop(ctx, info.ID); err != nil {
		return fmt.Errorf("stop kit local: %w", err)
	}
	return nil
}
func (k *KitLocalRuntimeBackend) ownedContainer(ctx context.Context, r assistantRuntimeRecord) (kitLocalContainerInfo, bool, error) {
	var empty kitLocalContainerInfo
	if err := k.validate(r); err != nil {
		return empty, false, err
	}
	name, _ := kitLocalNames(r)
	info, err := k.engine.InspectKit(ctx, name)
	if errors.Is(err, errLocalContainerNotFound) {
		return empty, false, nil
	}
	if err != nil {
		return empty, false, fmt.Errorf("inspect kit local: %w", err)
	}
	if err := k.checkContainer(r, info); err != nil {
		return empty, false, err
	}
	_, volume := kitLocalNames(r)
	labels, err := k.engine.InspectKitVolume(ctx, volume)
	if err != nil {
		return empty, false, fmt.Errorf("inspect owned kit HOME: %w", err)
	}
	if err := k.checkLabels(r, labels); err != nil {
		return empty, false, err
	}
	return info, true, nil
}
func (k *KitLocalRuntimeBackend) RuntimeExists(ctx context.Context, r assistantRuntimeRecord) (bool, error) {
	_, exists, err := k.ownedContainer(ctx, r)
	return exists, err
}
func (k *KitLocalRuntimeBackend) Reap(ctx context.Context, r assistantRuntimeRecord) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.reap(ctx, r)
}
func (k *KitLocalRuntimeBackend) reap(ctx context.Context, r assistantRuntimeRecord) error {
	info, exists, err := k.ownedContainer(ctx, r)
	if err != nil {
		return err
	}
	_, volume := kitLocalNames(r)
	labels, err := k.engine.InspectKitVolume(ctx, volume)
	volumeExists := !errors.Is(err, errLocalContainerNotFound)
	if err != nil && volumeExists {
		return fmt.Errorf("inspect kit HOME before reap: %w", err)
	}
	if volumeExists {
		if err := k.checkLabels(r, labels); err != nil {
			return err
		}
	}
	if exists {
		if err := k.engine.Remove(ctx, info.ID); err != nil {
			return fmt.Errorf("remove kit container: %w", err)
		}
	}
	if volumeExists {
		if err := k.engine.RemoveVolume(ctx, volume); err != nil {
			return fmt.Errorf("remove kit HOME: %w", err)
		}
	}
	return nil
}
func (k *KitLocalRuntimeBackend) ReapStoppedMachine(ctx context.Context, r assistantRuntimeRecord) error {
	// Hold local lifecycle exclusion through inspection and deletion as well as
	// the caller's shared admission lock.
	k.mu.Lock()
	defer k.mu.Unlock()
	info, exists, err := k.ownedContainer(ctx, r)
	if err != nil {
		return err
	}
	if exists && info.Running {
		return fmt.Errorf("refusing to reap a running kit local runtime")
	}
	return k.reap(ctx, r)
}

func kitLocalEndpoint(port int) *url.URL {
	return &url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(port), Path: "/acp/v2"}
}
