package functions

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	slogmulti "github.com/samber/slog-multi"
	"github.com/superfly/fly-go"
	"github.com/superfly/fly-go/flaps"
	"github.com/superfly/fly-go/tokens"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments/events"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/functions/repo"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	functionAuthSecretVar = "GRAM_FUNCTION_AUTH_SECRET"
	defaultFlyBaseURL     = "https://api.fly.io"
	defaultFlyMachinesURL = "https://api.machines.dev"
)

type FlyRunnerOptions struct {
	ServiceName    string
	ServiceVersion string

	FlyTokens          *tokens.Tokens
	FlyAPIURL          string
	FlyMachinesBaseURL string
	DefaultFlyOrg      string
	DefaultFlyRegion   string
}

type FlyRunner struct {
	logger          *slog.Logger
	db              *pgxpool.Pool
	assetStorage    assets.BlobStore
	client          *fly.Client
	tokens          *tokens.Tokens
	machinesAPIBase string
	machinesClient  *http.Client
	defaultOrg      string
	defaultRegion   string
	imgSelector     ImageSelector
	encryption      *encryption.Client
	ua              string
}

var _ interface {
	ToolCaller
	Deployer
} = (*FlyRunner)(nil)

func NewFlyRunner(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	assetStorage assets.BlobStore,
	imageSelector ImageSelector,
	encryption *encryption.Client,
	o FlyRunnerOptions,
) *FlyRunner {
	ua := fmt.Sprintf("%s/%s", o.ServiceName, o.ServiceVersion)

	flyAPIBase := conv.Default(o.FlyAPIURL, defaultFlyBaseURL)
	machinesAPIBase := conv.Default(o.FlyMachinesBaseURL, defaultFlyMachinesURL)
	machinesClient := &http.Client{
		Transport: otelhttp.NewTransport(
			retryablehttp.NewClient().StandardClient().Transport,
			otelhttp.WithTracerProvider(tracerProvider),
		),
	}

	c := fly.NewClientFromOptions(fly.ClientOptions{
		BaseURL: flyAPIBase,
		Tokens:  o.FlyTokens,
		Name:    o.ServiceName,
		Version: o.ServiceVersion,
		Transport: &fly.Transport{
			UnderlyingTransport: otelhttp.NewTransport(
				retryablehttp.NewClient().StandardClient().Transport,
				otelhttp.WithTracerProvider(tracerProvider),
			),
			UserAgent: ua,
			Tokens:    o.FlyTokens,
		},
		Logger: &flyLogger{
			logger:      logger.With(attr.SlogComponent("flyio-client")),
			contextFunc: context.Background,
		},
	})

	return &FlyRunner{
		logger:          logger.With(attr.SlogComponent("flyio-orchestrator")),
		db:              db,
		assetStorage:    assetStorage,
		client:          c,
		tokens:          o.FlyTokens,
		machinesAPIBase: machinesAPIBase,
		machinesClient:  machinesClient,
		defaultOrg:      o.DefaultFlyOrg,
		defaultRegion:   o.DefaultFlyRegion,
		imgSelector:     imageSelector,
		encryption:      encryption,
		ua:              ua,
	}
}

func (f *FlyRunner) CallTool(context.Context, RunnerToolCallRequest) (*http.Response, error) {
	return nil, oops.Permanent(errors.New("not implemented"))
}

