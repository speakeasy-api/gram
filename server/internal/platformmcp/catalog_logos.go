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
	// Logos are decoration: past it, the result shows initials.
	catalogLogoFetchTimeout = 3 * time.Second

	// catalogLogoSlowHost is how long a fetch must have run before running
	// out of time counts against its host. A host that slow is remembered as
	// having no logo, so later searches do not wait on it again; a fetch that
	// started late in the search is not, so it is tried again next time.
	catalogLogoSlowHost = time.Second

	// catalogLogoMaxRedirects bounds the redirects one icon fetch follows.
	catalogLogoMaxRedirects = 3

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

var (
	// errCatalogLogoURL rejects an icon URL, or a redirect, that is not https.
	errCatalogLogoURL = errors.New("catalog logo url is not https")

	// errCatalogLogoRedirects rejects an icon fetch that redirects too often.
	errCatalogLogoRedirects = errors.New("catalog logo fetch redirected too often")
)

// catalogLogos fetches catalog servers' registry icons as card logos.
type catalogLogos struct {
	logger *slog.Logger
	client *guardian.HTTPClient
	cache  *expirable.LRU[string, template.URL]
}

func newCatalogLogos(logger *slog.Logger, client *guardian.HTTPClient) *catalogLogos {
	// A copy, so the redirect rule applies only to logo fetches: the https
	// check on the icon URL must hold for every hop.
	logoClient := *client
	logoClient.CheckRedirect = catalogLogoRedirect
	return &catalogLogos{logger: logger, client: &logoClient, cache: expirable.NewLRU[string, template.URL](catalogLogoCacheSize, nil, catalogLogoCacheTTL)}
}

// catalogLogoRedirect follows a redirect only to another https URL, and only
// a few times.
func catalogLogoRedirect(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return errCatalogLogoURL
	}
	if len(via) >= catalogLogoMaxRedirects {
		return errCatalogLogoRedirects
	}
	return nil
}

// forCandidates returns a logo for each candidate, "" where it has none, the
// icon could not be fetched in time, or skip is set. Each distinct icon URL
// is fetched once.
func (l *catalogLogos) forCandidates(ctx context.Context, candidates []CatalogCandidate, skip []bool) []template.URL {
	logos := make([]template.URL, len(candidates))
	if l == nil || l.client == nil {
		return logos
	}
	byURL := make(map[string][]int, len(candidates))
	order := make([]string, 0, len(candidates))
	for i, candidate := range candidates {
		if candidate.iconURL == "" || i < len(skip) && skip[i] {
			continue
		}
		if _, seen := byURL[candidate.iconURL]; !seen {
			order = append(order, candidate.iconURL)
		}
		byURL[candidate.iconURL] = append(byURL[candidate.iconURL], i)
	}
	if len(order) == 0 {
		return logos
	}
	ctx, cancel := context.WithTimeout(ctx, catalogLogoFetchTimeout)
	defer cancel()
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(catalogLogoConcurrency)
	for _, iconURL := range order {
		indices := byURL[iconURL]
		group.Go(func() error {
			logo := l.logo(groupCtx, iconURL)
			for _, i := range indices {
				logos[i] = logo
			}
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
	started := time.Now()
	logo, err := l.fetch(ctx, iconURL)
	if err != nil {
		if ctx.Err() != nil && time.Since(started) < catalogLogoSlowHost {
			// The search ran out of time before this host had a fair
			// chance; the next search tries again.
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

// storedLogos reports which slides already show a stored logo, so their
// registry icon is not fetched.
func (c *catalogCarousel) storedLogos() []bool {
	stored := make([]bool, len(c.Slides))
	for i := range c.Slides {
		stored[i] = c.Slides[i].Logo != ""
	}
	return stored
}

// catalogLogoURIs is the fetched registry logo for each candidate on a page,
// or all empty when registry logos are not configured. Candidates marked in
// skip are not fetched.
func (s *RegistrationService) catalogLogoURIs(ctx context.Context, page []CatalogCandidate, skip []bool) []template.URL {
	if s == nil {
		return make([]template.URL, len(page))
	}
	return s.catalogLogos.forCandidates(ctx, page, skip)
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
