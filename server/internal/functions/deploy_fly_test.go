package functions

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

type mockAssetStorage struct{}

func (m *mockAssetStorage) Exists(ctx context.Context, objectURL *url.URL) (bool, error) {
	return true, nil
}

func (m *mockAssetStorage) Read(ctx context.Context, objectURL *url.URL) (io.ReadCloser, error) {
	data := strings.Repeat("a", 600*1024)
	return io.NopCloser(strings.NewReader(data)), nil
}

func (m *mockAssetStorage) ReadAt(ctx context.Context, objectURL *url.URL) (assets.ReaderAtCloser, int64, error) {
	return nil, 0, nil
}

func (m *mockAssetStorage) Write(ctx context.Context, urlpath string, src io.Reader, contentType string) (io.WriteCloser, *url.URL, error) {
	return nil, nil, nil
}

func TestFlyRunner_serializeAssets_ExceedsSizeLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := slog.Default()

	runner := &FlyRunner{
		assetStorage: &mockAssetStorage{},
		logger:       logger,
	}

	assetURL1, _ := url.Parse("file:///asset1")
	assetURL2, _ := url.Parse("file:///asset2")

	assets := []RunnerAsset{
		{AssetURL: assetURL1, GuestPath: "/app/file1", Mode: 0444},
		{AssetURL: assetURL2, GuestPath: "/app/file2", Mode: 0444},
	}

	_, err := runner.serializeAssets(ctx, logger, assets)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)

	errMsg := oopsErr.Error()
	require.Contains(t, errMsg, "Function bundle too large")
	require.Contains(t, errMsg, "MB exceeds the")
	require.Contains(t, errMsg, "MB limit")
	require.Contains(t, errMsg, "Consider reducing function dependencies")
}
