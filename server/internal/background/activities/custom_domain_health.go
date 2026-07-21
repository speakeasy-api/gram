package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/k8s"
)

type CustomDomainInfrastructureChecker interface {
	CheckCustomDomainInfrastructure(ctx context.Context, check k8s.CustomDomainInfrastructureCheck) (k8s.CustomDomainInfrastructureHealth, error)
}

type CustomDomainHealth struct {
	db             *pgxpool.Pool
	logger         *slog.Logger
	infrastructure CustomDomainInfrastructureChecker
	resolver       dns.Resolver
	expectedTarget string
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

func NewCustomDomainHealth(logger *slog.Logger, db *pgxpool.Pool, infrastructure CustomDomainInfrastructureChecker, expectedTarget string) *CustomDomainHealth {
	return &CustomDomainHealth{
		db:             db,
		logger:         logger,
		infrastructure: infrastructure,
		resolver:       dns.NewNetResolver(),
		expectedTarget: expectedTarget,
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

	current := customDomainHealthState(domain)
	preserveCertificateExpiry := false

	observation := customdomains.HealthObservation{
		Status:               customdomains.HealthStatusHealthy,
		Issue:                "",
		CertificateExpiresAt: nil,
	}
	routingIssue, routingErr := checkCustomDomainRouting(ctx, c.resolver, domain.Domain, c.expectedTarget)
	switch {
	case routingErr != nil:
		c.logger.WarnContext(ctx, "custom domain routing health check failed", attr.SlogURLDomain(domain.Domain), attr.SlogError(routingErr))
		observation.Status = customdomains.HealthStatusUnhealthy
		observation.Issue = customdomains.HealthIssueCheckFailed
		observation.CertificateExpiresAt = current.CertificateExpiresAt
		preserveCertificateExpiry = true
	case routingIssue != "":
		observation.Status = customdomains.HealthStatusUnhealthy
		observation.Issue = routingIssue
		observation.CertificateExpiresAt = current.CertificateExpiresAt
		preserveCertificateExpiry = true
	default:
		infrastructureHealth, infrastructureErr := c.infrastructure.CheckCustomDomainInfrastructure(ctx, k8s.CustomDomainInfrastructureCheck{
			Domain:          domain.Domain,
			ResourceName:    domain.IngressName.String,
			CertSecretName:  domain.CertSecretName.String,
			ProvisionerKind: k8s.ProvisionerKind(domain.ProvisionerKind),
		})
		if infrastructureErr != nil {
			c.logger.WarnContext(ctx, "custom domain infrastructure health check failed", attr.SlogURLDomain(domain.Domain), attr.SlogError(infrastructureErr))
			observation.Status = customdomains.HealthStatusUnhealthy
			observation.Issue = customdomains.HealthIssueCheckFailed
			observation.CertificateExpiresAt = current.CertificateExpiresAt
			preserveCertificateExpiry = true
		} else {
			observation.CertificateExpiresAt = infrastructureHealth.CertificateExpiresAt
			if infrastructureHealth.Issue != "" {
				observation.Status = customdomains.HealthStatusUnhealthy
				observation.Issue = customdomains.HealthIssue(infrastructureHealth.Issue)
			}
		}
	}

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
	return nil
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
