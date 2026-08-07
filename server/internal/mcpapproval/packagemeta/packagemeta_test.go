package packagemeta_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/identity"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/packagemeta"
)

// serve stands up a registry returning body for any request, recording the
// path so scoped-name handling can be asserted. The record is read through a
// mutex-guarded getter: the handler runs on the server's goroutine, and a
// completed HTTP round-trip is not a synchronization edge between it and the
// test.
func serve(t *testing.T, status int, body string) (*httptest.Server, func() string) {
	t.Helper()

	var mu sync.Mutex
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		path = r.URL.Path
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return server, func() string {
		mu.Lock()
		defer mu.Unlock()
		return path
	}
}

const npmBody = `{
  "name": "@scope/mcp-server",
  "dist-tags": {"latest": "1.2.3"},
  "time": {
    "created": "2023-01-15T10:00:00.000Z", "modified": "2026-07-01T12:00:00.000Z",
    "1.0.0": "2023-01-15T10:00:00.000Z", "1.2.0": "2025-03-10T08:00:00.000Z", "1.2.3": "2026-06-20T09:30:00.000Z"
  },
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
	require.Equal(t, "/@scope/mcp-server", path())
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
	require.Equal(t, "/pypi/mcp-thing/json", path())

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

// npm bumps top-level `modified` on any metadata edit, so maintenance recency
// reads the newest release time instead. A deprecation flagged yesterday must
// not make a package abandoned for years look actively maintained.
func TestLookup_NPMLastPublishedIsTheLastRelease(t *testing.T) {
	t.Parallel()

	body := `{"name":"p","dist-tags":{"latest":"1.0.0"},
	  "time":{"created":"2020-01-01T00:00:00.000Z","modified":"2026-07-01T00:00:00.000Z","1.0.0":"2020-01-01T00:00:00.000Z"},
	  "versions":{"1.0.0":{}}}`
	server, _ := serve(t, http.StatusOK, body)
	client := packagemeta.NewClient(server.Client(), packagemeta.WithNPMBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), identity.RegistryNPM, "p")
	require.NoError(t, err)
	require.Equal(t, 2020, got.LastPublished.Year(), "a metadata edit is not a release")
}

// A package that ever had a version unpublished carries `time.unpublished` as
// an object. The lookup must tolerate it rather than fail over a field it
// never reads.
func TestLookup_NPMToleratesUnpublishedTimeEntry(t *testing.T) {
	t.Parallel()

	body := `{"name":"p","dist-tags":{"latest":"2.0.0"},
	  "time":{"created":"2020-01-01T00:00:00.000Z","1.0.0":"2020-01-01T00:00:00.000Z",
	          "unpublished":{"time":"2021-06-01T00:00:00.000Z","versions":["1.5.0"]},
	          "2.0.0":"2022-05-01T00:00:00.000Z"},
	  "versions":{"2.0.0":{}}}`
	server, _ := serve(t, http.StatusOK, body)
	client := packagemeta.NewClient(server.Client(), packagemeta.WithNPMBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), identity.RegistryNPM, "p")
	require.NoError(t, err)
	require.Equal(t, 2022, got.LastPublished.Year())
}

// PEP 508 extras select optional dependencies of the same package, so the
// lookup strips them instead of asking PyPI for a project that does not exist.
// Only a well-formed, terminal extras expression counts: a malformed spec
// must not be quietly resolved to a different package's evidence.
func TestLookup_PyPIStripsExtras(t *testing.T) {
	t.Parallel()

	server, path := serve(t, http.StatusOK, pypiBody)
	client := packagemeta.NewClient(server.Client(), packagemeta.WithPyPIBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), identity.RegistryPyPI, "mcp-thing[sse]")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "/pypi/mcp-thing/json", path())

	for _, malformed := range []string{"foo[bar", "foo[a][b]", "foo[a[b]]", "foo[]"} {
		server, path := serve(t, http.StatusNotFound, `{}`)
		client := packagemeta.NewClient(server.Client(), packagemeta.WithPyPIBaseURL(server.URL))

		got, err := client.Lookup(t.Context(), identity.RegistryPyPI, malformed)
		require.NoError(t, err)
		require.Nil(t, got, "a malformed spec surfaces as unknown, never as another package")
		require.NotEqual(t, "/pypi/foo/json", path(), "spec %q must not be looked up as foo", malformed)
	}
}

// An oversized registry response fails loudly with a size error, never as a
// baffling decode failure on a silently truncated body.
func TestLookup_OversizedResponseIsAClearError(t *testing.T) {
	t.Parallel()

	server, _ := serve(t, http.StatusOK, strings.Repeat(" ", 33<<20))
	client := packagemeta.NewClient(server.Client(), packagemeta.WithNPMBaseURL(server.URL))

	_, err := client.Lookup(t.Context(), identity.RegistryNPM, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "byte limit")
}

// A 200 whose document carries no package name is an unrecognized response,
// not metadata — presenting it as a published package would put an empty
// finding in front of an approver.
func TestLookup_UnrecognizedResponseIsAnError(t *testing.T) {
	t.Parallel()

	server, _ := serve(t, http.StatusOK, `{}`)
	client := packagemeta.NewClient(server.Client(),
		packagemeta.WithNPMBaseURL(server.URL), packagemeta.WithPyPIBaseURL(server.URL))

	_, err := client.Lookup(t.Context(), identity.RegistryNPM, "p")
	require.Error(t, err)

	_, err = client.Lookup(t.Context(), identity.RegistryPyPI, "p")
	require.Error(t, err)
}
