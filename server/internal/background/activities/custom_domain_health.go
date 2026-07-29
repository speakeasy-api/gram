package activities

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
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
)

// CustomDomainHealthCheckMaxAttempts is shared by Temporal and final-attempt detection.
const CustomDomainHealthCheckMaxAttempts = 3

type CustomDomainInfrastructureChecker interface {
	CheckCustomDomainInfrastructure(ctx context.Context, check k8s.CustomDomainInfrastructureCheck) (k8s.CustomDomainInfrastructureHealth, error)
	ListManagedCustomDomainResources(ctx context.Context) ([]k8s.ManagedCustomDomainResource, error)
}

type CustomDomainHealth struct {
	db             *pgxpool.Pool
	logger         *slog.Logger
	infrastructure CustomDomainInfrastructureChecker
	resolver       dns.Resolver
	probe          func(ctx context.Context, domain string) error
	expectedTarget string
	emails         *email.Service
	siteURL        *url.URL
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
}

type AutoDisableCustomDomainArgs struct {
	CustomDomainID  uuid.UUID
	OrganizationID  string
	Domain          string
	Issue           customdomains.HealthIssue
	CheckedAt       time.Time
	IngressName     string
	CertSecretName  string
	ProvisionerKind k8s.ProvisionerKind
}

type CustomDomainHealthCheckResult struct {
	UnhealthyNotification NotifyCustomDomainUnhealthyArgs
	AutoDisable           AutoDisableCustomDomainArgs
}

type customDomainAdminNotification struct {
	CustomDomainID uuid.UUID
	OrganizationID string
	Domain         string
	Issue          customdomains.HealthIssue
	IssueMessage   string
	CheckedAt      time.Time
	IdempotencyKey string
	Disabled       bool
}

