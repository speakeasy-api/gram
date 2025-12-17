package functions

import (
	"bytes"
	"context"
	"database/sql"
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
	largeFunctionLimit    = 700 * 1024 // 700 KiB
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
	tracer          trace.Tracer
	serverURL       *url.URL
	db              *pgxpool.Pool
	assetStore      assets.BlobStore
	tigrisStore     *assets.TigrisStore
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
	serverURL *url.URL,
	db *pgxpool.Pool,
	assetStorage assets.BlobStore,
	tigrisStore *assets.TigrisStore,
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
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/functions"),
		serverURL:       serverURL,
		db:              db,
		assetStore:      assetStorage,
		tigrisStore:     tigrisStore,
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

func (f *FlyRunner) prepareFunctionAuth(ctx context.Context, logger *slog.Logger, baseReq RunnerBaseRequest) (appURL string, enc *encryption.Client, err error) {
	funcsRepo := repo.New(f.db)
	row, err := funcsRepo.GetFlyAppAccess(ctx, repo.GetFlyAppAccessParams{
		ProjectID:    baseReq.ProjectID,
		DeploymentID: baseReq.DeploymentID,
		FunctionID:   baseReq.FunctionsID,
		AccessID:     baseReq.FunctionsAccessID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", nil, oops.E(oops.CodeNotFound, err, "no function runner available").Log(ctx, logger)
	case err != nil:
		return "", nil, oops.E(oops.CodeUnexpected, err, "failed to fetch function runner").Log(ctx, logger)
	}

	format := row.BearerFormat.String
	sec := row.EncryptionKey.Reveal()
	if format == "" || len(sec) == 0 {
		return "", nil, oops.E(oops.CodeInvariantViolation, nil, "function runner does not have credentials").Log(ctx, logger)
	}

	unsealedAuthKey, err := f.encryption.Decrypt(string(sec))
	if err != nil {
		return "", nil, oops.E(oops.CodeUnexpected, err, "failed to access credentials").Log(ctx, logger)
	}
	enc, err = encryption.NewWithBytes([]byte(unsealedAuthKey))
	if err != nil {
		return "", nil, oops.E(oops.CodeUnexpected, err, "failed to create encryption client").Log(ctx, logger)
	}

	return row.AppUrl, enc, nil
}

func (f *FlyRunner) ToolCall(ctx context.Context, req RunnerToolCallRequest) (httpreq *http.Request, err error) {
	logger := f.logger.With(
		attr.SlogFunctionsBackend("flyio"),
		attr.SlogProjectID(req.ProjectID.String()),
		attr.SlogDeploymentID(req.DeploymentID.String()),
		attr.SlogDeploymentFunctionsID(req.FunctionsID.String()),
		attr.SlogToolURN(req.ToolURN.String()),
		attr.SlogToolName(req.ToolName),
	)

	if err := inv.Check(
		"flyio tool call",
		"organization id cannot be empty", req.OrganizationID != "",
		"organization slug cannot be empty", req.OrganizationSlug != "",
		"project id cannot be nil", req.ProjectID != uuid.Nil,
		"deployment id cannot be nil", req.DeploymentID != uuid.Nil,
		"functions id cannot be nil", req.FunctionsID != uuid.Nil,
		"tool urn cannot be empty", !req.ToolURN.IsZero(),
		"tool name cannot be empty", req.ToolName != "",
	); err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "malformed tool call request").Log(ctx, logger)
	}

	appURL, enc, err := f.prepareFunctionAuth(ctx, logger, req.RunnerBaseRequest)
	if err != nil {
		return nil, err
	}

	endpoint, err := url.JoinPath(appURL, "/tool-call")
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse function tool url").Log(ctx, logger)
	}

	token, err := TokenV1(enc, TokenRequestV1{
		ID:      req.InvocationID.String(),
		Exp:     time.Now().Add(10 * time.Minute).Unix(),
		Subject: req.ToolURN.String(),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create bearer token for function tool call").Log(ctx, logger)
	}

	payload, err := json.Marshal(CallToolPayload{
		ToolName:    req.ToolName,
		Input:       req.Input,
		Environment: req.Environment,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to marshal function tool payload").Log(ctx, logger)
	}

	httpreq, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create function tool request").Log(ctx, logger)
	}

	httpreq.Header.Set("Authorization", "Bearer "+token)
	httpreq.Header.Set("Content-Type", "application/json")

	return httpreq, nil
}

