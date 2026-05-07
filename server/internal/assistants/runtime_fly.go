package assistants

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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

// errFlyAppCorrupted signals the Fly Machines API reports the app missing on
// an established runtime — i.e. flaps 404s after we previously launched a
// machine on this app. GraphQL/orchestrator and the Machines backend have
// drifted; the only reliable recovery is to destroy + recreate. Distinct from
// the create-time propagation lag covered by isFlyAppPropagating.
var errFlyAppCorrupted = errors.New("assistant fly runtime app corrupted")

type flyRuntimeMetadata struct {
	AppName    string `json:"app_name"`
	AppID      string `json:"app_id,omitempty"`
	AppURL     string `json:"app_url"`
	AppIP      string `json:"app_ip,omitempty"`
	MachineID  string `json:"machine_id"`
	Region     string `json:"region"`
	LastBootID string `json:"last_boot_id,omitempty"`
}

type flyRuntimeAppIdentity struct {
	Name     string
	ID       string
	SharedIP string
	Created  bool
}

// runnerStateResponse mirrors agents/runner/src/wire.rs::RunnerStateResponse.
// IdleSeconds is `0` while a turn is in flight (the runner clears its idle
// clock on /turn enqueue) and absent only when the runner has never been
// /configured. Both shapes are the source the manager polls to gate expiry.
type runnerStateResponse struct {
	Configured  bool    `json:"configured"`
	IdleSeconds *uint64 `json:"idle_seconds,omitempty"`
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
	Stop(ctx context.Context, appName string, in fly.StopMachineInput, nonce string) error
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
	tracer       trace.Tracer
	config       FlyRuntimeConfig
	client       flyRuntimeAPIClient
	flapsFactory flyRuntimeFlapsFactory
	httpClient   flyRuntimeHTTPDoer
}

