package domainmeta_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/domainmeta"
)

const rdapBody = `{
  "ldhName": "somevendor.io",
  "events": [
    {"eventAction": "registration", "eventDate": "2019-03-12T09:00:00Z"},
    {"eventAction": "expiration", "eventDate": "2027-03-12T09:00:00Z"}
  ],
  "entities": [
    {"roles": ["registrar"],
     "vcardArray": ["vcard", [["version", {}, "text", "4.0"], ["fn", {}, "text", "Example Registrar, Inc."]]]}
  ]
}`

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

func TestLookup(t *testing.T) {
	t.Parallel()

	server, path := serve(t, http.StatusOK, rdapBody)
	client := domainmeta.NewClient(server.Client(), domainmeta.WithBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), "SomeVendor.io")
	require.NoError(t, err)
	require.NotNil(t, got)

	require.Equal(t, "somevendor.io", got.Domain)
	require.Equal(t, 2019, got.RegisteredAt.Year())
	require.Equal(t, "Example Registrar, Inc.", got.Registrar)
	require.Equal(t, "/domain/somevendor.io", path())
}

// A registry that knows no such domain answers 404, which is an answer.
func TestLookup_UnknownDomain(t *testing.T) {
	t.Parallel()

	server, _ := serve(t, http.StatusNotFound, `{"errorCode": 404}`)
	client := domainmeta.NewClient(server.Client(), domainmeta.WithBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), "unregistered.example")
	require.NoError(t, err)
	require.Nil(t, got)
}

// An empty domain has nothing to look up.
func TestLookup_EmptyDomain(t *testing.T) {
	t.Parallel()

	client := domainmeta.NewClient(http.DefaultClient)

	got, err := client.Lookup(t.Context(), "  ")
	require.NoError(t, err)
	require.Nil(t, got)
}

// A registry without a registration event yields a record whose date is
// unknown rather than a failure.
func TestLookup_NoRegistrationEvent(t *testing.T) {
	t.Parallel()

	server, _ := serve(t, http.StatusOK, `{"ldhName": "somevendor.io", "events": [], "entities": []}`)
	client := domainmeta.NewClient(server.Client(), domainmeta.WithBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), "somevendor.io")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, got.RegisteredAt.IsZero())
	require.Empty(t, got.Registrar)
}

// A failing RDAP service is an error the assembler records as a gap.
func TestLookup_FailureIsAnError(t *testing.T) {
	t.Parallel()

	server, _ := serve(t, http.StatusServiceUnavailable, ``)
	client := domainmeta.NewClient(server.Client(), domainmeta.WithBaseURL(server.URL))

	_, err := client.Lookup(t.Context(), "somevendor.io")
	require.Error(t, err)
}
