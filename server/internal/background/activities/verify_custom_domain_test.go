package activities_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	customdomainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func newPassingDNSResolverConfig(targetCNAME, domain, orgID string) dns.MockResolverConfig {
	return dns.MockResolverConfig{
		LookupCNAMEFunc: func(context.Context, string) (string, error) { return targetCNAME, nil },
		LookupHostFunc:  func(context.Context, string) ([]string, error) { return nil, fmt.Errorf("no A record") },
		LookupTXTFunc: func(context.Context, string) ([]string, error) {
			return []string{fmt.Sprintf("gram-domain-verify=%s,%s", domain, orgID)}, nil
		},
	}
}

type testInstance struct {
	conn     *pgxpool.Pool
	repo     *customdomainsRepo.Queries
	resolver dns.Resolver
}

const testTargetCNAME = "target.gram.ai."

func newTestInstance(t *testing.T, orgID, domain string) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, "verify_domain_test")
	require.NoError(t, err)

	return ctx, &testInstance{
		conn:     conn,
		repo:     customdomainsRepo.New(conn),
		resolver: dns.NewMockResolver(newPassingDNSResolverConfig(testTargetCNAME, domain, orgID)),
	}
}

func newActivity(t *testing.T, ti *testInstance) *activities.VerifyCustomDomain {
	t.Helper()

	logger := testenv.NewLogger(t)
	activity := activities.NewVerifyCustomDomain(logger, ti.conn, audit.NewLogger(), testTargetCNAME)
	activity.SetResolver(ti.resolver)

	return activity
}

func TestVerifyCustomDomain_CreatesNewDomain(t *testing.T) {
	t.Parallel()

	const orgID = "org-create-new"
	const domain = "new-domain.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)
	activity := newActivity(t, ti)

	err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:     orgID,
		Domain:    domain,
		CreatedBy: urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
	})
	require.NoError(t, err)

	// Verify domain was created in DB
	got, err := ti.repo.GetCustomDomainByDomain(ctx, domain)
	require.NoError(t, err)
	require.Equal(t, orgID, got.OrganizationID)
	require.Equal(t, domain, got.Domain)
}

func TestVerifyCustomDomain_ExistingDomainSameOrg(t *testing.T) {
	t.Parallel()

	const orgID = "org-existing"
	const domain = "existing.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)
	activity := newActivity(t, ti)

	// Pre-create the domain
	_, err := ti.repo.CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID: orgID,
		Domain:         domain,
	})
	require.NoError(t, err)

	// Calling Do should succeed without creating a duplicate
	err = activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:     orgID,
		Domain:    domain,
		CreatedBy: urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
	})
	require.NoError(t, err)
}

func TestVerifyCustomDomain_ExistingDomainDifferentOrg(t *testing.T) {
	t.Parallel()

	const ownerOrg = "org-owner"
	const otherOrg = "org-other"
	const domain = "owned.example.com"
	ctx, ti := newTestInstance(t, otherOrg, domain)
	activity := newActivity(t, ti)

	// Pre-create the domain owned by a different org
	_, err := ti.repo.CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID: ownerOrg,
		Domain:         domain,
	})
	require.NoError(t, err)

	err = activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:     otherOrg,
		Domain:    domain,
		CreatedBy: urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom domain does not belong to organization")
}

func TestVerifyCustomDomain_TransientDBError(t *testing.T) {
	t.Parallel()

	const orgID = "org-transient"
	const domain = "transient.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)
	activity := newActivity(t, ti)

	// Close the pool to simulate a transient DB error during GetCustomDomainByDomain
	ti.conn.Close()

	err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:     orgID,
		Domain:    domain,
		CreatedBy: urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
	})
	require.Error(t, err)

	// The error should NOT be about domain creation — it should be the lookup failure.
	// Before the fix, this would have attempted to create a domain.
	require.NotContains(t, err.Error(), "error creating custom domain")
}

func TestVerifyCustomDomain_InvalidDomain(t *testing.T) {
	t.Parallel()

	const orgID = "org-invalid"
	ctx, ti := newTestInstance(t, orgID, "x.example.com")
	activity := newActivity(t, ti)

	err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:     orgID,
		Domain:    "not a valid domain!!!",
		CreatedBy: urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "domain is invalid")
}

func TestVerifyCustomDomain_ProhibitedDomain(t *testing.T) {
	t.Parallel()

	const orgID = "org-prohibited"
	ctx, ti := newTestInstance(t, orgID, "x.example.com")
	activity := newActivity(t, ti)

	err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:     orgID,
		Domain:    "docs.getgram.ai",
		CreatedBy: urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "prohibited")
}

