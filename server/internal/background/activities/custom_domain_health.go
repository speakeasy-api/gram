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
	"github.com/speakeasy-api/gram/server/internal/k8s"
)

// CustomDomainHealthCheckMaxAttempts bounds temporal retries of the health
// check activity and is referenced by its workflow retry policy.
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

func NewCustomDomainHealth(logger *slog.Logger, db *pgxpool.Pool, infrastructure CustomDomainInfrastructureChecker, expectedTarget string, emails *email.Service, siteURL *url.URL) *CustomDomainHealth {
	return &CustomDomainHealth{
		db:             db,
		logger:         logger,
		infrastructure: infrastructure,
		resolver:       dns.NewNetResolver(),
		expectedTarget: expectedTarget,
		emails:         emails,
		siteURL:        siteURL,
	}
}

func (c *CustomDomainHealth) SetResolver(resolver dns.Resolver) {
	c.resolver = resolver
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

func (c *CustomDomainHealth) Check(ctx context.Context, args CheckCustomDomainHealthArgs) error {
	if c.expectedTarget == "" {
		c.logger.WarnContext(ctx, "skipping custom domain health check: expected target CNAME not configured")
		return nil
	}

	repository := customdomainsrepo.New(c.db)
	domain, err := repository.GetCustomDomainByIDAndOrganization(ctx, customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{
		ID:             args.CustomDomainID,
		OrganizationID: args.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get custom domain for health check: %w", err)
	}

	preserveCertificateExpiry := false

	observation := customdomains.HealthObservation{
		Status:               customdomains.HealthStatusHealthy,
		Issue:                "",
		CertificateExpiresAt: nil,
	}
	routingIssue, routingErr := checkCustomDomainRouting(ctx, c.resolver, domain.Domain, c.expectedTarget)
	switch {
	case routingErr != nil:
		if !isFinalHealthCheckAttempt(ctx) {
			return fmt.Errorf("check custom domain routing: %w", routingErr)
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
				return fmt.Errorf("check custom domain infrastructure: %w", infrastructureErr)
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

	notifyIssue := customdomains.HealthIssue("")
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
		next := customdomains.ReconcileHealthState(current, observation, args.CheckedAt)
		if customdomains.ShouldNotifyUnhealthyTransition(current, next) {
			notifyIssue = next.Issue
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
		return fmt.Errorf("save custom domain health: %w", err)
	}

	if notifyIssue != "" {
		c.notifyOrgAdminsBestEffort(ctx, args, domain.Domain, notifyIssue)
	}
	return nil
}

// notifyOrgAdminsBestEffort emails every organization admin about a fresh
// unhealthy transition. Failures are logged and never returned: the health
// row is the durable record, the email is advisory.
func (c *CustomDomainHealth) notifyOrgAdminsBestEffort(ctx context.Context, args CheckCustomDomainHealthArgs, domain string, issue customdomains.HealthIssue) {
	organizationID := args.OrganizationID
	repository := customdomainsrepo.New(c.db)
	logger := c.logger.With(attr.SlogOrganizationID(organizationID), attr.SlogURLDomain(domain))

	users, err := repository.ListOrganizationUsersForHealthNotification(ctx, organizationID)
	if err != nil {
		logger.ErrorContext(ctx, "notify custom domain unhealthy: list candidate users", attr.SlogError(err))
		return
	}

	domainLink := ""
	if c.siteURL != nil {
		domainLink = c.siteURL.String()
		slug, err := repository.GetOrganizationSlugForHealthNotification(ctx, organizationID)
		if err != nil {
			logger.WarnContext(ctx, "notify custom domain unhealthy: get organization slug", attr.SlogError(err))
		} else {
			domainLink = c.siteURL.JoinPath(slug, "domains").String()
		}
	}

	check := authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   organizationID,
		Dimensions:   nil,
	}
	seen := make(map[string]struct{}, len(users))
	for _, user := range users {
		principals, err := authz.ResolveUserPrincipals(ctx, c.db, organizationID, user.ID)
		if err != nil {
			logger.ErrorContext(ctx, "notify custom domain unhealthy: resolve principals", attr.SlogError(err))
			continue
		}
		grants, err := authz.LoadGrants(ctx, c.db, organizationID, principals)
		if err != nil {
			logger.ErrorContext(ctx, "notify custom domain unhealthy: load grants", attr.SlogError(err))
			continue
		}
		if !authz.GrantsSatisfy(grants, check) {
			continue
		}
		if _, ok := seen[user.Email]; ok {
			continue
		}
		seen[user.Email] = struct{}{}
		tmpl := email.CustomDomainUnhealthy{
			Email:        user.Email,
			Domain:       domain,
			IssueMessage: customdomains.HealthIssueMessage(issue),
			DomainLink:   domainLink,
		}
		// CheckedAt is pinned in the workflow's activity args, so retries of
		// the same check produce the same key and Loops drops the duplicate.
		// Hashed because Loops caps keys at 100 characters.
		digest := sha256.Sum256(fmt.Appendf(nil, "custom-domain-unhealthy:%s:%d:%s", args.CustomDomainID, args.CheckedAt.UnixMicro(), user.Email))
		if err := c.emails.SendIdempotent(ctx, user.Email, hex.EncodeToString(digest[:]), tmpl); err != nil {
			logger.ErrorContext(ctx, "notify custom domain unhealthy: send email", attr.SlogError(err), attr.SlogAuthUserEmail(user.Email))
		}
	}
}

// FindOrphanResources flags Kubernetes resources labeled as gram-managed that
// no longer map to a live custom domain row. It returns an error when orphans
// exist so the sweep workflow fails visibly; nothing is deleted automatically.
func (c *CustomDomainHealth) FindOrphanResources(ctx context.Context) error {
	resources, err := c.infrastructure.ListManagedCustomDomainResources(ctx)
	if err != nil {
		return fmt.Errorf("list managed custom domain resources: %w", err)
	}
	if len(resources) == 0 {
		return nil
	}

	activeDomains, err := customdomainsrepo.New(c.db).ListActiveCustomDomainNames(ctx)
	if err != nil {
		return fmt.Errorf("list active custom domains: %w", err)
	}
	active := make(map[string]struct{}, len(activeDomains))
	for _, domain := range activeDomains {
		active[domain] = struct{}{}
	}

	var orphans []string
	for _, resource := range resources {
		if _, ok := active[resource.Domain]; ok {
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

// isFinalHealthCheckAttempt: transient probe errors bubble up so temporal
// retries; check_failed is only persisted once retries are exhausted.
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
