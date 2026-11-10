package remotesessions

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// catalogBrandingRowLimit caps the catalog rows read for one consent page. A
// page has a handful of cards and the catalog rarely holds more than one row
// per authorization server, so 100 is far above any real render.
const catalogBrandingRowLimit = 100

// WithCatalogBranding fills a missing issuer name or logo from the platform
// catalog issuer describing the same authorization server. Best-effort: the
// clients come back unchanged when the catalog cannot be read.
func (m *ChallengeManager) WithCatalogBranding(ctx context.Context, clients []Client) []Client {
	var issuers []string
	for _, c := range clients {
		if !lacksIssuerBranding(c) {
			continue
		}
		for _, spelling := range issuerSpellings(c.IssuerURL) {
			if !slices.Contains(issuers, spelling) {
				issuers = append(issuers, spelling)
			}
		}
	}
	if len(issuers) == 0 {
		return clients
	}

	catalog, err := remotesessions_repo.New(m.db).ListGlobalRemoteSessionIssuersByIssuerURL(ctx, remotesessions_repo.ListGlobalRemoteSessionIssuersByIssuerURLParams{
		Issuers:    issuers,
		LimitValue: catalogBrandingRowLimit,
	})
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			m.logger.WarnContext(ctx, "list catalog issuers for consent branding", attr.SlogError(err))
		}
		return clients
	}
	return applyCatalogBranding(clients, catalog)
}

func lacksIssuerBranding(c Client) bool {
	return !c.IssuerLogoAssetID.Valid || issuerNameUnset(c)
}

func issuerNameUnset(c Client) bool {
	return strings.TrimSpace(conv.PtrValOr(c.IssuerName, "")) == ""
}

// issuerSpellings falls back to the stored spelling for a URL the canonical
// parser rejects, so an odd legacy row still matches itself exactly.
func issuerSpellings(raw string) []string {
	canonical, err := issuerurl.Parse(raw)
	if err != nil {
		return []string{raw}
	}
	return canonical.MatchCandidates()
}

func canonicalIssuer(raw string) string {
	canonical, err := issuerurl.Parse(raw)
	if err != nil {
		return raw
	}
	return canonical.String()
}

// applyCatalogBranding borrows only from a catalog issuer with the same
// authorization endpoint, so the brand always matches where the browser is
// sent. Each client borrows from one catalog row, the oldest that supplies
// something it lacks, so duplicate catalog rows never mix a name with
// another row's logo.
func applyCatalogBranding(clients []Client, catalog []remotesessions_repo.RemoteSessionIssuer) []Client {
	out := slices.Clone(clients)
	for i := range out {
		c := &out[i]
		if c.AuthorizationEndpoint == "" || !lacksIssuerBranding(*c) {
			continue
		}
		issuer := canonicalIssuer(c.IssuerURL)
		needsName, needsLogo := issuerNameUnset(*c), !c.IssuerLogoAssetID.Valid
		for _, g := range catalog {
			if g.AuthorizationEndpoint.String != c.AuthorizationEndpoint || canonicalIssuer(g.Issuer) != issuer {
				continue
			}
			name := strings.TrimSpace(g.Name.String)
			takeName, takeLogo := needsName && name != "", needsLogo && g.LogoAssetID.Valid
			if !takeName && !takeLogo {
				continue
			}
			if takeName {
				c.IssuerName = &name
			}
			if takeLogo {
				c.IssuerLogoAssetID = g.LogoAssetID
			}
			break
		}
	}
	return out
}
