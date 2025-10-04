package upload

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/deploy"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/urfave/cli/v2"
)

type workflowParams struct {
	apiKey      secret.Secret
	apiURL      *url.URL
	projectSlug string
}

func (p workflowParams) Validate() error {
	if p.projectSlug == "" {
		return fmt.Errorf("project slug is required")
	}
	if p.apiKey.Reveal() == "" {
		return fmt.Errorf("API key is required")
	}
	if p.apiURL == nil {
		return fmt.Errorf("API URL is required")
	}
	return nil
}

type workflowState struct {
	logger            *slog.Logger
	params            workflowParams
	assetsClient      *api.AssetsClient
	deploymentsClient *api.DeploymentsClient
	uploadedAssets    []*deployments.AddOpenAPIv3DeploymentAssetForm
	deployment        *types.Deployment
	Done              bool
	Err               error
}

func (s *workflowState) Fail(err error) *workflowState {
	s.Err = err
	return s
}

func (s *workflowState) shouldHalt() bool {
	return s.Done || s.Err != nil
}

func newWorkflow(
	ctx context.Context,
	params workflowParams,
) *workflowState {
	state := &workflowState{
		logger:            logging.PullLogger(ctx),
		params:            params,
		assetsClient:      nil,
		deploymentsClient: nil,
		uploadedAssets:    nil,
		deployment:        nil,
		Done:              false,
		Err:               nil,
	}

	if err := params.Validate(); err != nil {
		return state.Fail(err)
	}

	state.assetsClient = api.NewAssetsClient(&api.AssetsClientOptions{
		Scheme: params.apiURL.Scheme,
		Host:   params.apiURL.Host,
	})
	state.deploymentsClient = api.NewDeploymentsClient(
		&api.DeploymentsClientOptions{
			Scheme: params.apiURL.Scheme,
			Host:   params.apiURL.Host,
		},
	)

	return state
}

func (s *workflowState) UploadAssets(
	ctx context.Context,
	sources []deploy.Source,
) *workflowState {
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

		asset, err := deploy.Upload(
			ctx,
			s.logger,
			s.assetsClient,
			&deploy.UploadRequest{
				APIKey:       s.params.apiKey,
				ProjectSlug:  s.params.projectSlug,
				SourceReader: deploy.NewSourceReader(source),
			},
		)
		if err != nil {
			return s.Fail(fmt.Errorf("failed to upload asset: %w", err))
		}

		newAssets[idx] = asset
	}

	s.uploadedAssets = newAssets
	return s
}

func (s *workflowState) EvolveActiveDeployment(
	ctx context.Context,
) *workflowState {
	if s.shouldHalt() {
		return s
	}

	s.logger.DebugContext(
		ctx,
		"Fetching active deployment",
		slog.String("project", s.params.projectSlug),
	)

	active, err := s.deploymentsClient.GetActiveDeployment(
		ctx,
		s.params.apiKey,
		s.params.projectSlug,
	)
	if err != nil {
		return s.Fail(fmt.Errorf("failed to get active deployment: %w", err))
	}

	if active.Deployment == nil {
		return s
	}

	depID := active.Deployment.ID
	s.logger.InfoContext(
		ctx,
		"Found active deployment",
		slog.String("deployment_id", depID),
	)

	result, err := s.deploymentsClient.Evolve(ctx, api.EvolveRequest{
		Assets:       s.uploadedAssets,
		APIKey:       s.params.apiKey,
		DeploymentID: depID,
		ProjectSlug:  s.params.projectSlug,
	})
	if err != nil {
		return s.Fail(fmt.Errorf("failed to evolve deployment: %w", err))
	}

	s.logger.InfoContext(
		ctx,
		"Updated successfully",
		slog.String("deployment_id", result.Deployment.ID),
	)

	s.deployment = result.Deployment
	s.Done = true

	return s
}

func (s *workflowState) OrCreateDeployment(ctx context.Context) *workflowState {
	if s.shouldHalt() {
		return s
	}

	if s.deployment != nil {
		return s
	}

	result, err := s.deploymentsClient.CreateDeployment(
		ctx,
		api.CreateDeploymentRequest{
			OpenAPIv3Assets: s.uploadedAssets,

			APIKey:         s.params.apiKey,
			ProjectSlug:    s.params.projectSlug,
			IdempotencyKey: "",
		},
	)
	if err != nil {
		return s.Fail(fmt.Errorf("failed to create deployment: %w", err))
	}

	s.logger.InfoContext(
		ctx,
		"Created successfully",
		slog.String("deployment_id", result.Deployment.ID),
	)

	s.deployment = result.Deployment
	s.Done = true

	return s
}

func parseSource(c *cli.Context) deploy.Source {
	return deploy.Source{
		Type:     deploy.SourceType(c.String("type")),
		Location: c.String("location"),
		Name:     c.String("name"),
		Slug:     c.String("slug"),
	}
}
