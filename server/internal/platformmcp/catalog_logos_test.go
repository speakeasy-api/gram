package platformmcp

import (
	"context"
	"encoding/base64"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// logoServer serves body with contentType over TLS and counts its requests.
func logoServer(t *testing.T, contentType string, body []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var hits atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func TestCatalogLogosInlinesARasterIconOnce(t *testing.T) {
	t.Parallel()

	body := testPNG(t)
	server, hits := logoServer(t, "image/png", body)
	logos := newCatalogLogos(testenv.NewLogger(t), server.Client())
	candidates := []CatalogCandidate{{Name: "Linear", iconURL: server.URL + "/icon.png"}, {Name: "No icon"}}

	first := logos.forCandidates(t.Context(), candidates, nil)
	second := logos.forCandidates(t.Context(), candidates, nil)

	require.Equal(t, template.URL("data:image/png;base64,"+base64.StdEncoding.EncodeToString(body)), first[0])
	require.Empty(t, first[1])
	require.Equal(t, first, second)
	require.Equal(t, int32(1), hits.Load(), "a fetched icon is reused")
}

func TestCatalogLogosAcceptsATypeWithParameters(t *testing.T) {
	t.Parallel()

	server, _ := logoServer(t, "image/png; charset=binary", testPNG(t))
	logos := newCatalogLogos(testenv.NewLogger(t), server.Client())

	got := logos.forCandidates(t.Context(), []CatalogCandidate{{iconURL: server.URL + "/icon.png"}}, nil)

	require.True(t, strings.HasPrefix(string(got[0]), "data:image/png;base64,"))
}

func TestCatalogLogosRejectsUnsafeIcons(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		contentType string
		body        []byte
	}{
		{"svg", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)},
		{"html labelled as png", "image/png", []byte("<!doctype html><script>alert(1)</script>")},
		{"oversized", "image/png", append(testPNG(t), make([]byte, maxCardLogoBytes)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server, _ := logoServer(t, tc.contentType, tc.body)
			logos := newCatalogLogos(testenv.NewLogger(t), server.Client())

			got := logos.forCandidates(t.Context(), []CatalogCandidate{{iconURL: server.URL + "/icon"}}, nil)

			require.Empty(t, got[0])
		})
	}
}

func TestCatalogLogosFetchesOnlyHTTPS(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG(t))
	}))
	t.Cleanup(server.Close)
	logos := newCatalogLogos(testenv.NewLogger(t), server.Client())

	got := logos.forCandidates(t.Context(), []CatalogCandidate{{iconURL: server.URL + "/icon.png"}}, nil)

	require.Empty(t, got[0])
	require.Zero(t, hits.Load())
}

func TestCatalogLogosWithoutAClientShowInitials(t *testing.T) {
	t.Parallel()

	var logos *catalogLogos
	got := logos.forCandidates(t.Context(), []CatalogCandidate{{iconURL: "https://example.test/icon.png"}}, nil)

	require.Equal(t, []template.URL{""}, got)

	var registrations *RegistrationService
	require.Equal(t, []template.URL{""}, registrations.catalogLogoURIs(t.Context(), []CatalogCandidate{{}}, nil))
}

// A server already in the project keeps its stored logo; one that is not gets
// the fetched registry logo.
func TestCatalogCarouselFillsRegistryLogosBehindStoredOnes(t *testing.T) {
	t.Parallel()

	stored := template.URL("data:image/png;base64,c3RvcmVk")
	fetched := template.URL("data:image/png;base64,ZmV0Y2hlZA==")
	carousel := catalogCarousel{Slides: []catalogSlide{{Title: "In project", Logo: stored}, {Title: "New"}, {Title: "No icon"}}}

	carousel.fillLogos([]template.URL{fetched, fetched, ""})

	require.Equal(t, stored, carousel.Slides[0].Logo)
	require.Equal(t, fetched, carousel.Slides[1].Logo)
	require.Empty(t, carousel.Slides[2].Logo)
}

