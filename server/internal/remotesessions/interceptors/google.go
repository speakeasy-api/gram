package interceptors

import (
	"context"
	"log/slog"
	"net/url"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type google struct {
	logger *slog.Logger
}

// NewGoogle builds the Google offline-access authorize interceptor.
func NewGoogle(logger *slog.Logger) AuthorizeInterceptor {
	return &google{logger: logger.With(attr.SlogComponent("remotesessions_interceptor_google"))}
}

var _ AuthorizeInterceptor = (*google)(nil)

func (g *google) Name() string { return "google-offline-access" }

func (g *google) Match(issuerURL string) bool {
	u, err := url.Parse(issuerURL)
	if err != nil {
		return false
	}
	// Hostnames are case-insensitive (url.Parse does not normalise case), so
	// fold the comparison to catch mixed-case issuer URLs.
	return strings.EqualFold(u.Hostname(), "accounts.google.com")
}

// ModifyAuthorize requests offline access. Google issues a refresh token only
// with access_type=offline, and re-issues one on re-consent only with
// prompt=consent — a Google-proprietary deviation from RFC 6749 / OIDC
// offline_access.
func (g *google) ModifyAuthorize(ctx context.Context, q url.Values) {
	q.Set("access_type", "offline")

	// prompt is a space-delimited list. Merge consent in rather than
	// overwriting any value already carried on the authorization endpoint
	// (e.g. select_account) so we don't silently drop other prompt behaviour.
	// consent must always be present — Google omits the refresh token on a
	// silent re-consent.
	prompts := strings.Fields(q.Get("prompt"))
	if !slices.Contains(prompts, "consent") {
		prompts = append(prompts, "consent")
	}
	q.Set("prompt", strings.Join(prompts, " "))

	g.logger.InfoContext(ctx,
		"applied authorize interceptor: requesting offline access (access_type=offline, prompt=consent)")
}
