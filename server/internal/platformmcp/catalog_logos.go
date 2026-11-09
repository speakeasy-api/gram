package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// A catalog server that is not in the project yet has no stored logo, so the
// carousel shows its registry icon instead. The icon is fetched here, through
// the guardian client, checked by cardLogoDataURI and inlined as a data: URI;
// the card itself never loads anything from the network.

const (
	// catalogLogoFetchTimeout bounds how long a search waits for its logos.
	// Logos are decoration: a slow vendor host shows initials instead of
	// delaying the result.
	catalogLogoFetchTimeout = 3 * time.Second

	// catalogLogoConcurrency bounds parallel logo fetches for one page, so one
	// search does not open a burst of outbound connections.
	catalogLogoConcurrency = 4

	// catalogLogoCacheSize bounds how many icon URLs one server remembers. The
	// reviewed catalog holds a few hundred servers.
	catalogLogoCacheSize = 1024

	// catalogLogoCacheTTL is how long a fetched logo, or a failed fetch, is
	// reused. Vendors rarely change their icons, and an hour also stops a
	// broken icon URL from being retried on every search.
	catalogLogoCacheTTL = time.Hour
)

// errCatalogLogoURL rejects an icon URL that is not https.
var errCatalogLogoURL = errors.New("catalog logo url is not https")

// catalogLogos fetches catalog servers' registry icons as card logos.
type catalogLogos struct {
	logger *slog.Logger
	client *guardian.HTTPClient
	cache  *expirable.LRU[string, template.URL]
}

func newCatalogLogos(logger *slog.Logger, client *guardian.HTTPClient) *catalogLogos {
	return &catalogLogos{logger: logger, client: client, cache: expirable.NewLRU[string, template.URL](catalogLogoCacheSize, nil, catalogLogoCacheTTL)}
}

// forCandidates returns a logo for each candidate, "" where it has none or
// the icon could not be fetched in time.
func (l *catalogLogos) forCandidates(ctx context.Context, candidates []CatalogCandidate) []template.URL {
	logos := make([]template.URL, len(candidates))
	if l == nil || l.client == nil {
		return logos
	}
	ctx, cancel := context.WithTimeout(ctx, catalogLogoFetchTimeout)
	defer cancel()
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(catalogLogoConcurrency)
	for i, candidate := range candidates {
		if candidate.iconURL == "" {
			continue
		}
		group.Go(func() error {
			logos[i] = l.logo(groupCtx, candidate.iconURL)
			return nil
		})
	}
	_ = group.Wait() // Each fetch returns nil; a failure only leaves its logo empty.
	return logos
}

func (l *catalogLogos) logo(ctx context.Context, iconURL string) template.URL {
	if logo, ok := l.cache.Get(iconURL); ok {
		return logo
	}
	logo, err := l.fetch(ctx, iconURL)
	if err != nil {
		if ctx.Err() != nil {
			// Out of time for this search; the next one tries again.
			return ""
		}
		l.logger.DebugContext(ctx, "catalog card logo unavailable", attr.SlogError(err))
	}
	l.cache.Add(iconURL, logo)
	return logo
}

func (l *catalogLogos) fetch(ctx context.Context, iconURL string) (template.URL, error) {
	parsed, err := url.Parse(iconURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", errCatalogLogoURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build catalog logo request: %w", err)
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch catalog logo: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch catalog logo: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCardLogoBytes+1))
	if err != nil {
		return "", fmt.Errorf("read catalog logo: %w", err)
	}
	contentType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return "", fmt.Errorf("parse catalog logo type: %w", err)
	}
	return cardLogoDataURI(contentType, body)
}

// fillLogos gives each slide without a stored logo its fetched registry
// logo. logos lines up with the page the carousel was built from.
func (c *catalogCarousel) fillLogos(logos []template.URL) {
	for i := range c.Slides {
		if c.Slides[i].Logo == "" && i < len(logos) {
			c.Slides[i].Logo = logos[i]
		}
	}
}

// catalogLogoURIs is the fetched registry logo for each candidate on a page,
// or all empty when registry logos are not configured.
func (s *RegistrationService) catalogLogoURIs(ctx context.Context, page []CatalogCandidate) []template.URL {
	if s == nil {
		return make([]template.URL, len(page))
	}
	return s.catalogLogos.forCandidates(ctx, page)
}

// WithCatalogLogos lets the catalog carousel show each server's registry
// icon, fetched through client. Without it a server not yet in the project
// shows its initials.
func (s *RegistrationService) WithCatalogLogos(logger *slog.Logger, client *guardian.HTTPClient) *RegistrationService {
	if s != nil && logger != nil && client != nil {
		s.catalogLogos = newCatalogLogos(logger, client)
	}
	return s
}