func NewFlyRuntimeBackend(logger *slog.Logger, tracerProvider trace.TracerProvider, httpPolicy *guardian.Policy, config FlyRuntimeConfig) *FlyRuntimeBackend {
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
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/assistants"),
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

	app, err := f.tracedEnsureApp(ctx, appName)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	appIP := app.SharedIP
	if appIP == "" {
		appIP = metadata.AppIP
	}
	sameAppIncarnation := metadata.AppID != "" && app.ID != "" && metadata.AppID == app.ID
	if app.Created || (metadata.AppID != "" && app.ID != "" && metadata.AppID != app.ID) {
		// Metadata may have been recovered from a prior runtime row after a
		// failed/corrupted app was deleted. Once the app has been recreated,
		// old machine/boot IDs describe the previous app incarnation and must
		// not make normal Machines propagation look like established drift.
		metadata.MachineID = ""
		metadata.LastBootID = ""
	}

	machine, err := f.tracedResolveMachine(ctx, flapsClient, appName, runtime.AssistantThreadID, metadata.MachineID, metadata.LastBootID, sameAppIncarnation)
	if err != nil {
		if errors.Is(err, errFlyAppCorrupted) {
			f.logger.WarnContext(ctx,
				"assistant fly runtime app corrupted, tearing down for recreate",
				attr.SlogFlyAppName(appName),
				attr.SlogAssistantThreadID(runtime.AssistantThreadID.String()),
			)
			if delErr := f.deleteApp(ctx, appName); delErr != nil && !isFlyNotFound(delErr) {
				f.logger.WarnContext(ctx,
					"delete corrupted assistant fly runtime app failed",
					attr.SlogFlyAppName(appName),
					attr.SlogError(delErr),
				)
			}
		}
		return RuntimeBackendEnsureResult{}, err
	}

	coldStart := false
	if machine == nil {
		coldStart = true
		launched, err := f.tracedLaunchMachine(ctx, flapsClient, runtime, appName)
		if err != nil {
			return RuntimeBackendEnsureResult{}, err
		}
		machine = launched
		if err := f.tracedWaitStarted(ctx, flapsClient, appName, machine, coldStart); err != nil {
			return RuntimeBackendEnsureResult{}, fmt.Errorf("wait for assistant fly runtime machine launch: %w", err)
		}
	}

	if !machine.IsActive() {
		coldStart = true
		if _, err := flapsClient.Start(ctx, appName, machine.ID, ""); err != nil {
			return RuntimeBackendEnsureResult{}, fmt.Errorf("start assistant fly runtime machine: %w", err)
		}
		if err := f.tracedWaitStarted(ctx, flapsClient, appName, machine, coldStart); err != nil {
			return RuntimeBackendEnsureResult{}, fmt.Errorf("wait for assistant fly runtime machine start: %w", err)
		}
		refreshed, getErr := flapsClient.Get(ctx, appName, machine.ID)
		if getErr == nil && refreshed != nil {
			machine = refreshed
		}
	}

	if !coldStart && machine.InstanceID != "" && machine.InstanceID != metadata.LastBootID {
		coldStart = true
	}

	target := flyRuntimeTarget{URL: appURL, IP: appIP}
	if err := f.tracedWaitHealth(ctx, target, coldStart); err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("wait for assistant fly runtime health: %w", err)
	}

	state, err := f.tracedRuntimeState(ctx, target, coldStart)
	if err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("load assistant fly runtime state: %w", err)
	}

	needsConfigure := !state.Configured
	if needsConfigure {
		coldStart = true
	}

	nextMetadata := flyRuntimeMetadata{
		AppName:    appName,
		AppID:      app.ID,
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

// ensureApp returns the app identity and shared v4 IP so callers can dial it
// directly instead of waiting on public DNS propagation (see flyRuntimeTarget).
func (f *FlyRuntimeBackend) ensureApp(ctx context.Context, appName string) (flyRuntimeAppIdentity, error) {
	app, err := f.client.GetApp(ctx, appName)
	switch {
	case err == nil:
		return flyRuntimeAppIdentity{Name: app.Name, ID: app.ID, SharedIP: app.SharedIPAddress, Created: false}, nil
	case isFlyNotFound(err):
		org, err := f.client.GetOrganizationBySlug(ctx, f.config.DefaultFlyOrg)
		if err != nil {
			return flyRuntimeAppIdentity{}, fmt.Errorf("resolve assistant fly runtime organization: %w", err)
		}
		created := true
		_, err = f.client.CreateApp(ctx, fly.CreateAppInput{
			OrganizationID:  org.ID,
			Name:            appName,
			PreferredRegion: new(f.config.DefaultFlyRegion),
		})
		if err != nil {
			if !isFlyAppNameTaken(err) {
				return flyRuntimeAppIdentity{}, fmt.Errorf("create assistant fly runtime app: %w", err)
			}
			created = false
		}
		verified, getErr := f.client.GetApp(ctx, appName)
		if getErr != nil {
			return flyRuntimeAppIdentity{}, fmt.Errorf("verify assistant fly runtime app: %w", getErr)
		}
		ip, err := f.client.AllocateSharedIPAddress(ctx, appName)
		if err != nil && !isFlyIPAlreadyAssigned(err) {
			return flyRuntimeAppIdentity{}, fmt.Errorf("allocate assistant fly runtime shared ip: %w", err)
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
			return flyRuntimeAppIdentity{}, fmt.Errorf("allocate assistant fly runtime v6 ip: %w", err)
		}
		if ipStr == "" {
			// IP was already allocated previously (isFlyIPAlreadyAssigned path
			// or nil return); fetch it from the app record.
			if refreshed, getErr := f.client.GetApp(ctx, appName); getErr == nil {
				ipStr = refreshed.SharedIPAddress
			}
		}
		return flyRuntimeAppIdentity{Name: verified.Name, ID: verified.ID, SharedIP: ipStr, Created: created}, nil
	default:
		return flyRuntimeAppIdentity{}, fmt.Errorf("load assistant fly runtime app: %w", err)
	}
}

func (f *FlyRuntimeBackend) resolveMachine(
	ctx context.Context,
	flapsClient flyRuntimeFlapsClient,
	appName string,
	threadID uuid.UUID,
	machineID string,
	lastBootID string,
	sameAppIncarnation bool,
) (*fly.Machine, error) {
	wantThreadID := threadID.String()
	hadPriorBoot := sameAppIncarnation && (lastBootID != "" || machineID != "")

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
		// Established runtime + flaps notFound = backend drift, not propagation
		// lag. ensureApp just confirmed the app via GraphQL, so the two Fly
		// backends disagree and only a destroy+recreate clears it.
		if isFlyNotFound(err) && hadPriorBoot {
			return nil, errFlyAppCorrupted
		}
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
	if _, err := f.runtimeRequest(ctx, targetFromMetadata(metadata), runtimeHTTPRequest{
		Method:         http.MethodPost,
		Path:           "/configure",
		ContentType:    "application/json",
		Body:           body,
		MaxTimeSeconds: 0,
		IdempotencyKey: "",
	}); err != nil {
		return fmt.Errorf("configure assistant fly runtime: %w", err)
	}
	return nil
}

func (f *FlyRuntimeBackend) tracedEnsureApp(ctx context.Context, appName string) (app flyRuntimeAppIdentity, err error) {
	ctx, span := f.tracer.Start(ctx, "assistants.runtime.ensureApp")
	defer func() {
		if err != nil {
			span.SetAttributes(attr.AssistantSetupFailureClass(classifySetupError(err)))
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	app, err = f.ensureApp(ctx, appName)
	span.SetAttributes(attr.AssistantAppCreated(app.Created))
	return app, err
}

func (f *FlyRuntimeBackend) tracedResolveMachine(
	ctx context.Context,
	flapsClient flyRuntimeFlapsClient,
	appName string,
	threadID uuid.UUID,
	machineID string,
	lastBootID string,
	sameAppIncarnation bool,
) (machine *fly.Machine, err error) {
	ctx, span := f.tracer.Start(ctx, "assistants.runtime.resolveMachine")
	defer func() {
		if err != nil {
			span.SetAttributes(attr.AssistantSetupFailureClass(classifySetupError(err)))
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	return f.resolveMachine(ctx, flapsClient, appName, threadID, machineID, lastBootID, sameAppIncarnation)
}

func (f *FlyRuntimeBackend) tracedLaunchMachine(
	ctx context.Context,
	flapsClient flyRuntimeFlapsClient,
	runtime assistantRuntimeRecord,
	appName string,
) (machine *fly.Machine, err error) {
	ctx, span := f.tracer.Start(ctx, "assistants.runtime.launchMachine",
		trace.WithAttributes(attr.AssistantColdStart(true)),
	)
	defer func() {
		if err != nil {
			span.SetAttributes(attr.AssistantSetupFailureClass(classifySetupError(err)))
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	return f.launchMachine(ctx, flapsClient, runtime, appName)
}

func (f *FlyRuntimeBackend) tracedWaitStarted(
	ctx context.Context,
	flapsClient flyRuntimeFlapsClient,
	appName string,
	machine *fly.Machine,
	coldStart bool,
) (err error) {
	ctx, span := f.tracer.Start(ctx, "assistants.runtime.waitStarted",
		trace.WithAttributes(attr.AssistantColdStart(coldStart)),
	)
	defer func() {
		if err != nil {
			span.SetAttributes(attr.AssistantSetupFailureClass(classifySetupError(err)))
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if waitErr := flapsClient.Wait(ctx, appName, machine, "started", defaultFlyRuntimeHealthTimeout); waitErr != nil {
		return fmt.Errorf("flaps wait started: %w", waitErr)
	}
	return nil
}

func (f *FlyRuntimeBackend) tracedWaitHealth(ctx context.Context, target flyRuntimeTarget, coldStart bool) (err error) {
	ctx, span := f.tracer.Start(ctx, "assistants.runtime.waitHealth",
		trace.WithAttributes(attr.AssistantColdStart(coldStart)),
	)
	defer func() {
		if err != nil {
			span.SetAttributes(attr.AssistantSetupFailureClass(classifySetupError(err)))
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	return f.waitForRuntimeHealth(ctx, target)
}

func (f *FlyRuntimeBackend) tracedRuntimeState(ctx context.Context, target flyRuntimeTarget, coldStart bool) (state runnerStateResponse, err error) {
	ctx, span := f.tracer.Start(ctx, "assistants.runtime.runtimeState",
		trace.WithAttributes(attr.AssistantColdStart(coldStart)),
	)
	defer func() {
		if err != nil {
			span.SetAttributes(attr.AssistantSetupFailureClass(classifySetupError(err)))
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	return f.runtimeState(ctx, target)
}

func (f *FlyRuntimeBackend) RunTurn(ctx context.Context, runtime assistantRuntimeRecord, idempotencyKey string, authToken string, prompt string) error {
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
		Input:     prompt,
		AuthToken: authToken,
	})
	if err != nil {
		return fmt.Errorf("marshal assistant fly runtime turn request: %w", err)
	}

	if _, err := f.runtimeRequest(ctx, targetFromMetadata(metadata), runtimeHTTPRequest{
		Method:         http.MethodPost,
		Path:           "/turn",
		ContentType:    "application/json",
		Body:           reqBody,
		IdempotencyKey: idempotencyKey,
		MaxTimeSeconds: 30 * 60,
	}); err != nil {
		return fmt.Errorf("%w: execute fly turn request: %w", classifyTurnError(err), err)
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

func (f *FlyRuntimeBackend) Status(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	if err := validateRuntimeBackend(f, runtime.Backend); err != nil {
		return RuntimeBackendStatus{}, err
	}
	metadata, err := decodeFlyRuntimeMetadata(runtime.BackendMetadataJSON)
	if err != nil {
		return RuntimeBackendStatus{}, err
	}
	if metadata.AppURL == "" {
		return RuntimeBackendStatus{}, fmt.Errorf("assistant fly runtime app url is not available")
	}
	state, err := f.runtimeState(ctx, targetFromMetadata(metadata))
	if err != nil {
		return RuntimeBackendStatus{}, fmt.Errorf("load assistant fly runtime state: %w", err)
	}
	return RuntimeBackendStatus(state), nil
}

// Stop pauses the machine but preserves the app, allocated IP, and backend
// metadata so a subsequent admit can resume the same incarnation.
func (f *FlyRuntimeBackend) Stop(ctx context.Context, runtime assistantRuntimeRecord) error {
	if err := validateRuntimeBackend(f, runtime.Backend); err != nil {
		return err
	}
	metadata, err := decodeFlyRuntimeMetadata(runtime.BackendMetadataJSON)
	if err != nil {
		return err
	}
	if metadata.AppName == "" || metadata.MachineID == "" {
		return nil
	}

	flapsClient, err := f.flapsFactory.New(ctx)
	if err != nil {
		return fmt.Errorf("create fly runtime flaps client: %w", err)
	}

	if err := flapsClient.Stop(ctx, metadata.AppName, fly.StopMachineInput{
		ID: metadata.MachineID,
	}, ""); err != nil && !isFlyNotFound(err) {
		return fmt.Errorf("stop assistant fly runtime machine: %w", err)
	}
	return nil
}

func (f *FlyRuntimeBackend) Reap(ctx context.Context, runtime assistantRuntimeRecord) error {
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

func (f *FlyRuntimeBackend) runtimeState(ctx context.Context, target flyRuntimeTarget) (runnerStateResponse, error) {
	body, err := f.runtimeRequest(ctx, target, runtimeHTTPRequest{
		Method:         http.MethodGet,
		Path:           "/state",
		ContentType:    "",
		Body:           nil,
		MaxTimeSeconds: 0,
		IdempotencyKey: "",
	})
	if err != nil {
		return runnerStateResponse{}, err
	}
	var state runnerStateResponse
	if err := json.Unmarshal(body, &state); err != nil {
		return runnerStateResponse{}, fmt.Errorf("decode assistant fly runtime state: %w", err)
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
			AppID:      "",
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

// classifySetupError buckets a setup-phase error into one of a fixed set of
// failure classes for span attribute reporting. Sentinel/typed errors win
// over message substring matches; unmapped errors fall into "unknown".
func classifySetupError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errFlyAppCorrupted):
		return "app_corrupted"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case isFlyAppPropagating(err):
		return "app_propagation"
	case isFlyNotFound(err):
		return "not_found"
	case isFlyAppNameTaken(err):
		return "conflict"
	case isFlyIPAlreadyAssigned(err):
		return "ip_already_assigned"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timed out"), strings.Contains(msg, "deadline exceeded"):
		return "timeout"
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "eof"), strings.Contains(msg, "reset by peer"):
		return "network"
	}
	return "unknown"
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
