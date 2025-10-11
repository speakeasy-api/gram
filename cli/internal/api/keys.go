package api

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/cli/internal/secret"
	key_client "github.com/speakeasy-api/gram/server/gen/http/keys/client"
	"github.com/speakeasy-api/gram/server/gen/keys"
	goahttp "goa.design/goa/v3/http"
)

type KeysClientOptions struct {
	Scheme string
	Host   string
}

type KeysClient struct {
	client *keys.Client
}

func NewKeysClient(options *KeysClientOptions) *KeysClient {
	doer := goaSharedHTTPClient

	enc := goahttp.RequestEncoder
	dec := goahttp.ResponseDecoder
	restoreBody := true // Enable body restoration to allow reading raw response on decode errors

	h := key_client.NewClient(
		options.Scheme, options.Host, doer, enc, dec, restoreBody,
	)

	client := keys.NewClient(
		h.CreateKey(),
		h.ListKeys(),
		h.RevokeKey(),
		h.VerifyKey(),
	)

	return &KeysClient{client: client}
}

type VerifyKeyRequest struct {
	APIKey secret.Secret
}

func (c *KeysClient) VerifyKey(
	ctx context.Context,
	req *VerifyKeyRequest,
) (*keys.ValidateKeyResult, error) {
	key := req.APIKey.Reveal()

	payload := &keys.VerifyKeyPayload{
		ApikeyToken: &key,
	}

	result, err := c.client.VerifyKey(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to verify api key: %w", err)
	}

	return result, nil
}
