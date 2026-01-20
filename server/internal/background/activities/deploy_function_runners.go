package activities

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/url"
	"runtime"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sourcegraph/conc/pool"
	"go.temporal.io/sdk/temporal"

	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/functions"
	funcrepo "github.com/speakeasy-api/gram/server/internal/functions/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type DeployFunctionRunnersRequest struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
}

type DeployFunctionRunners struct {
	logger         *slog.Logger
	db             *pgxpool.Pool
	deployer       functions.Deployer
	defaultVersion functions.RunnerVersion
	enc            *encryption.Client
}

func NewDeployFunctionRunners(
	logger *slog.Logger,
	db *pgxpool.Pool,
	deployer functions.Deployer,
	defaultVersion functions.RunnerVersion,
	enc *encryption.Client,
) *DeployFunctionRunners {
	return &DeployFunctionRunners{
		logger:         logger.With(attr.SlogComponent("deploy-function-runner")),
		db:             db,
		deployer:       deployer,
		defaultVersion: defaultVersion,
		enc:            enc,
	}
}

func (d *DeployFunctionRunners) Do(ctx context.Context, args DeployFunctionRunnersRequest) error {
	err := d.do(ctx, args)
	if err != nil {
		return temporal.NewNonRetryableApplicationError("failed to deploy function runners", "deployment_error", err)
	}
	return nil
}

