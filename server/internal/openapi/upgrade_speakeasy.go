package openapi

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/openapi/openapi"
)

type upgradeOpenAPI30To31ResultSpeakeasy struct {
	Upgraded bool
	Document *openapi.OpenAPI
	Issues   []error
}

func upgradeOpenAPI30To31Speakeasy(ctx context.Context, doc *openapi.OpenAPI) (*upgradeOpenAPI30To31ResultSpeakeasy, error) {
	upgraded, err := openapi.Upgrade(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("error upgrading document: %w", err)
	}

	return &upgradeOpenAPI30To31ResultSpeakeasy{
		Upgraded: upgraded,
		Document: doc,
		Issues:   []error{},
	}, nil
}
