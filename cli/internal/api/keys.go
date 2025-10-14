package api

import (
	"context"
	"fmt"
	"net/url"

	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/server/gen/http/keys/client"
	"github.com/speakeasy-api/gram/server/gen/keys"
	goahttp "goa.design/goa/v3/http"
)

// KeysClientOptions configures the keys client.
type KeysClientOptions struct {
	Scheme string
	Host   string
}

// KeysClient wraps the generated keys service client.
type KeysClient struct {
	client *keys.Client
}

// NewKeysClient creates a new keys client.
func NewKeysClient(options *KeysClientOptions) *KeysClient {
	doer := goaSharedHTTPClient
	enc := goahttp.RequestEncoder
	dec := goahttp.ResponseDecoder
	restoreBody := true

	httpClient := client.NewClient(
		options.Scheme,
		options.Host,
		doer,
		enc,
		dec,
		restoreBody,
	)

	keysClient := keys.NewClient(
		httpClient.CreateKey(),
		httpClient.ListKeys(),
		httpClient.RevokeKey(),
		httpClient.VerifyKey(),
	)

	return &KeysClient{client: keysClient}
}

// NewKeysClientFromURL creates a new keys client from a URL.
func NewKeysClientFromURL(apiURL *url.URL) *KeysClient {
	return NewKeysClient(&KeysClientOptions{
		Scheme: apiURL.Scheme,
		Host:   apiURL.Host,
	})
}

// Verify validates an API key and returns organization and project info.
func (c *KeysClient) Verify(
	ctx context.Context,
	apiKey secret.Secret,
) (*keys.ValidateKeyResult, error) {
	key := apiKey.Reveal()
	payload := &keys.VerifyKeyPayload{
		ApikeyToken: &key,
	}

	result, err := c.client.VerifyKey(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to verify API key: %w", err)
	}

	return result, nil
}
