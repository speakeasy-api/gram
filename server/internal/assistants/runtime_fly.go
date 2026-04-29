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

	backoff "github.com/cenkalti/backoff/v5"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/superfly/fly-go"
	"github.com/superfly/fly-go/flaps"
	"github.com/superfly/fly-go/tokens"
)

const (
	defaultFlyRuntimeAPIURL         = "https://api.fly.io"
	defaultFlyRuntimeMachinesURL    = "https://api.machines.dev"
	defaultFlyRuntimeHealthTimeout  = 45 * time.Second
	defaultFlyRuntimeRequestTimeout = 2 * time.Minute

	flyMachineMetadataAssistantID   = "gram_assistant_id"
	flyMachineMetadataProjectID     = "gram_assistant_project_id"
	flyMachineMetadataThreadID      = "gram_assistant_thread_id"
	flyMachineMetadataRole          = "gram_role"
	flyMachineMetadataRoleAssistant = "assistant_runtime"
)

type flyRuntimeMetadata struct {
	AppName    string `json:"app_name"`
	AppURL     string `json:"app_url"`
	AppIP      string `json:"app_ip,omitempty"`
	MachineID  string `json:"machine_id"`
	Region     string `json:"region"`
	LastBootID string `json:"last_boot_id,omitempty"`
}

type flyRuntimeStateResponse struct {
	Configured bool `json:"configured"`
}

type flyRuntimeAPIClient interface {
	AllocateIPAddress(ctx context.Context, appName string, addrType string, region string, orgID string, network string) (*fly.IPAddress, error)
	AllocateSharedIPAddress(ctx context.Context, appName string) (net.IP, error)
	CreateApp(ctx context.Context, input fly.CreateAppInput) (*fly.App, error)
	DeleteApp(ctx context.Context, appName string) error
	GetApp(ctx context.Context, appName string) (*fly.App, error)
	GetOrganizationBySlug(ctx context.Context, slug string) (*fly.Organization, error)
}

type flyRuntimeFlapsClient interface {
	Get(ctx context.Context, appName, machineID string) (*fly.Machine, error)
	Launch(ctx context.Context, appName string, input fly.LaunchMachineInput) (*fly.Machine, error)
	List(ctx context.Context, appName, state string) ([]*fly.Machine, error)
	Start(ctx context.Context, appName, machineID string, nonce string) (*fly.MachineStartResponse, error)
	Wait(ctx context.Context, appName string, machine *fly.Machine, state string, timeout time.Duration) error
}

type flyRuntimeFlapsFactory interface {
	New(ctx context.Context) (flyRuntimeFlapsClient, error)
}

type flyRuntimeHTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type defaultFlyRuntimeFlapsFactory struct {
	serviceName    string
	serviceVersion string
	tokens         *tokens.Tokens
	logger         *slog.Logger
}

