package api

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/server/gen/deployments"
	depl_client "github.com/speakeasy-api/gram/server/gen/http/deployments/client"
	"github.com/speakeasy-api/gram/server/gen/types"
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
	NonBlocking     bool
	ProjectSlug     string
	IdempotencyKey  string
	OpenAPIv3Assets []*deployments.AddOpenAPIv3DeploymentAssetForm
	Functions       []*deployments.AddFunctionsForm
}

// CreateDeployment creates a remote deployment.
func (c *DeploymentsClient) CreateDeployment(
	ctx context.Context,
	req CreateDeploymentRequest,
) (*deployments.CreateDeploymentResult, error) {
	key := req.APIKey.Reveal()
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = uuid.New().String()
	}
	payload := &deployments.CreateDeploymentPayload{
		ApikeyToken:      &key,
		NonBlocking:      &req.NonBlocking,
		ProjectSlugInput: &req.ProjectSlug,
		IdempotencyKey:   req.IdempotencyKey,
		Openapiv3Assets:  req.OpenAPIv3Assets,
		Functions:        req.Functions,
		SessionToken:     nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
		Packages:         nil,
		ExternalMcps:     nil,
	}
	result, err := c.client.CreateDeployment(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("api error: %w", err)
	}

	return result, nil
}

// GetDeployment retrieves a deployment by its ID.
func (c *DeploymentsClient) GetDeployment(
	ctx context.Context,
	apiKey secret.Secret,
	projectSlug string,
	deploymentID string,
) (*types.Deployment, error) {
	key := apiKey.Reveal()
	result, err := c.client.GetDeployment(ctx, &deployments.GetDeploymentPayload{
		ApikeyToken:      &key,
		ProjectSlugInput: &projectSlug,
		ID:               deploymentID,
		SessionToken:     nil,
	})
	if err != nil {
		return nil, fmt.Errorf("api error: %w", err)
	}

	return &types.Deployment{
		ID:                 result.ID,
		OrganizationID:     result.OrganizationID,
		ProjectID:          result.ProjectID,
		UserID:             result.UserID,
		CreatedAt:          result.CreatedAt,
		Status:             result.Status,
		IdempotencyKey:     result.IdempotencyKey,
		GithubRepo:         result.GithubRepo,
		GithubPr:           result.GithubPr,
		GithubSha:          result.GithubSha,
		ExternalID:         result.ExternalID,
		ExternalURL:        result.ExternalURL,
		ClonedFrom:         result.ClonedFrom,
		Openapiv3ToolCount: result.Openapiv3ToolCount,
		Openapiv3Assets:    result.Openapiv3Assets,
		FunctionsToolCount:   result.FunctionsToolCount,
		FunctionsAssets:      result.FunctionsAssets,
		ExternalMcpToolCount: result.ExternalMcpToolCount,
		Packages:             result.Packages,
		ExternalMcps:         result.ExternalMcps,
	}, nil
}

// GetLatestDeployment retrieves the latest deployment for a project.
func (c *DeploymentsClient) GetLatestDeployment(
	ctx context.Context,
	apiKey secret.Secret,
	projectSlug string,
) (*types.Deployment, error) {
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
		return nil, fmt.Errorf("api error: %w", err)
	}

	return result.Deployment, nil
}

// GetActiveDeployment retrieves the active deployment for a project.
func (c *DeploymentsClient) GetActiveDeployment(
	ctx context.Context,
	apiKey secret.Secret,
	projectSlug string,
) (*types.Deployment, error) {
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
		return nil, fmt.Errorf("api error: %w", err)
	}

	return result.Deployment, nil
}

// EvolveRequest lists the assets to add to a deployment.
type EvolveRequest struct {
	OpenAPIv3Assets []*deployments.AddOpenAPIv3DeploymentAssetForm
	NonBlocking     bool
	Functions       []*deployments.AddFunctionsForm
	APIKey          secret.Secret
	DeploymentID    *string
	ProjectSlug     string
}

// Evolve adds assets to an existing deployment.
func (c *DeploymentsClient) Evolve(
	ctx context.Context,
	req EvolveRequest,
) (*deployments.EvolveResult, error) {
	key := req.APIKey.Reveal()
	result, err := c.client.Evolve(ctx, &deployments.EvolvePayload{
		ApikeyToken:            &key,
		NonBlocking:            &req.NonBlocking,
		ProjectSlugInput:       &req.ProjectSlug,
		DeploymentID:           req.DeploymentID,
		UpsertOpenapiv3Assets:  req.OpenAPIv3Assets,
		UpsertFunctions:        req.Functions,
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
		UpsertPackages:         []*deployments.AddPackageForm{},
		SessionToken:           nil,
		UpsertExternalMcps:     nil,
		ExcludeExternalMcps:    []string{},
	})
	if err != nil {
		return nil, fmt.Errorf("api error: %w", err)
	}

	return result, nil
}
