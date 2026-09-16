package identityproviders

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/platformmcp"
)

type ApplicationCatalogCandidate struct {
	ProviderKey string
	CatalogRef  string
	Name        string
}

type ApplicationCatalogDetails struct {
	ProviderKey string
	CatalogRef  string
	Name        string
	RemoteURL   string
}

type ApplicationCatalog interface {
	Search(ctx context.Context, query string) ([]ApplicationCatalogCandidate, error)
	Inspect(ctx context.Context, providerKey, catalogRef string) (ApplicationCatalogDetails, error)
}

type platformApplicationCatalog struct {
	catalog platformmcp.Catalog
}

func NewApplicationCatalog(catalog platformmcp.Catalog) ApplicationCatalog {
	return &platformApplicationCatalog{catalog: catalog}
}

func (c *platformApplicationCatalog) Search(ctx context.Context, query string) ([]ApplicationCatalogCandidate, error) {
	candidates, err := c.catalog.Search(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("search Platform MCP catalogue: %w", err)
	}
	result := make([]ApplicationCatalogCandidate, len(candidates))
	for i, candidate := range candidates {
		result[i] = ApplicationCatalogCandidate{
			ProviderKey: candidate.ProviderKey,
			CatalogRef:  candidate.CatalogRef,
			Name:        candidate.Name,
		}
	}
	return result, nil
}

func (c *platformApplicationCatalog) Inspect(ctx context.Context, providerKey, catalogRef string) (ApplicationCatalogDetails, error) {
	details, err := c.catalog.Inspect(ctx, providerKey, catalogRef)
	if err != nil {
		return ApplicationCatalogDetails{}, fmt.Errorf("inspect Platform MCP catalogue entry: %w", err)
	}
	return ApplicationCatalogDetails{
		ProviderKey: details.ProviderKey,
		CatalogRef:  details.CatalogRef,
		Name:        details.Name,
		RemoteURL:   details.ConcreteRemoteURL(),
	}, nil
}
