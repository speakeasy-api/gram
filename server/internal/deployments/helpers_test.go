package deployments_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	agen "github.com/speakeasy-api/gram/server/gen/assets"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func zipManifest(t *testing.T, path string, runtime string) (rdr io.Reader, err error) {
	t.Helper()

	buf := &bytes.Buffer{}
	rdr = buf

	manifest := testenv.ReadFixture(t, path)
	zipWriter := zip.NewWriter(buf)
	defer o11y.LogDefer(t.Context(), testenv.NewLogger(t), func() error {
		return zipWriter.Close()
	})

	writer, err := zipWriter.Create("manifest.json")
	if err != nil {
		return nil, fmt.Errorf("create manifest in zip: %w", err)
	}

	_, err = writer.Write(manifest)
	if err != nil {
		return nil, fmt.Errorf("write manifest to zip: %w", err)
	}

	var funcwriter io.Writer
	var comment string
	switch {
	case strings.HasPrefix(runtime, "nodejs"):
		comment = "// JavaScript functions"
		if funcwriter, err = zipWriter.Create("functions.js"); err != nil {
			return nil, fmt.Errorf("create functions.js in zip: %w", err)
		}
	case strings.HasPrefix(runtime, "python"):
		comment = "# Python functions"
		if funcwriter, err = zipWriter.Create("functions.py"); err != nil {
			return nil, fmt.Errorf("create functions.py in zip: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported runtime: %s", runtime)
	}

	// Create an empty functions file with a comment so the file exists. It does
	// not need to have any actual code when testing deployments.
	_, err = funcwriter.Write([]byte(comment + "\n"))
	if err != nil {
		return nil, fmt.Errorf("write functions to zip: %w", err)
	}

	return buf, nil
}

func uploadFunctionsWithManifest(t *testing.T, ctx context.Context, assetsService *assets.Service, manifestPath, runtime string) *agen.UploadFunctionsResult {
	t.Helper()

	// Create functions zip with manifest using the helper from setup_test.go
	zipReader, err := zipManifest(t, manifestPath, runtime)
	require.NoError(t, err, "failed to create functions zip with manifest")

	// Read the zip content
	zipBytes, err := io.ReadAll(zipReader)
	require.NoError(t, err, "failed to read zip content")

	result, err := assetsService.UploadFunctions(ctx, &agen.UploadFunctionsForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/zip",
		ContentLength:    int64(len(zipBytes)),
	}, io.NopCloser(bytes.NewBuffer(zipBytes)))
	require.NoError(t, err, "failed to upload functions")

	return result
}