func (f *defaultFlyRuntimeFlapsFactory) New(ctx context.Context) (flyRuntimeFlapsClient, error) {
	client, err := flaps.NewWithOptions(ctx, flaps.NewClientOpts{
		UserAgent: fmt.Sprintf("%s/%s", f.serviceName, f.serviceVersion),
		Tokens:    f.tokens,
		Logger: &assistantFlyLogger{
			logger: f.logger.With(attr.SlogComponent("assistants_flyio_flaps")),
			contextFunc: func() context.Context {
				return ctx
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create fly flaps client: %w", err)
	}
	return client, nil
}

type FlyRuntimeBackend struct {
	logger       *slog.Logger
	config       FlyRuntimeConfig
	client       flyRuntimeAPIClient
	flapsFactory flyRuntimeFlapsFactory
	httpClient   flyRuntimeHTTPDoer
}

func NewFlyRuntimeBackend(logger *slog.Logger, httpPolicy *guardian.Policy, config FlyRuntimeConfig) *FlyRuntimeBackend {
	config.DefaultFlyRegion = firstNonEmpty(config.DefaultFlyRegion, defaultFlyRuntimeRegion)
	config.AppNamePrefix = sanitizeFlyAppNamePrefix(firstNonEmpty(config.AppNamePrefix, defaultFlyRuntimePrefix))
	if config.AppNamePrefix == "" {
		config.AppNamePrefix = defaultFlyRuntimePrefix
	}
	if config.ServiceName == "" {
		config.ServiceName = "gram"
	}
	if config.FlyAPIURL == "" {
		config.FlyAPIURL = defaultFlyRuntimeAPIURL
	}
	if config.FlyMachinesBaseURL == "" {
		config.FlyMachinesBaseURL = defaultFlyRuntimeMachinesURL
	}

	client := fly.NewClientFromOptions(fly.ClientOptions{
		BaseURL: config.FlyAPIURL,
		Tokens:  config.FlyTokens,
		Name:    config.ServiceName,
		Version: config.ServiceVersion,
		Transport: &fly.Transport{
			UnderlyingTransport: httpPolicy.PooledClient(guardian.WithDefaultRetryConfig()).Transport,
			UserAgent:           fmt.Sprintf("%s/%s", config.ServiceName, config.ServiceVersion),
			Tokens:              config.FlyTokens,
		},
		Logger: &assistantFlyLogger{
			logger:      logger.With(attr.SlogComponent("assistants_flyio_client")),
			contextFunc: context.Background,
		},
	})

	return &FlyRuntimeBackend{
		logger: logger.With(attr.SlogComponent("assistants_flyio_runtime")),
		config: config,
		client: client,
		flapsFactory: &defaultFlyRuntimeFlapsFactory{
			serviceName:    config.ServiceName,
			serviceVersion: config.ServiceVersion,
			tokens:         config.FlyTokens,
			logger:         logger,
		},
		// /turn is idempotent (runner dedupes by event-id idempotency key) but
		// retries are driven by Temporal activity policy, not the HTTP client —
		// double layering would just inflate attempt counts on real failures.
		httpClient: httpPolicy.PooledClient(),
	}
}

func (f *FlyRuntimeBackend) Backend() string {
	return runtimeBackendFlyIO
}

func (f *FlyRuntimeBackend) SupportsBackend(backend string) bool {
	return backend == runtimeBackendFlyIO
}

// Ensure does not auto-recreate the app on ensureExisting errors. Health and
// configure timeouts must bubble so Temporal retries drive convergence.
// Destructive app recreation churns Fly's allocated IPs and creates DNS-stale
// feedback loops.
func (f *FlyRuntimeBackend) Ensure(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendEnsureResult, error) {
	if err := validateRuntimeBackend(f, runtime.Backend); err != nil {
		return RuntimeBackendEnsureResult{}, err
	}

	flapsClient, err := f.flapsFactory.New(ctx)
	if err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("create fly runtime flaps client: %w", err)
	}

	metadata, err := decodeFlyRuntimeMetadata(runtime.BackendMetadataJSON)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}

	return f.ensureExisting(ctx, runtime, flapsClient, metadata)
}

func (f *FlyRuntimeBackend) ensureExisting(
	ctx context.Context,
	runtime assistantRuntimeRecord,
	flapsClient flyRuntimeFlapsClient,
	metadata flyRuntimeMetadata,
) (RuntimeBackendEnsureResult, error) {
	appName := metadata.AppName
	if appName == "" {
		appName = f.appName(runtime.AssistantThreadID)
	}

	appURL := metadata.AppURL
	if appURL == "" {
		appURL = flyRuntimeAppURL(appName)
	}

	appIP, err := f.ensureApp(ctx, appName)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	if appIP == "" {
		appIP = metadata.AppIP
	}

	machine, err := f.resolveMachine(ctx, flapsClient, appName, runtime.AssistantThreadID, metadata.MachineID)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}

	coldStart := false
	if machine == nil {
		machine, err = f.launchMachine(ctx, flapsClient, runtime, appName)
		if err != nil {
			return RuntimeBackendEnsureResult{}, err
		}
		coldStart = true
	}

	if !machine.IsActive() {
		if _, err := flapsClient.Start(ctx, appName, machine.ID, ""); err != nil {
			return RuntimeBackendEnsureResult{}, fmt.Errorf("start assistant fly runtime machine: %w", err)
		}
		if err := flapsClient.Wait(ctx, appName, machine, "started", defaultFlyRuntimeHealthTimeout); err != nil {
			return RuntimeBackendEnsureResult{}, fmt.Errorf("wait for assistant fly runtime machine start: %w", err)
		}
		coldStart = true
		refreshed, getErr := flapsClient.Get(ctx, appName, machine.ID)
		if getErr == nil && refreshed != nil {
			machine = refreshed
		}
	}

	target := flyRuntimeTarget{URL: appURL, IP: appIP}
	if err := f.waitForRuntimeHealth(ctx, target); err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("wait for assistant fly runtime health: %w", err)
	}

	state, err := f.runtimeState(ctx, target)
	if err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("load assistant fly runtime state: %w", err)
	}

	if machine.InstanceID != "" && machine.InstanceID != metadata.LastBootID {
		coldStart = true
	}

	needsConfigure := !state.Configured
	if needsConfigure {
		coldStart = true
	}

	nextMetadata := flyRuntimeMetadata{
		AppName:    appName,
		AppURL:     appURL,
		AppIP:      appIP,
		MachineID:  machine.ID,
		Region:     firstNonEmpty(machine.Region, metadata.Region, f.config.DefaultFlyRegion),
		LastBootID: machine.InstanceID,
	}

	rawMetadata, err := json.Marshal(nextMetadata)
	if err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("marshal assistant fly runtime metadata: %w", err)
	}

	return RuntimeBackendEnsureResult{
		ColdStart:           coldStart,
		NeedsConfigure:      needsConfigure,
		BackendMetadataJSON: rawMetadata,
	}, nil
}

