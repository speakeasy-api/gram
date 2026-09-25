package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

func createLiveCustomDomain(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, domain string) uuid.UUID {
	t.Helper()

	queries := customdomainsrepo.New(conn)
	d, err := queries.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  orgID,
		Domain:          domain,
		IngressName:     pgtype.Text{String: "", Valid: false},
		CertSecretName:  pgtype.Text{String: "", Valid: false},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	_, err = queries.UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{
		Verified:        true,
		Activated:       true,
		IngressName:     pgtype.Text{String: "", Valid: false},
		CertSecretName:  pgtype.Text{String: "", Valid: false},
		ProvisionerKind: "ingress",
		ID:              d.ID,
	})
	require.NoError(t, err)
	return d.ID
}

// The consent redirect after an upstream login returns to the custom domain
// the challenge was minted on, even when the endpoint pins the id of another
// domain (such as a retired shared fallback domain), and falls back to the
// platform host only when the mint host does not admit the organization.
func TestCompleteRemoteLogin_ReturnsToMintCustomDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		mintDomainOf func(fx resourceDanceFixture) string
		wantBase     func(mintBase string) string
	}{
		{
			name:         "organization's domain while the endpoint pins another",
			mintDomainOf: func(fx resourceDanceFixture) string { return fx.parent.OrganizationID },
			wantBase:     func(mintBase string) string { return mintBase },
		},
		{
			name:         "domain owned by another organization",
			mintDomainOf: func(resourceDanceFixture) string { return "org_" + uuid.NewString() },
			wantBase:     func(string) string { return "http://localhost" },
		},
		{
			name:         "unknown domain",
			mintDomainOf: nil,
			wantBase:     func(string) string { return "http://localhost" },
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var spy upstreamSpy
			ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "cd-"+string(rune('a'+i)), &spy)

			suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
			pinnedHost := "pinned-" + suffix + ".example.com"
			mintHost := "mint-" + suffix + ".example.com"
			pinnedID := createLiveCustomDomain(t, ctx, fx.ti.conn, "org_"+uuid.NewString(), pinnedHost)
			if tc.mintDomainOf != nil {
				createLiveCustomDomain(t, ctx, fx.ti.conn, tc.mintDomainOf(fx), mintHost)
			}

			mintBase := "https://" + mintHost
			parent := fx.parent
			parent.Authority = networkingress.Authority{
				Surface:          requestorigin.SurfaceCustomDomain,
				BaseURL:          mintBase,
				OrganizationID:   fx.parent.OrganizationID,
				NetworkIngressID: uuid.Nil,
				NamespaceKind:    networkingress.NamespaceCustomDomain,
				CustomDomainID:   uuid.NullUUID{UUID: pinnedID, Valid: true},
			}

			authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, fx.clients[0])
			require.NoError(t, err)
			parsed, err := url.Parse(authURL)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?code=fake-code&state="+url.QueryEscape(parsed.Query().Get("state")), nil)
			result, err := fx.mgr.CompleteRemoteLogin(req.WithContext(ctx))
			require.NoError(t, err)
			require.NoError(t, spy.handlerErr)

			require.Equal(t, tc.wantBase(mintBase)+"/mcp/"+parent.McpSlug+"/connect?state="+url.QueryEscape(parent.ID), result.RedirectURL)
		})
	}
}
