package assistants

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	defaultRuntimeGuestPort       = 8081
	defaultRuntimeMemoryMiB       = 1024
	defaultRuntimeVCPUCount       = 2
	defaultRuntimeTapPrefix       = "gramast"
	defaultRuntimeNetworkBaseCIDR = "172.29.0.0/16"
	runtimeBootTimeout            = 30 * time.Second
	runtimeHTTPTimeout            = 2 * time.Minute

	RuntimeHostKindLinux = "linux"
	RuntimeHostKindLima  = "lima"
)

type RuntimeManagerConfig struct {
	FirecrackerBinPath string
	KernelImagePath    string
	RootFSPath         string
	Workdir            string
	GuestAPIPort       int
	MemoryMiB          int
	VCPUCount          int64
	TapPrefix          string
	NetworkBaseCIDR    string
	ServerURLOverride  *url.URL
	ServerHostname     string
	ServerIPOverride   string
	HostKind           string
	LimaInstance       string
	// OnUnexpectedExit is invoked when the firecracker process terminates
	// without a preceding Stop() call. Used by the service layer to
	// reconcile the DB runtime row so admit can re-provision the thread.
	OnUnexpectedExit func(threadID uuid.UUID)
}

type runtimeStartupConfig struct {
	Model          string             `json:"model"`
	Instructions   *string            `json:"instructions,omitempty"`
	AuthToken      string             `json:"auth_token"`
	CompletionsURL *string            `json:"completions_url,omitempty"`
	ChatID         string             `json:"chat_id"`
	MCPServers     []runtimeMCPServer `json:"mcp_servers"`
	History        []runtimeMessage   `json:"history,omitempty"`
}