// ensureApp returns the app's shared v4 IP so callers can dial it directly
// instead of waiting on public DNS propagation (see flyRuntimeTarget).
func (f *FlyRuntimeBackend) ensureApp(ctx context.Context, appName string) (string, error) {
	app, err := f.client.GetApp(ctx, appName)
	switch {
	case err == nil:
		return app.SharedIPAddress, nil
	case isFlyNotFound(err):
		org, err := f.client.GetOrganizationBySlug(ctx, f.config.DefaultFlyOrg)
		if err != nil {
			return "", fmt.Errorf("resolve assistant fly runtime organization: %w", err)
		}
		_, err = f.client.CreateApp(ctx, fly.CreateAppInput{
			OrganizationID:  org.ID,
			Name:            appName,
			PreferredRegion: new(f.config.DefaultFlyRegion),
		})
		if err != nil && !isFlyAppNameTaken(err) {
			return "", fmt.Errorf("create assistant fly runtime app: %w", err)
		}
		if _, getErr := f.client.GetApp(ctx, appName); getErr != nil {
			return "", fmt.Errorf("verify assistant fly runtime app: %w", getErr)
		}
		ip, err := f.client.AllocateSharedIPAddress(ctx, appName)
		if err != nil && !isFlyIPAlreadyAssigned(err) {
			return "", fmt.Errorf("allocate assistant fly runtime shared ip: %w", err)
		}
		ipStr := ""
		if ip != nil {
			ipStr = ip.String()
		}
		// Dedicated v6 in addition to the shared v4. Fresh apps with only a
		// shared v4 can take minutes for `<app>.fly.dev` A records to
		// propagate, blowing past waitForRuntimeHealth's budget. Allocating
		// a v6 forces an immediate DNS publish for both records.
		if _, err := f.client.AllocateIPAddress(ctx, appName, "v6", "", org.ID, ""); err != nil && !isFlyIPAlreadyAssigned(err) {
			return "", fmt.Errorf("allocate assistant fly runtime v6 ip: %w", err)
		}
		if ipStr == "" {
			// IP was already allocated previously (isFlyIPAlreadyAssigned path
			// or nil return); fetch it from the app record.
			if refreshed, getErr := f.client.GetApp(ctx, appName); getErr == nil {
				ipStr = refreshed.SharedIPAddress
			}
		}
		return ipStr, nil
	default:
		return "", fmt.Errorf("load assistant fly runtime app: %w", err)
	}
}

