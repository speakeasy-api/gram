package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type VerifyCustomDomain struct {
	db                  *pgxpool.Pool
	logger              *slog.Logger
	expectedTargetCNAME string
	audit               *audit.Logger
	resolver            dns.Resolver
}

func NewVerifyCustomDomain(logger *slog.Logger, db *pgxpool.Pool, auditLogger *audit.Logger, expectedTargetCNAME string) *VerifyCustomDomain {
	return &VerifyCustomDomain{
		db:                  db,
		logger:              logger,
		expectedTargetCNAME: expectedTargetCNAME,
		resolver:            dns.NewNetResolver(),
		audit:               auditLogger,
	}
}

// SetResolver replaces the DNS resolver. Intended for testing.
func (d *VerifyCustomDomain) SetResolver(r dns.Resolver) {
	d.resolver = r
}

type VerifyCustomDomainArgs struct {
	OrgID         string
	Domain        string
	CreatedBy     urn.Principal
	CreatedByName *string
}

var prohibitedDomainRoots = []string{"getgram.ai", "speakeasy.com", "speakeasyapi.dev"}
var specialTestDomains = []string{"chat.speakeasy.com", "chat.dev.speakeasy.com"}
var domainRegex = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z]{2,})+$`)

func (d *VerifyCustomDomain) Do(ctx context.Context, args VerifyCustomDomainArgs) error {
	if !domainRegex.MatchString(args.Domain) {
		return oops.E(oops.CodeBadRequest, errors.New("domain is invalid"), "domain is invalid %s", args.Domain).Log(ctx, d.logger)
	}

	for _, root := range prohibitedDomainRoots {
		if strings.Contains(args.Domain, root) && !slices.Contains(specialTestDomains, args.Domain) { // Temporarily allowed test domain
			return oops.E(oops.CodeBadRequest, errors.New("domain is prohibited"), "domain %s is prohibited", args.Domain).Log(ctx, d.logger)
		}
	}

	dbtx, err := d.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to access custom domains").Log(ctx, d.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	cdr := customdomainsRepo.New(dbtx)

	domain, err := cdr.GetCustomDomainByDomain(ctx, args.Domain)
	switch {
	case err == nil:
		// Domain already exists, continue
	case errors.Is(err, pgx.ErrNoRows):
		// Create a new unverified domain entry
		domain, err = cdr.CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
			OrganizationID: args.OrgID,
			Domain:         args.Domain,
			IngressName:    conv.PtrToPGText(nil),
			CertSecretName: conv.PtrToPGText(nil),
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "error creating custom domain").Log(ctx, d.logger)
		}

		if err := d.audit.LogCustomDomainCreate(ctx, dbtx, audit.LogCustomDomainCreateEvent{
			OrganizationID:   args.OrgID,
			Actor:            args.CreatedBy,
			ActorDisplayName: args.CreatedByName,
			ActorSlug:        nil,
			CustomDomainURN:  urn.NewCustomDomain(domain.ID),
			DomainName:       domain.Domain,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to create custom domain creation audit log").Log(ctx, d.logger)
		}
	default:
		return oops.E(oops.CodeUnexpected, err, "failed to get custom domain").Log(ctx, d.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to save custom domain creation").Log(ctx, d.logger)
	}

	if domain.OrganizationID != args.OrgID {
		return oops.E(oops.CodeUnauthorized, errors.New("custom domain does not belong to organization"), "custom domain does not belong to organization").Log(ctx, d.logger)
	}

	cname, err := d.resolver.LookupCNAME(ctx, domain.Domain)
	if err != nil {
		d.logger.InfoContext(ctx, "CNAME lookup failed for domain", attr.SlogURLDomain(domain.Domain), attr.SlogError(err))
		// Provide more info if an A record exists
		ips, aErr := d.resolver.LookupHost(ctx, domain.Domain)
		if aErr == nil && len(ips) > 0 {
			d.logger.InfoContext(ctx, fmt.Sprintf("CNAME not found. Found A record(s): %s", strings.Join(ips, ", ")))
		} else {
			return oops.E(oops.CodeUnexpected, err, "failed to find custom domain mapping for %s", domain.Domain).Log(ctx, d.logger)
		}
	} else {
		actualCNAMEFQDN := strings.TrimSuffix(cname, ".") + "."

		if actualCNAMEFQDN != d.expectedTargetCNAME {
			return oops.E(oops.CodeUnexpected, errors.New("custom domain is not pointing to expected target"), "custom domain %s is not pointing to %s", domain.Domain, d.expectedTargetCNAME).Log(ctx, d.logger)
		}
	}

	txtName := "_gram." + domain.Domain
	txts, err := d.resolver.LookupTXT(ctx, txtName)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to find TXT record for %s", txtName).Log(ctx, d.logger)
	}
	expectedTXT := fmt.Sprintf("gram-domain-verify=%s,%s", domain.Domain, args.OrgID)
	found := slices.Contains(txts, expectedTXT)
	if !found {
		return oops.E(oops.CodeUnexpected, errors.New("TXT record does not match expected value"), "TXT record for %s does not match expected value", txtName).Log(ctx, d.logger)
	}

	return nil
}
