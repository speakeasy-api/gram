package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const runtimeBackendLocal = "local"

// LocalRuntimeHostGatewayAlias is the hostname containers reach the host on.
// Docker Desktop resolves it natively; on native Linux engines the backend
// adds a host-gateway alias for it on every container.
const LocalRuntimeHostGatewayAlias = "host.docker.internal"

const (
	// localRuntimeHealthTimeout bounds the runner /healthz wait after a
	// container (re)start. The image is already present on the local daemon,
	// so there is no pull cost — this only covers the runner boot.
	localRuntimeHealthTimeout   = 60 * time.Second
	localRuntimeHealthPollDelay = 250 * time.Millisecond
	localRuntimeTurnTimeout     = 30 * time.Minute

	// localRuntimeWorkdirMountPath is where the per-assistant workspace volume
	// mounts inside the container. It matches the runner init script's workdir:
	// the unprivileged container cannot mount tmpfs over it, so init falls back
	// to symlinking the sandbox deps into the (volume-backed) directory.
	localRuntimeWorkdirMountPath = "/var/lib/gram-assistant/work"
	// localRuntimeCACertMountPath is where the optional extra CA bundle is
	// bind-mounted read-only. The runner init script appends it to the system
	// trust store so the runner (and its sandbox children) trust the local
	// mkcert-issued server certificate.
	localRuntimeCACertMountPath = "/usr/local/share/gram/extra-ca.pem"
)

// localRuntimeLoopbackCIDRs is the narrowly scoped guardian allowlist for the
// local backend's runner client: containers publish the runner guest port on a
// loopback ephemeral port, which the default egress policy blocks as SSRF.
// Only this one client is relaxed; the policy's global enforcement is unchanged.
var localRuntimeLoopbackCIDRs = []string{"127.0.0.0/8", "::1/128"}

var (
	errLocalContainerNotFound  = errors.New("local runtime container not found")
	errLocalContainerNameInUse = errors.New("local runtime container name in use")
	errLocalImageNotFound      = errors.New("local runtime image not found")
)

// containerEngine is the narrow surface of a Docker-compatible engine the
// local backend needs. Production shells out to the docker CLI
// (dockerCLIEngine); tests use a fake.
type containerEngine interface {
	// ImageID resolves an image reference to its content-addressed ID.
	// Returns errLocalImageNotFound when the image is absent.
	ImageID(ctx context.Context, imageRef string) (string, error)
	// Inspect returns the current state of a named container. Returns
	// errLocalContainerNotFound when absent.
	Inspect(ctx context.Context, name string) (localContainerInfo, error)
	// Run creates and starts a container. Returns errLocalContainerNameInUse
	// when a container with the spec's name already exists.
	Run(ctx context.Context, spec localContainerSpec) (containerID string, err error)
	// Start starts a stopped container.
	Start(ctx context.Context, name string) error
	// Stop stops a running container. Idempotent: a missing or already
	// stopped container is success.
	Stop(ctx context.Context, name string) error
	// Remove force-removes a container (by name or ID) regardless of state.
	// Idempotent.
	Remove(ctx context.Context, nameOrID string) error
	// RemoveVolume removes a named volume. Idempotent.
	RemoveVolume(ctx context.Context, name string) error
}

type localContainerInfo struct {
	ID      string
	Running bool
	ImageID string
	// HostPort is the loopback port the runner guest port is published on.
	// Zero when the container is not running (ephemeral publishes are
	// re-assigned on every start).
	HostPort int
}

type localContainerSpec struct {
	Name   string
	Image  string
	Labels map[string]string
	Env    map[string]string
	// VolumeName is mounted at localRuntimeWorkdirMountPath.
	VolumeName string
	// ExtraCACertFile, when set, is a host path bind-mounted read-only at
	// localRuntimeCACertMountPath.
	ExtraCACertFile string
}

