package packagemeta_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/identity"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/packagemeta"
)

// serve stands up a registry returning body for any request, recording the
// path so scoped-name handling can be asserted.
func serve(t *testing.T, status int, body string) (*httptest.Server, *string) {
	t.Helper()

	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return server, &path
}

const npmBody = `{
  "name": "@scope/mcp-server",
  "dist-tags": {"latest": "1.2.3"},
  "time": {"created": "2023-01-15T10:00:00.000Z", "modified": "2026-07-01T12:00:00.000Z"},
  "versions": {"1.0.0": {}, "1.2.0": {}, "1.2.3": {}},
  "maintainers": [{"name": "alice"}, {"name": "bob"}],
  "license": "MIT"
}`

func TestLookup_NPM(t *testing.T) {
	t.Parallel()

	server, path := serve(t, http.StatusOK, npmBody)
	client := packagemeta.NewClient(server.Client(), packagemeta.WithNPMBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), identity.RegistryNPM, "@scope/mcp-server")
	require.NoError(t, err)
	require.NotNil(t, got)

	require.Equal(t, identity.RegistryNPM, got.Registry)
	require.Equal(t, "MIT", got.License)
	require.Equal(t, "1.2.3", got.LatestVersion)
	require.Equal(t, 3, got.VersionCount)
	require.Equal(t, 2, got.MaintainerCount)
	require.Equal(t, 2023, got.FirstPublished.Year())
	require.Equal(t, 2026, got.LastPublished.Year())
	require.False(t, got.Deprecated)

	// A scope's slash is a path separator, not an escaped character.
	require.Equal(t, "/@scope/mcp-server", *path)
}

// Older packages publish license as an object rather than a string.
func TestLookup_NPMObjectLicense(t *testing.T) {
	t.Parallel()

	server, _ := serve(t, http.StatusOK, `{"name":"old","dist-tags":{"latest":"1.0.0"},"license":{"type":"Apache-2.0"}}`)
	client := packagemeta.NewClient(server.Client(), packagemeta.WithNPMBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), identity.RegistryNPM, "old")
	require.NoError(t, err)
	require.Equal(t, "Apache-2.0", got.License)
}

// Deprecation is per version. Only the version that installs today matters.
func TestLookup_NPMDeprecationTracksLatestOnly(t *testing.T) {
	t.Parallel()

	body := `{"name":"p","dist-tags":{"latest":"2.0.0"},
	  "versions":{"1.0.0":{"deprecated":"old and busted"},"2.0.0":{}}}`
	server, _ := serve(t, http.StatusOK, body)
	client := packagemeta.NewClient(server.Client(), packagemeta.WithNPMBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), identity.RegistryNPM, "p")
	require.NoError(t, err)
	require.False(t, got.Deprecated, "an old deprecated release says nothing about what installs today")

	current := `{"name":"p","dist-tags":{"latest":"2.0.0"},
	  "versions":{"2.0.0":{"deprecated":"unmaintained"}}}`
	server2, _ := serve(t, http.StatusOK, current)
	client2 := packagemeta.NewClient(server2.Client(), packagemeta.WithNPMBaseURL(server2.URL))

	got2, err := client2.Lookup(t.Context(), identity.RegistryNPM, "p")
	require.NoError(t, err)
	require.True(t, got2.Deprecated)
	require.Equal(t, "unmaintained", got2.DeprecationReason)
}

const pypiBody = `{
  "info": {"name": "mcp-thing", "version": "2.1.0", "license": "", "yanked": false,
           "classifiers": ["Programming Language :: Python :: 3", "License :: OSI Approved :: BSD License"]},
  "releases": {
    "1.0.0": [{"upload_time_iso_8601": "2024-03-01T08:00:00.000000Z"}],
    "2.1.0": [{"upload_time_iso_8601": "2026-06-20T09:30:00.000000Z"}]
  }
}`

func TestLookup_PyPI(t *testing.T) {
	t.Parallel()

	server, path := serve(t, http.StatusOK, pypiBody)
	client := packagemeta.NewClient(server.Client(), packagemeta.WithPyPIBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), identity.RegistryPyPI, "mcp-thing")
	require.NoError(t, err)
	require.NotNil(t, got)

	require.Equal(t, identity.RegistryPyPI, got.Registry)
	require.Equal(t, "2.1.0", got.LatestVersion)
	require.Equal(t, 2, got.VersionCount)
	require.Equal(t, 2024, got.FirstPublished.Year())
	require.Equal(t, 2026, got.LastPublished.Year())
	require.Equal(t, "/pypi/mcp-thing/json", *path)

	// The free-text license was empty, so the Trove classifier supplies it.
	require.Equal(t, "BSD License", got.License)
}

func TestLookup_PyPIPrefersExplicitLicense(t *testing.T) {
	t.Parallel()

	body := `{"info":{"name":"p","version":"1.0","license":"MIT","classifiers":["License :: OSI Approved :: BSD License"]},"releases":{}}`
	server, _ := serve(t, http.StatusOK, body)
	client := packagemeta.NewClient(server.Client(), packagemeta.WithPyPIBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), identity.RegistryPyPI, "p")
	require.NoError(t, err)
	require.Equal(t, "MIT", got.License)
}

func TestLookup_PyPIYankedIsDeprecated(t *testing.T) {
	t.Parallel()

	body := `{"info":{"name":"p","version":"1.0","yanked":true,"yanked_reason":"security"},"releases":{}}`
	server, _ := serve(t, http.StatusOK, body)
	client := packagemeta.NewClient(server.Client(), packagemeta.WithPyPIBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), identity.RegistryPyPI, "p")
	require.NoError(t, err)
	require.True(t, got.Deprecated)
	require.Equal(t, "security", got.DeprecationReason)
}

// A server nobody catalogued is the normal case for this workflow, not a
// failure, and it must be distinguishable from an error.
func TestLookup_UnknownPackageIsNotAnError(t *testing.T) {
	t.Parallel()

	server, _ := serve(t, http.StatusNotFound, `{"error":"Not found"}`)
	client := packagemeta.NewClient(server.Client(),
		packagemeta.WithNPMBaseURL(server.URL), packagemeta.WithPyPIBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), identity.RegistryNPM, "nope")
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestLookup_ServerErrorIsAnError(t *testing.T) {
	t.Parallel()

	server, _ := serve(t, http.StatusInternalServerError, "boom")
	client := packagemeta.NewClient(server.Client(), packagemeta.WithNPMBaseURL(server.URL))

	_, err := client.Lookup(t.Context(), identity.RegistryNPM, "p")
	require.Error(t, err)
}

// A remote endpoint has no package registry, and an empty name has nothing to
// look up. Neither is an error.
func TestLookup_UnsupportedRegistryOrEmptyName(t *testing.T) {
	t.Parallel()

	client := packagemeta.NewClient(http.DefaultClient)

	got, err := client.Lookup(t.Context(), identity.Registry("oci"), "some/image")
	require.NoError(t, err)
	require.Nil(t, got)

	got, err = client.Lookup(t.Context(), identity.RegistryNPM, "   ")
	require.NoError(t, err)
	require.Nil(t, got)
}