func (f *FlyRuntimeBackend) resolveMachine(
	ctx context.Context,
	flapsClient flyRuntimeFlapsClient,
	appName string,
	threadID uuid.UUID,
	machineID string,
) (*fly.Machine, error) {
	wantThreadID := threadID.String()

	if machineID != "" {
		machine, err := flapsClient.Get(ctx, appName, machineID)
		switch {
		case err == nil:
			if !machineBelongsToThread(machine, wantThreadID) {
				return nil, fmt.Errorf("assistant fly runtime machine %s does not belong to thread %s", machineID, wantThreadID)
			}
			return machine, nil
		case !isFlyNotFound(err):
			return nil, fmt.Errorf("load assistant fly runtime machine: %w", err)
		}
	}

	machines, err := flapsClient.List(ctx, appName, "")
	if err != nil {
		return nil, fmt.Errorf("list assistant fly runtime machines: %w", err)
	}

	for _, machine := range machines {
		if machine == nil || !machine.IsActive() {
			continue
		}
		if machineBelongsToThread(machine, wantThreadID) {
			return machine, nil
		}
	}
	return nil, nil
}

func machineBelongsToThread(machine *fly.Machine, threadID string) bool {
	if machine == nil || machine.Config == nil {
		return false
	}
	return machine.Config.Metadata[flyMachineMetadataThreadID] == threadID
}

func (f *FlyRuntimeBackend) launchMachine(
	ctx context.Context,
	flapsClient flyRuntimeFlapsClient,
	runtime assistantRuntimeRecord,
	appName string,
) (*fly.Machine, error) {
	input := fly.LaunchMachineInput{
		Config:              f.machineConfig(runtime),
		Region:              f.config.DefaultFlyRegion,
		Name:                "assistant-" + shortRuntimeName(runtime.AssistantThreadID),
		SkipLaunch:          false,
		RequiresReplacement: true,
	}

	machine, err := f.launchMachineWithRetry(ctx, flapsClient, appName, input)
	if err != nil {
		return nil, fmt.Errorf("launch assistant fly runtime machine: %w", err)
	}
	if err := flapsClient.Wait(ctx, appName, machine, "started", defaultFlyRuntimeHealthTimeout); err != nil {
		return nil, fmt.Errorf("wait for assistant fly runtime machine launch: %w", err)
	}
	return machine, nil
}

func (f *FlyRuntimeBackend) launchMachineWithRetry(
	ctx context.Context,
	flapsClient flyRuntimeFlapsClient,
	appName string,
	input fly.LaunchMachineInput,
) (*fly.Machine, error) {
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = time.Second
	bo.MaxInterval = 8 * time.Second
	bo.Multiplier = 2

	machine, err := backoff.Retry(ctx, func() (*fly.Machine, error) {
		machine, err := flapsClient.Launch(ctx, appName, input)
		if err == nil {
			return machine, nil
		}
		if !isFlyAppPropagating(err) {
			return nil, backoff.Permanent(fmt.Errorf("launch assistant fly runtime machine: %w", err))
		}
		return nil, fmt.Errorf("launch assistant fly runtime machine: %w", err)
	}, backoff.WithBackOff(bo), backoff.WithMaxTries(5))
	if err != nil {
		return nil, fmt.Errorf("retry launch assistant fly runtime machine: %w", err)
	}
	return machine, nil
}

func (f *FlyRuntimeBackend) Configure(ctx context.Context, runtime assistantRuntimeRecord, config runtimeStartupConfig) error {
	if err := validateRuntimeBackend(f, runtime.Backend); err != nil {
		return err
	}
	metadata, err := decodeFlyRuntimeMetadata(runtime.BackendMetadataJSON)
	if err != nil {
		return err
	}
	if metadata.AppURL == "" {
		return fmt.Errorf("assistant fly runtime app url is not available")
	}

	body, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal assistant fly runtime config: %w", err)
	}
	_, err = f.runtimeRequest(ctx, targetFromMetadata(metadata), runtimeHTTPRequest{
		Method:         http.MethodPost,
		Path:           "/configure",
		ContentType:    "application/json",
		Body:           body,
		MaxTimeSeconds: 0,
		IdempotencyKey: "",
	})
	if err != nil {
		return fmt.Errorf("configure assistant fly runtime: %w", err)
	}
	return nil
}