type runtimeMCPServer struct {
	ID      string            `json:"id"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// runtimeMessage is the wire shape for a single replayed transcript entry sent
// to the runner. It mirrors the shape the runner expects (see agents/runner's
// RunnerMessage) which in turn expands to one agentkit Item per message. Keep
// fields in sync with that struct.
type runtimeMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content,omitempty"`
	ToolCalls  []runtimeToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

type runtimeToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type runtimeTurnRequest struct {
	Input     string `json:"input"`
	AuthToken string `json:"auth_token,omitempty"`
}

type runtimeHTTPRequest struct {
	Method      string
	Path        string
	ContentType string
	Body        []byte
	// MaxTimeSeconds overrides the default `--max-time` passed to curl.
	// 0 uses a conservative default appropriate for health checks; long
	// turn requests should set this large enough to cover the full agent
	// loop (tool calls + LLM turns).
	MaxTimeSeconds int
	// IdempotencyKey sets X-Idempotency-Key; the runner skips re-running a
	// turn it has already processed under this key. Use the event DB id so
	// the same key flows through workflow retries, coordinator re-signals,
	// and reaper requeues.
	IdempotencyKey string
}

type firecrackerConfig struct {
	BootSource        firecrackerBootSource         `json:"boot-source"`
	Drives            []firecrackerDrive            `json:"drives"`
	MachineConfig     firecrackerMachineConfig      `json:"machine-config"`
	NetworkInterfaces []firecrackerNetworkInterface `json:"network-interfaces"`
	// Attach a virtio-rng device so the guest's RNG seeds quickly. Without
	// this, getrandom() blocks for ~1-2 minutes during userspace startup
	// (e.g. the runner hangs on TLS handshake setup waiting for entropy).
	Entropy *firecrackerEntropy `json:"entropy,omitempty"`
}

type firecrackerEntropy struct {
	// Empty object is sufficient: firecracker wires /dev/hwrng to the guest
	// and the kernel pulls entropy from it automatically.
}

type firecrackerBootSource struct {
	KernelImagePath string `json:"kernel_image_path"`
	BootArgs        string `json:"boot_args"`
}

type firecrackerDrive struct {
	DriveID      string `json:"drive_id"`
	PathOnHost   string `json:"path_on_host"`
	IsRootDevice bool   `json:"is_root_device"`
	IsReadOnly   bool   `json:"is_read_only"`
}

type firecrackerMachineConfig struct {
	VCPUCount       int64 `json:"vcpu_count"`
	MemSizeMiB      int   `json:"mem_size_mib"`
	Smt             bool  `json:"smt"`
	TrackDirtyPages bool  `json:"track_dirty_pages"`
}

type firecrackerNetworkInterface struct {
	IfaceID     string `json:"iface_id"`
	GuestMac    string `json:"guest_mac"`
	HostDevName string `json:"host_dev_name"`
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("write runtime stderr buffer: %w", err)
	}
	return n, nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type runtimeState struct {
	threadID      uuid.UUID
	slot          int
	workdir       string
	tapName       string
	hostIP        netip.Addr
	guestIP       netip.Addr
	apiBaseURL    string
	fcConfigPath  string
	fcSocketPath  string
	stderr        *lockedBuffer
	logFile       *os.File
	httpClient    *guardian.HTTPClient
	cmd           *exec.Cmd
	done          chan struct{}
	ready         chan struct{}
	bootErr       error
	configured    bool
	cleanupOnce   sync.Once
	waitErr       error
	stopRequested atomic.Bool
}

type RuntimeManager struct {
	logger           *slog.Logger
	config           RuntimeManagerConfig
	httpPolicy       *guardian.Policy
	mu               sync.Mutex
	runtimes         map[uuid.UUID]*runtimeState
	nextSlot         int
	freeSlots        []int
	onUnexpectedExit func(uuid.UUID)
}

// NewRuntimeManager wires the local Firecracker runtime. The httpPolicy must
// permit traffic to the private 172.x guest subnet (an unsafe loopback-allowing
// policy in local development); production never reaches this constructor.
func NewRuntimeManager(logger *slog.Logger, httpPolicy *guardian.Policy, config RuntimeManagerConfig) *RuntimeManager {
	if config.GuestAPIPort <= 0 {
		config.GuestAPIPort = defaultRuntimeGuestPort
	}
	if config.MemoryMiB <= 0 {
		config.MemoryMiB = defaultRuntimeMemoryMiB
	}
	if config.VCPUCount <= 0 {
		config.VCPUCount = defaultRuntimeVCPUCount
	}
	if config.TapPrefix == "" {
		config.TapPrefix = defaultRuntimeTapPrefix
	}
	if config.NetworkBaseCIDR == "" {
		config.NetworkBaseCIDR = defaultRuntimeNetworkBaseCIDR
	}
	if config.HostKind == "" {
		if goruntime.GOOS == "darwin" {
			config.HostKind = RuntimeHostKindLima
		} else {
			config.HostKind = RuntimeHostKindLinux
		}
	}
	if config.LimaInstance == "" {
		config.LimaInstance = "gram-firecracker"
	}
	if config.Workdir == "" {
		if root, err := repoRoot(); err == nil {
			config.Workdir = filepath.Join(root, ".tmp", "assistant-runtimes")
		}
	}
	if config.FirecrackerBinPath == "" || config.KernelImagePath == "" || config.RootFSPath == "" {
		arch := firecrackerArchForGOARCH(goruntime.GOARCH)
		if root, err := repoRoot(); err == nil {
			artifactDir := filepath.Join(root, "agents", "runtime-artifacts", arch)
			if config.FirecrackerBinPath == "" {
				config.FirecrackerBinPath = filepath.Join(artifactDir, "firecracker")
			}
			if config.KernelImagePath == "" {
				config.KernelImagePath = filepath.Join(artifactDir, "vmlinux.bin")
			}
			if config.RootFSPath == "" {
				config.RootFSPath = filepath.Join(artifactDir, "assistant-rootfs.ext4")
			}
		}
	}
	if config.HostKind == RuntimeHostKindLima && config.ServerURLOverride == nil {
		config.ServerHostname = firstNonEmpty(config.ServerHostname, "host.lima.internal")
	}
	if config.HostKind == RuntimeHostKindLinux {
		config.ServerHostname = firstNonEmpty(config.ServerHostname, "gram.local")
	}
	if config.HostKind == RuntimeHostKindLima && config.ServerIPOverride == "" && config.LimaInstance != "" {
		// Firecracker guests sit on a private /30 behind a tap in the Lima VM.
		// They reach the Mac host via NAT on Lima's eth0 — the destination IP
		// in outbound packets must be the Mac's IP *as seen from Lima* (what
		// `host.lima.internal` resolves to inside Lima, typically 192.168.5.2).
		// Resolve it once so init.sh can add a correct /etc/hosts entry.
		if ip := resolveLimaHostIP(config.LimaInstance); ip != "" {
			config.ServerIPOverride = ip
			if logger != nil {
				logger.InfoContext(context.Background(), "resolved assistant runtime server ip via lima", attr.SlogAssistantServerIP(ip))
			}
		} else if logger != nil {
			logger.WarnContext(context.Background(), "could not resolve host.lima.internal inside lima; guest will not be able to reach gram server — set GRAM_ASSISTANT_RUNTIME_SERVER_IP to override")
		}
	}

	return &RuntimeManager{
		logger:           logger,
		config:           config,
		httpPolicy:       httpPolicy,
		mu:               sync.Mutex{},
		runtimes:         make(map[uuid.UUID]*runtimeState),
		nextSlot:         0,
		freeSlots:        nil,
		onUnexpectedExit: config.OnUnexpectedExit,
	}
}

// ErrRuntimeUnhealthy signals that a turn failed because the runtime itself
// is unreachable or has exited (connection refused, context cancel from a
// closed done channel, missing state). Callers treat this as "tear down and
// re-admit" rather than "retry the event inline", which would otherwise
// hammer a dead VM with duplicate deliveries.
var ErrRuntimeUnhealthy = errors.New("assistant runtime unhealthy")

// ErrCompletionFailed signals that a turn failed because the upstream
// completion provider (OpenRouter/Anthropic/etc) refused the request or
// returned a non-retryable error. The runtime itself is healthy — replaying
// the same input would just produce the same failure, so callers terminally
// fail the event and leave the VM warm to handle subsequent events.
var ErrCompletionFailed = errors.New("assistant completion failed")

func (m *RuntimeManager) Backend() string {
	return runtimeBackendLocal
}

func (m *RuntimeManager) SupportsBackend(backend string) bool {
	switch backend {
	case runtimeBackendLocal, runtimeBackendLegacyFirecracker:
		return true
	default:
		return false
	}
}

func (m *RuntimeManager) Ensure(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendEnsureResult, error) {
	if err := validateRuntimeBackend(m, runtime.Backend); err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	threadID := runtime.AssistantThreadID

	m.mu.Lock()
	if state, ok := m.runtimes[threadID]; ok {
		select {
		case <-state.done:
			delete(m.runtimes, threadID)
		default:
			m.mu.Unlock()
			return m.awaitReady(ctx, state)
		}
	}

	state, err := m.beginStartLocked(threadID)
	if err != nil {
		m.mu.Unlock()
		return RuntimeBackendEnsureResult{}, err
	}
	m.runtimes[threadID] = state
	m.mu.Unlock()

	// The slow boot work (file copies, SSH, cmd start, health-check polling
	// up to runtimeBootTimeout) runs without holding m.mu so concurrent
	// Ensure calls for other threads can proceed in parallel. Concurrent
	// callers for this same threadID block on state.ready below.
	if err := m.completeStart(ctx, state); err != nil {
		m.mu.Lock()
		if cur, ok := m.runtimes[threadID]; ok && cur == state {
			delete(m.runtimes, threadID)
		}
		m.mu.Unlock()
		state.bootErr = err
		// state.cmd is only set after cmd.Start succeeds, which is the same
		// place waitForProcess is spawned. If cmd is nil here, no goroutine
		// will close state.done; close it ourselves so awaiters unblock.
		if state.cmd == nil {
			close(state.done)
		}
		close(state.ready)
		m.cleanupState(state)
		return RuntimeBackendEnsureResult{}, err
	}
	close(state.ready)
	return RuntimeBackendEnsureResult{
		ColdStart:           true,
		NeedsConfigure:      true,
		BackendMetadataJSON: nil,
	}, nil
}

func (m *RuntimeManager) awaitReady(ctx context.Context, state *runtimeState) (RuntimeBackendEnsureResult, error) {
	select {
	case <-state.ready:
	case <-ctx.Done():
		return RuntimeBackendEnsureResult{}, fmt.Errorf("await assistant runtime ready: %w", ctx.Err())
	}
	if state.bootErr != nil {
		return RuntimeBackendEnsureResult{}, state.bootErr
	}
	return RuntimeBackendEnsureResult{
		ColdStart:           false,
		NeedsConfigure:      !state.configured,
		BackendMetadataJSON: nil,
	}, nil
}

func (m *RuntimeManager) Configure(ctx context.Context, runtime assistantRuntimeRecord, config runtimeStartupConfig) error {
	if err := validateRuntimeBackend(m, runtime.Backend); err != nil {
		return err
	}
	state, err := m.getRuntime(runtime.AssistantThreadID)
	if err != nil {
		return err
	}
	stateBody, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal assistant runtime config: %w", err)
	}

	if _, err := m.runtimeRequest(ctx, state, runtimeHTTPRequest{
		Method:         http.MethodPost,
		Path:           "/configure",
		ContentType:    "application/json",
		Body:           stateBody,
		MaxTimeSeconds: 0,
		IdempotencyKey: "",
	}); err != nil {
		return fmt.Errorf("configure assistant runtime: %w", err)
	}

	m.mu.Lock()
	if current, ok := m.runtimes[runtime.AssistantThreadID]; ok && current == state {
		current.configured = true
	}
	m.mu.Unlock()
	return nil
}

