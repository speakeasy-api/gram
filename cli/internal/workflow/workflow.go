package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/deploy"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/gen/types"
)

type Params struct {
	APIKey      secret.Secret
	APIURL      *url.URL
	ProjectSlug string
}

func (p Params) Validate() error {
	if p.ProjectSlug == "" {
		return fmt.Errorf("project slug is required")
	}
	if p.APIKey.Reveal() == "" {
		return fmt.Errorf("API key is required")
	}
	if p.APIURL == nil {
		return fmt.Errorf("API URL is required")
	}
	return nil
}

type Workflow struct {
	Logger            *slog.Logger
	Params            Params
	AssetsClient      *api.AssetsClient
	DeploymentsClient *api.DeploymentsClient
	NewOpenAPIAssets  []*deployments.AddOpenAPIv3DeploymentAssetForm
	NewFunctionAssets []*deployments.AddFunctionsForm
	Deployment        *types.Deployment
	Err               error
}

// Fail indicates an unexpected error and halts execution.
func (s *Workflow) Fail(err error) *Workflow {
	s.Err = err
	return s
}

// Failed indicates an unexpected error has interrupted the workflow.
func (s *Workflow) Failed() bool {
	return s.Err != nil
}

func New(
	ctx context.Context,
	logger *slog.Logger,
	params Params,
) *Workflow {
	state := &Workflow{
		Logger:            logger,
		Params:            params,
		AssetsClient:      nil,
		DeploymentsClient: nil,
		NewOpenAPIAssets:  nil,
		NewFunctionAssets: nil,
		Deployment:        nil,
		Err:               nil,
	}

	if err := params.Validate(); err != nil {
		return state.Fail(err)
	}

	state.AssetsClient = api.NewAssetsClient(&api.AssetsClientOptions{
		Scheme: params.APIURL.Scheme,
		Host:   params.APIURL.Host,
	})
	state.DeploymentsClient = api.NewDeploymentsClient(
		&api.DeploymentsClientOptions{
			Scheme: params.APIURL.Scheme,
			Host:   params.APIURL.Host,
		},
	)

	return state
}

func (s *Workflow) UploadAssets(
	ctx context.Context,
	sources []deploy.Source,
) *Workflow {
	if s.Failed() {
		return s
	}

	s.Logger.InfoContext(ctx, "uploading assets")

	newOpenAPIAssets := make(
		[]*deployments.AddOpenAPIv3DeploymentAssetForm,
		0,
		len(sources),
	)
	newFunctionAssets := make(
		[]*deployments.AddFunctionsForm,
		0,
		len(sources),
	)

	for _, source := range sources {
		if err := source.Validate(); err != nil {
			return s.Fail(fmt.Errorf("invalid source: %w", err))
		}

		upReq := &deploy.UploadRequest{
			APIKey:       s.Params.APIKey,
			ProjectSlug:  s.Params.ProjectSlug,
			SourceReader: deploy.NewSourceReader(source),
		}
		asset, err := deploy.Upload(ctx, s.AssetsClient, upReq)
		if err != nil {
			return s.Fail(fmt.Errorf("failed to upload asset: %w", err))
		}

		switch source.Type {
		case deploy.SourceTypeOpenAPIV3:
			form := &deployments.AddOpenAPIv3DeploymentAssetForm{
				AssetID: asset.AssetID,
				Name:    asset.Name,
				Slug:    asset.Slug,
			}
			newOpenAPIAssets = append(newOpenAPIAssets, form)
		case deploy.SourceTypeFunction:
			form := &deployments.AddFunctionsForm{
				AssetID: asset.AssetID,
				Name:    asset.Name,
				Slug:    asset.Slug,
				Runtime: asset.Runtime,
			}
			newFunctionAssets = append(newFunctionAssets, form)
		}
	}

	s.NewOpenAPIAssets = newOpenAPIAssets
	s.NewFunctionAssets = newFunctionAssets
	return s
}

