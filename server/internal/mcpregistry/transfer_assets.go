package mcpregistry

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assets"
	assetrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
)

// CatalogAssetStore uses configured storage, not network requests to bundle URLs.
// Construct separate source and destination instances with their own services.
type CatalogAssetStore struct {
	Registry *Service
	Images   *assets.Service
	Blob     assets.BlobStore
	Logger   *slog.Logger
	Operator string
}

func (a *CatalogAssetStore) ReadCatalogAsset(ctx context.Context, src string) ([]byte, error) {
	id, err := a.Registry.catalogIconAssetID(src)
	if err != nil {
		return nil, fmt.Errorf("read catalog asset: %w", err)
	}
	valid, err := repo.New(a.Registry.db).IsCatalogSVGAsset(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read catalog asset: %w", err)
	}
	if !valid {
		return nil, fmt.Errorf("catalog asset must be a public platform SVG")
	}
	row, err := assetrepo.New(a.Registry.db).GetImageAssetURL(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read catalog asset: %w", err)
	}
	u, err := url.Parse(row.Url)
	if err != nil {
		return nil, fmt.Errorf("read catalog asset: %w", err)
	}
	r, err := a.Blob.Read(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("read catalog asset: %w", err)
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(io.LimitReader(r, assets.MaxFileSizeImage+1))
	if err != nil {
		return nil, fmt.Errorf("read catalog asset: %w", err)
	}
	if len(data) > assets.MaxFileSizeImage {
		return nil, fmt.Errorf("catalog logo exceeds size limit")
	}
	return data, nil
}
func (a *CatalogAssetStore) StoreCatalogAsset(ctx context.Context, data []byte) (string, error) {
	uploaded, err := a.Images.StorePlatformImage(ctx, a.Logger, a.Operator, bytes.NewReader(data), int64(len(data)), "image/svg+xml")
	if err != nil {
		return "", fmt.Errorf("store catalog asset: %w", err)
	}
	id, err := uuid.Parse(uploaded.Asset.ID)
	if err != nil {
		return "", fmt.Errorf("store catalog asset: %w", err)
	}
	return assets.ServeImageURL(a.Registry.assetBaseURL, id), nil
}
