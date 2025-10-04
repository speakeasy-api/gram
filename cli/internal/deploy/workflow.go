package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/gen/types"
)

type WorkflowParams struct {
	APIKey      secret.Secret
	APIURL      *url.URL
	ProjectSlug string
}

func (p WorkflowParams) Validate() error {
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

type WorkflowState struct {
	Logger            *slog.Logger
	Params            WorkflowParams
	AssetsClient      *api.AssetsClient
	DeploymentsClient *api.DeploymentsClient
	NewAssets         []*deployments.AddOpenAPIv3DeploymentAssetForm
	Deployment        *types.Deployment
	Err               error
}

func (s *WorkflowState) Fail(err error) *WorkflowState {
	s.Err = err
	return s
}

func (s *WorkflowState) shouldHalt() bool {
	return s.Err != nil
}

func NewWorkflow(
	ctx context.Context,
	params WorkflowParams,
) *WorkflowState {
	state := &WorkflowState{
		Logger:            logging.PullLogger(ctx),
		Params:            params,
		AssetsClient:      nil,
		DeploymentsClient: nil,
		NewAssets:         nil,
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

func (s *WorkflowState) UploadAssets(
	ctx context.Context,
	sources []Source,
) *WorkflowState {
	if s.shouldHalt() {
		return s
	}

	newAssets := make(
		[]*deployments.AddOpenAPIv3DeploymentAssetForm,
		len(sources),
	)

	for idx, source := range sources {
		if err := source.Validate(); err != nil {
			return s.Fail(fmt.Errorf("invalid source: %w", err))
		}

		upReq := &UploadRequest{
			APIKey:       s.Params.APIKey,
			ProjectSlug:  s.Params.ProjectSlug,
			SourceReader: NewSourceReader(source),
		}
		asset, err := Upload(ctx, s.AssetsClient, upReq)
		if err != nil {
			return s.Fail(fmt.Errorf("failed to upload asset: %w", err))
		}

		newAssets[idx] = asset
	}

	s.NewAssets = newAssets
	return s
}

func (s *WorkflowState) EvolveActiveDeployment(
	ctx context.Context,
) *WorkflowState {
	if s.shouldHalt() {
		return s
	}

	active, err := s.DeploymentsClient.GetActiveDeployment(
		ctx,
		s.Params.APIKey,
		s.Params.ProjectSlug,
	)
	if err != nil {
		return s.Fail(fmt.Errorf("failed to get active deployment: %w", err))
	}

	if active == nil {
		return s
	}

	s.Deployment = active
	evolved, err := s.DeploymentsClient.Evolve(ctx, api.EvolveRequest{
		Assets:       s.NewAssets,
		APIKey:       s.Params.APIKey,
		DeploymentID: s.Deployment.ID,
		ProjectSlug:  s.Params.ProjectSlug,
	})
	if err != nil {
		return s.Fail(fmt.Errorf("failed to evolve deployment: %w", err))
	}

	s.Logger.InfoContext(
		ctx,
		"Updated successfully",
		slog.String("deployment_id", evolved.Deployment.ID),
	)

	s.Deployment = evolved.Deployment

	return s
}

func (s *WorkflowState) OrCreateDeployment(ctx context.Context) *WorkflowState {
	if s.shouldHalt() {
		return s
	}

	if s.Deployment == nil {
		return s.CreateDeployment(ctx, "")
	}

	return s
}

func (s *WorkflowState) CreateDeployment(
	ctx context.Context,
	idem string,
) *WorkflowState {
	if s.shouldHalt() {
		return s
	}

	createReq := api.CreateDeploymentRequest{
		APIKey:          s.Params.APIKey,
		IdempotencyKey:  idem,
		OpenAPIv3Assets: s.NewAssets,
		ProjectSlug:     s.Params.ProjectSlug,
	}
	result, err := s.DeploymentsClient.CreateDeployment(ctx, createReq)
	if err != nil {
		return s.Fail(fmt.Errorf("failed to create deployment: %w", err))
	}

	s.Logger.InfoContext(
		ctx,
		"Created successfully",
		slog.String("deployment_id", result.Deployment.ID),
	)

	s.Deployment = result.Deployment

	return s
}

func (s *WorkflowState) Poll(ctx context.Context) *WorkflowState {
	if s.shouldHalt() {
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
			deployment, err := s.DeploymentsClient.GetDeployment(
				ctx,
				s.Params.APIKey,
				s.Params.ProjectSlug,
				s.Deployment.ID,
			)
			if err != nil {
				return s.Fail(
					fmt.Errorf("deployment polling failed: %w", err),
				)
			}
			s.Deployment = deployment

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