func (f *FlyRunner) Deploy(ctx context.Context, req RunnerDeployRequest) (res *RunnerDeployResult, err error) {
	// ⚠️ IMPLEMENTATION NOTE: Do as much preparation and validation as possible
	// before starting to create resources on fly.io. This is to avoid leaving
	// orphaned resources in case of errors.

	appsRepo := repo.New(f.db)

	slogArgs := []any{
		attr.SlogProjectID(req.ProjectID.String()),
		attr.SlogDeploymentID(req.DeploymentID.String()),
		attr.SlogDeploymentFunctionsID(req.FunctionID.String()),
		attr.SlogDeploymentFunctionsAccessID(req.AccessID.String()),
		attr.SlogFunctionsRuntime(req.Runtime),
	}

	eventsHandler := events.NewLogHandler()
	logger := slog.New(slogmulti.Fanout(
		f.logger.Handler(),
		eventsHandler,
	)).With(slogArgs...)

	defer func() {
		if _, flushErr := eventsHandler.Flush(ctx, f.db); flushErr != nil {
			f.logger.ErrorContext(
				ctx,
				"failed to flush function runner deployment events",
				append(slogArgs, attr.SlogError(flushErr))...,
			)
		}
	}()

	if err := inv.Check(
		"fly runner deploy",
		"project id cannot be nil", req.ProjectID != uuid.Nil,
		"deployment id cannot be nil", req.DeploymentID != uuid.Nil,
		"deployment function id cannot be nil", req.FunctionID != uuid.Nil,
		"runner version cannot be empty", req.Version != "",
	); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid function runner deploy request").Log(ctx, logger)
	}

	networkName := fmt.Sprint("gram-fn-", req.FunctionID.String())
	orgSlug := f.defaultOrg
	region := f.defaultRegion
	runnerVersion := req.Version
	sharedMetadata := map[string]string{
		fly.MachineConfigMetadataKeyFlyPlatformVersion: "v2",

		"gram_deployment_id": req.DeploymentID.String(),
		"gram_function_id":   req.FunctionID.String(),
		"gram_project_id":    req.ProjectID.String(),
		"gram_role":          "functions_runner",
	}

	files, err := f.serializeAssets(ctx, logger, req.Assets)
	var serr *oops.ShareableError
	switch {
	case errors.As(err, &serr):
		return nil, err
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to encode machine files").Log(ctx, logger)
	}

	image, err := f.imgSelector.Select(ctx, ImageRequest{
		ProjectID:    req.ProjectID,
		DeploymentID: req.DeploymentID,
		FunctionID:   req.FunctionID,
		Runtime:      req.Runtime,
		Version:      runnerVersion,
	})
	if err != nil {
		return nil, oops.E(
			oops.CodeUnexpected,
			err,
			"failed select runner image",
		).Log(ctx, logger, attr.SlogFunctionsRunnerVersion(runnerVersion))
	}

	machineConfig := f.newMachineConfig(req, image, files, sharedMetadata)

	logger = logger.With(
		attr.SlogFlyOrgSlug(orgSlug),
		attr.SlogFunctionsRunnerImage(image),
		attr.SlogFunctionsRunnerVersion(runnerVersion),
	)
	logger.InfoContext(ctx, "deploying functions runner to fly.io")

	org, err := f.client.GetOrganizationBySlug(ctx, orgSlug)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to resolve fly.io organization id").Log(ctx, logger)
	}

	appFull, err := f.client.CreateApp(ctx, fly.CreateAppInput{
		OrganizationID:  org.ID,
		Name:            "", // let fly generate a name
		PreferredRegion: &region,
		Network:         &networkName,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create fly.io app").Log(ctx, logger)
	}

	appName := appFull.Name
	rawURL := fmt.Sprintf("https://%s.fly.dev", appName)
	logger = logger.With(attr.SlogFlyAppName(appName))

	partialDeployState := partialDeploy{
		internalAppID: uuid.Nil,
		appName:       appName,
		orgSlug:       orgSlug,
	}

	defer func() {
		if err != nil {
			f.tryMarkDeployFailed(ctx, logger, appsRepo, req, partialDeployState)
		}
	}()

	internalID, err := appsRepo.InitFlyApp(ctx, repo.InitFlyAppParams{
		ProjectID:     req.ProjectID,
		DeploymentID:  req.DeploymentID,
		FunctionID:    req.FunctionID,
		AccessID:      req.AccessID,
		FlyOrgID:      org.ID,
		FlyOrgSlug:    orgSlug,
		AppName:       appName,
		AppUrl:        rawURL,
		RunnerVersion: string(runnerVersion),
		PrimaryRegion: region,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to record fly.io app in db").Log(ctx, logger)
	}

	logger = logger.With(
		attr.SlogFlyAppName(appName),
		attr.SlogFlyOrgSlug(orgSlug),
		attr.SlogFlyOrgID(org.ID),
		attr.SlogFlyAppInternalID(internalID.String()),
	)

	partialDeployState.internalAppID = internalID

	appURL, err := url.Parse(fmt.Sprintf("https://%s.fly.dev", appName))
	if err != nil {
		return nil, oops.E(
			oops.CodeUnexpected,
			fmt.Errorf("%s: %w", appFull.AppURL, err),
			"failed to parse fly app url",
		).Log(ctx, logger)
	}

	flapsc, err := flaps.NewWithOptions(ctx, flaps.NewClientOpts{
		AppName:   appName,
		UserAgent: f.ua,
		OrgSlug:   orgSlug,
		Tokens:    f.tokens,
		Logger: &flyLogger{
			logger: logger.With(attr.SlogComponent("flyio-flaps")),
			contextFunc: func() context.Context {
				return ctx
			},
		},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create flaps client").Log(ctx, logger)
	}

	minSecretVersion, err := f.setSecrets(ctx, logger, appName, map[string]string{functionAuthSecretVar: req.BearerSecret})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to set function runner secrets").Log(ctx, logger)
	}

	ms, err := f.launchN(ctx, flapsc, region, machineConfig, minSecretVersion, 2)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to spin up function runner machines").Log(ctx, logger)
	}

	_, err = f.client.AllocateSharedIPAddress(ctx, appName)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to allocate shared IP address").Log(ctx, logger)
	}

	machineIDs := make([]string, 0, len(ms))
	for _, m := range ms {
		machineIDs = append(machineIDs, m.ID)
	}

	_, err = appsRepo.FinalizeFlyApp(ctx, repo.FinalizeFlyAppParams{
		ID:           internalID,
		ProjectID:    req.ProjectID,
		DeploymentID: req.DeploymentID,
		FunctionID:   req.FunctionID,
		Status:       "ready",
		ReapedAt:     pgtype.Timestamptz{Valid: false, Time: time.Time{}, InfinityModifier: 0},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to mark fly.io app as ready").Log(ctx, logger)
	}

	msg := fmt.Sprintf("created fly.io function runner app (app=%s, org=%s, scale=%d)", appName, orgSlug, len(ms))
	logger.InfoContext(ctx, msg, attr.SlogFlyMachineIDs(machineIDs))

	return &RunnerDeployResult{
		URN:       urn.NewFunctionRunner(urn.FunctionRunnerKindFlyApp, orgSlug, appName),
		Version:   runnerVersion,
		Provider:  "fly",
		Region:    region,
		PublicURL: appURL,
		Scale:     len(ms),
	}, nil
}

