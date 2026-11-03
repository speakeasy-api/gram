package activities_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	routingTestOrganization = "test-organization"
	routingTestSharedTarget = "custom-domain.example.com"
	routingTestSuffix       = "domains.example.net"
	routingTestToken        = "owntoken23"
	routingTestDomainTarget = routingTestToken + "." + routingTestSuffix
	routingTestOtherTarget  = "othertok45." + routingTestSuffix

	// routingTestInfraTarget is a name under the suffix that is not shaped
	// like a token, such as an infrastructure record.
	routingTestInfraTarget = "lb1." + routingTestSuffix
)

var (
	// routingTestIngressIP is what the shared and per-domain targets resolve
	// to by default; it is also the static A record target.
	routingTestIngressIP = netip.MustParseAddr("1.2.3.4")

	// routingTestMovedIP is a data plane outside the static list that a
	// per-domain target can be moved to.
	routingTestMovedIP = netip.MustParseAddr("7.7.7.7")

	// routingTestProxyIP is an address that is not Speakeasy's, as served by
	// a proxy or CDN in front of the domain.
	routingTestProxyIP = netip.MustParseAddr("9.9.9.9")
)

// routingTestDNS describes the DNS a routing test serves for its domain.
type routingTestDNS struct {
	// CNAME is the domain's CNAME answer; empty means the name is flattened
	// (LookupCNAME returns the domain itself).
	CNAME string

	// Addrs are the domain's address records; nil means NXDOMAIN.
	Addrs []netip.Addr

	// DomainTargetAddrs are what the domain's per-domain target resolves to;
	// nil resolves it to routingTestIngressIP.
	DomainTargetAddrs []netip.Addr

	// DomainTargetErr fails resolution of the domain's per-domain target.
	DomainTargetErr error

	// DomainErr fails resolution of the domain's addresses with an error other
	// than not found.
	DomainErr error

	// CNAMEErr fails the domain's CNAME lookup.
	CNAMEErr error
}

func routingTestResolver(domain string, cfg routingTestDNS) dns.Resolver {
	return dns.NewMockResolver(dns.MockResolverConfig{
		LookupCNAMEFunc: func(_ context.Context, host string) (string, error) {
			if host == domain && cfg.CNAMEErr != nil {
				return "", cfg.CNAMEErr
			}
			if host == domain && cfg.CNAME != "" {
				return cfg.CNAME, nil
			}
			return host + ".", nil
		},
		LookupNetIPFunc: func(_ context.Context, _ string, host string) ([]netip.Addr, error) {
			switch host {
			case domain:
				if cfg.DomainErr != nil {
					return nil, cfg.DomainErr
				}
				if cfg.Addrs == nil {
					return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
				}
				return cfg.Addrs, nil
			case routingTestDomainTarget:
				if cfg.DomainTargetErr != nil {
					return nil, cfg.DomainTargetErr
				}
				if cfg.DomainTargetAddrs != nil {
					return cfg.DomainTargetAddrs, nil
				}
				return []netip.Addr{routingTestIngressIP}, nil
			default:
				return []netip.Addr{routingTestIngressIP}, nil
			}
		},
		LookupTXTFunc: func(context.Context, string) ([]string, error) { return nil, nil },
	})
}

// createRoutingTestDomain creates an activated domain whose per-domain token
// is routingTestToken.
func createRoutingTestDomain(t *testing.T, conn *pgxpool.Pool, domain string) uuid.UUID {
	t.Helper()

	repository := customdomainsrepo.New(conn)
	domainID := createActivatedCustomDomain(t, repository, routingTestOrganization, domain)
	_, err := repository.AssignCustomDomainDNSTargetToken(t.Context(), customdomainsrepo.AssignCustomDomainDNSTargetTokenParams{
		DnsTargetToken: routingTestToken,
		ID:             domainID,
		OrganizationID: routingTestOrganization,
	})
	require.NoError(t, err)
	return domainID
}

func newRoutingTestHealthChecker(t *testing.T, conn *pgxpool.Pool, suffix string) *activities.CustomDomainHealth {
	t.Helper()

	return activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, &stubInfrastructureChecker{resources: nil, provisioner: nil}, routingTestSharedTarget, []netip.Addr{routingTestIngressIP}, suffix, noopEmailService(t), nil, nil)
}