// LocalRuntimeConfig configures the local Docker-backed assistant runtime
// backend used for image development on a developer machine.
type LocalRuntimeConfig struct {
	// Enabled controls whether the backend is constructed. It is on whenever
	// the process runs in the local environment (so locally created rows stay
	// routable even when targeting another backend) or when local is the
	// target provider.
	Enabled bool
	// Environment must be "local": the backend launches containers on the
	// host's Docker daemon and dials them over loopback, which has no place in
	// a deployed process.
	Environment string
	OCIImage    string
	ImageTag    string
	GuestPort   int
	// ServerURL is the management API base as reachable from inside a
	// container — typically the --server-url with its host rewritten to
	// host.docker.internal.
	ServerURL *url.URL
	// ExtraCACertFile optionally points at a PEM CA bundle (typically the
	// mkcert root CA) mounted into containers so the runner trusts the local
	// server's TLS certificate.
	ExtraCACertFile string
}

func (c LocalRuntimeConfig) Validate() error {
	if c.Environment != "local" {
		return fmt.Errorf("assistant runtime provider %q is only supported when --environment=local; got %q", runtimeBackendLocal, c.Environment)
	}
	if c.OCIImage == "" {
		return fmt.Errorf("--assistant-runtime-oci-image is required")
	}
	if c.ServerURL == nil || c.ServerURL.Hostname() == "" {
		return fmt.Errorf("local assistant runtime requires a container-reachable --assistant-runtime-server-url or --server-url")
	}
	return nil
}

// localRuntimeMetadata is persisted opaquely in
// assistant_runtimes.backend_metadata_json for local-backed rows. HostPort is
// only valid for the container incarnation that Ensure observed: an
// out-of-band restart re-assigns the ephemeral publish, which surfaces as
// ErrRuntimeUnhealthy on the next turn and self-heals through re-admission.
type localRuntimeMetadata struct {
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
	HostPort      int    `json:"host_port"`
}

// LocalRuntimeBackend runs one Docker container per assistant on the
// developer's machine. Containers are named deterministically per assistant
// and labelled with assistant/project identity, so Ensure is idempotent and a
// restarted server rediscovers containers it launched before. The runner guest
// port is published to an ephemeral loopback port that is re-resolved on every
// (re)start.
type LocalRuntimeBackend struct {
	logger *slog.Logger
	tracer trace.Tracer
	config LocalRuntimeConfig
	engine containerEngine
	runner runnerClient
	// healthTimeout is localRuntimeHealthTimeout; tests shrink it.
	healthTimeout time.Duration
}

func NewLocalRuntimeBackend(logger *slog.Logger, tracerProvider trace.TracerProvider, httpClient runtimeHTTPDoer, engine containerEngine, config LocalRuntimeConfig) *LocalRuntimeBackend {
	if config.GuestPort == 0 {
		config.GuestPort = defaultRuntimeGuestPort
	}
	return &LocalRuntimeBackend{
		logger:        logger.With(attr.SlogComponent("assistants_local")),
		tracer:        tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/assistants"),
		config:        config,
		engine:        engine,
		runner:        runnerClient{do: httpClient, backend: runtimeBackendLocal},
		healthTimeout: localRuntimeHealthTimeout,
	}
}

func (l *LocalRuntimeBackend) Backend() string { return runtimeBackendLocal }

func (l *LocalRuntimeBackend) SupportsBackend(backend string) bool {
	return backend == runtimeBackendLocal
}

func (l *LocalRuntimeBackend) ServerURL() *url.URL { return l.config.ServerURL }

func (l *LocalRuntimeBackend) ImageRef() string { return l.desiredImageRef() }

// ReusesIdleRuntimes is true: Stop leaves the container and its workspace
// volume in place, and the next admission restarts the same container.
func (l *LocalRuntimeBackend) ReusesIdleRuntimes() bool { return true }

func (l *LocalRuntimeBackend) desiredImageRef() string {
	return runtimeImageRef(l.config.OCIImage, l.config.ImageTag)
}

func localContainerName(runtime assistantRuntimeRecord) string {
	return runtimeResourcePrefix + "-" + strings.ToLower(runtime.AssistantID.String())
}

func localVolumeName(runtime assistantRuntimeRecord) string {
	return runtimeResourcePrefix + "-work-" + strings.ToLower(runtime.AssistantID.String())
}

func localRuntimeEndpoint(hostPort int) string {
	return "http://127.0.0.1:" + strconv.Itoa(hostPort)
}