func NewCustomDomainHealth(logger *slog.Logger, db *pgxpool.Pool, infrastructure CustomDomainInfrastructureChecker, expectedTarget string, emails *email.Service, siteURL *url.URL, guardianPolicy *guardian.Policy) *CustomDomainHealth {
	probe := func(ctx context.Context, domain string) error {
		return errors.New("custom domain https probe is not configured")
	}
	if guardianPolicy != nil {
		probe = func(ctx context.Context, domain string) error {
			return probeCustomDomainHTTPS(ctx, guardianPolicy.Client(), domain)
		}
	}
	return &CustomDomainHealth{
		db:             db,
		logger:         logger,
		infrastructure: infrastructure,
		resolver:       dns.NewNetResolver(),
		probe:          probe,
		expectedTarget: expectedTarget,
		emails:         emails,
		siteURL:        siteURL,
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

func (c *CustomDomainHealth) Check(ctx context.Context, args CheckCustomDomainHealthArgs) (CustomDomainHealthCheckResult, error) {
	var result CustomDomainHealthCheckResult

	if c.expectedTarget == "" {
		c.logger.WarnContext(ctx, "skipping custom domain health check: expected target CNAME not configured")
		return result, nil
	}

	repository := customdomainsrepo.New(c.db)
	domain, err := repository.GetCustomDomainByIDAndOrganization(ctx, customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{
		ID:             args.CustomDomainID,
		OrganizationID: args.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("get custom domain for health check: %w", err)
	}

	preserveCertificateExpiry := false

	observation := customdomains.HealthObservation{
		Status:               customdomains.HealthStatusHealthy,
		Issue:                "",
		CertificateExpiresAt: nil,
	}
	routingIssue, routingErr := checkCustomDomainRouting(ctx, c.resolver, domain.Domain, c.expectedTarget)
	if routingErr == nil && routingIssue == customdomains.HealthIssueDNSTargetMismatch {
		// DNS shape says the domain points elsewhere, but proxied/CDN setups
		// legitimately do that. If the domain still answers HTTPS, traffic is
		// routing and the domain is healthy.
		if probeErr := c.probe(ctx, domain.Domain); probeErr == nil {
			routingIssue = ""
		} else {
			c.logger.InfoContext(ctx, "custom domain https probe failed after dns mismatch", attr.SlogURLDomain(domain.Domain), attr.SlogError(probeErr))
		}
	}
	switch {
	case routingErr != nil:
		if !isFinalHealthCheckAttempt(ctx) {
			return result, fmt.Errorf("check custom domain routing: %w", routingErr)
		}
		c.logger.WarnContext(ctx, "custom domain routing health check failed", attr.SlogURLDomain(domain.Domain), attr.SlogError(routingErr))
		observation.Status = customdomains.HealthStatusUnhealthy
		observation.Issue = customdomains.HealthIssueCheckFailed
		preserveCertificateExpiry = true
	case routingIssue != "":
		observation.Status = customdomains.HealthStatusUnhealthy
		observation.Issue = routingIssue
		preserveCertificateExpiry = true
	default:
		infrastructureHealth, infrastructureErr := c.infrastructure.CheckCustomDomainInfrastructure(ctx, k8s.CustomDomainInfrastructureCheck{
			Domain:          domain.Domain,
			ResourceName:    domain.IngressName.String,
			CertSecretName:  domain.CertSecretName.String,
			ProvisionerKind: k8s.ProvisionerKind(domain.ProvisionerKind),
		})
		if infrastructureErr != nil {
			if !isFinalHealthCheckAttempt(ctx) {
				return result, fmt.Errorf("check custom domain infrastructure: %w", infrastructureErr)
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

	var next customdomains.HealthState
	if err := pgx.BeginFunc(ctx, c.db, func(tx pgx.Tx) error {
		repository := customdomainsrepo.New(tx)
		lockedDomain, err := repository.GetCustomDomainByIDAndOrganizationForHealthUpdate(ctx, customdomainsrepo.GetCustomDomainByIDAndOrganizationForHealthUpdateParams{
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
		switch {
		case customdomains.ShouldNotifyUnhealthyTransition(current, next):
			result.UnhealthyNotification = NotifyCustomDomainUnhealthyArgs{
				CustomDomainID: domain.ID,
				OrganizationID: args.OrganizationID,
				Domain:         domain.Domain,
				Issue:          next.Issue,
				CheckedAt:      args.CheckedAt,
			}
		case customdomains.IsRetryOfUnhealthyTransition(current, args.CheckedAt):
			// A previous attempt committed the transition but died before
			// reporting it; re-emit the same args so the retry returns the same
			// answer. The notify workflow ID and the email idempotency key both
			// derive from CheckedAt, so nothing is delivered twice.
			result.UnhealthyNotification = NotifyCustomDomainUnhealthyArgs{
				CustomDomainID: domain.ID,
				OrganizationID: args.OrganizationID,
				Domain:         domain.Domain,
				Issue:          current.Issue,
				CheckedAt:      args.CheckedAt,
			}
		}
		if lockedDomain.Activated && customdomains.ShouldAutoDisable(next) {
			result.AutoDisable = AutoDisableCustomDomainArgs{
				CustomDomainID:  domain.ID,
				OrganizationID:  args.OrganizationID,
				Domain:          domain.Domain,
				Issue:           next.Issue,
				CheckedAt:       args.CheckedAt,
				IngressName:     lockedDomain.IngressName.String,
				CertSecretName:  lockedDomain.CertSecretName.String,
				ProvisionerKind: k8s.ProvisionerKind(lockedDomain.ProvisionerKind),
			}
		}
		_, err = repository.UpdateCustomDomainHealth(ctx, customdomainsrepo.UpdateCustomDomainHealthParams{
			HealthStatus:         conv.ToPGText(string(next.Status)),
			HealthIssue:          conv.ToPGTextEmpty(string(next.Issue)),
			CheckedAt:            conv.ToPGTimestamptz(*next.CheckedAt),
			UnhealthySince:       conv.PtrToPGTimestamptz(next.UnhealthySince),
			CertificateExpiresAt: conv.PtrToPGTimestamptz(next.CertificateExpiresAt),
			ConsecutiveFailures:  pgtype.Int4{Int32: next.ConsecutiveFailures, Valid: true},
			ID:                   domain.ID,
			OrganizationID:       args.OrganizationID,
		})
		if err != nil {
			return fmt.Errorf("update custom domain health: %w", err)
		}
		return nil
	}); err != nil {
		return result, fmt.Errorf("save custom domain health: %w", err)
	}

	// One line per check keeps the production health breakdown established
	// during the observation-only rollout.
	if next.CheckedAt != nil {
		c.logger.InfoContext(ctx, "observed custom domain health",
			attr.SlogURLDomain(domain.Domain),
			attr.SlogOrganizationID(args.OrganizationID),
			attr.SlogCustomDomainHealthStatus(string(next.Status)),
			attr.SlogCustomDomainHealthIssue(string(next.Issue)),
		)
	}

	return result, nil
}

// Deactivate marks a domain inactive after its Kubernetes resources have been
// removed. It is idempotent so Temporal can safely retry after a committed
// update whose completion was not recorded.
func (c *CustomDomainHealth) Deactivate(ctx context.Context, args AutoDisableCustomDomainArgs) (bool, error) {
	repository := customdomainsrepo.New(c.db)
	domain, err := repository.GetCustomDomainByIDAndOrganization(ctx, customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{
		ID:             args.CustomDomainID,
		OrganizationID: args.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get custom domain for automatic deactivation: %w", err)
	}
	state := customDomainHealthState(domain)
	if !customdomains.ShouldAutoDisable(state) ||
		state.CheckedAt == nil ||
		!state.CheckedAt.Equal(args.CheckedAt) {
		return false, nil
	}
	if !domain.Activated {
		return true, nil
	}

	if _, err := repository.DeactivateCustomDomainAfterHealthFailures(ctx, customdomainsrepo.DeactivateCustomDomainAfterHealthFailuresParams{
		ID:             args.CustomDomainID,
		OrganizationID: args.OrganizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("deactivate unhealthy custom domain: %w", err)
	}

	c.logger.InfoContext(ctx, "disabled custom domain after prolonged health failure",
		attr.SlogURLDomain(args.Domain),
		attr.SlogOrganizationID(args.OrganizationID),
		attr.SlogCustomDomainHealthIssue(string(args.Issue)),
	)
	return true, nil
}

// NotifyOrgAdmins returns delivery failures for Temporal retry; recipient keys make retries idempotent.
func (c *CustomDomainHealth) NotifyOrgAdmins(ctx context.Context, args NotifyCustomDomainUnhealthyArgs) error {
	return c.notifyOrgAdmins(ctx, customDomainAdminNotification{
		CustomDomainID: args.CustomDomainID,
		OrganizationID: args.OrganizationID,
		Domain:         args.Domain,
		Issue:          args.Issue,
		IssueMessage:   customdomains.HealthIssueMessage(args.Issue),
		CheckedAt:      args.CheckedAt,
		IdempotencyKey: "custom-domain-unhealthy",
		Disabled:       false,
	})
}

// NotifyDisabledOrgAdmins sends the final deactivation notice. Delivery
// failures are returned for Temporal retry; recipient keys make retries
// idempotent.
func (c *CustomDomainHealth) NotifyDisabledOrgAdmins(ctx context.Context, args AutoDisableCustomDomainArgs) error {
	return c.notifyOrgAdmins(ctx, customDomainAdminNotification{
		CustomDomainID: args.CustomDomainID,
		OrganizationID: args.OrganizationID,
		Domain:         args.Domain,
		Issue:          args.Issue,
		IssueMessage: fmt.Sprintf(
			"This custom domain stayed unhealthy for %d consecutive checks and was disabled. Fix its DNS or certificate configuration, then reverify it to reactivate.",
			customdomains.CustomDomainAutoDisableFailureThreshold,
		),
		CheckedAt:      args.CheckedAt,
		IdempotencyKey: "custom-domain-disabled",
		Disabled:       true,
	})
}

func (c *CustomDomainHealth) notifyOrgAdmins(ctx context.Context, notification customDomainAdminNotification) error {
	organizationID := notification.OrganizationID
	repository := customdomainsrepo.New(c.db)

	users, err := repository.ListOrganizationUsersForHealthNotification(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("list custom domain health notification recipients: %w", err)
	}

	domainLink := ""
	if c.siteURL != nil {
		slug, err := repository.GetOrganizationSlugForHealthNotification(ctx, organizationID)
		if err != nil {
			return fmt.Errorf("get organization slug for custom domain health notification: %w", err)
		}
		domainLink = c.siteURL.JoinPath(slug, "domains").String()
	}

	check := authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   organizationID,
		Dimensions:   nil,
	}
	seen := make(map[string]struct{}, len(users))
	var notificationErrors []error
	for _, user := range users {
		principals, err := authz.ResolveUserPrincipals(ctx, c.db, organizationID, user.ID)
		if err != nil {
			notificationErrors = append(notificationErrors, fmt.Errorf("resolve custom domain health notification recipient: %w", err))
			continue
		}
		grants, err := authz.LoadGrants(ctx, c.db, organizationID, principals)
		if err != nil {
			notificationErrors = append(notificationErrors, fmt.Errorf("load custom domain health notification recipient grants: %w", err))
			continue
		}
		if !authz.GrantsSatisfy(grants, check) {
			continue
		}
		// Dedupe case-insensitively: user rows can carry the same mailbox with
		// different casing, and the idempotency digest must collapse them too.
		emailKey := strings.ToLower(user.Email)
		if _, ok := seen[emailKey]; ok {
			continue
		}
		seen[emailKey] = struct{}{}
		tmpl := email.CustomDomainUnhealthy{
			Email:        user.Email,
			Domain:       notification.Domain,
			IssueMessage: notification.IssueMessage,
			DomainLink:   domainLink,
		}
		// CheckedAt is stable across retries; hashing satisfies Loops's 100-character key limit.
		digest := sha256.Sum256(fmt.Appendf(nil, "%s:%s:%d:%s", notification.IdempotencyKey, notification.CustomDomainID, notification.CheckedAt.UnixMicro(), emailKey))
		if err := c.emails.SendIdempotent(ctx, user.Email, hex.EncodeToString(digest[:]), tmpl); err != nil {
			notificationErrors = append(notificationErrors, fmt.Errorf("send custom domain health notification: %w", err))
		}
	}

	if err := errors.Join(notificationErrors...); err != nil {
		return err
	}
	// Aggregate only after every recipient resolved cleanly. A zero count is
	// intentionally logged: it identifies organizations with no reachable
	// administrators without exposing addresses.
	message := "emailed org admins about unhealthy custom domain"
	if notification.Disabled {
		message = "emailed org admins about disabled custom domain"
	}
	c.logger.InfoContext(ctx, message,
		attr.SlogURLDomain(notification.Domain),
		attr.SlogOrganizationID(organizationID),
		attr.SlogCustomDomainHealthIssue(string(notification.Issue)),
		attr.SlogCustomDomainNotifyRecipientCount(len(seen)),
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

	activeResources, err := customdomainsrepo.New(c.db).ListActivatedCustomDomainResources(ctx)
	if err != nil {
		return fmt.Errorf("list activated custom domain resources: %w", err)
	}
	active := make(map[k8s.ManagedCustomDomainResource]struct{}, len(activeResources))
	for _, resource := range activeResources {
		active[k8s.ManagedCustomDomainResource{
			Kind:   k8s.ProvisionerKind(resource.ProvisionerKind),
			Name:   resource.ResourceName,
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