// The carousel carries both scroll arrows, labelled and hidden until the view
// measures that it can scroll.
func TestCatalogCarouselHasLabelledScrollArrows(t *testing.T) {
	t.Parallel()

	var rendered strings.Builder
	carousel := buildCatalogCarousel([]CatalogCandidate{{ProviderKey: "p", CatalogRef: "r", Name: "Linear"}}, nil, false)
	require.NoError(t, catalogCarouselTemplate.Execute(&rendered, carousel))

	require.Contains(t, rendered.String(), `<button type="button" class="nav nav-prev" data-scroll="-1" aria-label="Previous servers" hidden>`)
	require.Contains(t, rendered.String(), `<button type="button" class="nav nav-next" data-scroll="1" aria-label="Next servers" hidden>`)
}

// A redirect away from https is refused, so the https check holds for every
// hop and not only the icon URL.
func TestCatalogLogosRefuseARedirectToHTTP(t *testing.T) {
	t.Parallel()

	var plainHits atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		plainHits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG(t))
	}))
	t.Cleanup(plain.Close)
	secure := httptest.NewTLSServer(http.RedirectHandler(plain.URL+"/icon.png", http.StatusFound))
	t.Cleanup(secure.Close)
	logos := newCatalogLogos(testenv.NewLogger(t), secure.Client())

	got := logos.forCandidates(t.Context(), []CatalogCandidate{{iconURL: secure.URL + "/icon.png"}}, nil)

	require.Empty(t, got[0])
	require.Zero(t, plainHits.Load(), "the http target is never requested")
}

// A host that is still slow after catalogLogoSlowHost is remembered, so the
// next search does not wait on it again.
func TestCatalogLogosRememberASlowHost(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	server := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	logos := newCatalogLogos(testenv.NewLogger(t), server.Client())
	candidates := []CatalogCandidate{{iconURL: server.URL + "/icon.png"}}

	ctx, cancel := context.WithTimeout(t.Context(), catalogLogoSlowHost+200*time.Millisecond)
	defer cancel()
	require.Empty(t, logos.forCandidates(ctx, candidates, nil)[0])
	require.Empty(t, logos.forCandidates(t.Context(), candidates, nil)[0])
	require.Equal(t, int32(1), hits.Load(), "the slow host is not asked again")
}

// A fetch cut off before the host had a fair chance is not remembered.
func TestCatalogLogosRetryAFetchCutOffEarly(t *testing.T) {
	t.Parallel()

	body := testPNG(t)
	server, hits := logoServer(t, "image/png", body)
	logos := newCatalogLogos(testenv.NewLogger(t), server.Client())
	candidates := []CatalogCandidate{{iconURL: server.URL + "/icon.png"}}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.Empty(t, logos.forCandidates(ctx, candidates, nil)[0])
	require.NotEmpty(t, logos.forCandidates(t.Context(), candidates, nil)[0], "the next search fetches it")
	require.LessOrEqual(t, hits.Load(), int32(1))
}

// Slides with a stored logo are not fetched, and an icon URL shared by
// several candidates is fetched once.
func TestCatalogLogosSkipStoredLogosAndFetchEachURLOnce(t *testing.T) {
	t.Parallel()

	server, hits := logoServer(t, "image/png", testPNG(t))
	logos := newCatalogLogos(testenv.NewLogger(t), server.Client())
	shared := server.URL + "/shared.png"

	got := logos.forCandidates(t.Context(), []CatalogCandidate{
		{iconURL: server.URL + "/stored.png"},
		{iconURL: shared},
		{iconURL: shared},
	}, []bool{true, false, false})

	require.Empty(t, got[0])
	require.NotEmpty(t, got[1])
	require.Equal(t, got[1], got[2])
	require.Equal(t, int32(1), hits.Load())
}
