package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/orghost"
)

// CustomDomainHealthCheckMaxAttempts is shared by Temporal and final-attempt detection.
const CustomDomainHealthCheckMaxAttempts = 3

type CustomDomainInfrastructureChecker interface {
	CheckCustomDomainInfrastructure(ctx context.Context, check k8s.CustomDomainInfrastructureCheck) (k8s.CustomDomainInfrastructureHealth, error)
	ListManagedCustomDomainResources(ctx context.Context) ([]k8s.ManagedCustomDomainResource, error)
	Provisioner(kind k8s.ProvisionerKind) k8s.CustomDomainProvisioner
}

type CustomDomainHealth struct {
	db               *pgxpool.Pool
	logger           *slog.Logger
	infrastructure   CustomDomainInfrastructureChecker
	resolver         dns.Resolver
	probe            func(ctx context.Context, domain string) error
	expectedTarget   string
	expectedARecords []netip.Addr
	targetSuffix     string
	emails           *email.Service
	orgHosts         *orghost.Resolver
}

type ListCustomDomainsForHealthCheckArgs struct {
	AfterID  uuid.UUID
	PageSize int32
}

type CustomDomainHealthCheckTarget struct {
	ID             uuid.UUID
	OrganizationID string
}

type CheckCustomDomainHealthArgs struct {
	CustomDomainID uuid.UUID
	OrganizationID string
	CheckedAt      time.Time
}

type NotifyCustomDomainUnhealthyArgs struct {
	CustomDomainID uuid.UUID
	OrganizationID string
	Domain         string
	Issue          customdomains.HealthIssue
	CheckedAt      time.Time

	// DNSTarget is the domain's per-domain CNAME target when the check ran;
	// empty when it had none. Remediation emails point customers at it
	// instead of the shared target.
	DNSTarget string

	// OtherDNSTarget reports that the domain's CNAME pointed at a per-domain
	// target that is not its own.
	OtherDNSTarget bool
}

// NewCustomDomainHealth returns the health check activities. targetSuffix is
// the normalized parent of per-domain CNAME targets; empty disables them.
func NewCustomDomainHealth(logger *slog.Logger, db *pgxpool.Pool, infrastructure CustomDomainInfrastructureChecker, expectedTarget string, expectedARecords []netip.Addr, targetSuffix string, emails *email.Service, orgHosts *orghost.Resolver, guardianPolicy *guardian.Policy) *CustomDomainHealth {
	probe := func(ctx context.Context, domain string) error {
		return errors.New("custom domain https probe is not configured")
	}
	if guardianPolicy != nil {
		probe = func(ctx context.Context, domain string) error {
			return probeCustomDomainHTTPS(ctx, guardianPolicy.Client(), domain)
		}
	}
	return &CustomDomainHealth{
		db:               db,
		logger:           logger,
		infrastructure:   infrastructure,
		resolver:         dns.NewNetResolver(),
		probe:            probe,
		expectedTarget:   expectedTarget,
		expectedARecords: expectedARecords,
		targetSuffix:     targetSuffix,
		emails:           emails,
		orgHosts:         orgHosts,
	}
}

func (c *CustomDomainHealth) SetResolver(resolver dns.Resolver) {
	c.resolver = resolver
}

// SetProbe replaces the HTTPS probe. Intended for testing.
func (c *CustomDomainHealth) SetProbe(probe func(ctx context.Context, domain string) error) {
	c.probe = probe
}

