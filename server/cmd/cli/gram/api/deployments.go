package api

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/speakeasy-api/gram/server/cmd/cli/gram/env"

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

func (c *DeploymentsClient) ListDeployments(
	apiKey string,
	projectSlug string,
) *deployments.ListDeploymentResult {
	ctx := context.Background()
	payload := &deployments.ListDeploymentsPayload{
		ApikeyToken:      &apiKey,
		ProjectSlugInput: &projectSlug,
		SessionToken:     nil,
		Cursor:           nil,
	}

	result, err := c.client.ListDeployments(ctx, payload)
	if err != nil {
		log.Fatalf("Error calling ListDeployments: %v", err)
	}

	return result
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
	dc DeploymentCreator,
) (*deployments.CreateDeploymentResult, error) {
	ctx := context.Background()

	apiKey := dc.GetApiKey()
	projectSlug := dc.GetProjectSlug()

	payload := &deployments.CreateDeploymentPayload{
		ApikeyToken:      &apiKey,
		ProjectSlugInput: &projectSlug,
		IdempotencyKey:   dc.GetIdempotencyKey(),
		Openapiv3Assets:  dc.GetOpenAPIv3Assets(),
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
		enhancedErr := enhanceDeploymentError(err)
		return nil, fmt.Errorf("failed to create deployment: %w", enhancedErr)
	}

	return result, nil
}

// enhanceDeploymentError provides more context for deployment errors,
// especially decode errors that may indicate server issues.
func enhanceDeploymentError(err error) error {
	errStr := err.Error()

	// Check if this is a decode error that suggests the server returned HTML instead of JSON
	if strings.Contains(errStr, "can't decode") && strings.Contains(errStr, "text/html") {
		return fmt.Errorf("%w\n\nThis error typically occurs when the server returns an HTML error page (e.g., 500 error) instead of the expected JSON response. Check server logs or try again later", err)
	}

	return err
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