func (f *FlyRuntimeBackend) RunTurn(ctx context.Context, runtime assistantRuntimeRecord, idempotencyKey string, authToken string, history []runtimeMessage, prompt string) error {
	if err := validateRuntimeBackend(f, runtime.Backend); err != nil {
		return err
	}
	metadata, err := decodeFlyRuntimeMetadata(runtime.BackendMetadataJSON)
	if err != nil {
		return err
	}
	if metadata.AppURL == "" {
		return fmt.Errorf("assistant fly runtime app url is not available")
	}

	reqBody, err := json.Marshal(runtimeTurnRequest{
		History:   history,
		Input:     prompt,
		AuthToken: authToken,
	})
	if err != nil {
		return fmt.Errorf("marshal assistant fly runtime turn request: %w", err)
	}

	body, err := f.runtimeRequest(ctx, targetFromMetadata(metadata), runtimeHTTPRequest{
		Method:         http.MethodPost,
		Path:           "/turn",
		ContentType:    "application/json",
		Body:           reqBody,
		IdempotencyKey: idempotencyKey,
		MaxTimeSeconds: 30 * 60,
	})
	if err != nil {
		return fmt.Errorf("%w: execute fly turn request: %w", classifyTurnError(err), err)
	}

	var turnResp runtimeTurnResponse
	if err := json.Unmarshal(body, &turnResp); err != nil {
		return fmt.Errorf("decode assistant fly runtime turn response: %w; body=%s", err, truncateForMetadata(string(body), 16*1024))
	}
	if turnResp.Error != "" {
		return fmt.Errorf("assistant fly runtime turn error: %s", turnResp.Error)
	}
	return nil
}

// classifyTurnError distinguishes upstream completion failures (provider
// rejected the request — replaying it won't help) from real runtime
// problems (VM gone, connection refused, runner crashed). Only the latter
// should churn the Fly app; surfacing every provider 4xx as
// ErrRuntimeUnhealthy nukes-and-respawns the VM on each retry, producing
// thousands of dead assistant_runtimes rows on a single bad input.
//
// Match is body-substring based because the runner wraps the upstream error
// before returning it; agentkit-provider-openrouter prefixes failed Gram
// completion calls with "provider error", and Gram's own gateway path
// stamps "completion failed" with oops.CodeGatewayError.
func classifyTurnError(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "provider error") || strings.Contains(msg, "completion failed") {
		return ErrCompletionFailed
	}
	return ErrRuntimeUnhealthy
}

func (f *FlyRuntimeBackend) ServerURL(_ context.Context, runtime assistantRuntimeRecord, raw *url.URL) (*url.URL, error) {
	if err := validateRuntimeBackend(f, runtime.Backend); err != nil {
		return nil, err
	}

	candidate := raw
	if f.config.ServerURLOverride != nil {
		candidate = f.config.ServerURLOverride
	}
	if candidate == nil {
		return nil, fmt.Errorf("assistant runtime server URL is not configured")
	}
	if host := candidate.Hostname(); host == "" || isLoopbackHost(host) {
		return nil, fmt.Errorf("assistant fly runtime requires a public --assistant-runtime-server-url or --server-url; got %q", candidate.String())
	}

	cloned := *candidate
	return &cloned, nil
}

func (f *FlyRuntimeBackend) Stop(ctx context.Context, runtime assistantRuntimeRecord) error {
	if err := validateRuntimeBackend(f, runtime.Backend); err != nil {
		return err
	}
	metadata, err := decodeFlyRuntimeMetadata(runtime.BackendMetadataJSON)
	if err != nil {
		return err
	}
	if metadata.AppName == "" {
		return nil
	}
	return f.deleteApp(ctx, metadata.AppName)
}

func (f *FlyRuntimeBackend) deleteApp(ctx context.Context, appName string) error {
	if err := f.client.DeleteApp(ctx, appName); err != nil && !isFlyNotFound(err) {
		return fmt.Errorf("delete assistant fly runtime app: %w", err)
	}
	return nil
}