func TestVerifyCustomDomain_CNAMEMismatch(t *testing.T) {
	t.Parallel()

	const orgID = "org-cname-mismatch"
	const domain = "cname-mismatch.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	// Return a CNAME that doesn't match the expected target
	cfg := newPassingDNSResolverConfig(testTargetCNAME, domain, orgID)
	cfg.LookupCNAMEFunc = func(context.Context, string) (string, error) { return "wrong.target.com.", nil }
	ti.resolver = dns.NewMockResolver(cfg)

	activity := newActivity(t, ti)

	err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:     orgID,
		Domain:    domain,
		CreatedBy: urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not pointing to")
}

func TestVerifyCustomDomain_TXTRecordMismatch(t *testing.T) {
	t.Parallel()

	const orgID = "org-txt-mismatch"
	const domain = "txt-mismatch.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	// Return a TXT record that doesn't match
	cfg := newPassingDNSResolverConfig(testTargetCNAME, domain, orgID)
	cfg.LookupTXTFunc = func(context.Context, string) ([]string, error) { return []string{"wrong-value"}, nil }
	ti.resolver = dns.NewMockResolver(cfg)

	activity := newActivity(t, ti)

	err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:     orgID,
		Domain:    domain,
		CreatedBy: urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "TXT record")
}

func TestVerifyCustomDomain_DNSLookupFailsNoARecord(t *testing.T) {
	t.Parallel()

	const orgID = "org-dns-fail"
	const domain = "dns-fail.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	// CNAME fails and no A record either
	cfg := newPassingDNSResolverConfig(testTargetCNAME, domain, orgID)
	cfg.LookupCNAMEFunc = func(context.Context, string) (string, error) { return "", fmt.Errorf("no CNAME") }
	cfg.LookupHostFunc = func(context.Context, string) ([]string, error) { return nil, fmt.Errorf("no A record") }
	ti.resolver = dns.NewMockResolver(cfg)

	activity := newActivity(t, ti)

	err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:     orgID,
		Domain:    domain,
		CreatedBy: urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to find custom domain mapping")
}

func TestVerifyCustomDomain_CNAMEFailsButARecordExists(t *testing.T) {
	t.Parallel()

	const orgID = "org-a-record"
	const domain = "a-record.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	// CNAME fails but A record exists — should continue to TXT check
	ti.resolver = dns.NewMockResolver(dns.MockResolverConfig{
		LookupCNAMEFunc: func(context.Context, string) (string, error) { return "", fmt.Errorf("no CNAME") },
		LookupHostFunc:  func(context.Context, string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		LookupTXTFunc: func(context.Context, string) ([]string, error) {
			return []string{fmt.Sprintf("gram-domain-verify=%s,%s", domain, orgID)}, nil
		},
	})

	activity := newActivity(t, ti)

	err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:     orgID,
		Domain:    domain,
		CreatedBy: urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
	})

	// Domain was created in DB
	got, dbErr := ti.repo.GetCustomDomainByDomain(ctx, domain)
	require.NoError(t, dbErr)
	require.Equal(t, orgID, got.OrganizationID)

	require.NoError(t, err)
}

func TestVerifyCustomDomain_SpecialTestDomainAllowed(t *testing.T) {
	t.Parallel()

	const orgID = "org-special"
	const domain = "chat.speakeasy.com"
	ctx, ti := newTestInstance(t, orgID, domain)
	activity := newActivity(t, ti)

	err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:     orgID,
		Domain:    domain,
		CreatedBy: urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
	})
	// Should not be rejected as prohibited
	if err != nil {
		require.NotContains(t, err.Error(), "domain is prohibited")
	}
}

func TestVerifyCustomDomain_TXTLookupError(t *testing.T) {
	t.Parallel()

	const orgID = "org-txt-error"
	const domain = "txt-error.example.com"
	ctx, ti := newTestInstance(t, orgID, domain)

	// TXT lookup fails entirely
	cfg := newPassingDNSResolverConfig(testTargetCNAME, domain, orgID)
	cfg.LookupTXTFunc = func(context.Context, string) ([]string, error) { return nil, fmt.Errorf("DNS timeout") }
	ti.resolver = dns.NewMockResolver(cfg)

	activity := newActivity(t, ti)

	err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
		OrgID:     orgID,
		Domain:    domain,
		CreatedBy: urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to find TXT record")

	// Domain should still have been created in DB (creation happens before DNS checks)
	got, dbErr := ti.repo.GetCustomDomainByDomain(ctx, domain)
	require.NoError(t, dbErr)
	require.Equal(t, orgID, got.OrganizationID)
}

// Verify ErrNoRows is what sqlc returns for missing rows (sanity check).
func TestGetCustomDomainByDomain_ReturnsErrNoRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestInstance(t, "org-sanity", "x.example.com")

	_, err := ti.repo.GetCustomDomainByDomain(ctx, "nonexistent.example.com")
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
