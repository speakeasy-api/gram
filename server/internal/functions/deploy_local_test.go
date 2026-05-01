package functions_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestLocalRunner_ToolCallAndReadResource(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, root.Close())
	})
	assetStore := assets.NewFSBlobStore(testenv.NewLogger(t), root)
	serverURL, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)

	codeRoot := t.TempDir()
	runner := functions.NewLocalRunner(logger, tracerProvider, codeRoot, serverURL, assetStore)

	archive := buildLocalRunnerArchive(t, `
export async function handleToolCall({ name, input }) {
  if (name !== "show_dashboard") {
    return new Response(JSON.stringify({ error: "unknown tool" }), {
      status: 404,
      headers: { "Content-Type": "application/json" },
    });
  }

  return new Response(JSON.stringify({ query: input.query, ok: true }), {
    headers: { "Content-Type": "application/json" },
  });
}

export async function handleResources({ uri, input }) {
  if (uri !== "ui://demo/dashboard") {
    return new Response("missing", {
      status: 404,
      headers: { "Content-Type": "text/plain" },
    });
  }

  return new Response("<html><body>" + input.query + "</body></html>", {
    headers: { "Content-Type": "text/html;profile=mcp-app" },
  });
}
`)
	assetURL := uploadLocalRunnerArchive(t, ctx, assetStore, archive)

	projectID := uuid.New()
	deploymentID := uuid.New()
	functionID := uuid.New()
	accessID := uuid.New()

	_, err = runner.Deploy(ctx, functions.RunnerDeployRequest{
		Version:      "dev",
		ProjectID:    projectID,
		DeploymentID: deploymentID,
		FunctionID:   functionID,
		AccessID:     accessID,
		Runtime:      functions.RuntimeNodeJS22,
		Assets: []functions.RunnerAsset{{
			AssetID:       uuid.New(),
			AssetURL:      assetURL,
			GuestPath:     "/data/code.zip",
			Mode:          0444,
			SHA256Sum:     "",
			ContentLength: int64(len(archive)),
			ContentType:   "application/zip",
		}},
		BearerSecret: base64.StdEncoding.EncodeToString([]byte("12345678901234567890123456789012")),
	})
	require.NoError(t, err)

	invocationID := uuid.New()
	toolReq, err := runner.ToolCall(ctx, functions.RunnerToolCallRequest{
		RunnerBaseRequest: functions.RunnerBaseRequest{
			InvocationID:      invocationID,
			OrganizationID:    "org-123",
			OrganizationSlug:  "organization-123",
			ProjectID:         projectID,
			ProjectSlug:       "demo-project",
			DeploymentID:      deploymentID,
			FunctionsID:       functionID,
			FunctionsAccessID: accessID,
			Input:             json.RawMessage(`{"query":"hello"}`),
			Environment:       map[string]string{},
		},
		ToolURN:  urn.NewTool(urn.ToolKindFunction, "demo", "show_dashboard"),
		ToolName: "show_dashboard",
	})
	require.NoError(t, err)

	toolResp, err := http.DefaultClient.Do(toolReq)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, toolResp.Body.Close())
	}()

	toolBody, err := io.ReadAll(toolResp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, toolResp.StatusCode)
	require.Equal(t, invocationID.String(), toolResp.Header.Get("Gram-Invoke-ID"))
	require.JSONEq(t, `{"query":"hello","ok":true}`, string(toolBody))

	resourceReq, err := runner.ReadResource(ctx, functions.RunnerResourceReadRequest{
		RunnerBaseRequest: functions.RunnerBaseRequest{
			InvocationID:      invocationID,
			OrganizationID:    "org-123",
			OrganizationSlug:  "organization-123",
			ProjectID:         projectID,
			ProjectSlug:       "demo-project",
			DeploymentID:      deploymentID,
			FunctionsID:       functionID,
			FunctionsAccessID: accessID,
			Input:             json.RawMessage(`{"query":"hello"}`),
			Environment:       map[string]string{},
		},
		ResourceURN:  urn.NewResource(urn.ResourceKindFunction, "demo", "ui://demo/dashboard"),
		ResourceURI:  "ui://demo/dashboard",
		ResourceName: "dashboard",
	})
	require.NoError(t, err)

	resourceResp, err := http.DefaultClient.Do(resourceReq)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, resourceResp.Body.Close())
	}()

	resourceBody, err := io.ReadAll(resourceResp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resourceResp.StatusCode)
	require.Equal(t, "text/html;profile=mcp-app", resourceResp.Header.Get("Content-Type"))
	require.Equal(t, invocationID.String(), resourceResp.Header.Get("Gram-Invoke-ID"))
	require.Equal(t, "<html><body>hello</body></html>", string(resourceBody))
	require.NotEmpty(t, resourceResp.Trailer.Get(functions.FunctionsExecutionTimeHeader))
}

func buildLocalRunnerArchive(t *testing.T, functionsJS string) []byte {
	t.Helper()

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	file, err := zw.Create(filepath.Base("functions.js"))
	require.NoError(t, err)
	_, err = file.Write([]byte(functionsJS))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	return buf.Bytes()
}

func uploadLocalRunnerArchive(t *testing.T, ctx context.Context, assetStore interface {
	Write(context.Context, string, string, int64) (io.WriteCloser, *url.URL, error)
}, archive []byte) *url.URL {
	t.Helper()

	writer, assetURL, err := assetStore.Write(ctx, "test/functions.zip", "application/zip", int64(len(archive)))
	require.NoError(t, err)
	_, err = writer.Write(archive)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NotNil(t, assetURL)

	return assetURL
}
