package api

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/server/gen/deployments"
	depl_client "github.com/speakeasy-api/gram/server/gen/http/deployments/client"
	goahttp "goa.design/goa/v3/http"
)

type DeploymentsClientOptions struct {
	Scheme string
	Host   string
}

type DeploymentsClient struct {
	client *deployments.Client
}

func NewDeploymentsClient(options *DeploymentsClientOptions) *DeploymentsClient {
	doer := goaSharedHTTPClient

	enc := goahttp.RequestEncoder
	dec := goahttp.ResponseDecoder
	restoreBody := true // Enable body restoration to allow reading raw response on decode errors

	h := depl_client.NewClient(options.Scheme, options.Host, doer, enc, dec, restoreBody)

	client := deployments.NewClient(
		h.GetDeployment(),
		h.GetLatestDeployment(),
		h.GetActiveDeployment(),
		h.CreateDeployment(),
		h.Evolve(),
		h.Redeploy(),
		h.ListDeployments(),
		h.GetDeploymentLogs(),
	)

	return &DeploymentsClient{client: client}
}

type CreateDeploymentRequest struct {
	APIKey          secret.Secret
	ProjectSlug     string
	IdempotencyKey  string
	OpenAPIv3Assets []*deployments.AddOpenAPIv3DeploymentAssetForm
}

// CreateDeployment creates a remote deployment.
func (c *DeploymentsClient) CreateDeployment(
	ctx context.Context,
	req CreateDeploymentRequest,
) (*deployments.CreateDeploymentResult, error) {
	key := req.APIKey.Reveal()
	payload := &deployments.CreateDeploymentPayload{
		ApikeyToken:      &key,
		ProjectSlugInput: &req.ProjectSlug,
		IdempotencyKey:   req.IdempotencyKey,
		Openapiv3Assets:  req.OpenAPIv3Assets,
		Functions:        []*deployments.AddFunctionsForm{},
		SessionToken:     nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
		Packages:         nil,
	}
	result, err := c.client.CreateDeployment(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment: %w", err)
	}

	return result, nil
}

// GetDeployment retrieves a deployment by its ID.
func (c *DeploymentsClient) GetDeployment(
	ctx context.Context,
	apiKey secret.Secret,
	projectSlug string,
	deploymentID string,
) (*deployments.GetDeploymentResult, error) {
	key := apiKey.Reveal()
	result, err := c.client.GetDeployment(ctx, &deployments.GetDeploymentPayload{
		ApikeyToken:      &key,
		ProjectSlugInput: &projectSlug,
		ID:               deploymentID,
		SessionToken:     nil,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	return result, nil
}

// GetLatestDeployment retrieves the latest deployment for a project.
func (c *DeploymentsClient) GetLatestDeployment(
	ctx context.Context,
	apiKey secret.Secret,
	projectSlug string,
) (*deployments.GetLatestDeploymentResult, error) {
	key := apiKey.Reveal()
	result, err := c.client.GetLatestDeployment(
		ctx,
		&deployments.GetLatestDeploymentPayload{
			ApikeyToken:      &key,
			ProjectSlugInput: &projectSlug,
			SessionToken:     nil,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest deployment: %w", err)
	}

	return result, nil
}

// GetActiveDeployment retrieves the active deployment for a project.
func (c *DeploymentsClient) GetActiveDeployment(
	ctx context.Context,
	apiKey secret.Secret,
	projectSlug string,
) (*deployments.GetActiveDeploymentResult, error) {
	key := apiKey.Reveal()
	result, err := c.client.GetActiveDeployment(
		ctx,
		&deployments.GetActiveDeploymentPayload{
			ApikeyToken:      &key,
			ProjectSlugInput: &projectSlug,
			SessionToken:     nil,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get active deployment: %w", err)
	}

	return result, nil
}

// EvolveRequest lists the assets to add to a deployment.
type EvolveRequest struct {
	APIKey       secret.Secret
	ProjectSlug  string
	DeploymentID string
	Assets       []*deployments.AddOpenAPIv3DeploymentAssetForm
}

// Evolve adds assets to an existing deployment.
func (c *DeploymentsClient) Evolve(
	ctx context.Context,
	req EvolveRequest,
) (*deployments.EvolveResult, error) {
	key := req.APIKey.Reveal()
	result, err := c.client.Evolve(ctx, &deployments.EvolvePayload{
		ApikeyToken:            &key,
		ProjectSlugInput:       &req.ProjectSlug,
		DeploymentID:           &req.DeploymentID,
		UpsertOpenapiv3Assets:  req.Assets,
		UpsertFunctions:        []*deployments.AddFunctionsForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
		UpsertPackages:         []*deployments.AddPackageForm{},
		SessionToken:           nil,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to evolve deployment: %w", err)
	}

	return result, nil
}
