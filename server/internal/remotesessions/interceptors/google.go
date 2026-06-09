package interceptors

import (
	"context"
	"log/slog"
	"net/url"

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
	return u.Hostname() == "accounts.google.com"
}

// ModifyAuthorize requests offline access. Google issues a refresh token only
// with access_type=offline, and re-issues one on re-consent only with
// prompt=consent — a Google-proprietary deviation from RFC 6749 / OIDC
// offline_access.
func (g *google) ModifyAuthorize(ctx context.Context, q url.Values) {
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	g.logger.InfoContext(ctx,
		"applied authorize interceptor: requesting offline access (access_type=offline, prompt=consent)")
}
