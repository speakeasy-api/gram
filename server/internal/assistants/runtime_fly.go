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
// One VM serves every thread under an assistant, so the runner reports per-
// thread idle clocks rather than a single VM-wide value. Threads is empty
// when the VM has booted but is not yet driving any thread.
type runnerStateResponse struct {
	AssistantID   string              `json:"assistant_id"`
	UptimeSeconds uint64              `json:"uptime_seconds"`
	Threads       []runnerThreadState `json:"threads"`
}

type runnerThreadState struct {
	ThreadID    string `json:"thread_id"`
	ChatID      string `json:"chat_id"`
	IdleSeconds uint64 `json:"idle_seconds"`
}

// minThreadIdle returns the minimum idle_seconds across the runner's threads,
// or nil when no threads exist. A nil signal means "no per-thread activity to
// gate on" — callers treat that as fully idle (safe to recycle, no warm
// remaining).
func (r runnerStateResponse) minThreadIdle() *uint64 {
	if len(r.Threads) == 0 {
		return nil
	}
	minIdle := r.Threads[0].IdleSeconds
	for _, t := range r.Threads[1:] {
		if t.IdleSeconds < minIdle {
			minIdle = t.IdleSeconds
		}
	}
	return &minIdle
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
	Destroy(ctx context.Context, appName string, in fly.RemoveMachineInput, nonce string) error
	Get(ctx context.Context, appName, machineID string) (*fly.Machine, error)
	Launch(ctx context.Context, appName string, input fly.LaunchMachineInput) (*fly.Machine, error)
	List(ctx context.Context, appName, state string) ([]*fly.Machine, error)
	Start(ctx context.Context, appName, machineID string, nonce string) (*fly.MachineStartResponse, error)
	Stop(ctx context.Context, appName string, in fly.StopMachineInput, nonce string) error
	Update(ctx context.Context, appName string, input fly.LaunchMachineInput, nonce string) (*fly.Machine, error)
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

func (f *FlyRuntimeBackend) ServerURL() *url.URL {
	return f.config.ServerURL
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
		appName = f.appName(runtime.AssistantID)
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

	machine, err := f.tracedResolveMachine(ctx, flapsClient, appName, runtime, metadata.MachineID, metadata.LastBootID, sameAppIncarnation)
	if err != nil {
		if errors.Is(err, errFlyAppCorrupted) {
			f.logger.WarnContext(ctx,
				"assistant fly runtime app corrupted, tearing down for recreate",
				attr.SlogFlyAppName(appName),
				attr.SlogAssistantID(runtime.AssistantID.String()),
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
	} else {
		recycled, err := f.maybeRecycleImage(ctx, flapsClient, runtime, appName, appURL, appIP, machine)
		if err != nil {
			return RuntimeBackendEnsureResult{}, err
		}
		if recycled != nil {
			machine = recycled
			coldStart = true
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

	target := flyRuntimeTarget{URL: appURL, IP: appIP, MachineID: machine.ID}
	if err := f.tracedWaitHealth(ctx, target, coldStart); err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("wait for assistant fly runtime health: %w", err)
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
	runtime assistantRuntimeRecord,
	machineID string,
	lastBootID string,
	sameAppIncarnation bool,
) (*fly.Machine, error) {
	matches := machineMatcherForRuntime(runtime)
	hadPriorBoot := sameAppIncarnation && (lastBootID != "" || machineID != "")

	if machineID != "" {
		machine, err := flapsClient.Get(ctx, appName, machineID)
		switch {
		case err == nil:
			if !matches(machine) {
				return nil, fmt.Errorf("assistant fly runtime machine %s does not belong to runtime %s", machineID, runtime.ID)
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
		if matches(machine) {
			return machine, nil
		}
	}
	return nil, nil
}

// machineMatcherForRuntime returns a predicate that picks the active
// machine in the per-assistant app — one VM serves every thread under the
// assistant.
func machineMatcherForRuntime(runtime assistantRuntimeRecord) func(*fly.Machine) bool {
	want := runtime.AssistantID.String()
	return func(machine *fly.Machine) bool {
		if machine == nil || machine.Config == nil {
			return false
		}
		return machine.Config.Metadata[flyMachineMetadataAssistantID] == want
	}
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
		Name:                "assistant-" + shortRuntimeName(runtime.AssistantID),
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
	runtime assistantRuntimeRecord,
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
	return f.resolveMachine(ctx, flapsClient, appName, runtime, machineID, lastBootID, sameAppIncarnation)
}

// maybeRecycleImage applies an in-place machine update when the running
// machine's image lags behind the configured runtime image. Preserves the
// app + allocated IP so callers don't pay the DNS-propagation cost of a
// fresh app, and gates on the runner's /state idle clock so an in-flight
// turn isn't interrupted — the next admission catches it idle. Returns the
// post-update machine when a recycle happened, nil otherwise.
func (f *FlyRuntimeBackend) maybeRecycleImage(
	ctx context.Context,
	flapsClient flyRuntimeFlapsClient,
	runtime assistantRuntimeRecord,
	appName string,
	appURL string,
	appIP string,
	machine *fly.Machine,
) (*fly.Machine, error) {
	desired := f.desiredImageRef()
	actual := machineImageRef(machine)
	// Empty ImageRef means flaps returned a partial machine record (e.g.
	// HostStatus != ok or a stale list response). Recycling on missing data
	// would churn healthy runtimes — skip and let the next admission retry.
	if actual == "" || desired == actual {
		return nil, nil
	}

	target := flyRuntimeTarget{URL: appURL, IP: appIP, MachineID: machine.ID}
	if machine.State == fly.MachineStateStarted {
		state, stateErr := f.runtimeState(ctx, target)
		// A turn in flight reads as min(idle_seconds) == 0 across the
		// runner's threads (the runner clears a thread's idle clock
		// synchronously on /turn enqueue). Skip recycling so we don't reboot
		// mid-turn; a later admission with idle threads picks the upgrade
		// up. Probe errors fall through to recycle — the runner is
		// unreachable on the stale image anyway and waitForRuntimeHealth
		// would just fail next.
		if idle := state.minThreadIdle(); stateErr == nil && idle != nil && *idle == 0 {
			f.logger.InfoContext(ctx,
				"assistant fly runtime image recycle skipped: turn in flight",
				attr.SlogFlyAppName(appName),
				attr.SlogAssistantImageDesired(desired),
				attr.SlogAssistantImageActual(actual),
			)
			return nil, nil
		}
	}

	f.logger.InfoContext(ctx,
		"assistant fly runtime recycling: image upgrade",
		attr.SlogFlyAppName(appName),
		attr.SlogAssistantImageDesired(desired),
		attr.SlogAssistantImageActual(actual),
	)

	updated, err := f.tracedRecycleMachine(ctx, flapsClient, runtime, appName, machine, desired, actual)
	if err != nil {
		return nil, fmt.Errorf("recycle assistant fly runtime machine: %w", err)
	}
	if err := f.tracedWaitStarted(ctx, flapsClient, appName, updated, true); err != nil {
		return nil, fmt.Errorf("wait for assistant fly runtime machine recycle: %w", err)
	}
	return updated, nil
}

// desiredImageRef returns the configured runtime image reference in the same
// "<repo>:<tag>" form fly's MachineImageRef serialises into.
func (f *FlyRuntimeBackend) desiredImageRef() string {
	return fmt.Sprintf("%s:%s", f.config.OCIImage, f.config.ImageVersion)
}

// machineImageRef rebuilds the "<registry>/<repo>:<tag>" form from a fly
// Machine's ImageRef so it can be compared verbatim to desiredImageRef. The
// digest is intentionally omitted — fly populates it post-pull, but the
// configured image version is always a tag (helm sets it from image.tag).
func machineImageRef(machine *fly.Machine) string {
	if machine == nil {
		return ""
	}
	ref := machine.ImageRef
	repo := ref.Repository
	if ref.Registry != "" && repo != "" {
		repo = ref.Registry + "/" + ref.Repository
	} else if ref.Registry != "" {
		repo = ref.Registry
	}
	if ref.Tag == "" {
		return repo
	}
	return repo + ":" + ref.Tag
}

func (f *FlyRuntimeBackend) recycleMachine(
	ctx context.Context,
	flapsClient flyRuntimeFlapsClient,
	runtime assistantRuntimeRecord,
	appName string,
	machine *fly.Machine,
) (*fly.Machine, error) {
	updated, err := flapsClient.Update(ctx, appName, fly.LaunchMachineInput{
		ID:                  machine.ID,
		Config:              f.machineConfig(runtime),
		Region:              firstNonEmpty(machine.Region, f.config.DefaultFlyRegion),
		Name:                machine.Name,
		RequiresReplacement: false,
	}, "")
	if err != nil {
		return nil, fmt.Errorf("update assistant fly runtime machine: %w", err)
	}
	return updated, nil
}

func (f *FlyRuntimeBackend) tracedRecycleMachine(
	ctx context.Context,
	flapsClient flyRuntimeFlapsClient,
	runtime assistantRuntimeRecord,
	appName string,
	machine *fly.Machine,
	desired string,
	actual string,
) (updated *fly.Machine, err error) {
	ctx, span := f.tracer.Start(ctx, "assistants.runtime.recycleMachine",
		trace.WithAttributes(
			attr.AssistantImageRecycle(true),
			attr.AssistantImageDesired(desired),
			attr.AssistantImageActual(actual),
			attr.FlyAppName(appName),
		),
	)
	defer func() {
		if err != nil {
			span.SetAttributes(attr.AssistantSetupFailureClass(classifySetupError(err)))
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	return f.recycleMachine(ctx, flapsClient, runtime, appName, machine)
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

func (f *FlyRuntimeBackend) RunTurn(ctx context.Context, runtime assistantRuntimeRecord, threadID uuid.UUID, idempotencyKey string, authToken string, prompt string) error {
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

	// One VM serves every thread under the assistant. The runner expects
	// /threads/{thread_id}/turn — the URL segment is the signal the runner
	// uses to dispatch to the right per-thread tokio task.
	if _, err := f.runtimeRequest(ctx, targetFromMetadata(metadata), runtimeHTTPRequest{
		Method:         http.MethodPost,
		Path:           "/threads/" + threadID.String() + "/turn",
		ContentType:    "application/json",
		Body:           reqBody,
		IdempotencyKey: idempotencyKey,
		MaxTimeSeconds: 30 * 60,
	}); err != nil {
		return fmt.Errorf("%w: execute fly turn request: %w", classifyTurnError(err), err)
	}
	return nil
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
	return RuntimeBackendStatus{
		Configured:  true,
		IdleSeconds: state.minThreadIdle(),
	}, nil
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

// Reap tears down one thread's slot in the assistant's Fly app: destroys
// the thread's machine, then deletes the app only if no other thread still
// holds a machine in it. Legacy per-thread apps (one machine each) collapse
// to app deletion in one call; new per-assistant apps survive until the
// last thread's machine is reaped.
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

	flapsClient, err := f.flapsFactory.New(ctx)
	if err != nil {
		return fmt.Errorf("create fly runtime flaps client: %w", err)
	}

	if metadata.MachineID != "" {
		if err := flapsClient.Destroy(ctx, metadata.AppName, fly.RemoveMachineInput{
			ID:   metadata.MachineID,
			Kill: true,
		}, ""); err != nil && !isFlyNotFound(err) {
			return fmt.Errorf("destroy assistant fly runtime machine: %w", err)
		}
	}

	machines, err := flapsClient.List(ctx, metadata.AppName, "")
	switch {
	case err == nil:
		for _, m := range machines {
			if m == nil || m.ID == metadata.MachineID {
				continue
			}
			if m.IsActive() {
				return nil
			}
		}
	case isFlyNotFound(err):
		return nil
	default:
		return fmt.Errorf("list assistant fly runtime machines: %w", err)
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
	env := map[string]string{
		"GRAM_ASSISTANT_ID":         runtime.AssistantID.String(),
		"GRAM_ASSISTANT_PROJECT_ID": runtime.ProjectID.String(),
		"GRAM_SERVER_URL":           f.config.ServerURL.String(),
	}
	metadata := map[string]string{
		fly.MachineConfigMetadataKeyFlyPlatformVersion: "v2",
		fly.MachineConfigMetadataKeyFlyProcessGroup:    flyMachineMetadataRoleAssistant,
		flyMachineMetadataAssistantID:                  runtime.AssistantID.String(),
		flyMachineMetadataProjectID:                    runtime.ProjectID.String(),
		flyMachineMetadataRole:                         flyMachineMetadataRoleAssistant,
	}
	return &fly.MachineConfig{
		Image: fmt.Sprintf("%s:%s", f.config.OCIImage, f.config.ImageVersion),
		Env:   env,
		Guest: &fly.MachineGuest{
			CPUKind:       "shared",
			CPUs:          2,
			MemoryMB:      1024,
			PersistRootfs: fly.MachinePersistRootfsNever,
		},
		Metadata: metadata,
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
// SNI + wildcard cert verification still pass. MachineID pins the Fly proxy
// to this thread's VM via fly-force-instance-id so siblings sharing the
// per-assistant app can't intercept the request.
type flyRuntimeTarget struct {
	URL       string
	IP        string
	MachineID string
}

func targetFromMetadata(md flyRuntimeMetadata) flyRuntimeTarget {
	return flyRuntimeTarget{URL: md.AppURL, IP: md.AppIP, MachineID: md.MachineID}
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
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for runtime health: %w", ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
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

// clientForTarget is a hook point for per-target dialing. The runner is
// reachable via the app's hostname today; a future dedicated-IP design can
// swap this to pin a request to a specific IP without changing callers.
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
	if target.MachineID != "" {
		// Pin the request to this thread's VM. With many machines on the
		// per-assistant app, the proxy would otherwise round-robin and let
		// /configure, /turn, /state hit a sibling — cross-thread leak that
		// the runner cannot recover from (configure returns 409, turn
		// enqueues onto the wrong runtime).
		req.Header.Set("Fly-Force-Instance-Id", target.MachineID)
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

// appName derives the Fly app name from the assistant ID. One app per
// assistant, many machines (one per thread) inside it. Legacy rows whose
// metadata still records a per-thread app name keep using that app until
// Reap drains it.
func (f *FlyRuntimeBackend) appName(assistantID uuid.UUID) string {
	return fmt.Sprintf("%s-%s", f.config.AppNamePrefix, strings.ToLower(assistantID.String()))
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