func (f *FlyRuntimeBackend) machineConfig(runtime assistantRuntimeRecord) *fly.MachineConfig {
	stop := fly.MachineAutostopOff
	autostart := true
	return &fly.MachineConfig{
		Image: fmt.Sprintf("%s:%s", f.config.OCIImage, f.config.ImageVersion),
		Env: map[string]string{
			"GRAM_ASSISTANT_ID":         runtime.AssistantID.String(),
			"GRAM_ASSISTANT_PROJECT_ID": runtime.ProjectID.String(),
			"GRAM_ASSISTANT_THREAD_ID":  runtime.AssistantThreadID.String(),
		},
		Guest: &fly.MachineGuest{
			CPUKind:       "shared",
			CPUs:          2,
			MemoryMB:      1024,
			PersistRootfs: fly.MachinePersistRootfsNever,
		},
		Metadata: map[string]string{
			fly.MachineConfigMetadataKeyFlyPlatformVersion: "v2",
			fly.MachineConfigMetadataKeyFlyProcessGroup:    flyMachineMetadataRoleAssistant,
			flyMachineMetadataAssistantID:                  runtime.AssistantID.String(),
			flyMachineMetadataProjectID:                    runtime.ProjectID.String(),
			flyMachineMetadataThreadID:                     runtime.AssistantThreadID.String(),
			flyMachineMetadataRole:                         flyMachineMetadataRoleAssistant,
		},
		Services: []fly.MachineService{
			{
				Protocol:           "tcp",
				InternalPort:       defaultRuntimeGuestPort,
				Autostop:           &stop,
				Autostart:          &autostart,
				MinMachinesRunning: new(0),
				Ports: []fly.MachinePort{
					{
						Handlers: []string{"tls"},
						Port:     new(443),
					},
				},
				Checks: []fly.MachineServiceCheck{
					{
						Type:         new("http"),
						HTTPProtocol: new("http"),
						HTTPMethod:   new(http.MethodGet),
						HTTPPath:     new("/healthz"),
						Interval:     &fly.Duration{Duration: 15 * time.Second},
						Timeout:      &fly.Duration{Duration: 5 * time.Second},
						GracePeriod:  &fly.Duration{Duration: 5 * time.Second},
					},
				},
			},
		},
		Restart: &fly.MachineRestart{
			Policy:     "on-failure",
			MaxRetries: 3,
		},
	}
}

// flyRuntimeTarget bundles the app URL with its allocated public IP so HTTP
// calls can bypass public DNS — a freshly created app's A record can take
// 30-60s to propagate, but the shared IP is routable the moment Fly's proxy
// sees the allocation. Dial the IP directly, keep the hostname on the URL so
// SNI + wildcard cert verification still pass.
type flyRuntimeTarget struct {
	URL string
	IP  string
}

func targetFromMetadata(md flyRuntimeMetadata) flyRuntimeTarget {
	return flyRuntimeTarget{URL: md.AppURL, IP: md.AppIP}
}