func (f *FlyRunner) newMachineConfig(req RunnerDeployRequest, image string, files []*fly.File, baseMetadata map[string]string) *fly.MachineConfig {
	machineMeta := maps.Clone(baseMetadata)
	machineMeta[fly.MachineConfigMetadataKeyFlyProcessGroup] = "gram_functions_runner"

	return &fly.MachineConfig{
		Image: image,
		Env: map[string]string{
			"GRAM_DEPLOYMENT_ID": req.DeploymentID.String(),
			"GRAM_FUNCTION_ID":   req.FunctionID.String(),
			"GRAM_PROJECT_ID":    req.ProjectID.String(),
		},
		Guest: &fly.MachineGuest{
			CPUKind:       "shared",
			CPUs:          2,
			MemoryMB:      512,
			GPUs:          0,
			PersistRootfs: fly.MachinePersistRootfsNever,
		},
		Metadata: machineMeta,
		Files:    files,
		Services: []fly.MachineService{
			{
				Protocol:           "tcp",
				InternalPort:       8888,
				Autostop:           conv.Ptr(fly.MachineAutostopStop),
				Autostart:          conv.Ptr(true),
				MinMachinesRunning: conv.Ptr(0),
				Ports: []fly.MachinePort{
					{
						Handlers: []string{"tls"},
						Port:     conv.Ptr(443),
					},
				},
				Checks: []fly.MachineServiceCheck{
					{
						Type:         conv.Ptr("http"),
						HTTPProtocol: conv.Ptr("http"),
						HTTPMethod:   conv.Ptr(http.MethodGet),
						HTTPPath:     conv.Ptr("/healthz"),
						Interval:     &fly.Duration{Duration: 30 * time.Second},
						Timeout:      &fly.Duration{Duration: 5 * time.Second},
						GracePeriod:  &fly.Duration{Duration: 5 * time.Second},
					},
				},
				Concurrency: &fly.MachineServiceConcurrency{
					Type:      "connections",
					SoftLimit: 20,
				},
			},
		},
		Restart: &fly.MachineRestart{
			Policy:     "on-failure",
			MaxRetries: 5,
		},
	}
}

func (f *FlyRunner) launchN(ctx context.Context, flapsc *flaps.Client, region string, config *fly.MachineConfig, minSecretVersion *uint64, n uint8) ([]*fly.Machine, error) {
	ms := make([]*fly.Machine, 0, n)

	for i := range n {
		m, err := flapsc.Launch(ctx, fly.LaunchMachineInput{
			Region:                  region,
			Timeout:                 0,
			RequiresReplacement:     true,
			SkipLaunch:              false,
			SkipServiceRegistration: false,
			SkipSecrets:             false,
			LeaseTTL:                0,
			Config:                  config,
			MinSecretsVersion:       minSecretVersion,
		})
		if err != nil {
			return ms, fmt.Errorf("failed to launch machine %d: %w", i, err)
		}
		ms = append(ms, m)

		if err := flapsc.Wait(ctx, m, "started", 30*time.Second); err != nil {
			return nil, fmt.Errorf("waiting for machine %s to start: %w", m.ID, err)
		}
	}

	return ms, nil
}

