package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type VerifyCustomDomain struct {
	domains             *customdomainsRepo.Queries
	logger              *slog.Logger
	expectedTargetCNAME string
}

func NewVerifyCustomDomain(logger *slog.Logger, db *pgxpool.Pool, expectedTargetCNAME string) *VerifyCustomDomain {
	return &VerifyCustomDomain{
		domains:             customdomainsRepo.New(db),
		logger:              logger,
		expectedTargetCNAME: expectedTargetCNAME,
	}
}

type VerifyCustomDomainArgs struct {
	OrgID  string
	Domain string
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

	domain, err := d.domains.GetCustomDomainByDomain(ctx, args.Domain)
	if err != nil {
		// Create a new unverified domain entry
		domain, err = d.domains.CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
			OrganizationID: args.OrgID,
			Domain:         args.Domain,
			IngressName:    conv.PtrToPGText(nil),
			CertSecretName: conv.PtrToPGText(nil),
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "error creating custom domain").Log(ctx, d.logger)
		}
	}

	if domain.OrganizationID != args.OrgID {
		return oops.E(oops.CodeUnauthorized, errors.New("custom domain does not belong to organization"), "custom domain does not belong to organization").Log(ctx, d.logger)
	}

	cname, err := net.LookupCNAME(domain.Domain)
	if err != nil {
		d.logger.InfoContext(ctx, "CNAME lookup failed for domain", attr.SlogURLDomain(domain.Domain), attr.SlogError(err))
		// Provide more info if an A record exists
		ips, aErr := net.LookupHost(domain.Domain)
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
	txts, err := net.LookupTXT(txtName)
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