func (f *FlyRuntimeBackend) waitForRuntimeHealth(ctx context.Context, target flyRuntimeTarget) error {
	deadline := time.Now().Add(defaultFlyRuntimeHealthTimeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("runtime health check timed out")
		}
		if ctx.Err() != nil {
			return fmt.Errorf("wait for runtime health: %w", ctx.Err())
		}
		if _, err := f.runtimeRequest(ctx, target, runtimeHTTPRequest{
			Method:         http.MethodGet,
			Path:           "/healthz",
			ContentType:    "",
			Body:           nil,
			MaxTimeSeconds: 0,
			IdempotencyKey: "",
		}); err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (f *FlyRuntimeBackend) runtimeState(ctx context.Context, target flyRuntimeTarget) (flyRuntimeStateResponse, error) {
	body, err := f.runtimeRequest(ctx, target, runtimeHTTPRequest{
		Method:         http.MethodGet,
		Path:           "/state",
		ContentType:    "",
		Body:           nil,
		MaxTimeSeconds: 0,
		IdempotencyKey: "",
	})
	if err != nil {
		return flyRuntimeStateResponse{}, err
	}
	var state flyRuntimeStateResponse
	if err := json.Unmarshal(body, &state); err != nil {
		return flyRuntimeStateResponse{}, fmt.Errorf("decode assistant fly runtime state: %w", err)
	}
	return state, nil
}

// clientForTarget used to dial target.IP directly to bypass public DNS
// propagation on fresh apps. Doesn't work on shared Fly IPs: the edge
// accepts TLS but drops the backend with EOF until the SNI→app mapping
// finishes registering — same propagation window as DNS. Kept as a hook
// point; a future dedicated-IP or single-app-many-machines design (see
// plan B) can flip the pinning back on.
func (f *FlyRuntimeBackend) clientForTarget(_ flyRuntimeTarget) flyRuntimeHTTPDoer {
	return f.httpClient
}

func (f *FlyRuntimeBackend) runtimeRequest(ctx context.Context, target flyRuntimeTarget, request runtimeHTTPRequest) ([]byte, error) {
	reqCtx, cancel := runtimeRequestContext(ctx, request.MaxTimeSeconds, defaultFlyRuntimeRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, request.Method, strings.TrimRight(target.URL, "/")+request.Path, bytes.NewReader(request.Body))
	if err != nil {
		return nil, fmt.Errorf("build assistant fly runtime request: %w", err)
	}
	if request.ContentType != "" {
		req.Header.Set("Content-Type", request.ContentType)
	}
	if request.IdempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", request.IdempotencyKey)
	}

	client := f.clientForTarget(target)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send assistant fly runtime request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read assistant fly runtime response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (f *FlyRuntimeBackend) appName(threadID uuid.UUID) string {
	return fmt.Sprintf("%s-%s", f.config.AppNamePrefix, strings.ToLower(threadID.String()))
}

func decodeFlyRuntimeMetadata(raw []byte) (flyRuntimeMetadata, error) {
	if len(raw) == 0 {
		return flyRuntimeMetadata{
			AppName:    "",
			AppURL:     "",
			AppIP:      "",
			MachineID:  "",
			Region:     "",
			LastBootID: "",
		}, nil
	}
	var metadata flyRuntimeMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return flyRuntimeMetadata{}, fmt.Errorf("decode assistant fly runtime metadata: %w", err)
	}
	return metadata, nil
}

func shortRuntimeName(id uuid.UUID) string {
	raw := strings.ReplaceAll(strings.ToLower(id.String()), "-", "")
	if len(raw) > 12 {
		return raw[:12]
	}
	return raw
}

func flyRuntimeAppURL(appName string) string {
	return fmt.Sprintf("https://%s.fly.dev", appName)
}

func sanitizeFlyAppNamePrefix(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	var out strings.Builder
	lastDash := false
	for _, ch := range raw {
		switch {
		case ch >= 'a' && ch <= 'z':
			out.WriteRune(ch)
			lastDash = false
		case ch >= '0' && ch <= '9':
			out.WriteRune(ch)
			lastDash = false
		case ch == '-':
			if !lastDash {
				out.WriteRune(ch)
				lastDash = true
			}
		}
	}
	return strings.Trim(out.String(), "-")
}

func isFlyNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "could not find") ||
		strings.Contains(msg, "no rows in result set")
}

func isFlyAppNameTaken(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "name has already been taken")
}

func isFlyIPAlreadyAssigned(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already allocated") || strings.Contains(msg, "already has")
}

// isFlyAppPropagating reports whether the error indicates the Machines API
// hasn't yet seen the freshly created app — a transient state worth retrying.
func isFlyAppPropagating(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no rows in result set") || strings.Contains(msg, "failed to get app")
}

type assistantFlyLogger struct {
	logger      *slog.Logger
	contextFunc func() context.Context
}

func (f *assistantFlyLogger) Debug(v ...any) {
	f.logger.DebugContext(f.contextFunc(), fmt.Sprint(v...))
}

func (f *assistantFlyLogger) Debugf(format string, v ...any) {
	f.logger.DebugContext(f.contextFunc(), fmt.Sprintf(format, v...))
}