func getRoutingTestDomain(t *testing.T, conn *pgxpool.Pool, domainID uuid.UUID) customdomainsrepo.CustomDomain {
	t.Helper()

	domain, err := customdomainsrepo.New(conn).GetCustomDomainByIDAndOrganization(t.Context(), customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{
		ID:             domainID,
		OrganizationID: routingTestOrganization,
	})
	require.NoError(t, err)
	return domain
}

func TestCustomDomainHealthCheckRoutingMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		suffix     string
		dns        routingTestDNS
		probeErr   error
		wantStatus customdomains.HealthStatus
		wantIssue  customdomains.HealthIssue
		wantMethod customdomains.DNSRoutingMethod
	}{
		{
			name:       "own target with trailing dot",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: routingTestDomainTarget + ".", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil},
			probeErr:   errors.New("connection refused"),
			wantStatus: customdomains.HealthStatusHealthy,
			wantIssue:  "",
			wantMethod: customdomains.DNSRoutingMethodPerDomainCNAME,
		},
		{
			name:       "own target without trailing dot",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: routingTestDomainTarget, Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil},
			probeErr:   errors.New("connection refused"),
			wantStatus: customdomains.HealthStatusHealthy,
			wantIssue:  "",
			wantMethod: customdomains.DNSRoutingMethodPerDomainCNAME,
		},
		{
			name:       "own target moved off the static IPs",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: routingTestDomainTarget + ".", Addrs: []netip.Addr{routingTestMovedIP}, DomainTargetAddrs: []netip.Addr{routingTestMovedIP}},
			probeErr:   errors.New("connection refused"),
			wantStatus: customdomains.HealthStatusHealthy,
			wantIssue:  "",
			wantMethod: customdomains.DNSRoutingMethodPerDomainCNAME,
		},
		{
			name:       "own target with stray address records",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: routingTestDomainTarget + ".", Addrs: []netip.Addr{routingTestIngressIP, routingTestProxyIP}, DomainTargetAddrs: nil},
			probeErr:   errors.New("connection refused"),
			wantStatus: customdomains.HealthStatusUnhealthy,
			wantIssue:  customdomains.HealthIssueDNSTargetMismatch,
			wantMethod: customdomains.DNSRoutingMethodPerDomainCNAME,
		},
		{
			name:       "own target resolution failure trusts the cname",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: routingTestDomainTarget + ".", Addrs: []netip.Addr{routingTestMovedIP}, DomainTargetAddrs: nil, DomainTargetErr: errors.New("dns timeout")},
			probeErr:   errors.New("connection refused"),
			wantStatus: customdomains.HealthStatusHealthy,
			wantIssue:  "",
			wantMethod: customdomains.DNSRoutingMethodPerDomainCNAME,
		},
		{
			name:       "another domain's target is not rescued by the probe",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: routingTestOtherTarget + ".", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil},
			probeErr:   nil,
			wantStatus: customdomains.HealthStatusUnhealthy,
			wantIssue:  customdomains.HealthIssueDNSTargetMismatch,
			wantMethod: customdomains.DNSRoutingMethodUnknown,
		},
		{
			name:       "non-token name under the suffix is rescued by the probe",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: routingTestInfraTarget + ".", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil},
			probeErr:   nil,
			wantStatus: customdomains.HealthStatusHealthy,
			wantIssue:  "",
			wantMethod: customdomains.DNSRoutingMethodUnknown,
		},
		{
			name:       "shared target with stray address records",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: routingTestSharedTarget + ".", Addrs: []netip.Addr{routingTestIngressIP, routingTestProxyIP}, DomainTargetAddrs: nil},
			probeErr:   errors.New("connection refused"),
			wantStatus: customdomains.HealthStatusUnhealthy,
			wantIssue:  customdomains.HealthIssueDNSTargetMismatch,
			wantMethod: customdomains.DNSRoutingMethodSharedCNAME,
		},
		{
			name:       "shared target with stray address records rescued by the probe keeps its method",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: routingTestSharedTarget + ".", Addrs: []netip.Addr{routingTestIngressIP, routingTestProxyIP}, DomainTargetAddrs: nil},
			probeErr:   nil,
			wantStatus: customdomains.HealthStatusHealthy,
			wantIssue:  "",
			wantMethod: customdomains.DNSRoutingMethodSharedCNAME,
		},
		{
			name:       "routing check failure on the final attempt",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: "", Addrs: nil, DomainTargetAddrs: nil, DomainErr: errors.New("dns timeout")},
			probeErr:   errors.New("connection refused"),
			wantStatus: customdomains.HealthStatusUnhealthy,
			wantIssue:  customdomains.HealthIssueCheckFailed,
			wantMethod: customdomains.DNSRoutingMethodUnknown,
		},
		{
			name:       "shared target",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: routingTestSharedTarget + ".", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil},
			probeErr:   errors.New("connection refused"),
			wantStatus: customdomains.HealthStatusHealthy,
			wantIssue:  "",
			wantMethod: customdomains.DNSRoutingMethodSharedCNAME,
		},
		{
			name:       "a records",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: "", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil},
			probeErr:   errors.New("connection refused"),
			wantStatus: customdomains.HealthStatusHealthy,
			wantIssue:  "",
			wantMethod: customdomains.DNSRoutingMethodARecord,
		},
		{
			name:       "a records after a failed cname lookup are not classified",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: "", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil, CNAMEErr: errors.New("dns timeout")},
			probeErr:   errors.New("connection refused"),
			wantStatus: customdomains.HealthStatusHealthy,
			wantIssue:  "",
			wantMethod: customdomains.DNSRoutingMethodUnknown,
		},
		{
			name:       "proxied domain rescued by the probe",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: "", Addrs: []netip.Addr{routingTestProxyIP}, DomainTargetAddrs: nil},
			probeErr:   nil,
			wantStatus: customdomains.HealthStatusHealthy,
			wantIssue:  "",
			wantMethod: customdomains.DNSRoutingMethodUnknown,
		},
		{
			name:       "dns not found",
			suffix:     routingTestSuffix,
			dns:        routingTestDNS{CNAME: "", Addrs: nil, DomainTargetAddrs: nil},
			probeErr:   errors.New("connection refused"),
			wantStatus: customdomains.HealthStatusUnhealthy,
			wantIssue:  customdomains.HealthIssueDNSNotFound,
			wantMethod: customdomains.DNSRoutingMethodUnknown,
		},
		{
			name:       "suffix unset accepts the shared target",
			suffix:     "",
			dns:        routingTestDNS{CNAME: routingTestSharedTarget + ".", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil},
			probeErr:   errors.New("connection refused"),
			wantStatus: customdomains.HealthStatusHealthy,
			wantIssue:  "",
			wantMethod: customdomains.DNSRoutingMethodSharedCNAME,
		},
		{
			name:       "suffix unset treats a per-domain target as unrecognized",
			suffix:     "",
			dns:        routingTestDNS{CNAME: routingTestDomainTarget + ".", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil},
			probeErr:   errors.New("connection refused"),
			wantStatus: customdomains.HealthStatusUnhealthy,
			wantIssue:  customdomains.HealthIssueDNSTargetMismatch,
			wantMethod: customdomains.DNSRoutingMethodUnknown,
		},
		{
			name:       "suffix unset lets the probe rescue an unrecognized target",
			suffix:     "",
			dns:        routingTestDNS{CNAME: routingTestOtherTarget + ".", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil},
			probeErr:   nil,
			wantStatus: customdomains.HealthStatusHealthy,
			wantIssue:  "",
			wantMethod: customdomains.DNSRoutingMethodUnknown,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			conn, err := infra.CloneTestDatabase(t, "custom_domain_health_routing_methods")
			require.NoError(t, err)
			domainName := "routing.example.com"
			domainID := createRoutingTestDomain(t, conn, domainName)

			checker := newRoutingTestHealthChecker(t, conn, tc.suffix)
			checker.SetResolver(routingTestResolver(domainName, tc.dns))
			checker.SetProbe(func(context.Context, string) error { return tc.probeErr })

			_, err = checker.Check(t.Context(), activities.CheckCustomDomainHealthArgs{
				CustomDomainID: domainID,
				OrganizationID: routingTestOrganization,
				CheckedAt:      time.Now().UTC().Truncate(time.Microsecond),
			})
			require.NoError(t, err)

			domain := getRoutingTestDomain(t, conn, domainID)
			require.Equal(t, string(tc.wantStatus), domain.HealthStatus.String)
			require.Equal(t, string(tc.wantIssue), domain.HealthIssue.String)
			require.Equal(t, string(tc.wantMethod), domain.DnsRoutingMethod.String)
		})
	}
}