func (m *RuntimeManager) Status(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	if err := validateRuntimeBackend(m, runtime.Backend); err != nil {
		return RuntimeBackendStatus{}, err
	}
	state, err := m.getRuntime(runtime.AssistantThreadID)
	if err != nil {
		return RuntimeBackendStatus{}, fmt.Errorf("%w: %w", ErrRuntimeUnhealthy, err)
	}
	body, err := m.runtimeRequest(ctx, state, runtimeHTTPRequest{
		Method:         http.MethodGet,
		Path:           "/state",
		ContentType:    "",
		Body:           nil,
		MaxTimeSeconds: 0,
		IdempotencyKey: "",
	})
	if err != nil {
		return RuntimeBackendStatus{}, fmt.Errorf("%w: load assistant runtime state: %w", ErrRuntimeUnhealthy, err)
	}
	var resp runnerStateResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return RuntimeBackendStatus{}, fmt.Errorf("decode assistant runtime state: %w", err)
	}
	return RuntimeBackendStatus(resp), nil
}

func (m *RuntimeManager) Stop(_ context.Context, runtime assistantRuntimeRecord) error {
	if err := validateRuntimeBackend(m, runtime.Backend); err != nil {
		return err
	}

	m.mu.Lock()
	state, ok := m.runtimes[runtime.AssistantThreadID]
	if ok {
		delete(m.runtimes, runtime.AssistantThreadID)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	// Wait for in-progress boot to finish populating state fields so our
	// cleanup does not race the boot goroutine. runtimeBootTimeout caps the
	// wait; the +5s slack covers stopState's own 10s done timeout window.
	select {
	case <-state.ready:
	case <-time.After(runtimeBootTimeout + 5*time.Second):
	}
	m.stopState(state)
	return nil
}

// Reap on the local Firecracker manager has the same effect as Stop: there
// is no out-of-process resource that survives Stop, so cleanup is identical.
func (m *RuntimeManager) Reap(ctx context.Context, runtime assistantRuntimeRecord) error {
	return m.Stop(ctx, runtime)
}

func (m *RuntimeManager) ServerURL(_ context.Context, runtime assistantRuntimeRecord, raw *url.URL) (*url.URL, error) {
	if err := validateRuntimeBackend(m, runtime.Backend); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("assistant runtime server URL is not configured")
	}
	if m.config.ServerURLOverride != nil {
		cloned := *m.config.ServerURLOverride
		return &cloned, nil
	}

	host := raw.Hostname()
	if host != "" && !isLoopbackHost(host) {
		cloned := *raw
		return &cloned, nil
	}

	if hostname := strings.TrimSpace(m.config.ServerHostname); hostname != "" {
		cloned := *raw
		port := raw.Port()
		if port == "" {
			switch raw.Scheme {
			case "https":
				port = "443"
			default:
				port = "80"
			}
		}
		cloned.Host = net.JoinHostPort(hostname, port)
		return &cloned, nil
	}

	state, err := m.getRuntime(runtime.AssistantThreadID)
	if err != nil {
		return nil, err
	}
	cloned := *raw
	port := raw.Port()
	if port == "" {
		switch raw.Scheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	cloned.Host = net.JoinHostPort(state.hostIP.String(), port)
	return &cloned, nil
}

func (m *RuntimeManager) RunTurn(
	ctx context.Context,
	runtime assistantRuntimeRecord,
	idempotencyKey string,
	authToken string,
	prompt string,
) error {
	if err := validateRuntimeBackend(m, runtime.Backend); err != nil {
		return err
	}
	state, err := m.getRuntime(runtime.AssistantThreadID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRuntimeUnhealthy, err)
	}
	// If the firecracker process already exited, don't bother making an HTTP
	// call we know will fail — surface the unhealthy signal immediately.
	select {
	case <-state.done:
		return fmt.Errorf("%w: runtime process has exited", ErrRuntimeUnhealthy)
	default:
	}

	reqBody, err := json.Marshal(runtimeTurnRequest{
		Input:     prompt,
		AuthToken: authToken,
	})
	if err != nil {
		return fmt.Errorf("marshal assistant runtime turn request: %w", err)
	}

	if _, err := m.runtimeRequest(ctx, state, runtimeHTTPRequest{
		Method:         http.MethodPost,
		Path:           "/turn",
		ContentType:    "application/json",
		Body:           reqBody,
		MaxTimeSeconds: 30 * 60,
		IdempotencyKey: idempotencyKey,
	}); err != nil {
		return fmt.Errorf("%w: execute turn request: %w", ErrRuntimeUnhealthy, err)
	}
	return nil
}