// resolveDesiredImageID resolves the configured image reference to its local
// image ID. Image rebuilds keep the same tag (locally always :dev) while the
// ID changes, so drift is detected by ID rather than by tag.
func (l *LocalRuntimeBackend) resolveDesiredImageID(ctx context.Context) (string, error) {
	imageID, err := l.engine.ImageID(ctx, l.desiredImageRef())
	if errors.Is(err, errLocalImageNotFound) {
		return "", fmt.Errorf("assistant runtime image %q is not available locally: build it with `mise run build:assistants-runtime-image`: %w", l.desiredImageRef(), err)
	}
	if err != nil {
		return "", fmt.Errorf("resolve local runtime image id: %w", err)
	}
	return imageID, nil
}

func (l *LocalRuntimeBackend) Ensure(ctx context.Context, runtime assistantRuntimeRecord) (result RuntimeBackendEnsureResult, err error) {
	if err := validateRuntimeBackend(l, runtime.Backend); err != nil {
		return RuntimeBackendEnsureResult{}, err
	}

	ctx, span := l.tracer.Start(ctx, "assistants.runtime.local.ensure")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	desiredImageID, err := l.resolveDesiredImageID(ctx)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}

	name := localContainerName(runtime)
	info, err := l.engine.Inspect(ctx, name)
	exists := true
	if errors.Is(err, errLocalContainerNotFound) {
		exists = false
	} else if err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("inspect local runtime container %s: %w", name, err)
	}

	// A rebuilt image changes the image ID under the same tag. Replace the
	// container when it is safe: a busy runner (a turn in flight) keeps its
	// current container and a later admission picks the new image up. The
	// workspace volume always survives replacement. Removal targets the
	// inspected incarnation's ID, not the name, so a racing Ensure can never
	// tear down a replacement it did not examine.
	if exists && info.ImageID != desiredImageID && !l.runnerBusy(ctx, info) {
		if err := l.engine.Remove(ctx, info.ID); err != nil {
			return RuntimeBackendEnsureResult{}, fmt.Errorf("remove drifted local runtime container %s: %w", name, err)
		}
		exists = false
	}

	if !exists {
		info = localContainerInfo{ID: "", Running: false, ImageID: "", HostPort: 0}
	}
	coldStart := !info.Running
	payload, err := l.startContainer(ctx, runtime, name, info, exists)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	return RuntimeBackendEnsureResult{ColdStart: coldStart, BackendMetadataJSON: payload}, nil
}

// runnerBusy reports whether the container's runner has a turn in flight. Any
// probe failure counts as busy so a replace never races an unreachable-but-
// live runner.
func (l *LocalRuntimeBackend) runnerBusy(ctx context.Context, info localContainerInfo) bool {
	if !info.Running {
		return false
	}
	if info.HostPort == 0 {
		return true
	}
	state, err := l.runner.state(ctx, localRuntimeEndpoint(info.HostPort))
	if err != nil {
		return true
	}
	idle := state.minThreadIdle()
	return idle != nil && *idle == 0
}