func (s *Workflow) EvolveDeployment(
	ctx context.Context,
) *Workflow {
	if s.Failed() {
		return s
	}

	if s.Deployment == nil {
		return s.Fail(fmt.Errorf("update failed: no deployment found"))
	}
	s.Logger.InfoContext(
		ctx,
		"updating deployment",
		slog.String("deployment_id", s.Deployment.ID),
	)
	evolved, err := s.DeploymentsClient.Evolve(ctx, api.EvolveRequest{
		OpenAPIv3Assets: s.NewOpenAPIAssets,
		Functions:       s.NewFunctionAssets,
		APIKey:          s.Params.APIKey,
		DeploymentID:    s.Deployment.ID,
		ProjectSlug:     s.Params.ProjectSlug,
	})
	if err != nil {
		return s.Fail(fmt.Errorf("failed to evolve deployment: %w", err))
	}

	s.Logger.InfoContext(
		ctx,
		"updated deployment",
		slog.String("deployment_id", evolved.Deployment.ID),
	)

	s.Deployment = evolved.Deployment

	return s
}

func (s *Workflow) CreateDeployment(
	ctx context.Context,
	idem string,
) *Workflow {
	if s.Failed() {
		return s
	}

	s.Logger.InfoContext(ctx, "creating deployment")
	createReq := api.CreateDeploymentRequest{
		APIKey:          s.Params.APIKey,
		IdempotencyKey:  idem,
		OpenAPIv3Assets: s.NewOpenAPIAssets,
		Functions:       s.NewFunctionAssets,
		ProjectSlug:     s.Params.ProjectSlug,
	}
	result, err := s.DeploymentsClient.CreateDeployment(ctx, createReq)
	if err != nil {
		return s.Fail(fmt.Errorf("failed to create deployment: %w", err))
	}

	s.Logger.InfoContext(
		ctx,
		"created new deployment",
		slog.String("deployment_id", result.Deployment.ID),
	)

	s.Deployment = result.Deployment

	return s
}

func (s *Workflow) LoadDeploymentByID(
	ctx context.Context,
	deploymentID string,
) *Workflow {
	if s.Failed() {
		return s
	}

	result, err := s.DeploymentsClient.GetDeployment(
		ctx,
		s.Params.APIKey,
		s.Params.ProjectSlug,
		deploymentID,
	)
	if err != nil {
		return s.Fail(
			fmt.Errorf("failed to get deployment '%s': %w", deploymentID, err),
		)
	}

	s.Deployment = result
	return s
}

func (s *Workflow) LoadLatestDeployment(
	ctx context.Context,
) *Workflow {
	if s.Failed() {
		return s
	}

	result, err := s.DeploymentsClient.GetLatestDeployment(
		ctx,
		s.Params.APIKey,
		s.Params.ProjectSlug,
	)
	if err != nil {
		return s.Fail(fmt.Errorf("failed to get latest deployment: %w", err))
	}

	s.Deployment = result
	return s
}

func (s *Workflow) LoadActiveDeployment(
	ctx context.Context,
) *Workflow {
	if s.Failed() {
		return s
	}

	result, err := s.DeploymentsClient.GetActiveDeployment(
		ctx,
		s.Params.APIKey,
		s.Params.ProjectSlug,
	)
	if err != nil {
		return s.Fail(fmt.Errorf("failed to get active deployment: %w", err))
	}

	s.Deployment = result
	return s
}

func (s *Workflow) Poll(ctx context.Context) *Workflow {
	if s.Failed() {
		return s
	}

	if s.Deployment == nil {
		return s.Fail(fmt.Errorf("poll failed: no deployment found"))
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	s.Logger.InfoContext(
		ctx,
		"Polling deployment status...",
		slog.String("deployment_id", s.Deployment.ID),
	)

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return s.Fail(
					fmt.Errorf("deployment polling timed out after 2 minutes"),
				)
			}
			return s.Fail(
				fmt.Errorf("deployment polling cancelled: %w", ctx.Err()),
			)

		case <-ticker.C:
			s.LoadDeploymentByID(ctx, s.Deployment.ID)
			s.Logger.DebugContext(ctx, "Deployment status check",
				slog.String("deployment_id", s.Deployment.ID),
				slog.String("status", s.Deployment.Status),
			)

			switch s.Deployment.Status {
			case "completed", "failed":
				return s
			case "pending":
				continue
			default:
				s.Logger.WarnContext(
					ctx,
					"Unknown deployment status",
					slog.String("status", s.Deployment.Status),
				)
				continue
			}
		}
	}
}