func (m *RuntimeManager) getRuntime(threadID uuid.UUID) (*runtimeState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.runtimes[threadID]
	if !ok {
		return nil, fmt.Errorf("assistant runtime %s is not active", threadID)
	}
	select {
	case <-state.done:
		delete(m.runtimes, threadID)
		return nil, fmt.Errorf("assistant runtime exited: %w", state.waitErr)
	default:
	}
	return state, nil
}

// beginStartLocked validates the manager and reserves a slot. It must be called
// with m.mu held. Returns a state shell whose slot is allocated; the slow boot
// work (file copies, SSH, cmd start, health-check polling) runs in
// completeStart without holding m.mu.
func (m *RuntimeManager) beginStartLocked(threadID uuid.UUID) (*runtimeState, error) {
	if m.config.HostKind == RuntimeHostKindLinux && goruntime.GOOS != "linux" {
		return nil, fmt.Errorf("assistant Firecracker runtime host kind %q requires linux hosts", m.config.HostKind)
	}
	if err := m.validateConfigLocked(); err != nil {
		return nil, err
	}

	slot := m.allocateSlotLocked()
	return &runtimeState{
		threadID:      threadID,
		slot:          slot,
		workdir:       "",
		tapName:       "",
		hostIP:        netip.Addr{},
		guestIP:       netip.Addr{},
		apiBaseURL:    "",
		fcConfigPath:  "",
		fcSocketPath:  "",
		stderr:        nil,
		logFile:       nil,
		httpClient:    nil,
		cmd:           nil,
		done:          make(chan struct{}),
		ready:         make(chan struct{}),
		bootErr:       nil,
		configured:    false,
		cleanupOnce:   sync.Once{},
		waitErr:       nil,
		stopRequested: atomic.Bool{},
	}, nil
}