// startContainer converges the named container onto a running, healthy state
// and returns the encoded runtime metadata: it launches a fresh container when
// none exists (adopting one a racing Ensure created), restarts a stopped one,
// and waits for runner health. The caller passes its inspect result via
// info/exists so the warm path costs no extra engine calls. A container this
// call created is removed again on any subsequent failure, so the next
// admission relaunches cleanly instead of adopting a wedged container; the
// workspace volume is never part of that cleanup.
func (l *LocalRuntimeBackend) startContainer(ctx context.Context, runtime assistantRuntimeRecord, name string, info localContainerInfo, exists bool) (payload []byte, err error) {
	needInspect := false
	if !exists {
		createdID, runErr := l.engine.Run(ctx, l.containerSpec(runtime, name))
		switch {
		case runErr == nil:
			needInspect = true
			// Clean up the container this call created on every error exit,
			// with a fresh context so cleanup still runs on ctx timeout.
			// Removal targets the created ID, never the shared name, so a
			// failed admission cannot tear down a replacement container a
			// concurrent Ensure launched in the meantime.
			defer func() {
				if err == nil {
					return
				}
				cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
				defer cancel()
				if removeErr := l.engine.Remove(cleanupCtx, createdID); removeErr != nil {
					l.logger.WarnContext(ctx, "remove local runtime container after failed launch",
						attr.SlogAssistantID(runtime.AssistantID.String()),
						attr.SlogError(removeErr),
					)
				}
			}()
		case errors.Is(runErr, errLocalContainerNameInUse):
			// A racing Ensure created the container between our inspect and
			// run; adopt it.
			needInspect = true
		default:
			return nil, fmt.Errorf("run local runtime container %s: %w", name, runErr)
		}
	} else if !info.Running {
		if startErr := l.engine.Start(ctx, name); startErr != nil {
			return nil, fmt.Errorf("start local runtime container %s: %w", name, startErr)
		}
		needInspect = true
	}

	// Re-inspect only after a launch or start: ephemeral loopback publishes
	// are re-assigned on every container start, so the persisted port is only
	// valid for the current incarnation. The warm path keeps the caller's
	// inspect result.
	if needInspect {
		fresh, inspectErr := l.engine.Inspect(ctx, name)
		if inspectErr != nil {
			return nil, fmt.Errorf("inspect local runtime container %s after start: %w", name, inspectErr)
		}
		info = fresh
	}
	if !info.Running || info.HostPort == 0 {
		return nil, fmt.Errorf("%w: local runtime container %s is not running with a published port", ErrRuntimeUnhealthy, name)
	}

	if err := l.runner.health(ctx, localRuntimeEndpoint(info.HostPort), l.healthTimeout, localRuntimeHealthPollDelay); err != nil {
		return nil, err
	}

	metadata := localRuntimeMetadata{
		ContainerID:   info.ID,
		ContainerName: name,
		HostPort:      info.HostPort,
	}
	payload, err = json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode local runtime metadata: %w", err)
	}
	return payload, nil
}

func (l *LocalRuntimeBackend) containerSpec(runtime assistantRuntimeRecord, name string) localContainerSpec {
	return localContainerSpec{
		Name:  name,
		Image: l.desiredImageRef(),
		Labels: map[string]string{
			runtimeLabelAssistantID: strings.ToLower(runtime.AssistantID.String()),
			runtimeLabelProjectID:   strings.ToLower(runtime.ProjectID.String()),
			runtimeLabelRole:        runtimeLabelRoleAssistantRuntime,
		},
		Env: map[string]string{
			"GRAM_ASSISTANT_ID":         runtime.AssistantID.String(),
			"GRAM_ASSISTANT_PROJECT_ID": runtime.ProjectID.String(),
			"GRAM_SERVER_URL":           l.config.ServerURL.String(),
		},
		VolumeName:      localVolumeName(runtime),
		ExtraCACertFile: l.config.ExtraCACertFile,
	}
}

// RecycleImage replaces an idle container whose image ID drifted from the
// currently built image, without launching anything for rows that have no
// container. Busy runners are skipped — the next admission's Ensure picks the
// new image up lazily.
func (l *LocalRuntimeBackend) RecycleImage(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
	if err := validateRuntimeBackend(l, runtime.Backend); err != nil {
		return RuntimeBackendRecycleResult{}, err
	}

	desiredImageID, err := l.resolveDesiredImageID(ctx)
	if err != nil {
		return RuntimeBackendRecycleResult{}, err
	}

	name := localContainerName(runtime)
	info, err := l.engine.Inspect(ctx, name)
	if errors.Is(err, errLocalContainerNotFound) {
		return RuntimeBackendRecycleResult{Recycled: false, BackendMetadataJSON: nil}, nil
	}
	if err != nil {
		return RuntimeBackendRecycleResult{}, fmt.Errorf("inspect local runtime container %s: %w", name, err)
	}
	// A stopped container is never resurrected here — its row may be expired
	// or stopped on purpose, and the next admission's Ensure picks the new
	// image up anyway.
	if !info.Running || info.ImageID == desiredImageID || l.runnerBusy(ctx, info) {
		return RuntimeBackendRecycleResult{Recycled: false, BackendMetadataJSON: nil}, nil
	}

	if err := l.engine.Remove(ctx, info.ID); err != nil {
		return RuntimeBackendRecycleResult{}, fmt.Errorf("remove drifted local runtime container %s: %w", name, err)
	}
	payload, err := l.startContainer(ctx, runtime, name, localContainerInfo{ID: "", Running: false, ImageID: "", HostPort: 0}, false)
	if err != nil {
		return RuntimeBackendRecycleResult{}, err
	}
	return RuntimeBackendRecycleResult{Recycled: true, BackendMetadataJSON: payload}, nil
}