func TestCustomDomainHealthCheckBackfilledTokenAcceptsOwnTarget(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_health_backfill_own_target")
	require.NoError(t, err)
	repository := customdomainsrepo.New(conn)
	domainName := "backfill-target.example.com"
	domainID := createActivatedCustomDomain(t, repository, routingTestOrganization, domainName)

	// The token does not exist until the check backfills it, so the CNAME is
	// read from the row at lookup time.
	checker := newRoutingTestHealthChecker(t, conn, routingTestSuffix)
	checker.SetResolver(dns.NewMockResolver(dns.MockResolverConfig{
		LookupCNAMEFunc: func(ctx context.Context, host string) (string, error) {
			if host != domainName {
				return host + ".", nil
			}
			token, err := repository.GetCustomDomainDNSTargetToken(ctx, customdomainsrepo.GetCustomDomainDNSTargetTokenParams{
				ID:             domainID,
				OrganizationID: routingTestOrganization,
			})
			if err != nil {
				return "", fmt.Errorf("get dns target token: %w", err)
			}
			return token.String + "." + routingTestSuffix + ".", nil
		},
		LookupNetIPFunc: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{routingTestIngressIP}, nil
		},
		LookupTXTFunc: func(context.Context, string) ([]string, error) { return nil, nil },
	}))
	checker.SetProbe(func(context.Context, string) error { return errors.New("connection refused") })

	_, err = checker.Check(t.Context(), activities.CheckCustomDomainHealthArgs{
		CustomDomainID: domainID,
		OrganizationID: routingTestOrganization,
		CheckedAt:      time.Now().UTC().Truncate(time.Microsecond),
	})
	require.NoError(t, err)

	domain := getRoutingTestDomain(t, conn, domainID)
	require.Equal(t, string(customdomains.HealthStatusHealthy), domain.HealthStatus.String)
	require.Equal(t, string(customdomains.DNSRoutingMethodPerDomainCNAME), domain.DnsRoutingMethod.String)
}

