package admin

import (
	"context"
	"github.com/jackc/pgx/v5/pgtype"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	customdomainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
)

// seedCustomDomainRouting gives a custom domain a DNS target token and, when
// method is non-empty, an observed routing method.
func seedCustomDomainRouting(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, id uuid.UUID, token, method string) {
	t.Helper()

	queries := customdomainsRepo.New(conn)
	_, err := queries.AssignCustomDomainDNSTargetToken(ctx, customdomainsRepo.AssignCustomDomainDNSTargetTokenParams{
		DnsTargetToken: token,
		ID:             id,
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	if method == "" {
		return
	}
	checkedAt := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	_, err = queries.UpdateCustomDomainHealth(ctx, customdomainsRepo.UpdateCustomDomainHealthParams{
		DnsRoutingMethod:       pgtype.Text{String: method, Valid: true},
		DnsConfigurationStatus: pgtype.Text{String: "matched", Valid: true},
		DnsCheckedAt:           checkedAt,
		CheckedAt:              checkedAt,
		HealthStatus:           pgtype.Text{String: "healthy", Valid: true},
		ID:                     id,
		OrganizationID:         orgID,
	})
	require.NoError(t, err)
}

func listCustomDomainNames(t *testing.T, ctx context.Context, svc *Service, payload *gen.ListCustomDomainsPayload) []string {
	t.Helper()

	res, err := svc.ListCustomDomains(ctx, payload)
	require.NoError(t, err)
	names := make([]string, len(res.CustomDomains))
	for i, domain := range res.CustomDomains {
		names[i] = domain.Domain
	}
	return names
}

func TestListCustomDomains_ReturnsTargetAndRoutingMethod(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	svc.SetCustomDomainTargetSuffix("domains.example.net")
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	id := seedCustomDomain(t, ctx, conn, "org_one", "mcp.one.example.com", true)
	seedCustomDomainRouting(t, ctx, conn, "org_one", id, "abcdefgh23", "shared_cname")

	res, err := svc.ListCustomDomains(ctx, &gen.ListCustomDomainsPayload{})
	require.NoError(t, err)
	require.Len(t, res.CustomDomains, 1)
	domain := res.CustomDomains[0]
	require.Equal(t, id.String(), domain.ID)
	require.Equal(t, "org_one", domain.OrganizationID)
	require.Equal(t, "One", domain.OrganizationName)
	require.Equal(t, "one", domain.OrganizationSlug)
	require.Equal(t, "mcp.one.example.com", domain.Domain)
	require.True(t, domain.Verified)
	require.True(t, domain.Activated)
	require.Equal(t, new("abcdefgh23"), domain.DNSTargetToken)
	require.Equal(t, new("abcdefgh23.domains.example.net"), domain.DNSTarget)
	require.Equal(t, new("shared_cname"), domain.DNSRoutingMethod)
	require.Equal(t, new("matched"), domain.DNSConfigurationStatus)
	require.NotNil(t, domain.DNSCheckedAt)
	require.Equal(t, domain.DNSCheckedAt, domain.HealthCheckedAt)
	require.Equal(t, new("healthy"), domain.HealthStatus)
}

func TestListCustomDomains_OmitsTargetWithoutSuffix(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	id := seedCustomDomain(t, ctx, conn, "org_one", "mcp.one.example.com", true)
	seedCustomDomainRouting(t, ctx, conn, "org_one", id, "abcdefgh23", "")

	res, err := svc.ListCustomDomains(ctx, &gen.ListCustomDomainsPayload{})
	require.NoError(t, err)
	require.Len(t, res.CustomDomains, 1)
	require.Equal(t, new("abcdefgh23"), res.CustomDomains[0].DNSTargetToken)
	require.Nil(t, res.CustomDomains[0].DNSTarget)
	require.Nil(t, res.CustomDomains[0].DNSRoutingMethod)
	require.Nil(t, res.CustomDomains[0].DNSCheckedAt)
	require.Equal(t, new("unknown"), res.CustomDomains[0].HealthStatus)
}

func TestListCustomDomains_FiltersByRoutingMethod(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_two", name: "Two", slug: "two"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_three", name: "Three", slug: "three"})
	one := seedCustomDomain(t, ctx, conn, "org_one", "mcp.one.example.com", true)
	two := seedCustomDomain(t, ctx, conn, "org_two", "mcp.two.example.com", true)
	seedCustomDomain(t, ctx, conn, "org_three", "mcp.three.example.com", false)
	seedCustomDomainRouting(t, ctx, conn, "org_one", one, "abcdefgh23", "per_domain_cname")
	seedCustomDomainRouting(t, ctx, conn, "org_two", two, "bcdefgh234", "shared_cname")

	require.Equal(t, []string{"mcp.one.example.com", "mcp.three.example.com", "mcp.two.example.com"},
		listCustomDomainNames(t, ctx, svc, &gen.ListCustomDomainsPayload{}))
	require.Equal(t, []string{"mcp.two.example.com"},
		listCustomDomainNames(t, ctx, svc, &gen.ListCustomDomainsPayload{DNSRoutingMethod: new("shared_cname")}))
	require.Equal(t, []string{"mcp.one.example.com"},
		listCustomDomainNames(t, ctx, svc, &gen.ListCustomDomainsPayload{DNSRoutingMethod: new("per_domain_cname")}))
	// A domain no health check has observed yet counts as unknown.
	require.Equal(t, []string{"mcp.three.example.com"},
		listCustomDomainNames(t, ctx, svc, &gen.ListCustomDomainsPayload{DNSRoutingMethod: new("unknown")}))
}