func (c *CustomDomainHealth) List(ctx context.Context, args ListCustomDomainsForHealthCheckArgs) ([]CustomDomainHealthCheckTarget, error) {
	domains, err := customdomainsrepo.New(c.db).ListActivatedCustomDomainsForHealthCheck(ctx, customdomainsrepo.ListActivatedCustomDomainsForHealthCheckParams{
		AfterID:   args.AfterID,
		PageLimit: args.PageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("list custom domains for health check: %w", err)
	}
	targets := make([]CustomDomainHealthCheckTarget, 0, len(domains))
	for _, domain := range domains {
		targets = append(targets, CustomDomainHealthCheckTarget{
			ID:             domain.ID,
			OrganizationID: domain.OrganizationID,
		})
	}
	return targets, nil
}

func (c *CustomDomainHealth) Check(ctx context.Context, args CheckCustomDomainHealthArgs) (NotifyCustomDomainUnhealthyArgs, error) {
	var noNotification NotifyCustomDomainUnhealthyArgs

	if c.expectedTarget == "" && len(c.expectedARecords) == 0 {
		c.logger.WarnContext(ctx, "skipping custom domain health check: no expected CNAME target or A records configured")
		return noNotification, nil
	}

	repository := customdomainsrepo.New(c.db)
	domain, err := repository.GetCustomDomainByIDAndOrganization(ctx, customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{
		ID:             args.CustomDomainID,
		OrganizationID: args.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return noNotification, nil
	}
	if err != nil {
		return noNotification, fmt.Errorf("get custom domain for health check: %w", err)
	}

	// Backfill rows created before DNS target tokens existed. A failure only
	// means this check accepts the shared target alone, so it must not fail
	// the health check.
	dnsTargetToken := domain.DnsTargetToken
	if !dnsTargetToken.Valid {
		token, err := customdomains.EnsureDNSTargetToken(ctx, c.db, domain.OrganizationID, domain.ID, customdomains.NewDNSTargetToken)
		if err != nil {
			c.logger.WarnContext(ctx, "failed to backfill custom domain dns target token", attr.SlogError(err), attr.SlogURLDomain(domain.Domain))
		}
		dnsTargetToken = conv.ToPGTextEmpty(token)
	}
	targets := newCustomDomainRoutingTargets(c.expectedTarget, c.expectedARecords, c.targetSuffix, dnsTargetToken)

	route, err := repository.GetCustomDomainRouteConfig(ctx, domain.ID)
	if err != nil {
		return noNotification, fmt.Errorf("get custom domain route for health check: %w", err)
	}
	rootResourceName := ""
	wellKnownRootResourceName := ""
	if route.RootMcpEndpointID != uuid.Nil {
		rootResourceName, err = k8s.RootIngressNameForDomain(domain.Domain)
		if err != nil {
			return noNotification, fmt.Errorf("derive custom domain root resource name: %w", err)
		}
		wellKnownRootResourceName, err = k8s.WellKnownRootIngressNameForDomain(domain.Domain)
		if err != nil {
			return noNotification, fmt.Errorf("derive custom domain well-known root resource name: %w", err)
		}
	}

	preserveCertificateExpiry := false

	observation := customdomains.HealthObservation{
		Status:               customdomains.HealthStatusHealthy,
		Issue:                "",
		CertificateExpiresAt: nil,
	}
	routing, routingErr := checkCustomDomainRouting(ctx, c.resolver, domain.Domain, targets)
	if routingErr == nil && routing.Issue == customdomains.HealthIssueDNSTargetMismatch && !routing.OtherDNSTarget {
		// DNS shape says the domain points elsewhere, but proxied/CDN setups
		// legitimately do that. If the domain still answers HTTPS, traffic is
		// routing and the domain is healthy. A CNAME to another domain's
		// per-domain target also answers HTTPS today, but it would follow that
		// domain when it moves, so the probe never clears it.
		if probeErr := c.probe(ctx, domain.Domain); probeErr == nil {
			routing.Issue = ""
		} else {
			c.logger.InfoContext(ctx, "custom domain https probe failed after dns mismatch", attr.SlogURLDomain(domain.Domain), attr.SlogError(probeErr))
		}
	}
	switch {
	case routingErr != nil:
		if !isFinalHealthCheckAttempt(ctx) {
			return noNotification, fmt.Errorf("check custom domain routing: %w", routingErr)
		}
		c.logger.WarnContext(ctx, "custom domain routing health check failed", attr.SlogURLDomain(domain.Domain), attr.SlogError(routingErr))
		observation.Status = customdomains.HealthStatusUnhealthy
		observation.Issue = customdomains.HealthIssueCheckFailed
		preserveCertificateExpiry = true
		routing = customDomainRouting{Issue: "", Method: customdomains.DNSRoutingMethodUnknown, OtherDNSTarget: false}
	case routing.Issue != "":
		observation.Status = customdomains.HealthStatusUnhealthy
		observation.Issue = routing.Issue
		preserveCertificateExpiry = true
	default:
		infrastructureHealth, infrastructureErr := c.infrastructure.CheckCustomDomainInfrastructure(ctx, k8s.CustomDomainInfrastructureCheck{
			Domain:                    domain.Domain,
			ResourceName:              domain.IngressName.String,
			RootResourceName:          rootResourceName,
			WellKnownRootResourceName: wellKnownRootResourceName,
			CertSecretName:            domain.CertSecretName.String,
			ProvisionerKind:           k8s.ProvisionerKind(domain.ProvisionerKind),
		})
		if infrastructureErr != nil {
			if !isFinalHealthCheckAttempt(ctx) {
				return noNotification, fmt.Errorf("check custom domain infrastructure: %w", infrastructureErr)
			}
			c.logger.WarnContext(ctx, "custom domain infrastructure health check failed", attr.SlogURLDomain(domain.Domain), attr.SlogError(infrastructureErr))
			observation.Status = customdomains.HealthStatusUnhealthy
			observation.Issue = customdomains.HealthIssueCheckFailed
			preserveCertificateExpiry = true
		} else {
			observation.CertificateExpiresAt = infrastructureHealth.CertificateExpiresAt
			if infrastructureHealth.Issue != "" {
				observation.Status = customdomains.HealthStatusUnhealthy
				observation.Issue = customdomains.HealthIssue(infrastructureHealth.Issue)
			}
		}
	}

	var notification NotifyCustomDomainUnhealthyArgs
	var next customdomains.HealthState
	autoDisabled := false
	if err := pgx.BeginFunc(ctx, c.db, func(tx pgx.Tx) error {
		repository := customdomainsrepo.New(tx)
		lockedDomain, err := repository.LockCustomDomainByIDAndOrganization(ctx, customdomainsrepo.LockCustomDomainByIDAndOrganizationParams{
			ID:             domain.ID,
			OrganizationID: args.OrganizationID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock custom domain for health update: %w", err)
		}
		current := customDomainHealthState(lockedDomain)
		if preserveCertificateExpiry {
			observation.CertificateExpiresAt = current.CertificateExpiresAt
		}
		next = customdomains.ReconcileHealthState(current, observation, args.CheckedAt)
		// A stale or retried check leaves the health state untouched, and must
		// not overwrite the routing method a newer check recorded either.
		routingMethod := lockedDomain.DnsRoutingMethod
		if customdomains.IsNewerHealthCheck(current, args.CheckedAt) {
			routingMethod = conv.ToPGText(string(routing.Method))
		}
		dnsTarget, _ := customdomains.DNSTarget(c.targetSuffix, dnsTargetToken)
		switch {
		case customdomains.ShouldNotifyUnhealthyTransition(current, next):
			notification = NotifyCustomDomainUnhealthyArgs{
				CustomDomainID: domain.ID,
				OrganizationID: args.OrganizationID,
				Domain:         domain.Domain,
				Issue:          next.Issue,
				CheckedAt:      args.CheckedAt,
				DNSTarget:      dnsTarget,
				OtherDNSTarget: routing.OtherDNSTarget,
			}
		case customdomains.IsRetryOfUnhealthyTransition(current, args.CheckedAt):
			// A previous attempt committed the transition but died before
			// reporting it; re-emit the same args so the retry returns the same
			// answer. The notify workflow ID and the email idempotency key both
			// derive from CheckedAt, so nothing is delivered twice. DNSTarget
			// and OtherDNSTarget come from this retry's own observation, which
			// only affects the email wording.
			notification = NotifyCustomDomainUnhealthyArgs{
				CustomDomainID: domain.ID,
				OrganizationID: args.OrganizationID,
				Domain:         domain.Domain,
				Issue:          current.Issue,
				CheckedAt:      args.CheckedAt,
				DNSTarget:      dnsTarget,
				OtherDNSTarget: routing.OtherDNSTarget,
			}
		}
		_, err = repository.UpdateCustomDomainHealth(ctx, customdomainsrepo.UpdateCustomDomainHealthParams{
			HealthStatus:         conv.ToPGText(string(next.Status)),
			HealthIssue:          conv.ToPGTextEmpty(string(next.Issue)),
			CheckedAt:            conv.ToPGTimestamptz(*next.CheckedAt),
			UnhealthySince:       conv.PtrToPGTimestamptz(next.UnhealthySince),
			CertificateExpiresAt: conv.PtrToPGTimestamptz(next.CertificateExpiresAt),
			ConsecutiveFailures:  pgtype.Int4{Int32: next.ConsecutiveFailures, Valid: true},
			DnsRoutingMethod:     routingMethod,
			ID:                   domain.ID,
			OrganizationID:       args.OrganizationID,
		})
		if err != nil {
			return fmt.Errorf("update custom domain health: %w", err)
		}
		if customdomains.ShouldAutoDisable(next, args.CheckedAt) {
			// Disable under the same row lock that persisted the decision so
			// a recovery cannot commit in between; k8s teardown waits for commit.
			if _, err := repository.DisableCustomDomainForHealth(ctx, customdomainsrepo.DisableCustomDomainForHealthParams{
				ID:             domain.ID,
				OrganizationID: args.OrganizationID,
			}); err != nil {
				return fmt.Errorf("disable custom domain after failed health checks: %w", err)
			}
			autoDisabled = true
		}
		return nil
	}); err != nil {
		return noNotification, fmt.Errorf("save custom domain health: %w", err)
	}

	// One line per check keeps the Datadog health breakdown that the dry-run
	// phase established. CheckedAt is nil only when the locked row vanished.
	if next.CheckedAt != nil {
		c.logger.InfoContext(ctx, "observed custom domain health",
			attr.SlogURLDomain(domain.Domain),
			attr.SlogOrganizationID(args.OrganizationID),
			attr.SlogCustomDomainHealthStatus(string(next.Status)),
			attr.SlogCustomDomainHealthIssue(string(next.Issue)),
			attr.SlogCustomDomainDNSRoutingMethod(string(routing.Method)),
		)
	}

	if autoDisabled {
		// Retries reuse CheckedAt, so the reconcile no-ops and this idempotent
		// teardown re-runs until it succeeds.
		if domain.IngressName.String != "" {
			provisioner := c.infrastructure.Provisioner(k8s.ProvisionerKind(domain.ProvisionerKind))
			if err := provisioner.Delete(ctx, domain.IngressName.String, domain.CertSecretName.String); err != nil {
				return noNotification, fmt.Errorf("tear down auto-disabled custom domain resources: %w", err)
			}
		}
		c.logger.WarnContext(ctx, "auto-disabled custom domain after prolonged failed health checks",
			attr.SlogURLDomain(domain.Domain),
			attr.SlogOrganizationID(args.OrganizationID),
		)
	}

	return notification, nil
}

// NotifyOrgAdmins returns delivery failures for Temporal retry; recipient keys make retries idempotent.
func (c *CustomDomainHealth) NotifyOrgAdmins(ctx context.Context, args NotifyCustomDomainUnhealthyArgs) error {
	organizationID := args.OrganizationID
	repository := customdomainsrepo.New(c.db)

	organization, err := repository.GetOrganizationForHealthNotification(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("get organization for custom domain health notification: %w", err)
	}
	domainLink := ""
	if siteURL := c.orgHosts.SiteURL(organization.DefaultHost); siteURL != nil {
		domainLink = siteURL.JoinPath(organization.Slug, "domains").String()
	}

	recipients, resolutionErr := authz.ResolveOrganizationAdminEmails(ctx, c.db, organizationID)
	notificationErrors := []error{resolutionErr}
	for _, recipient := range recipients {
		tmpl := email.CustomDomainUnhealthy{
			Email:  recipient,
			Domain: args.Domain,
			IssueMessage: customdomains.HealthIssueMessage(args.Issue, customdomains.DNSRemediation{
				Domain:           args.Domain,
				ExpectedCNAME:    conv.Default(args.DNSTarget, c.expectedTarget),
				ExpectedARecords: customdomains.FormatARecords(c.expectedARecords),
				OtherDNSTarget:   args.OtherDNSTarget,
			}),
			DomainLink: domainLink,
		}
		idempotencyKey := recipientEmailIdempotencyKey(recipient, "custom-domain-unhealthy", args.CustomDomainID.String(), strconv.FormatInt(args.CheckedAt.UnixMicro(), 10))
		if err := c.emails.SendIdempotent(ctx, recipient, idempotencyKey, tmpl); err != nil {
			notificationErrors = append(notificationErrors, fmt.Errorf("send custom domain health notification: %w", err))
		}
	}

	if err := errors.Join(notificationErrors...); err != nil {
		return err
	}
	// Aggregate line only after every recipient resolved cleanly, so a retried
	// activity cannot log a misleading partial count. Addresses stay out of the
	// logs. A zero count is the dead-org signal: nobody holds org:admin, so the
	// alert reached no one.
	c.logger.InfoContext(ctx, "emailed org admins about unhealthy custom domain",
		attr.SlogURLDomain(args.Domain),
		attr.SlogOrganizationID(organizationID),
		attr.SlogCustomDomainHealthIssue(string(args.Issue)),
		attr.SlogCustomDomainNotifyRecipientCount(len(recipients)),
	)
	return nil
}

// FindOrphanResources reports but never deletes unmatched managed resources.
func (c *CustomDomainHealth) FindOrphanResources(ctx context.Context) error {
	resources, err := c.infrastructure.ListManagedCustomDomainResources(ctx)
	if err != nil {
		return fmt.Errorf("list managed custom domain resources: %w", err)
	}
	if len(resources) == 0 {
		return nil
	}

	repository := customdomainsrepo.New(c.db)
	activeResources, err := repository.ListActivatedCustomDomainResources(ctx)
	if err != nil {
		return fmt.Errorf("list activated custom domain resources: %w", err)
	}
	active := make(map[k8s.ManagedCustomDomainResource]struct{}, len(activeResources)*3)
	for _, resource := range activeResources {
		active[k8s.ManagedCustomDomainResource{
			Kind:   k8s.ProvisionerKind(resource.ProvisionerKind),
			Name:   resource.ResourceName,
			Domain: resource.Domain,
		}] = struct{}{}

		if !resource.HasRootMapping {
			continue
		}
		rootName, err := k8s.RootIngressNameForDomain(resource.Domain)
		if err != nil {
			return fmt.Errorf("derive custom domain root resource name for orphan reconciliation: %w", err)
		}
		active[k8s.ManagedCustomDomainResource{
			Kind:   k8s.ProvisionerKindIngress,
			Name:   rootName,
			Domain: resource.Domain,
		}] = struct{}{}
		wellKnownRootName, err := k8s.WellKnownRootIngressNameForDomain(resource.Domain)
		if err != nil {
			return fmt.Errorf("derive custom domain well-known root resource name for orphan reconciliation: %w", err)
		}
		active[k8s.ManagedCustomDomainResource{
			Kind:   k8s.ProvisionerKindIngress,
			Name:   wellKnownRootName,
			Domain: resource.Domain,
		}] = struct{}{}
	}

	var orphans []string
	for _, resource := range resources {
		if _, ok := active[resource]; ok {
			continue
		}
		c.logger.ErrorContext(ctx, "orphaned custom domain resource: labeled as gram-managed but no live custom domain row",
			attr.SlogURLDomain(resource.Domain),
			attr.SlogResourceName(fmt.Sprintf("%s/%s", resource.Kind, resource.Name)),
		)
		orphans = append(orphans, fmt.Sprintf("%s/%s (domain %q)", resource.Kind, resource.Name, resource.Domain))
	}
	if len(orphans) > 0 {
		return fmt.Errorf("found %d orphaned custom domain resources: %s", len(orphans), strings.Join(orphans, ", "))
	}
	return nil
}

// Probe errors retry until the final attempt, which persists check_failed.
func isFinalHealthCheckAttempt(ctx context.Context) bool {
	if !activity.IsActivity(ctx) {
		return true
	}
	return activity.GetInfo(ctx).Attempt >= CustomDomainHealthCheckMaxAttempts
}

func customDomainHealthState(domain customdomainsrepo.CustomDomain) customdomains.HealthState {
	state := customdomains.HealthState{
		Status:               conv.FromPGTextOrEmpty[customdomains.HealthStatus](domain.HealthStatus),
		Issue:                conv.FromPGTextOrEmpty[customdomains.HealthIssue](domain.HealthIssue),
		CheckedAt:            nil,
		UnhealthySince:       nil,
		CertificateExpiresAt: nil,
		ConsecutiveFailures:  domain.ConsecutiveFailures.Int32,
	}
	if state.Status == "" {
		state.Status = customdomains.HealthStatusUnknown
	}
	if domain.HealthCheckedAt.Valid {
		checkedAt := domain.HealthCheckedAt.Time.UTC()
		state.CheckedAt = &checkedAt
	}
	if domain.UnhealthySince.Valid {
		unhealthySince := domain.UnhealthySince.Time.UTC()
		state.UnhealthySince = &unhealthySince
	}
	if domain.CertificateExpiresAt.Valid {
		expiresAt := domain.CertificateExpiresAt.Time.UTC()
		state.CertificateExpiresAt = &expiresAt
	}
	return state
}