func TestCustomDomainHealthCheckOtherDNSTargetNotifies(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_health_other_target_notify")
	require.NoError(t, err)
	domainName := "other.example.com"
	domainID := createRoutingTestDomain(t, conn, domainName)

	checker := newRoutingTestHealthChecker(t, conn, routingTestSuffix)
	checker.SetResolver(routingTestResolver(domainName, routingTestDNS{CNAME: routingTestOtherTarget + ".", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil}))
	checker.SetProbe(func(context.Context, string) error { return nil })

	notification, err := checker.Check(t.Context(), activities.CheckCustomDomainHealthArgs{
		CustomDomainID: domainID,
		OrganizationID: routingTestOrganization,
		CheckedAt:      time.Now().UTC().Truncate(time.Microsecond),
	})
	require.NoError(t, err)
	require.Equal(t, customdomains.HealthIssueDNSTargetMismatch, notification.Issue)
	require.Equal(t, routingTestDomainTarget, notification.DNSTarget)
	require.True(t, notification.OtherDNSTarget)
}

func TestCustomDomainHealthCheckStaleCheckKeepsRoutingMethod(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_health_stale_method")
	require.NoError(t, err)
	domainName := "stale.example.com"
	domainID := createRoutingTestDomain(t, conn, domainName)
	checker := newRoutingTestHealthChecker(t, conn, routingTestSuffix)
	checker.SetProbe(func(context.Context, string) error { return errors.New("connection refused") })

	checkedAt := time.Now().UTC().Truncate(time.Microsecond)
	checker.SetResolver(routingTestResolver(domainName, routingTestDNS{CNAME: routingTestDomainTarget + ".", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil}))
	_, err = checker.Check(t.Context(), activities.CheckCustomDomainHealthArgs{
		CustomDomainID: domainID,
		OrganizationID: routingTestOrganization,
		CheckedAt:      checkedAt,
	})
	require.NoError(t, err)

	// An older check observing the shared target must not overwrite the newer
	// observation, and neither must a retry of the newer check.
	checker.SetResolver(routingTestResolver(domainName, routingTestDNS{CNAME: routingTestSharedTarget + ".", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil}))
	for _, staleCheckedAt := range []time.Time{checkedAt.Add(-time.Hour), checkedAt} {
		_, err = checker.Check(t.Context(), activities.CheckCustomDomainHealthArgs{
			CustomDomainID: domainID,
			OrganizationID: routingTestOrganization,
			CheckedAt:      staleCheckedAt,
		})
		require.NoError(t, err)
	}

	domain := getRoutingTestDomain(t, conn, domainID)
	require.Equal(t, string(customdomains.DNSRoutingMethodPerDomainCNAME), domain.DnsRoutingMethod.String)
}