func TestListCustomDomains_FiltersByOrganization(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_two", name: "Two", slug: "two"})
	seedCustomDomain(t, ctx, conn, "org_one", "mcp.one.example.com", true)
	seedCustomDomain(t, ctx, conn, "org_two", "mcp.two.example.com", true)

	require.Equal(t, []string{"mcp.two.example.com"},
		listCustomDomainNames(t, ctx, svc, &gen.ListCustomDomainsPayload{OrganizationID: new("org_two")}))
	require.Empty(t, listCustomDomainNames(t, ctx, svc, &gen.ListCustomDomainsPayload{OrganizationID: new("org_missing")}))
}

func TestListCustomDomains_ExcludesDeletedDomains(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	seedCustomDomain(t, ctx, conn, "org_one", "mcp.one.example.com", true)
	require.NoError(t, customdomainsRepo.New(conn).DeleteCustomDomain(ctx, "org_one"))

	require.Empty(t, listCustomDomainNames(t, ctx, svc, &gen.ListCustomDomainsPayload{}))
}

func TestListCustomDomains_OlderWorkerHealthIsUnknown(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_one", name: "One", slug: "one"})
	id := seedCustomDomain(t, ctx, conn, "org_one", "mcp.one.example.com", true)
	seedCustomDomainRouting(t, ctx, conn, "org_one", id, "abcdefgh23", "per_domain_cname")
	err := testrepo.New(conn).SetCustomDomainHealthWithoutDNSObservation(ctx, testrepo.SetCustomDomainHealthWithoutDNSObservationParams{
		CheckedAt: pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true}, ID: id, OrganizationID: "org_one",
	})
	require.NoError(t, err)
	res, err := svc.ListCustomDomains(ctx, &gen.ListCustomDomainsPayload{})
	require.NoError(t, err)
	require.Len(t, res.CustomDomains, 1)
	domain := res.CustomDomains[0]
	require.Equal(t, new("matched"), domain.DNSConfigurationStatus)
	require.NotEqual(t, domain.DNSCheckedAt, domain.HealthCheckedAt)
	require.Equal(t, new("unknown"), domain.HealthStatus)
	require.Nil(t, domain.HealthIssue)
}