func (f *FlyRunner) setSecrets(ctx context.Context, logger *slog.Logger, appName string, secrets map[string]string) (*uint64, error) {
	var minSecretVersion *uint64

	type setSecretResponse struct {
		Version *uint64 `json:"version"`
	}

	bearer := "Bearer " + f.tokens.GraphQL()

	for k, v := range secrets {
		payload, err := json.Marshal(map[string]string{"value": v})
		if err != nil {
			return nil, fmt.Errorf("%s: marshal secret value: %w", k, err)
		}

		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			fmt.Sprintf("%s/v1/apps/%s/secrets/%s", f.machinesAPIBase, appName, k),
			bytes.NewReader(payload),
		)
		if err != nil {
			return nil, fmt.Errorf("%s: create set secret request: %w", k, err)
		}

		req.Header.Set("User-Agent", f.ua)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", bearer)

		res, err := f.machinesClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%s: send set secret request: %w", k, err)
		}
		defer o11y.LogDefer(ctx, logger, func() error { return res.Body.Close() })

		if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
			return nil, fmt.Errorf("%s: unexpected set secret response code: %d", k, res.StatusCode)
		}

		raw, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, fmt.Errorf("%s: read set secret response: %w", k, err)
		}

		var body setSecretResponse
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, fmt.Errorf("%s: decode set secret response: %w", k, err)
		}

		if body.Version == nil {
			return nil, fmt.Errorf("%s: no version from set secret response", k)
		}

		if minSecretVersion == nil || *minSecretVersion > *body.Version {
			minSecretVersion = body.Version
		}
	}

	return minSecretVersion, nil
}

func (f *FlyRunner) serializeAssets(
	ctx context.Context,
	logger *slog.Logger,
	assets []RunnerAsset,
) ([]*fly.File, error) {
	total := 0

	files := make([]*fly.File, 0, len(assets))
	for _, asset := range assets {
		rdr, err := f.assetStorage.Read(ctx, asset.AssetURL)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch function asset").Log(ctx, logger)
		}
		defer o11y.LogDefer(ctx, f.logger, func() error { return rdr.Close() })

		data, err := io.ReadAll(rdr)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to read function asset").Log(ctx, logger)
		}

		encoded := base64.StdEncoding.EncodeToString(data)
		total += len(data)

		if total > 1*1024*1024 {
			return nil, oops.E(oops.CodeInvalid, nil, "total function assets size exceeds 1MiB limit").Log(ctx, logger)
		}

		files = append(files, &fly.File{
			Mode:      conv.Default(asset.Mode, 0444),
			GuestPath: asset.GuestPath,
			RawValue:  &encoded,
		})
	}

	return files, nil
}

type partialDeploy struct {
	internalAppID uuid.UUID
	appName       string
	orgSlug       string
}

func (f *FlyRunner) tryMarkDeployFailed(
	ctx context.Context,
	logger *slog.Logger,
	flyrepo *repo.Queries,
	req RunnerDeployRequest,
	state partialDeploy,
) {
	var reapedAt time.Time
	if state.appName != "" {
		if err := f.client.DeleteApp(ctx, state.appName); err != nil {
			logger.ErrorContext(
				ctx,
				"failed to delete fly.io app after deployment failure",
				attr.SlogError(err),
				attr.SlogFlyAppName(state.appName),
				attr.SlogFlyOrgSlug(state.orgSlug),
				attr.SlogFlyAppInternalID(state.internalAppID.String()),
			)
		} else {
			reapedAt = time.Now().UTC()
		}
	}

	if state.internalAppID != uuid.Nil {
		reapColumn := pgtype.Timestamptz{
			Time:             reapedAt,
			InfinityModifier: 0,
			Valid:            !reapedAt.IsZero(),
		}

		if _, err := flyrepo.FinalizeFlyApp(ctx, repo.FinalizeFlyAppParams{
			ID:           state.internalAppID,
			Status:       "failed",
			ReapedAt:     reapColumn,
			ProjectID:    req.ProjectID,
			DeploymentID: req.DeploymentID,
			FunctionID:   req.FunctionID,
		}); err != nil {
			logger.ErrorContext(
				ctx,
				"failed to mark fly app as failed",
				attr.SlogError(err),
				attr.SlogFlyAppName(state.appName),
				attr.SlogFlyOrgSlug(state.orgSlug),
				attr.SlogFlyAppInternalID(state.internalAppID.String()),
			)
		}
	}
}

// flyLogger adapts slog.Logger to the fly.Logger interface	for use with the fly-go client.
type flyLogger struct {
	logger      *slog.Logger
	contextFunc func() context.Context
}

func (f *flyLogger) Debug(v ...interface{}) {
	f.logger.DebugContext(f.contextFunc(), fmt.Sprint(v...))
}

func (f *flyLogger) Debugf(format string, v ...interface{}) {
	f.logger.DebugContext(f.contextFunc(), fmt.Sprintf(format, v...))
}