func (l *LocalRuntimeBackend) RunTurn(ctx context.Context, runtime assistantRuntimeRecord, threadID uuid.UUID, idempotencyKey string, authToken string, prompt string, mcpServers []runtimeMCPServer) error {
	if err := validateRuntimeBackend(l, runtime.Backend); err != nil {
		return err
	}
	metadata, err := decodeLocalRuntimeMetadata(runtime.BackendMetadataJSON)
	if err != nil {
		return err
	}
	if metadata.HostPort == 0 {
		return fmt.Errorf("%w: local runtime host port is not available", ErrRuntimeUnhealthy)
	}

	return l.runner.turn(ctx, localRuntimeEndpoint(metadata.HostPort), runtime, threadID, idempotencyKey, authToken, prompt, mcpServers, localRuntimeTurnTimeout)
}

func (l *LocalRuntimeBackend) Status(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	if err := validateRuntimeBackend(l, runtime.Backend); err != nil {
		return RuntimeBackendStatus{}, err
	}
	metadata, err := decodeLocalRuntimeMetadata(runtime.BackendMetadataJSON)
	if err != nil {
		return RuntimeBackendStatus{}, err
	}
	if metadata.HostPort == 0 {
		return RuntimeBackendStatus{}, fmt.Errorf("%w: local runtime host port is not available", ErrRuntimeUnhealthy)
	}
	state, err := l.runner.state(ctx, localRuntimeEndpoint(metadata.HostPort))
	if err != nil {
		return RuntimeBackendStatus{}, fmt.Errorf("load local runtime state: %w", err)
	}
	return RuntimeBackendStatus{Configured: true, IdleSeconds: state.minThreadIdle()}, nil
}

// Stop halts the container while keeping it and its workspace volume in place
// for warm restart on the next admission.
func (l *LocalRuntimeBackend) Stop(ctx context.Context, runtime assistantRuntimeRecord) error {
	if err := validateRuntimeBackend(l, runtime.Backend); err != nil {
		return err
	}
	name := localContainerName(runtime)
	if err := l.engine.Stop(ctx, name); err != nil {
		return fmt.Errorf("stop local runtime container %s: %w", name, err)
	}
	return nil
}

// Reap permanently removes the container and its workspace volume. Idempotent.
func (l *LocalRuntimeBackend) Reap(ctx context.Context, runtime assistantRuntimeRecord) error {
	if err := validateRuntimeBackend(l, runtime.Backend); err != nil {
		return err
	}
	name := localContainerName(runtime)
	if err := l.engine.Remove(ctx, name); err != nil {
		return fmt.Errorf("remove local runtime container %s: %w", name, err)
	}
	volume := localVolumeName(runtime)
	if err := l.engine.RemoveVolume(ctx, volume); err != nil {
		return fmt.Errorf("remove local runtime volume %s: %w", volume, err)
	}
	return nil
}

// ReapStoppedMachine removes only the container, preserving the workspace
// volume so the next admission for this assistant cold-launches into the same
// workspace. Idempotent.
func (l *LocalRuntimeBackend) ReapStoppedMachine(ctx context.Context, runtime assistantRuntimeRecord) error {
	if err := validateRuntimeBackend(l, runtime.Backend); err != nil {
		return err
	}
	name := localContainerName(runtime)
	if err := l.engine.Remove(ctx, name); err != nil {
		return fmt.Errorf("remove stopped local runtime container %s: %w", name, err)
	}
	return nil
}

func decodeLocalRuntimeMetadata(raw []byte) (localRuntimeMetadata, error) {
	if len(raw) == 0 {
		return localRuntimeMetadata{}, fmt.Errorf("local runtime metadata is empty")
	}
	var metadata localRuntimeMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return localRuntimeMetadata{}, fmt.Errorf("decode local runtime metadata: %w", err)
	}
	return metadata, nil
}

var _ RuntimeBackend = (*LocalRuntimeBackend)(nil)
