package plugins_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/url"
	"path"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const (
	// testConfiguredServerURL is the configured server URL, the canonical
	// platform host.
	testConfiguredServerURL = "https://ai.speakeasy.com"

	// testStoredHostURL is an extra platform host an organization keeps as its
	// stored default host.
	testStoredHostURL = "https://app.getgram.ai"
)

// newOrganizationHostPluginsService returns a Service on ti's database whose
// configured server URL is testConfiguredServerURL and which resolves stored
// default hosts against testStoredHostURL as an extra platform host.
func newOrganizationHostPluginsService(t *testing.T, ti *testInstance) *plugins.Service {
	t.Helper()

	logger := testenv.NewLogger(t)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	serverURL, err := url.Parse(testConfiguredServerURL)
	require.NoError(t, err)

	resolver := orghost.New(orghost.Config{
		ServerURL:                  serverURL,
		SiteURL:                    serverURL,
		PlatformHosts:              map[string]string{"app.getgram.ai": testStoredHostURL},
		LegacyDefaultHost:          nil,
		NewOrganizationDefaultHost: nil,
	})

	return plugins.NewService(logger, testenv.NewTracerProvider(t), ti.conn, ti.sessionManager, cache.NewRedisCacheAdapter(redisClient), authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), audit.NewLogger(), nil, "local", testConfiguredServerURL, nil, nil).
		WithOrganizationHosts(resolver)
}

func setOrganizationDefaultHost(t *testing.T, ctx context.Context, ti *testInstance, defaultHost string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NoError(t, organizationsrepo.New(ti.conn).SetOrganizationDefaultHostForTest(ctx, organizationsrepo.SetOrganizationDefaultHostForTestParams{
		DefaultHost: pgtype.Text{String: defaultHost, Valid: true},
		ID:          authCtx.ActiveOrganizationID,
	}))
}

func readZipFiles(t *testing.T, body io.ReadCloser) map[string][]byte {
	t.Helper()

	archive, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())

	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	require.NoError(t, err)
	files := make(map[string][]byte, len(zr.File))
	for _, file := range zr.File {
		entry, err := file.Open()
		require.NoError(t, err)
		content, err := io.ReadAll(entry)
		require.NoError(t, err)
		require.NoError(t, entry.Close())
		files[file.Name] = content
	}
	return files
}

func downloadPluginServerPackage(t *testing.T, ctx context.Context, ti *testInstance, svc *plugins.Service) (string, map[string][]byte) {
	t.Helper()

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Host Test"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "host-test")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(toolset.ID.String()), Policy: "required", SortOrder: 0})
	require.NoError(t, err)

	_, body, err := svc.DownloadPluginPackage(ctx, &gen.DownloadPluginPackagePayload{PluginID: plugin.ID, Platform: "claude"})
	require.NoError(t, err)
	return toolset.McpSlug.String, readZipFiles(t, body)
}

func downloadObservabilityServerURL(t *testing.T, ctx context.Context, svc *plugins.Service) string {
	t.Helper()

	_, body, err := svc.DownloadObservabilityPlugin(ctx, &gen.DownloadObservabilityPluginPayload{Platform: "claude"})
	require.NoError(t, err)
	for name, content := range readZipFiles(t, body) {
		if path.Base(name) != "speakeasy.json" {
			continue
		}
		var config struct {
			ServerURL string `json:"server_url"`
		}
		require.NoError(t, json.Unmarshal(content, &config))
		return config.ServerURL
	}
	require.Fail(t, "observability package has no speakeasy.json")
	return ""
}

func TestPluginMCPAddressUsesStoredOrganizationHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	setOrganizationDefaultHost(t, ctx, ti, testStoredHostURL)
	svc := newOrganizationHostPluginsService(t, ti)

	slug, files := downloadPluginServerPackage(t, ctx, ti, svc)
	mcpJSON := string(files[".mcp.json"])
	require.Contains(t, mcpJSON, testStoredHostURL+"/mcp/"+slug)
	require.NotContains(t, mcpJSON, testConfiguredServerURL)
}

func TestPluginMCPAddressWithoutStoredHostUsesConfiguredURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	svc := newOrganizationHostPluginsService(t, ti)

	slug, files := downloadPluginServerPackage(t, ctx, ti, svc)
	mcpJSON := string(files[".mcp.json"])
	require.Contains(t, mcpJSON, testConfiguredServerURL+"/mcp/"+slug)
	require.NotContains(t, mcpJSON, testStoredHostURL)
}

func TestObservabilityPluginServerURLUsesStoredOrganizationHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	setOrganizationDefaultHost(t, ctx, ti, testStoredHostURL)
	svc := newOrganizationHostPluginsService(t, ti)

	require.Equal(t, testStoredHostURL, downloadObservabilityServerURL(t, ctx, svc))
}

func TestObservabilityPluginServerURLWithoutStoredHostUsesConfiguredURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	svc := newOrganizationHostPluginsService(t, ti)

	require.Equal(t, testConfiguredServerURL, downloadObservabilityServerURL(t, ctx, svc))
}

func TestPluginServerURLIgnoresUnknownStoredHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	setOrganizationDefaultHost(t, ctx, ti, "https://unknown.example.com")
	svc := newOrganizationHostPluginsService(t, ti)

	require.Equal(t, testConfiguredServerURL, downloadObservabilityServerURL(t, ctx, svc))
}

func TestPublishedServerURLMatchesMCPAddressHost(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	ti.service.WithOrganizationHosts(orghost.New(orghost.Config{
		ServerURL:                  &url.URL{Scheme: "https", Host: "app.getgram.ai"},
		SiteURL:                    nil,
		PlatformHosts:              map[string]string{"platform.example.test": "https://platform.example.test"},
		LegacyDefaultHost:          nil,
		NewOrganizationDefaultHost: nil,
	}))
	setOrganizationDefaultHost(t, ctx, ti, "https://platform.example.test")

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Agreement"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "agreement")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(toolset.ID.String()), Policy: "required", SortOrder: 0})
	require.NoError(t, err)
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	require.Contains(t, string(mock.lastPushedFiles[plugin.Slug+"/.mcp.json"]), "https://platform.example.test/mcp/"+toolset.McpSlug.String)
	var serverURLs []string
	for name, content := range mock.lastPushedFiles {
		if path.Base(name) != "speakeasy.json" {
			continue
		}
		var config struct {
			ServerURL string `json:"server_url"`
		}
		require.NoError(t, json.Unmarshal(content, &config))
		serverURLs = append(serverURLs, config.ServerURL)
	}
	require.NotEmpty(t, serverURLs)
	for _, serverURL := range serverURLs {
		require.Equal(t, "https://platform.example.test", serverURL)
	}
}