func (m *RuntimeManager) completeStart(ctx context.Context, state *runtimeState) error {
	threadID := state.threadID
	slot := state.slot

	hostIP, guestIP, err := slotAddresses(m.config.NetworkBaseCIDR, slot)
	if err != nil {
		return err
	}
	state.hostIP = hostIP
	state.guestIP = guestIP
	state.apiBaseURL = fmt.Sprintf("http://%s:%d", guestIP.String(), m.config.GuestAPIPort)

	workdir := filepath.Join(m.config.Workdir, threadID.String())
	if err := os.MkdirAll(workdir, 0750); err != nil {
		return fmt.Errorf("create assistant runtime workdir: %w", err)
	}
	state.workdir = workdir

	rootfsPath := filepath.Join(workdir, "rootfs.ext4")
	if err := copyFile(m.config.RootFSPath, rootfsPath, 0640); err != nil {
		return fmt.Errorf("copy assistant runtime rootfs: %w", err)
	}

	tapName := fmt.Sprintf("%s%d", m.config.TapPrefix, slot)
	fcConfigPath := filepath.Join(workdir, "firecracker-config.json")
	// Firecracker's API socket path is subject to AF_UNIX sun_path (~108 bytes),
	// which a repo-nested workdir easily exceeds. Use a short per-slot path in
	// /run (tmpfs) — unique across concurrent runtimes and removed by cleanup.
	fcSocketPath := fmt.Sprintf("/run/gramfc-%d.socket", slot)
	state.tapName = tapName
	state.fcConfigPath = fcConfigPath
	state.fcSocketPath = fcSocketPath

	// Prepare tap + routing + orphan-socket cleanup in a single SSH round-trip
	// to save ~2s of sudo/PAM overhead versus running them as separate calls.
	if err := m.prepareSlotHost(ctx, tapName, hostIP, fcSocketPath); err != nil {
		return err
	}
	fcConfig := firecrackerConfig{
		BootSource: firecrackerBootSource{
			KernelImagePath: m.config.KernelImagePath,
			BootArgs:        buildBootArgs(hostIP, guestIP, m.serverHostnameForGuest(), m.serverIPForGuest(hostIP)),
		},
		Drives: []firecrackerDrive{
			{
				DriveID:      "rootfs",
				PathOnHost:   rootfsPath,
				IsRootDevice: true,
				IsReadOnly:   false,
			},
		},
		MachineConfig: firecrackerMachineConfig{
			VCPUCount:       m.config.VCPUCount,
			MemSizeMiB:      m.config.MemoryMiB,
			Smt:             false,
			TrackDirtyPages: false,
		},
		NetworkInterfaces: []firecrackerNetworkInterface{
			{
				IfaceID:     "eth0",
				GuestMac:    macForSlot(slot),
				HostDevName: tapName,
			},
		},
		Entropy: &firecrackerEntropy{},
	}
	configJSON, err := json.Marshal(fcConfig)
	if err != nil {
		return fmt.Errorf("marshal assistant firecracker config: %w", err)
	}
	if err := os.WriteFile(fcConfigPath, configJSON, 0600); err != nil {
		return fmt.Errorf("write assistant firecracker config: %w", err)
	}

	stderr := &lockedBuffer{
		mu:  sync.Mutex{},
		buf: bytes.Buffer{},
	}
	state.stderr = stderr
	state.httpClient = m.newRuntimeHTTPClient()

	cmd, err := m.startFirecrackerCommand(ctx, fcConfigPath, fcSocketPath)
	if err != nil {
		return err
	}
	cmd.Dir = workdir
	// Log file lives in the workdir ROOT so cleanupState's workdir removal
	// doesn't wipe evidence of a failed boot. latest.log symlink is updated
	// per boot for `tail -F` in the assistant-runtime pane.
	runtimeLogPath := filepath.Join(filepath.Dir(workdir), fmt.Sprintf("%s.log", threadID.String()))
	runtimeLogFile, err := os.OpenFile(runtimeLogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640) //nolint:gosec // runtimeLogPath is derived from the manager's own workdir, not user input
	if err != nil {
		return fmt.Errorf("open runtime log file: %w", err)
	}
	state.logFile = runtimeLogFile
	m.updateLatestLogSymlink(runtimeLogPath)
	cmd.Stdout = io.MultiWriter(stderr, runtimeLogFile)
	cmd.Stderr = io.MultiWriter(stderr, runtimeLogFile)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start firecracker runtime: %w", err)
	}
	state.cmd = cmd

	go m.waitForProcess(threadID, state)

	if err := m.waitForRuntimeHealth(ctx, state); err != nil {
		m.stopState(state)
		return fmt.Errorf("wait for assistant runtime health: %w; stderr=%s", err, truncateForMetadata(state.stderr.String(), 32*1024))
	}

	return nil
}

func (m *RuntimeManager) waitForProcess(threadID uuid.UUID, state *runtimeState) {
	err := state.cmd.Wait()
	state.waitErr = err
	close(state.done)

	m.mu.Lock()
	current, ok := m.runtimes[threadID]
	if ok && current == state {
		delete(m.runtimes, threadID)
	}
	m.mu.Unlock()

	m.cleanupState(state)

	// If the process exited without a Stop() call, it crashed or was killed
	// out from under us — notify the service layer so the DB runtime row can
	// be marked stopped and the thread re-admitted on the next event.
	if !state.stopRequested.Load() && m.onUnexpectedExit != nil {
		m.onUnexpectedExit(threadID)
	}
}

func (m *RuntimeManager) stopState(state *runtimeState) {
	if state == nil {
		return
	}
	state.stopRequested.Store(true)
	// Kill the remote firecracker process first. On macOS+Lima the `cmd` is a
	// local ssh without a tty, so Signal/Kill on it does not propagate to the
	// remote `sudo firecracker`. Target it explicitly by its config-file path
	// to avoid orphaning a VM that still holds the tap and rootfs.
	m.killRemoteFirecracker(state)
	if state.cmd != nil && state.cmd.Process != nil {
		_ = state.cmd.Process.Kill()
	}
	select {
	case <-state.done:
	case <-time.After(10 * time.Second):
	}
	m.cleanupState(state)
}

func (m *RuntimeManager) killRemoteFirecracker(state *runtimeState) {
	if state == nil || state.fcConfigPath == "" {
		return
	}
	if m.config.HostKind != RuntimeHostKindLima {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// pkill exits 1 when no process matches — treat as success.
	_ = m.runHostCommand(ctx, "pkill", "-9", "-f", state.fcConfigPath)
}

func (m *RuntimeManager) waitForRuntimeHealth(ctx context.Context, state *runtimeState) error {
	deadline := time.Now().Add(runtimeBootTimeout)
	// Poll aggressively: the guest typically answers /healthz within 1-2s of
	// fc launching. A 100ms interval catches that without burning cycles.
	// runtimeRequestViaLima's curl has its own `--connect-timeout 3` which
	// caps wasted time per failed poll.
	interval := 100 * time.Millisecond
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("runtime health check timed out")
		}
		if ctx.Err() != nil {
			return fmt.Errorf("wait for runtime health: %w", ctx.Err())
		}

		_, err := m.runtimeRequest(ctx, state, runtimeHTTPRequest{
			Method:         http.MethodGet,
			Path:           "/healthz",
			ContentType:    "",
			Body:           nil,
			MaxTimeSeconds: 0,
			IdempotencyKey: "",
		})
		if err == nil {
			return nil
		}
		time.Sleep(interval)
	}
}