func TestCustomDomainNotifyOrgAdminsNamesPerDomainTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		dnsTarget      string
		otherDNSTarget bool
		wantMessage    string
	}{
		{
			name:           "per-domain target",
			dnsTarget:      routingTestDomainTarget,
			otherDNSTarget: false,
			wantMessage:    "The domain's DNS no longer resolves to the expected target. Create a CNAME record pointing the domain at " + routingTestDomainTarget + ".",
		},
		{
			name:           "another domain's target",
			dnsTarget:      routingTestDomainTarget,
			otherDNSTarget: true,
			wantMessage:    "The domain's CNAME record points at a routing target that belongs to another domain or is no longer in use. Create a CNAME record pointing the domain at " + routingTestDomainTarget + " instead.",
		},
		{
			name:           "no per-domain target",
			dnsTarget:      "",
			otherDNSTarget: false,
			wantMessage:    "The domain's DNS no longer resolves to the expected target. Create a CNAME record pointing the domain at " + routingTestSharedTarget + ".",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			conn, err := infra.CloneTestDatabase(t, "custom_domain_notify_per_domain_target")
			require.NoError(t, err)
			organizationID := "org-custom-domain-notify-target"
			seedCustomDomainAdminRecipient(t, conn, organizationID)

			captured := &captureLoopsClient{sent: nil, failNext: 0}
			checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, &stubInfrastructureChecker{resources: nil, provisioner: nil}, routingTestSharedTarget, nil, routingTestSuffix, email.NewService(testenv.NewLogger(t), captured, email.NewTemplateIDs(map[string]string{
				"custom_domain_unhealthy": "domain-unhealthy-test-id",
			}), true), customDomainTestOrgHosts(t), nil)

			err = checker.NotifyOrgAdmins(t.Context(), activities.NotifyCustomDomainUnhealthyArgs{
				CustomDomainID: uuid.New(),
				OrganizationID: organizationID,
				Domain:         "chat.example.com",
				Issue:          customdomains.HealthIssueDNSTargetMismatch,
				CheckedAt:      time.Now().UTC().Truncate(time.Microsecond),
				DNSTarget:      tc.dnsTarget,
				OtherDNSTarget: tc.otherDNSTarget,
			})
			require.NoError(t, err)

			sent := captured.Sent()
			require.Len(t, sent, 1)
			require.Equal(t, tc.wantMessage, sent[0].DataVariables["issue_message"])
		})
	}
}

