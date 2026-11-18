package api

import (
	"context"
	"fmt"
	"net/url"

	cliauth "github.com/speakeasy-api/gram/server/gen/cli_auth"
	"github.com/speakeasy-api/gram/server/gen/http/cli_auth/client"
	goahttp "goa.design/goa/v3/http"
)

// CliAuthClient wraps the generated cliAuth service client, which redeems the
// one-time code a browser login hands back to the CLI.
type CliAuthClient struct {
	client *cliauth.Client
}

// NewCliAuthClientFromURL creates a new cliAuth client from a URL.
func NewCliAuthClientFromURL(apiURL *url.URL) *CliAuthClient {
	httpClient := client.NewClient(
		apiURL.Scheme,
		apiURL.Host,
		goaSharedHTTPClient,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		true,
	)

	return &CliAuthClient{client: cliauth.NewClient(httpClient.Authorize(), httpClient.Redeem())}
}

// Redeem exchanges a one-time code and the PKCE verifier it was bound to for
// the API key the browser login authorized.
func (c *CliAuthClient) Redeem(ctx context.Context, code, codeVerifier string) (*cliauth.RedeemResult, error) {
	result, err := c.client.Redeem(ctx, &cliauth.RedeemPayload{
		Code:         code,
		CodeVerifier: codeVerifier,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to redeem login code: %w", err)
	}

	return result, nil
}