func (m *RuntimeManager) runtimeRequest(
	ctx context.Context,
	state *runtimeState,
	request runtimeHTTPRequest,
) ([]byte, error) {
	switch m.config.HostKind {
	case RuntimeHostKindLima:
		return m.runtimeRequestViaLima(ctx, state, request)
	default:
		return m.runtimeRequestDirect(ctx, state, request)
	}
}

func (m *RuntimeManager) runtimeRequestDirect(
	ctx context.Context,
	state *runtimeState,
	request runtimeHTTPRequest,
) ([]byte, error) {
	reqCtx, cancel := runtimeRequestContext(ctx, request.MaxTimeSeconds, runtimeHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, request.Method, state.apiBaseURL+request.Path, bytes.NewReader(request.Body))
	if err != nil {
		return nil, fmt.Errorf("build assistant runtime request: %w", err)
	}
	if request.ContentType != "" {
		req.Header.Set("Content-Type", request.ContentType)
	}
	if request.IdempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", request.IdempotencyKey)
	}

	resp, err := state.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send assistant runtime request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read assistant runtime response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (m *RuntimeManager) runtimeRequestViaLima(
	ctx context.Context,
	state *runtimeState,
	request runtimeHTTPRequest,
) ([]byte, error) {
	maxTime := request.MaxTimeSeconds
	if maxTime <= 0 {
		maxTime = 30
	}
	curlArgs := []string{
		"curl",
		"--silent",
		"--show-error",
		"--fail-with-body",
		"--connect-timeout", "1",
		"--max-time", strconv.Itoa(maxTime),
		"-X",
		request.Method,
	}
	if request.ContentType != "" {
		curlArgs = append(curlArgs, "-H", "Content-Type: "+request.ContentType)
	}
	if request.IdempotencyKey != "" {
		curlArgs = append(curlArgs, "-H", "X-Idempotency-Key: "+request.IdempotencyKey)
	}
	if len(request.Body) > 0 {
		curlArgs = append(curlArgs, "--data-binary", "@-")
	}
	curlArgs = append(curlArgs, state.apiBaseURL+request.Path)

	cmd, err := m.hostCommand(ctx, curlArgs[0], curlArgs[1:]...)
	if err != nil {
		return nil, err
	}
	if len(request.Body) > 0 {
		cmd.Stdin = bytes.NewReader(request.Body)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("curl runtime request: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (m *RuntimeManager) startFirecrackerCommand(ctx context.Context, configPath, socketPath string) (*exec.Cmd, error) {
	// Use a per-instance API socket path so parallel runtimes don't collide on
	// the default /run/firecracker.socket and stale sockets from prior crashes
	// don't block new instances (workdir is fresh each run).
	return m.hostCommand(ctx, m.config.FirecrackerBinPath, "--api-sock", socketPath, "--config-file", configPath)
}

func (m *RuntimeManager) hostCommand(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	switch m.config.HostKind {
	case RuntimeHostKindLinux:
		//nolint:gosec // Command names and arguments are repository-controlled or validated runtime settings.
		return exec.CommandContext(ctx, name, args...), nil
	case RuntimeHostKindLima:
		if m.config.LimaInstance == "" {
			return nil, fmt.Errorf("assistant runtime host kind lima requires a Lima instance name")
		}
		limaArgs := []string{"shell", m.config.LimaInstance, "--", "sudo", name}
		limaArgs = append(limaArgs, args...)
		//nolint:gosec // limactl command and arguments are controlled by local configuration.
		return exec.CommandContext(ctx, "limactl", limaArgs...), nil
	default:
		return nil, fmt.Errorf("unsupported assistant runtime host kind %q", m.config.HostKind)
	}
}

func (m *RuntimeManager) cleanupState(state *runtimeState) {
	state.cleanupOnce.Do(func() {
		if state.tapName != "" {
			_ = m.deleteTapDevice(context.Background(), state.tapName)
		}
		if state.fcSocketPath != "" {
			cctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = m.runHostCommand(cctx, "rm", "-f", state.fcSocketPath)
			cancel()
		}
		if state.logFile != nil {
			_ = state.logFile.Close()
		}
		if state.workdir != "" {
			_ = os.RemoveAll(state.workdir)
		}
		m.mu.Lock()
		m.releaseSlotLocked(state.slot)
		m.mu.Unlock()
	})
}

// updateLatestLogSymlink maintains a stable `<workdir-root>/latest.log` pointer
// to the most recently started runtime's log file so `tail -F` in a separate
// pane keeps following output across restarts. Best-effort; failures are
// non-fatal since the log file itself is still written.
func (m *RuntimeManager) updateLatestLogSymlink(target string) {
	if m.config.Workdir == "" {
		return
	}
	link := filepath.Join(m.config.Workdir, "latest.log")
	_ = os.Remove(link)
	if err := os.Symlink(target, link); err != nil && m.logger != nil {
		m.logger.WarnContext(context.Background(), "update assistant runtime latest.log symlink", attr.SlogError(err))
	}
}

func (m *RuntimeManager) validateConfigLocked() error {
	switch m.config.HostKind {
	case RuntimeHostKindLinux:
	case RuntimeHostKindLima:
		if m.config.LimaInstance == "" {
			return fmt.Errorf("assistant firecracker lima instance is not configured")
		}
	default:
		return fmt.Errorf("unsupported assistant runtime host kind %q", m.config.HostKind)
	}
	if m.config.FirecrackerBinPath == "" {
		return fmt.Errorf("assistant firecracker binary path is not configured")
	}
	if m.config.KernelImagePath == "" {
		return fmt.Errorf("assistant firecracker kernel path is not configured")
	}
	if m.config.RootFSPath == "" {
		return fmt.Errorf("assistant firecracker rootfs path is not configured")
	}
	if m.config.Workdir == "" {
		return fmt.Errorf("assistant firecracker workdir is not configured")
	}
	return nil
}

func (m *RuntimeManager) newRuntimeHTTPClient() *guardian.HTTPClient {
	return m.httpPolicy.PooledClient()
}

func runtimeRequestContext(
	parent context.Context,
	maxTimeSeconds int,
	defaultTimeout time.Duration,
) (context.Context, context.CancelFunc) {
	timeout := defaultTimeout
	if maxTimeSeconds > 0 {
		timeout = time.Duration(maxTimeSeconds) * time.Second
	}
	if timeout <= 0 {
		return parent, func() {}
	}
	//nolint:gosec // caller receives and invokes the cancel func via defer
	return context.WithTimeout(parent, timeout)
}

func (m *RuntimeManager) allocateSlotLocked() int {
	if n := len(m.freeSlots); n > 0 {
		slot := m.freeSlots[n-1]
		m.freeSlots = m.freeSlots[:n-1]
		return slot
	}
	slot := m.nextSlot
	m.nextSlot++
	return slot
}

func (m *RuntimeManager) releaseSlotLocked(slot int) {
	if slot < 0 {
		return
	}
	if slices.Contains(m.freeSlots, slot) {
		return
	}
	m.freeSlots = append(m.freeSlots, slot)
}

func buildBootArgs(hostIP netip.Addr, guestIP netip.Addr, serverHostname string, serverIP string) string {
	args := []string{
		"console=ttyS0",
		"reboot=k",
		"panic=1",
		"pci=off",
		"init=/init",
		// Trust architectural entropy sources at boot so userspace (the rust
		// runner) isn't blocked on getrandom() waiting for the CRNG to be
		// credited. Combined with firecracker's virtio-rng device.
		"random.trust_cpu=on",
		"random.trust_bootloader=on",
		fmt.Sprintf("ip=%s::%s:255.255.255.252::eth0:off", guestIP.String(), hostIP.String()),
	}
	if serverHostname != "" && serverIP != "" {
		args = append(args,
			"gram_server_hostname="+shellKernelEscape(serverHostname),
			"gram_server_ip="+shellKernelEscape(serverIP),
		)
	}
	return strings.Join(args, " ")
}

func (m *RuntimeManager) serverHostnameForGuest() string {
	if hostname := strings.TrimSpace(m.config.ServerHostname); hostname != "" {
		return hostname
	}
	if m.config.ServerURLOverride != nil {
		if hostname := strings.TrimSpace(m.config.ServerURLOverride.Hostname()); hostname != "" && net.ParseIP(hostname) == nil {
			return hostname
		}
	}
	return ""
}

func (m *RuntimeManager) serverIPForGuest(hostIP netip.Addr) string {
	if raw := strings.TrimSpace(m.config.ServerIPOverride); raw != "" {
		return raw
	}
	if m.config.ServerURLOverride != nil {
		if parsed := net.ParseIP(m.config.ServerURLOverride.Hostname()); parsed != nil {
			return parsed.String()
		}
	}
	if m.config.HostKind == RuntimeHostKindLinux {
		return hostIP.String()
	}
	return ""
}

// resolveLimaHostIP asks the Lima VM what IP `host.lima.internal` resolves to
// (the address the Mac host is reachable at from inside the VM). Returns "" on
// failure — callers must tolerate this, since the Lima instance may be down
// at process startup.
func resolveLimaHostIP(instance string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	//nolint:gosec // instance name is operator-configured.
	out, err := exec.CommandContext(ctx, "limactl", "shell", instance, "--", "getent", "hosts", "host.lima.internal").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) == 0 {
		return ""
	}
	if net.ParseIP(fields[0]) == nil {
		return ""
	}
	return fields[0]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func shellKernelEscape(value string) string {
	return strings.ReplaceAll(value, " ", `\ `)
}

// prepareSlotHost collapses tap creation, iptables NAT/FORWARD rules, and
// orphan-socket cleanup into a single `limactl shell ... sudo bash -c '...'`
// call. Each separate hostCommand adds ~200-400ms of SSH + PAM overhead, so
// batching saves ~2s off every cold start.
func (m *RuntimeManager) prepareSlotHost(ctx context.Context, tapName string, hostIP netip.Addr, fcSocketPath string) error {
	script := fmt.Sprintf(`set -euo pipefail
ip link del dev %[1]s 2>/dev/null || true
ip tuntap add dev %[1]s mode tap
ip addr add %[2]s/30 dev %[1]s
ip link set dev %[1]s up
sysctl -w net.ipv4.ip_forward=1 >/dev/null
iptables -t nat -C POSTROUTING -s %[3]s ! -o %[1]s -j MASQUERADE 2>/dev/null || iptables -t nat -A POSTROUTING -s %[3]s ! -o %[1]s -j MASQUERADE
iptables -C FORWARD -i %[1]s -j ACCEPT 2>/dev/null || iptables -A FORWARD -i %[1]s -j ACCEPT
iptables -C FORWARD -o %[1]s -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || iptables -A FORWARD -o %[1]s -m state --state RELATED,ESTABLISHED -j ACCEPT
pkill -9 -f %[4]s 2>/dev/null || true
rm -f %[4]s
`, shellEscape(tapName), shellEscape(hostIP.String()), shellEscape(m.config.NetworkBaseCIDR), shellEscape(fcSocketPath))
	return m.runHostCommandStdin(ctx, script, "bash", "-s")
}

func (m *RuntimeManager) deleteTapDevice(ctx context.Context, name string) error {
	_ = m.disableTapRouting(ctx, name)
	return m.runHostCommand(ctx, "ip", "link", "del", "dev", name)
}

func (m *RuntimeManager) runHostCommand(ctx context.Context, name string, args ...string) error {
	cmd, err := m.hostCommand(ctx, name, args...)
	if err != nil {
		return err
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

// runHostCommandStdin pipes stdinBody into the command's stdin. Useful when
// shipping multi-line shell scripts to `bash -s` over limactl/ssh, which
// mangles multi-line argv values.
func (m *RuntimeManager) runHostCommandStdin(ctx context.Context, stdinBody, name string, args ...string) error {
	cmd, err := m.hostCommand(ctx, name, args...)
	if err != nil {
		return err
	}
	cmd.Stdin = strings.NewReader(stdinBody)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (m *RuntimeManager) disableTapRouting(ctx context.Context, tapName string) error {
	script := fmt.Sprintf(`set -euo pipefail
iptables -t nat -D POSTROUTING -s %[2]s ! -o %[1]s -j MASQUERADE 2>/dev/null || true
iptables -D FORWARD -i %[1]s -j ACCEPT 2>/dev/null || true
iptables -D FORWARD -o %[1]s -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true
`, shellEscape(tapName), shellEscape(m.config.NetworkBaseCIDR))
	return m.runHostCommandStdin(ctx, script, "bash", "-s")
}

func slotAddresses(rawPrefix string, slot int) (netip.Addr, netip.Addr, error) {
	prefix, err := netip.ParsePrefix(rawPrefix)
	if err != nil {
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("parse assistant runtime network prefix: %w", err)
	}
	base := prefix.Masked().Addr()
	if !base.Is4() {
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("assistant runtime network prefix must be IPv4")
	}

	baseBytes := base.As4()
	baseInt := binary.BigEndian.Uint32(baseBytes[:])
	if slot < 0 {
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("assistant runtime slot cannot be negative")
	}
	hostOffset := uint64(slot)*4 + 1
	guestOffset := uint64(slot)*4 + 2
	maxOffset := uint64(1) << (32 - prefix.Bits())
	if guestOffset >= maxOffset {
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("assistant runtime network prefix %s exhausted", rawPrefix)
	}

	hostOffset32, err := safeUint32FromUint64(hostOffset)
	if err != nil {
		return netip.Addr{}, netip.Addr{}, err
	}
	guestOffset32, err := safeUint32FromUint64(guestOffset)
	if err != nil {
		return netip.Addr{}, netip.Addr{}, err
	}

	return netip.AddrFrom4(uint32ToIPv4(baseInt + hostOffset32)), netip.AddrFrom4(uint32ToIPv4(baseInt + guestOffset32)), nil
}

func uint32ToIPv4(v uint32) [4]byte {
	var out [4]byte
	binary.BigEndian.PutUint32(out[:], v)
	return out
}

func macForSlot(slot int) string {
	if slot < 0 {
		slot = 0
	}
	value32, err := safeUint32FromUint64(uint64(slot) + 1)
	if err != nil {
		value32 = 1
	}
	value := uint32ToIPv4(value32)
	return fmt.Sprintf(
		"06:00:%02x:%02x:%02x:%02x",
		value[0],
		value[1],
		value[2],
		value[3],
	)
}

func copyFile(src, dst string, mode os.FileMode) error {
	// Try a copy-on-write clone first — on APFS (macOS) and btrfs/xfs/reflink
	// filesystems this returns in milliseconds regardless of file size. Falls
	// back to a regular byte copy elsewhere (e.g. ext4 without reflink).
	_ = os.Remove(dst)
	//nolint:gosec // paths are internal, controlled by server.
	if err := exec.Command("cp", "-c", src, dst).Run(); err == nil {
		if err := os.Chmod(dst, mode); err != nil {
			return fmt.Errorf("chmod %s: %w", dst, err)
		}
		return nil
	}
	// Fallback: plain byte copy.
	//nolint:gosec // Source path is an explicit runtime artifact path from local configuration.
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() {
		_ = in.Close()
	}()

	//nolint:gosec // Destination path is created under the assistant runtime workdir controlled by the server.
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("open %s: %w", dst, err)
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", dst, err)
	}
	return nil
}

func repoRoot() (string, error) {
	_, filename, _, ok := goruntime.Caller(0)
	if !ok {
		return "", fmt.Errorf("determine repository root: caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..")), nil
}

func firecrackerArchForGOARCH(goarch string) string {
	switch goarch {
	case "arm64":
		return "aarch64"
	default:
		return "x86_64"
	}
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "", "localhost":
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return ip.IsLoopback()
}

func truncateForMetadata(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func safeUint32FromUint64(v uint64) (uint32, error) {
	if v > math.MaxUint32 {
		return 0, fmt.Errorf("value %d exceeds uint32", v)
	}
	return uint32(v), nil
}

func shellEscape(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
