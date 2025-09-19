package api

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/cli/internal/env"

	"github.com/speakeasy-api/gram/server/gen/deployments"
	depl_client "github.com/speakeasy-api/gram/server/gen/http/deployments/client"
	goahttp "goa.design/goa/v3/http"
)

type DeploymentsClient struct {
	client *deployments.Client
}

func NewDeploymentsClient() *DeploymentsClient {
	return &DeploymentsClient{
		client: newDeploymentClient(),
	}
}

// DeploymentCreator represents a request for creating a deployment
type DeploymentCreator interface {
	CredentialGetter

	// GetIdempotencyKey returns a unique identifier that will mitigate against
	// duplicate deployments.
	GetIdempotencyKey() string

	// GetOpenAPIv3Assets returns the OpenAPI v3 assets to include in the
	// deployment.
	GetOpenAPIv3Assets() []*deployments.AddOpenAPIv3DeploymentAssetForm
}

// CreateDeployment creates a remote deployment.
func (c *DeploymentsClient) CreateDeployment(
	ctx context.Context,
	dc DeploymentCreator,
) (*deployments.CreateDeploymentResult, error) {
	apiKey := dc.GetApiKey()
	projectSlug := dc.GetProjectSlug()

	payload := &deployments.CreateDeploymentPayload{
		ApikeyToken:      &apiKey,
		ProjectSlugInput: &projectSlug,
		IdempotencyKey:   dc.GetIdempotencyKey(),
		Openapiv3Assets:  dc.GetOpenAPIv3Assets(),
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

func newDeploymentClient() *deployments.Client {
	h := deploymentService()
	return deployments.NewClient(
		h.GetDeployment(),
		h.GetLatestDeployment(),
		h.CreateDeployment(),
		h.Evolve(),
		h.Redeploy(),
		h.ListDeployments(),
		h.GetDeploymentLogs(),
	)
}

func deploymentService() *depl_client.Client {
	doer := goaSharedHTTPClient

	scheme := env.APIScheme()
	host := env.APIHost()
	enc := goahttp.RequestEncoder
	dec := goahttp.ResponseDecoder
	restoreBody := true // Enable body restoration to allow reading raw response on decode errors

	return depl_client.NewClient(scheme, host, doer, enc, dec, restoreBody)
}