func TestVerifyCustomDomain_RecordsRoutingMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dns        routingTestDNS
		wantStatus activities.VerifyCustomDomainStatus
		wantMethod customdomains.DNSRoutingMethod
	}{
		{
			name:       "own target",
			dns:        routingTestDNS{CNAME: routingTestDomainTarget + ".", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil},
			wantStatus: activities.VerifyStatusVerified,
			wantMethod: customdomains.DNSRoutingMethodPerDomainCNAME,
		},
		{
			name:       "another domain's target",
			dns:        routingTestDNS{CNAME: routingTestOtherTarget + ".", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil},
			wantStatus: activities.VerifyStatusVerified,
			wantMethod: customdomains.DNSRoutingMethodUnknown,
		},
		{
			name:       "dns pending",
			dns:        routingTestDNS{CNAME: "", Addrs: nil, DomainTargetAddrs: nil},
			wantStatus: activities.VerifyStatusDNSPending,
			wantMethod: customdomains.DNSRoutingMethodUnknown,
		},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			orgID := "org-verify-routing-" + strconv.Itoa(i)
			domainName := "verify-routing.example.com"
			ctx, ti := newTestInstance(t, orgID, domainName)
			row := createDomainRow(t, ctx, ti, orgID, domainName)
			_, err := ti.repo.AssignCustomDomainDNSTargetToken(ctx, customdomainsrepo.AssignCustomDomainDNSTargetTokenParams{
				DnsTargetToken: routingTestToken,
				ID:             row.ID,
				OrganizationID: orgID,
			})
			require.NoError(t, err)

			resolver := routingTestResolver(domainName, tc.dns)
			activity := activities.NewVerifyCustomDomain(testenv.NewLogger(t), ti.conn, audit.NewLogger(), routingTestSharedTarget, []netip.Addr{routingTestIngressIP}, routingTestSuffix)
			activity.SetResolver(dns.NewMockResolver(dns.MockResolverConfig{
				LookupCNAMEFunc: resolver.LookupCNAME,
				LookupNetIPFunc: resolver.LookupNetIP,
				LookupTXTFunc: func(context.Context, string) ([]string, error) {
					return []string{"gram-domain-verify=" + domainName + "," + orgID}, nil
				},
			}))

			result, err := activity.Do(ctx, activities.VerifyCustomDomainArgs{
				OrgID:          orgID,
				Domain:         domainName,
				CustomDomainID: row.ID,
			})
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, result.Status)

			domain, err := ti.repo.GetCustomDomainByID(ctx, row.ID)
			require.NoError(t, err)
			require.Equal(t, string(tc.wantMethod), domain.DnsRoutingMethod.String)
		})
	}
}

func TestVerifyCustomDomain_RoutingErrorRecordsUnknown(t *testing.T) {
	t.Parallel()

	orgID := "org-verify-routing-error"
	domainName := "verify-routing-error.example.com"
	ctx, ti := newTestInstance(t, orgID, domainName)
	row := createDomainRow(t, ctx, ti, orgID, domainName)
	txt := func(context.Context, string) ([]string, error) {
		return []string{"gram-domain-verify=" + domainName + "," + orgID}, nil
	}

	activity := activities.NewVerifyCustomDomain(testenv.NewLogger(t), ti.conn, audit.NewLogger(), routingTestSharedTarget, []netip.Addr{routingTestIngressIP}, routingTestSuffix)
	passing := routingTestResolver(domainName, routingTestDNS{CNAME: routingTestSharedTarget + ".", Addrs: []netip.Addr{routingTestIngressIP}, DomainTargetAddrs: nil})
	activity.SetResolver(dns.NewMockResolver(dns.MockResolverConfig{LookupCNAMEFunc: passing.LookupCNAME, LookupNetIPFunc: passing.LookupNetIP, LookupTXTFunc: txt}))
	args := activities.VerifyCustomDomainArgs{OrgID: orgID, Domain: domainName, CustomDomainID: row.ID}
	_, err := activity.Do(ctx, args)
	require.NoError(t, err)

	// A routing error on a later pass replaces the earlier observation.
	failing := routingTestResolver(domainName, routingTestDNS{CNAME: "", Addrs: nil, DomainTargetAddrs: nil, DomainErr: errors.New("dns timeout")})
	activity.SetResolver(dns.NewMockResolver(dns.MockResolverConfig{LookupCNAMEFunc: failing.LookupCNAME, LookupNetIPFunc: failing.LookupNetIP, LookupTXTFunc: txt}))
	_, err = activity.Do(ctx, args)
	require.Error(t, err)

	domain, err := ti.repo.GetCustomDomainByID(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, string(customdomains.DNSRoutingMethodUnknown), domain.DnsRoutingMethod.String)
}