func (f *FlyRunner) ReadResource(ctx context.Context, req RunnerResourceReadRequest) (httpreq *http.Request, err error) {
	logger := f.logger.With(
		attr.SlogFunctionsBackend("flyio"),
		attr.SlogProjectID(req.ProjectID.String()),
		attr.SlogDeploymentID(req.DeploymentID.String()),
		attr.SlogDeploymentFunctionsID(req.FunctionsID.String()),
		attr.SlogResourceURI(req.ResourceURI),
		attr.SlogResourceURN(req.ResourceURN.String()),
	)

	if err := inv.Check(
		"flyio read resource",
		"organization id cannot be empty", req.OrganizationID != "",
		"organization slug cannot be empty", req.OrganizationSlug != "",
		"project id cannot be nil", req.ProjectID != uuid.Nil,
		"deployment id cannot be nil", req.DeploymentID != uuid.Nil,
		"functions id cannot be nil", req.FunctionsID != uuid.Nil,
		"resource urn cannot be empty", !req.ResourceURN.IsZero(),
		"resource uri cannot be empty", req.ResourceURI != "",
	); err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "malformed read resource request").Log(ctx, logger)
	}

	appURL, enc, err := f.prepareFunctionAuth(ctx, logger, req.RunnerBaseRequest)
	if err != nil {
		return nil, err
	}

	endpoint, err := url.JoinPath(appURL, "/resource-request")
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse function resource request url").Log(ctx, logger)
	}

	token, err := TokenV1(enc, TokenRequestV1{
		ID:      req.InvocationID.String(),
		Exp:     time.Now().Add(10 * time.Minute).Unix(),
		Subject: req.ResourceURN.String(),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create bearer token for function read resource call").Log(ctx, logger)
	}

	payload, err := json.Marshal(ReadResourcePayload{
		URI:         req.ResourceURI,
		Input:       req.Input,
		Environment: req.Environment,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to marshal function read resource payload").Log(ctx, logger)
	}

	httpreq, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create function read resource request").Log(ctx, logger)
	}

	httpreq.Header.Set("Authorization", "Bearer "+token)
	httpreq.Header.Set("Content-Type", "application/json")

	return httpreq, nil
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

	if err := f.reap(ctx, logger, appsRepo, ReapRequest{
		ProjectID:    req.ProjectID,
		DeploymentID: req.DeploymentID,
		FunctionID:   req.FunctionID,
	}); err != nil {
		logger.ErrorContext(ctx, "failed to reap existing app before deploy", attr.SlogError(err))
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
	logger.InfoContext(ctx, "deploying functions runner app")

	org, err := f.client.GetOrganizationBySlug(ctx, orgSlug)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to resolve runner target organization id").Log(ctx, logger)
	}

	appFull, err := f.client.CreateApp(ctx, fly.CreateAppInput{
		OrganizationID:  org.ID,
		Name:            "", // let fly generate a name
		PreferredRegion: &region,
		Network:         &networkName,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create app").Log(ctx, logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to save app details").Log(ctx, logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to mark app as ready").Log(ctx, logger)
	}

	msg := fmt.Sprintf("deployed function runner app (app=%s, scale=%d)", appName, len(ms))
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

func (f *FlyRunner) Reap(ctx context.Context, req ReapRequest) error {
	ctx, span := f.tracer.Start(ctx, "FlyRunner.Reap")
	defer span.End()

	logger := f.logger.With(
		attr.SlogVisibilityInternal(),
		attr.SlogProjectID(req.ProjectID.String()),
		attr.SlogDeploymentID(req.DeploymentID.String()),
		attr.SlogDeploymentFunctionsID(req.FunctionID.String()),
	)

	appsRepo := repo.New(f.db)

	if err := f.reap(ctx, logger, appsRepo, req); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to reap app").Log(ctx, logger)
	}

	return nil
}

func (f *FlyRunner) reap(ctx context.Context, logger *slog.Logger, appsRepo *repo.Queries, req ReapRequest) error {
	app, err := appsRepo.GetFlyAppNameForFunction(ctx, repo.GetFlyAppNameForFunctionParams{
		ProjectID:    req.ProjectID,
		DeploymentID: req.DeploymentID,
		FunctionID:   req.FunctionID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("get existing app name: %w", err)
	}

	logger = logger.With(
		attr.SlogFlyAppName(app.AppName),
		attr.SlogFlyOrgSlug(app.FlyOrgSlug),
	)

	logger.InfoContext(ctx, fmt.Sprintf("deleting existing app: %s", app.AppName))

	if app.AppName == "" {
		logger.InfoContext(ctx, "app name is empty, skipping reap")
		return nil
	}

	deleteRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodDelete,
		fmt.Sprintf("%s/v1/apps/%s", f.machinesAPIBase, app.AppName),
		nil,
	)
	if err != nil {
		return fmt.Errorf("create delete app request: %w", err)
	}

	bearer := "Bearer " + f.tokens.GraphQL()
	deleteRequest.Header.Set("User-Agent", f.ua)
	deleteRequest.Header.Set("Content-Type", "application/json")
	deleteRequest.Header.Set("Authorization", bearer)

	res, err := f.machinesClient.Do(deleteRequest)
	if err != nil {
		return fmt.Errorf("send delete app request: %w", err)
	}
	defer o11y.LogDefer(ctx, logger, func() error { return res.Body.Close() })

	if res.StatusCode == http.StatusNotFound {
		logger.InfoContext(ctx, "app not found during delete, assuming already deleted")
		return nil
	}

	reapErrorMessage := ""
	if res.StatusCode >= 400 {
		bodyBytes, readErr := io.ReadAll(res.Body)
		if readErr == nil {
			message := string(bodyBytes)
			if len(message) > 500 {
				message = message[:500]
			}

			reapErrorMessage = fmt.Sprintf("status %d: %s", res.StatusCode, message)
		} else {
			reapErrorMessage = fmt.Sprintf("status %d: failed to read response body: %v", res.StatusCode, readErr)
		}
	}

	// Mark the app as reaped in the database
	if err := appsRepo.MarkFlyAppReaped(ctx, repo.MarkFlyAppReapedParams{
		ID:        app.ID,
		ReapError: conv.ToPGTextEmpty(reapErrorMessage),
		ReapedAt: pgtype.Timestamptz{
			Time:             time.Now().UTC(),
			Valid:            true,
			InfinityModifier: 0,
		},
	}); err != nil {
		return fmt.Errorf("mark app as reaped: %w", err)
	}

	logger.InfoContext(ctx, fmt.Sprintf("successfully reaped app: %s", app.AppName))
	return nil
}

func (f *FlyRunner) newMachineConfig(req RunnerDeployRequest, image string, files []*fly.File, baseMetadata map[string]string) *fly.MachineConfig {
	machineMeta := maps.Clone(baseMetadata)
	machineMeta[fly.MachineConfigMetadataKeyFlyProcessGroup] = "gram_functions_runner"

	return &fly.MachineConfig{
		Image: image,
		Env: map[string]string{
			"GRAM_SERVER_URL":    f.serverURL.String(),
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
	total := int64(0)
	useTigris := false
	for _, asset := range assets {
		total += asset.ContentLength
		if total >= largeFunctionLimit {
			useTigris = true
			msg := fmt.Sprintf("copying function assets to blob storage: total function assets greater than %gKiB", float64(largeFunctionLimit)/1024)
			logger.InfoContext(ctx, msg)
			break
		}
	}

	files := make([]*fly.File, 0, len(assets))
	for _, asset := range assets {
		rdr, err := f.assetStore.Read(ctx, asset.AssetURL)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to fetch function asset").Log(ctx, logger)
		}
		defer o11y.LogDefer(ctx, f.logger, func() error {
			if cerr := rdr.Close(); cerr != nil {
				return fmt.Errorf("close function asset reader: %w", cerr)
			}

			return nil
		})

		if useTigris {
			wr, _, err := f.tigrisStore.Write(ctx, asset.AssetURL.Path, asset.ContentType, asset.ContentLength)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to create writer for function asset").Log(ctx, logger)
			}
			defer o11y.LogDefer(ctx, f.logger, func() error {
				if werr := wr.Close(); werr != nil {
					return fmt.Errorf("close function asset tigris writer: %w", werr)
				}

				return nil
			})

			if _, err := io.Copy(wr, rdr); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to copy function asset to blob storage").Log(ctx, logger)
			}

			encoded := base64.StdEncoding.EncodeToString([]byte(asset.AssetID.String()))
			files = append(files, &fly.File{
				Mode:      conv.Default(asset.Mode, 0444),
				GuestPath: fmt.Sprintf("%s.lazy", asset.GuestPath),
				RawValue:  &encoded,
			})
		} else {
			data, err := io.ReadAll(rdr)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to read function asset").Log(ctx, logger)
			}

			encoded := base64.StdEncoding.EncodeToString(data)
			files = append(files, &fly.File{
				Mode:      conv.Default(asset.Mode, 0444),
				GuestPath: asset.GuestPath,
				RawValue:  &encoded,
			})
		}
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
				"failed to delete app after deployment failure",
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