func (d *DeployFunctionRunners) do(ctx context.Context, args DeployFunctionRunnersRequest) error {
	logger := d.logger

	dbtx, err := d.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error starting transaction").Log(ctx, d.logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	arepo := assetsrepo.New(dbtx)
	drepo := repo.New(dbtx)

	depfuncs, err := drepo.GetDeploymentFunctions(ctx, repo.GetDeploymentFunctionsParams{
		ProjectID:    args.ProjectID,
		DeploymentID: args.DeploymentID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error fetching deployment functions").Log(ctx, d.logger)
	}

	ids := make([]uuid.UUID, 0, len(depfuncs))
	assetids := make([]uuid.UUID, 0, len(depfuncs))
	for _, df := range depfuncs {
		ids = append(ids, df.ID)
		assetids = append(assetids, df.AssetID)
	}

	credrows, err := drepo.GetFunctionCredentialsBatch(ctx, repo.GetFunctionCredentialsBatchParams{
		ProjectID:    args.ProjectID,
		DeploymentID: args.DeploymentID,
		FunctionIds:  ids,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error fetching function credentials").Log(ctx, d.logger)
	}

	creds := make(map[uuid.UUID]repo.GetFunctionCredentialsBatchRow, len(credrows))
	for _, row := range credrows {
		creds[row.FunctionID] = row
	}

	urlrows, err := arepo.GetAssetsByID(ctx, assetsrepo.GetAssetsByIDParams{
		ProjectID: args.ProjectID,
		Ids:       assetids,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error reading function asset URLs").Log(ctx, d.logger)
	}

	assetURLs := make(map[uuid.UUID]assetsrepo.GetAssetsByIDRow, len(urlrows))
	for _, row := range urlrows {
		assetURLs[row.ID] = row
	}

	tasks := make([]deployFunctionRunnerTask, 0, len(depfuncs))
	stop := false
	for _, fnc := range depfuncs {
		if task, err := d.preflightFunction(ctx, logger, args, fnc, creds, assetURLs); err != nil {
			stop = true
		} else {
			tasks = append(tasks, task)
		}
	}
	if stop {
		return oops.E(oops.CodeInvalid, nil, "one or more functions failed preflight checks").Log(ctx, d.logger)
	}

	workers := pool.New().WithErrors().WithMaxGoroutines(runtime.GOMAXPROCS(0))
	for _, task := range tasks {
		workers.Go(func() error {
			version := d.resolveRunnerVersion(ctx, logger, task.projectID, task.deploymentID, task.functionID)
			_, err := d.deployer.Deploy(ctx, functions.RunnerDeployRequest{
				Version:      version,
				ProjectID:    task.projectID,
				DeploymentID: task.deploymentID,
				FunctionID:   task.functionID,
				AccessID:     task.accessID,
				Runtime:      task.runtime,
				Assets: []functions.RunnerAsset{{
					AssetID:       task.assetID,
					AssetURL:      task.assetURL,
					GuestPath:     "/data/code.zip",
					Mode:          0444,
					SHA256Sum:     task.assetSHA256,
					ContentLength: task.assetSize,
					ContentType:   task.assetContentType,
				}},
				BearerSecret: task.bearerSecret,
			})
			var serr *oops.ShareableError
			switch {
			case errors.As(err, &serr):
				return serr
			case err != nil:
				return oops.E(oops.CodeUnexpected, err, "error deploying function runner").Log(ctx, d.logger)
			}
			return nil
		})
	}
	if err := workers.Wait(); err != nil {
		return err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error committing transaction").Log(ctx, d.logger)
	}

	return nil
}

type deployFunctionRunnerTask struct {
	projectID        uuid.UUID
	deploymentID     uuid.UUID
	functionID       uuid.UUID
	accessID         uuid.UUID
	runtime          functions.Runtime
	bearerSecret     string
	assetID          uuid.UUID
	assetURL         *url.URL
	assetSHA256      string
	assetSize        int64
	assetContentType string
}

func (d *DeployFunctionRunners) preflightFunction(
	ctx context.Context,
	logger *slog.Logger,
	args DeployFunctionRunnersRequest,
	fnc repo.DeploymentsFunction,
	credentials map[uuid.UUID]repo.GetFunctionCredentialsBatchRow,
	fncAssets map[uuid.UUID]assetsrepo.GetAssetsByIDRow,
) (deployFunctionRunnerTask, error) {
	var empty deployFunctionRunnerTask

	if !functions.IsSupportedRuntime(fnc.Runtime) {
		return empty, oops.E(oops.CodeInvariantViolation, nil, "function has unsupported runtime %q", fnc.Runtime).Log(ctx, logger)
	}

	fa, ok := fncAssets[fnc.AssetID]
	if !ok {
		return empty, oops.E(oops.CodeInvariantViolation, nil, "function is missing asset URL").Log(ctx, logger)
	}
	if fa.Url == "" {
		return empty, oops.E(oops.CodeInvariantViolation, nil, "function has empty asset URL").Log(ctx, logger)
	}
	if fa.Sha256 == "" {
		return empty, oops.E(oops.CodeInvariantViolation, nil, "function has empty asset integrity hash").Log(ctx, logger)
	}

	assetURL, err := url.Parse(fa.Url)
	if err != nil {
		return empty, oops.E(oops.CodeInvariantViolation, err, "function has malformed asset URL").Log(ctx, logger)
	}

	c, ok := credentials[fnc.ID]
	if !ok {
		return empty, oops.E(oops.CodeInvariantViolation, nil, "function is missing credentials").Log(ctx, logger)
	}

	enckey := c.EncryptionKey.Reveal()
	if len(enckey) == 0 || c.BearerFormat.String == "" {
		return empty, oops.E(oops.CodeInvariantViolation, nil, "malformed credentials generated for function").Log(ctx, logger)
	}

	sec, err := d.enc.Decrypt(string(enckey))
	if err != nil {
		return empty, oops.E(oops.CodeInvariantViolation, err, "failed to unseal function credentials").Log(ctx, logger)
	}

	if len(sec) == 0 {
		return empty, oops.E(oops.CodeInvariantViolation, nil, "function has empty credentials").Log(ctx, logger)
	}

	return deployFunctionRunnerTask{
		projectID:        args.ProjectID,
		deploymentID:     args.DeploymentID,
		functionID:       fnc.ID,
		accessID:         c.ID,
		runtime:          functions.Runtime(fnc.Runtime),
		bearerSecret:     base64.StdEncoding.EncodeToString([]byte(sec)),
		assetID:          fnc.AssetID,
		assetURL:         assetURL,
		assetSHA256:      fa.Sha256,
		assetSize:        fa.ContentLength,
		assetContentType: fa.ContentType,
	}, nil
}

func (d *DeployFunctionRunners) resolveRunnerVersion(
	ctx context.Context,
	logger *slog.Logger,
	projectID uuid.UUID,
	deploymentID uuid.UUID,
	functionID uuid.UUID,
) functions.RunnerVersion {
	pr := funcrepo.New(d.db)
	pinned, err := pr.GetFunctionsRunnerVersion(ctx, funcrepo.GetFunctionsRunnerVersionParams{
		ProjectID:    projectID,
		FunctionID:   functionID,
		DeploymentID: deploymentID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "falling back to default runner version: failed to get functions runner version for project", attr.SlogError(err))
		return d.defaultVersion
	}

	return conv.Default(functions.RunnerVersion(pinned), d.defaultVersion)
}
